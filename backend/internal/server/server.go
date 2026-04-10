package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/ali-asghar/agent-runtime/internal/agent"
	"github.com/ali-asghar/agent-runtime/internal/llm"
	"github.com/ali-asghar/agent-runtime/internal/store"
	"github.com/ali-asghar/agent-runtime/internal/tools"
	"github.com/gorilla/websocket"
)

type Server struct {
	agent    *agent.Agent
	sessions *agent.SessionStore
	registry *tools.Registry
	upgrader websocket.Upgrader
}

func New(provider llm.Provider, registry *tools.Registry, db *store.Store) *Server {
	return &Server{
		agent:    agent.New(provider, registry),
		sessions: agent.NewSessionStore(db),
		registry: registry,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/tools", s.handleListTools)
	mux.HandleFunc("GET /api/sessions", s.handleListSessions)
	mux.HandleFunc("GET /api/sessions/{id}", s.handleGetSession)
	mux.HandleFunc("POST /api/sessions", rateLimitMiddleware(100, time.Hour)(s.handleCreateSession))
	mux.HandleFunc("DELETE /api/sessions/{id}", s.handleDeleteSession)
	mux.HandleFunc("GET /ws/{sessionID}", rateLimitMiddleware(200, time.Hour)(s.handleWebSocket))

	return corsMiddleware(mux)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleListTools(w http.ResponseWriter, r *http.Request) {
	toolList := s.registry.List()
	type toolInfo struct {
		Name        string                        `json:"name"`
		Description string                        `json:"description"`
		Parameters  map[string]tools.ParameterDef `json:"parameters"`
	}
	out := make([]toolInfo, len(toolList))
	for i, t := range toolList {
		out[i] = toolInfo{Name: t.Name, Description: t.Description, Parameters: t.Parameters}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	sessions := s.sessions.List()
	type sessionInfo struct {
		ID        string `json:"id"`
		Title     string `json:"title"`
		Messages  int    `json:"message_count"`
		UpdatedAt string `json:"updated_at"`
	}
	out := make([]sessionInfo, len(sessions))
	for i, sess := range sessions {
		title := sess.Title
		if title == "" {
			title = "New chat"
		}
		out[i] = sessionInfo{
			ID:        sess.ID,
			Title:     title,
			Messages:  len(sess.Messages),
			UpdatedAt: sess.UpdatedAt.Format(time.RFC3339),
		}
	}
	// Sort most recent first
	sort.Slice(out, func(i, j int) bool {
		return out[i].UpdatedAt > out[j].UpdatedAt
	})
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	session, ok := s.sessions.Get(id)
	if !ok {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	type messageOut struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	out := struct {
		ID       string       `json:"id"`
		Messages []messageOut `json:"messages"`
	}{ID: session.ID, Messages: []messageOut{}}
	for _, m := range session.Messages {
		// Only expose user and assistant text messages to the frontend
		if (m.Role == "user" || m.Role == "assistant") && m.Content != "" {
			out.Messages = append(out.Messages, messageOut{Role: string(m.Role), Content: m.Content})
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	session := s.sessions.Create()
	writeJSON(w, http.StatusCreated, map[string]string{"id": session.ID})
}

func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.sessions.Delete(id)
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

type wsIncoming struct {
	Type    string `json:"type"`
	Content string `json:"content"`
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("sessionID")

	session, ok := s.sessions.Get(sessionID)
	if !ok {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("websocket upgrade failed", "error", err)
		return
	}
	defer conn.Close()

	slog.Info("websocket connected", "session_id", sessionID)

	// Read incoming WS messages in a goroutine so we can select on them.
	incomingCh := make(chan wsIncoming, 4)
	go func() {
		defer close(incomingCh)
		for {
			var msg wsIncoming
			if err := conn.ReadJSON(&msg); err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
					slog.Error("websocket read error", "error", err)
				}
				return
			}
			incomingCh <- msg
		}
	}()

	var (
		cancelAgent context.CancelFunc
		eventsCh    <-chan agent.Event
	)

	for {
		select {
		case msg, ok := <-incomingCh:
			if !ok {
				return
			}
			switch msg.Type {
			case "cancel":
				if cancelAgent != nil {
					cancelAgent()
					slog.Info("agent cancelled by client", "session_id", sessionID)
				}
			case "message":
				if msg.Content == "" {
					continue
				}
				// Cancel any running agent before starting a new one.
				if cancelAgent != nil {
					cancelAgent()
				}
				ctx, cancel := context.WithCancel(r.Context())
				cancelAgent = cancel
				ch := make(chan agent.Event, 32)
				eventsCh = ch
				go s.agent.Run(ctx, session, msg.Content, ch)
			}

		case event, ok := <-eventsCh:
			if !ok {
				eventsCh = nil
				cancelAgent = nil
				continue
			}
			if err := conn.WriteJSON(event); err != nil {
				slog.Error("websocket write error", "error", err)
				return
			}
		}
	}
}

// rateLimitMiddleware limits each IP to max requests per window.
func rateLimitMiddleware(max int, window time.Duration) func(http.HandlerFunc) http.HandlerFunc {
	type entry struct {
		mu    sync.Mutex
		count int
		reset time.Time
	}
	var store sync.Map

	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			ip := r.Header.Get("X-Forwarded-For")
			if ip == "" {
				ip = r.RemoteAddr
			}

			now := time.Now()
			v, _ := store.LoadOrStore(ip, &entry{reset: now.Add(window)})
			e := v.(*entry)

			e.mu.Lock()
			if now.After(e.reset) {
				e.count = 0
				e.reset = now.Add(window)
			}
			e.count++
			over := e.count > max
			e.mu.Unlock()

			if over {
				writeJSON(w, http.StatusTooManyRequests, map[string]string{
					"error": "rate limit exceeded — try again later",
				})
				return
			}
			next(w, r)
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		slog.Error("json encode error", "error", err)
	}
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func Start(addr string, provider llm.Provider, registry *tools.Registry, db *store.Store) error {
	srv := New(provider, registry, db)
	handler := srv.Handler()

	slog.Info("agent runtime starting", "addr", addr, "provider", provider.Name(), "tools", len(registry.List()))
	for _, t := range registry.List() {
		slog.Info("tool registered", "name", t.Name)
	}

	fmt.Printf("\n🚀 Agent Runtime running at http://localhost%s\n\n", addr)

	return http.ListenAndServe(addr, handler)
}
