package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/ali-asghar/agent-runtime/internal/llm"
)

type Store struct {
	db *sql.DB
}

type SessionRow struct {
	ID        string
	CreatedAt time.Time
	UpdatedAt time.Time
}

func New(dsn string) (*Store, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS sessions (
			id         TEXT PRIMARY KEY,
			created_at TIMESTAMPTZ NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL
		);
		CREATE TABLE IF NOT EXISTS messages (
			id          SERIAL PRIMARY KEY,
			session_id  TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
			role        TEXT NOT NULL,
			content     TEXT NOT NULL DEFAULT '',
			tool_calls  JSONB,
			tool_call_id TEXT NOT NULL DEFAULT '',
			name        TEXT NOT NULL DEFAULT '',
			created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
	`)
	return err
}

func (s *Store) SaveSession(id string, createdAt, updatedAt time.Time) error {
	_, err := s.db.Exec(
		`INSERT INTO sessions (id, created_at, updated_at) VALUES ($1, $2, $3)
		 ON CONFLICT (id) DO UPDATE SET updated_at = $3`,
		id, createdAt, updatedAt,
	)
	return err
}

func (s *Store) SaveMessage(ctx context.Context, sessionID string, msg llm.Message) error {
	toolCallsJSON, err := json.Marshal(msg.ToolCalls)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO messages (session_id, role, content, tool_calls, tool_call_id, name)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		sessionID, string(msg.Role), msg.Content, toolCallsJSON, msg.ToolCallID, msg.Name,
	)
	// Also bump updated_at on the session
	if err == nil {
		_, err = s.db.ExecContext(ctx,
			`UPDATE sessions SET updated_at = NOW() WHERE id = $1`, sessionID,
		)
	}
	return err
}

func (s *Store) LoadSessions(ctx context.Context) ([]SessionRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, created_at, updated_at FROM sessions ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []SessionRow
	for rows.Next() {
		var r SessionRow
		if err := rows.Scan(&r.ID, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		sessions = append(sessions, r)
	}
	return sessions, rows.Err()
}

func (s *Store) LoadMessages(ctx context.Context, sessionID string) ([]llm.Message, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT role, content, tool_calls, tool_call_id, name
		 FROM messages WHERE session_id = $1 ORDER BY id`,
		sessionID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []llm.Message
	for rows.Next() {
		var (
			m             llm.Message
			role          string
			toolCallsJSON []byte
		)
		if err := rows.Scan(&role, &m.Content, &toolCallsJSON, &m.ToolCallID, &m.Name); err != nil {
			return nil, err
		}
		m.Role = llm.Role(role)
		if len(toolCallsJSON) > 0 && string(toolCallsJSON) != "null" {
			if err := json.Unmarshal(toolCallsJSON, &m.ToolCalls); err != nil {
				return nil, err
			}
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

func (s *Store) DeleteSession(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = $1`, id)
	return err
}
