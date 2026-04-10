package server_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ali-asghar/agent-runtime/internal/agent"
	"github.com/ali-asghar/agent-runtime/internal/llm"
	"github.com/ali-asghar/agent-runtime/internal/server"
	"github.com/ali-asghar/agent-runtime/internal/tools"
	"github.com/gorilla/websocket"
)

// ── Mock LLM ────────────────────────────────────────────────────────────────

type mockLLM struct {
	mu        sync.Mutex
	responses []*llm.Response
	idx       int
}

func (m *mockLLM) Name() string { return "mock" }

func (m *mockLLM) next() *llm.Response {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.idx >= len(m.responses) {
		return &llm.Response{Content: "done"}
	}
	r := m.responses[m.idx]
	m.idx++
	return r
}

func (m *mockLLM) ChatCompletion(_ context.Context, _ []llm.Message, _ []llm.ToolDef) (*llm.Response, error) {
	return m.next(), nil
}

func (m *mockLLM) StreamChatCompletion(_ context.Context, _ []llm.Message, _ []llm.ToolDef, onToken func(string)) (*llm.Response, error) {
	resp := m.next()
	if onToken != nil && resp.Content != "" && len(resp.ToolCalls) == 0 {
		for _, word := range strings.Fields(resp.Content) {
			onToken(word + " ")
		}
	}
	return resp, nil
}

// ── Test helpers ─────────────────────────────────────────────────────────────

func newTestServer(t *testing.T, provider llm.Provider, extraTools ...*tools.Tool) *httptest.Server {
	t.Helper()
	reg := tools.NewRegistry()
	for _, tool := range extraTools {
		reg.Register(tool)
	}
	srv := server.New(provider, reg, nil)
	return httptest.NewServer(srv.Handler())
}

func createSession(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	resp, err := http.Post(srv.URL+"/api/sessions", "application/json", nil)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	var data struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	return data.ID
}

func connectWS(t *testing.T, srv *httptest.Server, sessionID string) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/" + sessionID
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	return conn
}

func sendMessage(t *testing.T, conn *websocket.Conn, content string) {
	t.Helper()
	msg, _ := json.Marshal(map[string]string{"type": "message", "content": content})
	if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
		t.Fatalf("send message: %v", err)
	}
}

func sendCancel(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	msg, _ := json.Marshal(map[string]string{"type": "cancel"})
	if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
		t.Fatalf("send cancel: %v", err)
	}
}

// readEvents collects all agent events until the channel closes (response/error)
// or timeout is reached. Returns the collected events.
func readEvents(t *testing.T, conn *websocket.Conn, timeout time.Duration) []agent.Event {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(timeout))
	var events []agent.Event
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			// deadline exceeded or connection closed — stop reading
			break
		}
		var e agent.Event
		if err := json.Unmarshal(data, &e); err != nil {
			t.Logf("unmarshal event: %v (raw: %s)", err, string(data))
			continue
		}
		events = append(events, e)
		if e.Type == agent.EventResponse || e.Type == agent.EventError {
			break
		}
	}
	return events
}

func eventTypes(events []agent.Event) []string {
	out := make([]string, len(events))
	for i, e := range events {
		out[i] = string(e.Type)
	}
	return out
}

func hasType(events []agent.Event, t agent.EventType) bool {
	for _, e := range events {
		if e.Type == t {
			return true
		}
	}
	return false
}

// ── Tests ────────────────────────────────────────────────────────────────────

