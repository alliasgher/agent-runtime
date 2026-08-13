package docs

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
)

// MaxDocumentsPerSession bounds how much text one conversation can hold in
// memory. Documents live for the life of the session, and the free tier has
// 512MB to work with.
const MaxDocumentsPerSession = 20

// Document is one uploaded file, already extracted to plain text and chunked.
type Document struct {
	ID        string    `json:"id"`
	SessionID string    `json:"session_id"`
	Name      string    `json:"name"`
	SizeBytes int64     `json:"size_bytes"`
	Chars     int       `json:"chars"`
	NumChunks int       `json:"num_chunks"`
	CreatedAt time.Time `json:"created_at"`

	Text   string `json:"-"`
	chunks []string
}

// Persister mirrors the document methods on the Postgres store. It's declared
// here so this package doesn't depend on the store package, and so the whole
// feature degrades to in-memory when DATABASE_URL isn't set.
type Persister interface {
	SaveDocument(ctx context.Context, doc *Document) error
	DeleteDocument(ctx context.Context, id string) error
	LoadDocuments(ctx context.Context) ([]*Document, error)
}

// Store holds every session's documents and their BM25 index.
type Store struct {
	mu        sync.RWMutex
	bySession map[string]*sessionDocs
	db        Persister // nil = in-memory only
}

type sessionDocs struct {
	docs []*Document
	idx  *index
}

func NewStore(db Persister) *Store {
	s := &Store{
		bySession: make(map[string]*sessionDocs),
		db:        db,
	}
	if db != nil {
		s.loadFromDB()
	}
	return s
}

func (s *Store) loadFromDB() {
	loaded, err := s.db.LoadDocuments(context.Background())
	if err != nil {
		slog.Error("failed to load documents from db", "error", err)
		return
	}
	for _, doc := range loaded {
		doc.chunks = SplitIntoChunks(doc.Text)
		doc.NumChunks = len(doc.chunks)
		sd := s.bySession[doc.SessionID]
		if sd == nil {
			sd = &sessionDocs{}
			s.bySession[doc.SessionID] = sd
		}
		sd.docs = append(sd.docs, doc)
	}
	for _, sd := range s.bySession {
		sd.reindex()
	}
	slog.Info("loaded documents from db", "count", len(loaded), "sessions", len(s.bySession))
}

// Add extracts, chunks and indexes an uploaded file. Re-uploading a file whose
// name already exists in the session replaces the previous version rather than
// creating a duplicate the model would have to disambiguate.
func (s *Store) Add(ctx context.Context, sessionID, filename string, data []byte) (*Document, error) {
	text, err := Extract(filename, data)
	if err != nil {
		return nil, err
	}

	chunks := SplitIntoChunks(text)
	if len(chunks) == 0 {
		return nil, fmt.Errorf("no readable text found in %s", filename)
	}

	doc := &Document{
		ID:        uuid.New().String(),
		SessionID: sessionID,
		Name:      filename,
		SizeBytes: int64(len(data)),
		Chars:     len(text),
		NumChunks: len(chunks),
		CreatedAt: time.Now(),
		Text:      text,
		chunks:    chunks,
	}

	s.mu.Lock()
	sd := s.bySession[sessionID]
	if sd == nil {
		sd = &sessionDocs{}
		s.bySession[sessionID] = sd
	}

	var replaced *Document
	for i, existing := range sd.docs {
		if existing.Name == filename {
			replaced = existing
			sd.docs[i] = doc
			break
		}
	}
	if replaced == nil {
		if len(sd.docs) >= MaxDocumentsPerSession {
			s.mu.Unlock()
			return nil, fmt.Errorf("this conversation already has %d documents — remove one first", MaxDocumentsPerSession)
		}
		sd.docs = append(sd.docs, doc)
	}
	sd.reindex()
	s.mu.Unlock()

	if s.db != nil {
		if replaced != nil {
			if err := s.db.DeleteDocument(ctx, replaced.ID); err != nil {
				slog.Error("failed to delete replaced document", "id", replaced.ID, "error", err)
			}
		}
		if err := s.db.SaveDocument(ctx, doc); err != nil {
			slog.Error("failed to persist document", "id", doc.ID, "error", err)
		}
	}

	return doc, nil
}

// List returns a session's documents in upload order.
func (s *Store) List(sessionID string) []*Document {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	sd := s.bySession[sessionID]
	if sd == nil {
		return nil
	}
	out := make([]*Document, len(sd.docs))
	copy(out, sd.docs)
	return out
}

// Delete removes one document from a session. It reports whether the document
// was found.
func (s *Store) Delete(ctx context.Context, sessionID, docID string) bool {
	s.mu.Lock()
	sd := s.bySession[sessionID]
	if sd == nil {
		s.mu.Unlock()
		return false
	}
	found := false
	for i, doc := range sd.docs {
		if doc.ID == docID {
			sd.docs = append(sd.docs[:i], sd.docs[i+1:]...)
			found = true
			break
		}
	}
	if found {
		sd.reindex()
	}
	s.mu.Unlock()

	if found && s.db != nil {
		if err := s.db.DeleteDocument(ctx, docID); err != nil {
			slog.Error("failed to delete document from db", "id", docID, "error", err)
		}
	}
	return found
}

// DeleteSession drops every document attached to a session. Postgres cleans up
// its own rows via ON DELETE CASCADE when the session row goes.
func (s *Store) DeleteSession(sessionID string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.bySession, sessionID)
}

// Search ranks the session's chunks against a query.
func (s *Store) Search(sessionID, query string, limit int) []Result {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	sd := s.bySession[sessionID]
	if sd == nil {
		return nil
	}
	return sd.idx.search(query, limit)
}

// reindex rebuilds the BM25 index. Callers must hold the write lock.
func (sd *sessionDocs) reindex() {
	var chunks []Chunk
	for _, doc := range sd.docs {
		for i, text := range doc.chunks {
			chunks = append(chunks, Chunk{
				DocID:   doc.ID,
				DocName: doc.Name,
				Ordinal: i + 1,
				Total:   len(doc.chunks),
				Text:    text,
			})
		}
	}
	sd.idx = buildIndex(chunks)
}

// sessionIDKey scopes tool calls to the conversation that made them. Tools get
// only a context, so the agent stamps the session onto it before dispatch.
type sessionIDKey struct{}

func WithSessionID(ctx context.Context, sessionID string) context.Context {
	return context.WithValue(ctx, sessionIDKey{}, sessionID)
}

func SessionIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(sessionIDKey{}).(string)
	return id
}
