package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/ali-asghar/agent-runtime/internal/agent"
	"github.com/ali-asghar/agent-runtime/internal/llm"
	"github.com/ali-asghar/agent-runtime/internal/tools"
	"github.com/gorilla/websocket"
)

type Server struct {
	agent    *agent.Agent
	sessions *agent.SessionStore
	registry *tools.Registry
	upgrader websocket.Upgrader
}

func New(provider llm.Provider, registry *tools.Registry) *Server {
	return &Server{
		agent:    agent.New(provider, registry),
		sessions: agent.NewSessionStore(),
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
	mux.HandleFunc("POST /api/sessions", s.handleCreateSession)
	mux.HandleFunc("DELETE /api/sessions/{id}", s.handleDeleteSession)
	mux.HandleFunc("GET /ws/{sessionID}", s.handleWebSocket)

	return corsMiddleware(mux)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleListTools(w http.ResponseWriter, r *http.Request) {
	toolList := s.registry.List()
	type toolInfo struct {
		Name        string                   `json:"name"`
		Description string                   `json:"description"`
		Parameters  map[string]tools.ParameterDef `json:"parameters"`
	}
	out := make([]toolInfo, len(toolList))
	for i, t := range toolList {
		out[i] = toolInfo{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  t.Parameters,
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	sessions := s.sessions.List()
	type sessionInfo struct {
		ID        string `json:"id"`
		Messages  int    `json:"message_count"`
		CreatedAt string `json:"created_at"`
	}
	out := make([]sessionInfo, len(sessions))
	for i, sess := range sessions {
		out[i] = sessionInfo{
			ID:        sess.ID,
			Messages:  len(sess.Messages),
			CreatedAt: sess.CreatedAt.Format(time.RFC3339),
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

// WebSocket message types
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
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}
	defer conn.Close()

	log.Printf("WebSocket connected: session=%s", sessionID)

	for {
		var msg wsIncoming
		if err := conn.ReadJSON(&msg); err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("WebSocket read error: %v", err)
			}
			return
		}

		if msg.Type != "message" || msg.Content == "" {
			continue
		}

		// Run agent and stream events
		events := make(chan agent.Event, 32)
		go s.agent.Run(r.Context(), session, msg.Content, events)

		for event := range events {
			if err := conn.WriteJSON(event); err != nil {
				log.Printf("WebSocket write error: %v", err)
				return
			}
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("JSON encode error: %v", err)
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

func Start(addr string, provider llm.Provider, registry *tools.Registry) error {
	srv := New(provider, registry)
	handler := srv.Handler()

	log.Printf("Agent Runtime server starting on %s", addr)
	log.Printf("LLM Provider: %s", provider.Name())
	log.Printf("Tools registered: %d", len(registry.List()))
	for _, t := range registry.List() {
		log.Printf("  - %s: %s", t.Name, t.Description)
	}

	fmt.Printf("\n🚀 Agent Runtime running at http://localhost%s\n", addr)
	fmt.Printf("   WebSocket: ws://localhost%s/ws/<session-id>\n", addr)
	fmt.Printf("   API: http://localhost%s/api/\n\n", addr)

	return http.ListenAndServe(addr, handler)
}