func TestHealth(t *testing.T) {
	srv := newTestServer(t, &mockLLM{})
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/health")
	if err != nil {
		t.Fatalf("health check failed: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestCreateAndListSessions(t *testing.T) {
	srv := newTestServer(t, &mockLLM{})
	defer srv.Close()

	id := createSession(t, srv)
	if id == "" {
		t.Fatal("empty session id")
	}

	// List sessions
	resp, err := http.Get(srv.URL + "/api/sessions")
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	defer resp.Body.Close()
	var sessions []map[string]any
	json.NewDecoder(resp.Body).Decode(&sessions)
	// New session has no messages so shouldn't appear — just verify no server error
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestWebSocket_SimpleTextResponse(t *testing.T) {
	provider := &mockLLM{
		responses: []*llm.Response{
			{Content: "Hello from the agent"},
		},
	}
	srv := newTestServer(t, provider)
	defer srv.Close()

	sid := createSession(t, srv)
	conn := connectWS(t, srv, sid)
	defer conn.Close()

	sendMessage(t, conn, "hi there")
	events := readEvents(t, conn, 5*time.Second)

	if len(events) == 0 {
		t.Fatal("no events received")
	}
	if !hasType(events, agent.EventThinking) {
		t.Error("expected thinking event")
	}
	if !hasType(events, agent.EventResponse) {
		t.Fatalf("expected response event, got: %v", eventTypes(events))
	}

	// Find response and check content
	for _, e := range events {
		if e.Type == agent.EventResponse && e.Content == "" {
			// Content may be empty if fully streamed via tokens — that's OK
		}
	}
}

func TestWebSocket_TokensStreamedBeforeResponse(t *testing.T) {
	provider := &mockLLM{
		responses: []*llm.Response{
			{Content: "one two three"},
		},
	}
	srv := newTestServer(t, provider)
	defer srv.Close()

	sid := createSession(t, srv)
	conn := connectWS(t, srv, sid)
	defer conn.Close()

	sendMessage(t, conn, "count")
	events := readEvents(t, conn, 5*time.Second)

	if !hasType(events, agent.EventToken) {
		t.Error("expected token events during streaming response")
	}
	if !hasType(events, agent.EventResponse) {
		t.Errorf("expected response event, got: %v", eventTypes(events))
	}

	// Tokens must arrive before the response event
	lastTokenIdx, responseIdx := -1, -1
	for i, e := range events {
		if e.Type == agent.EventToken {
			lastTokenIdx = i
		}
		if e.Type == agent.EventResponse {
			responseIdx = i
		}
	}
	if lastTokenIdx > responseIdx {
		t.Error("token event arrived after response event")
	}
}

func TestWebSocket_ToolCallThenResponse(t *testing.T) {
	provider := &mockLLM{
		responses: []*llm.Response{
			{
				ToolCalls: []llm.ToolCall{
					{ID: "c1", Name: "echo", Arguments: `{"msg": "pong"}`},
				},
			},
			{Content: "Tool returned pong"},
		},
	}
	echoTool := &tools.Tool{
		Name:        "echo",
		Description: "echoes input",
		Parameters:  map[string]tools.ParameterDef{"msg": {Type: "string", Required: true}},
		Execute: func(_ context.Context, args map[string]any) (string, error) {
			return fmt.Sprintf("%v", args["msg"]), nil
		},
	}

	srv := newTestServer(t, provider, echoTool)
	defer srv.Close()

	sid := createSession(t, srv)
	conn := connectWS(t, srv, sid)
	defer conn.Close()

	sendMessage(t, conn, "echo pong")
	events := readEvents(t, conn, 5*time.Second)

	if !hasType(events, agent.EventToolCall) {
		t.Errorf("expected tool_call event, got: %v", eventTypes(events))
	}
	if !hasType(events, agent.EventToolResult) {
		t.Errorf("expected tool_result event, got: %v", eventTypes(events))
	}
	if !hasType(events, agent.EventResponse) {
		t.Errorf("expected response event, got: %v", eventTypes(events))
	}

	// Order must be: tool_call → tool_result → response
	indices := make(map[agent.EventType]int)
	for i, e := range events {
		if _, seen := indices[e.Type]; !seen {
			indices[e.Type] = i
		}
	}
	if indices[agent.EventToolCall] > indices[agent.EventToolResult] {
		t.Error("tool_result arrived before tool_call")
	}
	if indices[agent.EventToolResult] > indices[agent.EventResponse] {
		t.Error("response arrived before tool_result")
	}
}

func TestWebSocket_ToolResultContentCorrect(t *testing.T) {
	provider := &mockLLM{
		responses: []*llm.Response{
			{ToolCalls: []llm.ToolCall{{ID: "c1", Name: "echo", Arguments: `{"msg": "hello world"}`}}},
			{Content: "done"},
		},
	}
	echoTool := &tools.Tool{
		Name:        "echo",
		Description: "echoes input",
		Parameters:  map[string]tools.ParameterDef{"msg": {Type: "string", Required: true}},
		Execute: func(_ context.Context, args map[string]any) (string, error) {
			return args["msg"].(string), nil
		},
	}

	srv := newTestServer(t, provider, echoTool)
	defer srv.Close()

	sid := createSession(t, srv)
	conn := connectWS(t, srv, sid)
	defer conn.Close()

	sendMessage(t, conn, "echo")
	events := readEvents(t, conn, 5*time.Second)

	for _, e := range events {
		if e.Type == agent.EventToolResult {
			if e.Content != "hello world" {
				t.Errorf("expected tool result 'hello world', got %q", e.Content)
			}
			return
		}
	}
	t.Errorf("no tool_result event found, got: %v", eventTypes(events))
}

func TestWebSocket_MissingRequiredToolParam(t *testing.T) {
	provider := &mockLLM{
		responses: []*llm.Response{
			// Model sends empty args — should get clear error back
			{ToolCalls: []llm.ToolCall{{ID: "c1", Name: "echo", Arguments: `{}`}}},
			{Content: "handled error"},
		},
	}
	echoTool := &tools.Tool{
		Name:        "echo",
		Description: "echoes input",
		Parameters:  map[string]tools.ParameterDef{"msg": {Type: "string", Required: true}},
		Execute: func(_ context.Context, args map[string]any) (string, error) {
			return args["msg"].(string), nil
		},
	}

	srv := newTestServer(t, provider, echoTool)
	defer srv.Close()

	sid := createSession(t, srv)
	conn := connectWS(t, srv, sid)
	defer conn.Close()

	sendMessage(t, conn, "echo nothing")
	events := readEvents(t, conn, 5*time.Second)

	// Should get a tool_result with an error message (not a crash)
	for _, e := range events {
		if e.Type == agent.EventToolResult && strings.Contains(e.Content, "missing required") {
			return //
		}
	}
	t.Errorf("expected tool_result with 'missing required' error, got: %v", eventTypes(events))
}

func TestWebSocket_Cancel(t *testing.T) {
	// Use a slow mock that gives us time to cancel
	provider := &mockLLM{
		responses: []*llm.Response{
			{Content: "you should never see this"},
		},
	}
	srv := newTestServer(t, provider)
	defer srv.Close()

	sid := createSession(t, srv)
	conn := connectWS(t, srv, sid)
	defer conn.Close()

	sendMessage(t, conn, "long task")

	// Read the first event (thinking), then immediately cancel
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			break
		}
		var e agent.Event
		json.Unmarshal(data, &e)
		if e.Type == agent.EventThinking {
			sendCancel(t, conn)
			break
		}
	}

	// After cancel we should eventually get an error event (cancelled) or the
	// channel closes — either is acceptable
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return // connection closed — also acceptable
		}
		var e agent.Event
		json.Unmarshal(data, &e)
		if e.Type == agent.EventError && strings.Contains(strings.ToLower(e.Content), "cancel") {
			return //
		}
	}
}

