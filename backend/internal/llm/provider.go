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
	ID       string `json:"id"`
	Name     string `json:"name"`
	Arguments string `json:"arguments"`
}

type Response struct {
	Content   string
	ToolCalls []ToolCall
}

type ToolDef = map[string]any

type Provider interface {
	ChatCompletion(ctx context.Context, messages []Message, tools []ToolDef) (*Response, error)
	Name() string
}
