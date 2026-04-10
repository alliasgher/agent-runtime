package agent

import (
	"sync"
	"time"

	"github.com/ali-asghar/agent-runtime/internal/llm"
	"github.com/google/uuid"
)

type Session struct {
	ID        string        `json:"id"`
	Messages  []llm.Message `json:"messages"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
}

func NewSession() *Session {
	now := time.Now()
	return &Session{
		ID:        uuid.New().String(),
		Messages:  []llm.Message{},
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func (s *Session) AddMessage(msg llm.Message) {
	s.Messages = append(s.Messages, msg)
	s.UpdatedAt = time.Now()
}

type SessionStore struct {
	sessions map[string]*Session
	mu       sync.RWMutex
}

func NewSessionStore() *SessionStore {
	return &SessionStore{sessions: make(map[string]*Session)}
}

func (s *SessionStore) Create() *Session {
	session := NewSession()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[session.ID] = session
	return session
}

func (s *SessionStore) Get(id string) (*Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	session, ok := s.sessions[id]
	return session, ok
}

func (s *SessionStore) List() []*Session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		out = append(out, sess)
	}
	return out
}

func (s *SessionStore) Delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
}