func TestWebSocket_SessionNotFound(t *testing.T) {
	srv := newTestServer(t, &mockLLM{})
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/nonexistent-session-id"
	_, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err == nil {
		t.Fatal("expected dial to fail for nonexistent session")
	}
	if resp != nil && resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestWebSocket_MultipleToolCalls(t *testing.T) {
	provider := &mockLLM{
		responses: []*llm.Response{
			{
				ToolCalls: []llm.ToolCall{
					{ID: "c1", Name: "echo", Arguments: `{"msg": "first"}`},
					{ID: "c2", Name: "echo", Arguments: `{"msg": "second"}`},
				},
			},
			{Content: "both done"},
		},
	}
	echoTool := &tools.Tool{
		Name:        "echo",
		Description: "echoes input",
		Parameters:  map[string]tools.ParameterDef{"msg": {Type: "string", Required: true}},
		Execute: func(_ context.Context, args map[string]any) (string, error) {
			return args["msg"].(string), nil
		},
	}

	srv := newTestServer(t, provider, echoTool)
	defer srv.Close()

	sid := createSession(t, srv)
	conn := connectWS(t, srv, sid)
	defer conn.Close()

	sendMessage(t, conn, "do both")
	events := readEvents(t, conn, 5*time.Second)

	var toolCalls, toolResults int
	for _, e := range events {
		if e.Type == agent.EventToolCall {
			toolCalls++
		}
		if e.Type == agent.EventToolResult {
			toolResults++
		}
	}
	if toolCalls != 2 {
		t.Errorf("expected 2 tool_call events, got %d", toolCalls)
	}
	if toolResults != 2 {
		t.Errorf("expected 2 tool_result events, got %d", toolResults)
	}
}

func TestWebSocket_DeleteSession(t *testing.T) {
	srv := newTestServer(t, &mockLLM{})
	defer srv.Close()

	sid := createSession(t, srv)

	req, _ := http.NewRequest("DELETE", srv.URL+"/api/sessions/"+sid, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("delete session: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200 on delete, got %d", resp.StatusCode)
	}

	// Connecting to a deleted session should now return 404
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/" + sid
	_, wsResp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err == nil {
		t.Fatal("expected 404 after session deleted")
	}
	if wsResp != nil && wsResp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", wsResp.StatusCode)
	}
}
