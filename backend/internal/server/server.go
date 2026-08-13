package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/ali-asghar/agent-runtime/internal/agent"
	"github.com/ali-asghar/agent-runtime/internal/docs"
	"github.com/ali-asghar/agent-runtime/internal/llm"
	"github.com/ali-asghar/agent-runtime/internal/store"
	"github.com/ali-asghar/agent-runtime/internal/tools"
	"github.com/gorilla/websocket"
)

type Server struct {
	agent    *agent.Agent
	sessions *agent.SessionStore
	registry *tools.Registry
	docs     *docs.Store
	upgrader websocket.Upgrader
}

// New wires the server. docStore may be nil, in which case an in-memory one is
// created — callers that also register the search_documents tool must pass the
// same store both places so uploads and retrieval see the same documents.
func New(provider llm.Provider, registry *tools.Registry, db *store.Store, docStore *docs.Store) *Server {
	if docStore == nil {
		docStore = docs.NewStore(nil)
	}
	return &Server{
		agent:    agent.New(provider, registry, docStore),
		sessions: agent.NewSessionStore(db),
		registry: registry,
		docs:     docStore,
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
	mux.HandleFunc("GET /api/sessions/{id}/documents", s.handleListDocuments)
	mux.HandleFunc("POST /api/sessions/{id}/documents", rateLimitMiddleware(60, time.Hour)(s.handleUploadDocument))
	mux.HandleFunc("DELETE /api/sessions/{id}/documents/{docID}", s.handleDeleteDocument)
	mux.HandleFunc("GET /ws/{sessionID}", rateLimitMiddleware(200, time.Hour)(s.handleWebSocket))

	return corsMiddleware(mux)
}

// buildCommit is the git commit this binary was built from. Render sets
// RENDER_GIT_COMMIT on every deploy, so /api/health can report exactly which
// commit is serving. Without it a deploy that silently never happened looks
// identical to one that succeeded — which is precisely how the backend sat two
// commits behind production while every dashboard read green.
var buildCommit = func() string {
	if c := os.Getenv("RENDER_GIT_COMMIT"); c != "" {
		return c
	}
	return "dev"
}()

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "ok",
		"commit": buildCommit,
	})
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
	// Postgres clears its own rows through ON DELETE CASCADE; this drops the
	// in-memory copy and its index.
	s.docs.DeleteSession(id)
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) handleListDocuments(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if _, ok := s.sessions.Get(sessionID); !ok {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	out := s.docs.List(sessionID)
	if out == nil {
		out = []*docs.Document{}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleUploadDocument(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if _, ok := s.sessions.Get(sessionID); !ok {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	// Bound the request before parsing so an oversized upload can't exhaust
	// memory on the free tier. The slack above the file limit covers multipart
	// framing overhead.
	r.Body = http.MaxBytesReader(w, r.Body, docs.MaxFileSize+(1<<20))
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{
			"error": fmt.Sprintf("upload too large — the limit is %dMB per file", docs.MaxFileSize>>20),
		})
		return
	}
	defer r.MultipartForm.RemoveAll()

	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no file found in the request"})
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "could not read the uploaded file"})
		return
	}

	// filepath.Base strips any directory component the browser sent along.
	name := filepath.Base(header.Filename)

	doc, err := s.docs.Add(r.Context(), sessionID, name, data)
	if err != nil {
		// These errors are written for the user — bad format, empty file,
		// scanned PDF — so pass them through rather than flattening them.
		slog.Info("document upload rejected", "session_id", sessionID, "name", name, "error", err)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	slog.Info("document uploaded", "session_id", sessionID, "name", doc.Name, "chars", doc.Chars, "chunks", doc.NumChunks)
	writeJSON(w, http.StatusCreated, doc)
}

func (s *Server) handleDeleteDocument(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	docID := r.PathValue("docID")

	if !s.docs.Delete(r.Context(), sessionID, docID) {
		http.Error(w, "document not found", http.StatusNotFound)
		return
	}
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

func Start(addr string, provider llm.Provider, registry *tools.Registry, db *store.Store, docStore *docs.Store) error {
	srv := New(provider, registry, db, docStore)
	handler := srv.Handler()

	slog.Info("agent runtime starting", "addr", addr, "provider", provider.Name(), "tools", len(registry.List()))
	for _, t := range registry.List() {
		slog.Info("tool registered", "name", t.Name)
	}

	fmt.Printf("\n🚀 Agent Runtime running at http://localhost%s\n\n", addr)

	return http.ListenAndServe(addr, handler)
}
