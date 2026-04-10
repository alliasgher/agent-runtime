package agent

import (
	"context"
	"testing"
	"time"

	"github.com/ali-asghar/agent-runtime/internal/llm"
	"github.com/ali-asghar/agent-runtime/internal/tools"
)

// mockProvider implements llm.Provider for testing.
type mockProvider struct {
	responses []*llm.Response
	callCount int
}

func (m *mockProvider) Name() string { return "mock" }

func (m *mockProvider) ChatCompletion(_ context.Context, _ []llm.Message, _ []llm.ToolDef) (*llm.Response, error) {
	return m.next(), nil
}

func (m *mockProvider) StreamChatCompletion(_ context.Context, _ []llm.Message, _ []llm.ToolDef, onToken func(string)) (*llm.Response, error) {
	resp := m.next()
	// Simulate streaming by calling onToken for text responses
	if onToken != nil && resp != nil && resp.Content != "" && len(resp.ToolCalls) == 0 {
		for _, ch := range resp.Content {
			onToken(string(ch))
		}
	}
	return resp, nil
}

func (m *mockProvider) next() *llm.Response {
	if m.callCount >= len(m.responses) {
		return &llm.Response{Content: "done"}
	}
	r := m.responses[m.callCount]
	m.callCount++
	return r
}

func collectEvents(t *testing.T, events <-chan Event, timeout time.Duration) []Event {
	t.Helper()
	var out []Event
	deadline := time.After(timeout)
	for {
		select {
		case e, ok := <-events:
			if !ok {
				return out
			}
			out = append(out, e)
		case <-deadline:
			t.Fatal("timed out waiting for events")
			return out
		}
	}
}

func TestAgent_SimpleTextResponse(t *testing.T) {
	provider := &mockProvider{
		responses: []*llm.Response{
			{Content: "Hello!"},
		},
	}
	registry := tools.NewRegistry()
	a := New(provider, registry)

	session := &Session{ID: "test-1"}
	events := make(chan Event, 32)

	go a.Run(context.Background(), session, "hi", events)

	evts := collectEvents(t, events, 5*time.Second)

	var types []EventType
	for _, e := range evts {
		types = append(types, e.Type)
	}

	// Must have thinking then response
	if len(evts) < 2 {
		t.Fatalf("expected at least 2 events, got %d: %v", len(evts), types)
	}

	// Find response event
	var responseEvent *Event
	for i := range evts {
		if evts[i].Type == EventResponse {
			responseEvent = &evts[i]
		}
	}
	if responseEvent == nil {
		t.Fatalf("no response event found, events: %v", types)
	}
}

func TestAgent_TokensStreamedBeforeResponse(t *testing.T) {
	provider := &mockProvider{
		responses: []*llm.Response{
			{Content: "ABC"},
		},
	}
	registry := tools.NewRegistry()
	a := New(provider, registry)

	session := &Session{ID: "test-2"}
	events := make(chan Event, 64)

	go a.Run(context.Background(), session, "hi", events)
	evts := collectEvents(t, events, 5*time.Second)

	var tokens []string
	for _, e := range evts {
		if e.Type == EventToken {
			tokens = append(tokens, e.Content)
		}
	}
	if len(tokens) == 0 {
		t.Error("expected token events to be emitted during streaming")
	}
}

func TestAgent_ToolCallThenResponse(t *testing.T) {
	provider := &mockProvider{
		responses: []*llm.Response{
			{
				ToolCalls: []llm.ToolCall{
					{ID: "c1", Name: "echo", Arguments: `{"msg":"hello"}`},
				},
			},
			{Content: "Tool done"},
		},
	}

	registry := tools.NewRegistry()
	registry.Register(&tools.Tool{
		Name:        "echo",
		Description: "echoes msg",
		Parameters: map[string]tools.ParameterDef{
			"msg": {Type: "string", Required: true},
		},
		Execute: func(_ context.Context, args map[string]any) (string, error) {
			return args["msg"].(string), nil
		},
	})

	a := New(provider, registry)
	session := &Session{ID: "test-3"}
	events := make(chan Event, 64)

	go a.Run(context.Background(), session, "run echo", events)
	evts := collectEvents(t, events, 5*time.Second)

	typeSet := make(map[EventType]bool)
	for _, e := range evts {
		typeSet[e.Type] = true
	}

	if !typeSet[EventToolCall] {
		t.Error("expected tool_call event")
	}
	if !typeSet[EventToolResult] {
		t.Error("expected tool_result event")
	}
	if !typeSet[EventResponse] {
		t.Error("expected response event")
	}
}

func TestAgent_Cancellation(t *testing.T) {
	provider := &mockProvider{
		responses: []*llm.Response{
			{Content: "never"},
		},
	}
	registry := tools.NewRegistry()
	a := New(provider, registry)

	session := &Session{ID: "test-cancel"}
	events := make(chan Event, 32)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	go a.Run(ctx, session, "hi", events)
	evts := collectEvents(t, events, 3*time.Second)

	var hasError bool
	for _, e := range evts {
		if e.Type == EventError {
			hasError = true
		}
	}
	if !hasError {
		t.Error("expected error event on cancellation")
	}
}

func TestAgent_SessionMessageHistory(t *testing.T) {
	provider := &mockProvider{
		responses: []*llm.Response{
			{Content: "reply"},
		},
	}
	registry := tools.NewRegistry()
	a := New(provider, registry)

	session := &Session{ID: "test-history"}
	events := make(chan Event, 32)

	go a.Run(context.Background(), session, "input", events)
	collectEvents(t, events, 5*time.Second)

	// Session should have user + assistant messages
	if len(session.Messages) < 2 {
		t.Errorf("expected at least 2 messages in session, got %d", len(session.Messages))
	}
	if session.Messages[0].Role != llm.RoleUser {
		t.Errorf("first message should be user, got %s", session.Messages[0].Role)
	}
	if session.Messages[0].Content != "input" {
		t.Errorf("user message content wrong: %s", session.Messages[0].Content)
	}
}
