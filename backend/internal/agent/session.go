package agent

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/ali-asghar/agent-runtime/internal/llm"
	"github.com/ali-asghar/agent-runtime/internal/store"
	"github.com/google/uuid"
)

type Session struct {
	ID        string        `json:"id"`
	Title     string        `json:"title"`
	Messages  []llm.Message `json:"messages"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
	persist   func(llm.Message)
}

func newSession(persist func(llm.Message)) *Session {
	now := time.Now()
	return &Session{
		ID:        uuid.New().String(),
		Messages:  []llm.Message{},
		CreatedAt: now,
		UpdatedAt: now,
		persist:   persist,
	}
}

func (s *Session) AddMessage(msg llm.Message) {
	s.Messages = append(s.Messages, msg)
	s.UpdatedAt = time.Now()
	// Set title from first user message
	if s.Title == "" && msg.Role == llm.RoleUser && msg.Content != "" {
		title := msg.Content
		if len(title) > 60 {
			title = title[:57] + "..."
		}
		s.Title = title
	}
	if s.persist != nil {
		s.persist(msg)
	}
}

type SessionStore struct {
	sessions map[string]*Session
	mu       sync.RWMutex
	db       *store.Store // nil = in-memory only
}

func NewSessionStore(db *store.Store) *SessionStore {
	ss := &SessionStore{
		sessions: make(map[string]*Session),
		db:       db,
	}
	if db != nil {
		ss.loadFromDB()
	}
	return ss
}

func (ss *SessionStore) loadFromDB() {
	rows, err := ss.db.LoadSessions(context.Background())
	if err != nil {
		slog.Error("failed to load sessions from db", "error", err)
		return
	}
	for _, row := range rows {
		msgs, err := ss.db.LoadMessages(context.Background(), row.ID)
		if err != nil {
			slog.Error("failed to load messages", "session_id", row.ID, "error", err)
			continue
		}
		s := &Session{
			ID:        row.ID,
			Messages:  msgs,
			CreatedAt: row.CreatedAt,
			UpdatedAt: row.UpdatedAt,
		}
		for _, m := range msgs {
			if m.Role == llm.RoleUser && m.Content != "" {
				title := m.Content
				if len(title) > 60 {
					title = title[:57] + "..."
				}
				s.Title = title
				break
			}
		}
		s.persist = ss.makePersist(s.ID)
		ss.sessions[s.ID] = s
	}
	slog.Info("loaded sessions from db", "count", len(rows))
}

func (ss *SessionStore) makePersist(sessionID string) func(llm.Message) {
	return func(msg llm.Message) {
		if err := ss.db.SaveMessage(context.Background(), sessionID, msg); err != nil {
			slog.Error("failed to persist message", "session_id", sessionID, "error", err)
		}
	}
}

func (ss *SessionStore) Create() *Session {
	var persist func(llm.Message)
	session := newSession(persist) // create first to get ID

	if ss.db != nil {
		session.persist = ss.makePersist(session.ID)
		if err := ss.db.SaveSession(session.ID, session.CreatedAt, session.UpdatedAt); err != nil {
			slog.Error("failed to persist session", "session_id", session.ID, "error", err)
		}
	}

	ss.mu.Lock()
	defer ss.mu.Unlock()
	ss.sessions[session.ID] = session
	return session
}

func (ss *SessionStore) Get(id string) (*Session, bool) {
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	session, ok := ss.sessions[id]
	return session, ok
}

func (ss *SessionStore) List() []*Session {
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	out := make([]*Session, 0, len(ss.sessions))
	for _, sess := range ss.sessions {
		out = append(out, sess)
	}
	return out
}

func (ss *SessionStore) Delete(id string) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	delete(ss.sessions, id)
	if ss.db != nil {
		if err := ss.db.DeleteSession(context.Background(), id); err != nil {
			slog.Error("failed to delete session from db", "session_id", id, "error", err)
		}
	}
}
