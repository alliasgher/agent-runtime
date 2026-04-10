package llm

import "context"

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type Message struct {
	Role       Role       `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
}

type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type Response struct {
	Content   string
	ToolCalls []ToolCall
}

type ToolDef = map[string]any

type Provider interface {
	// ChatCompletion returns a complete response without streaming.
	ChatCompletion(ctx context.Context, messages []Message, tools []ToolDef) (*Response, error)
	// StreamChatCompletion streams text tokens via onToken as they arrive.
	// It still returns the full Response when complete (for tool calls etc.).
	StreamChatCompletion(ctx context.Context, messages []Message, tools []ToolDef, onToken func(string)) (*Response, error)
	Name() string
}
