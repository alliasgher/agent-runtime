package llm

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeServer returns an httptest.Server that responds with the given body and status.
func fakeServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	}))
}

func TestParseResponse_TextOnly(t *testing.T) {
	body := `{"choices":[{"message":{"content":"Hello world","tool_calls":null}}]}`
	p := NewOpenAIProvider("", "", "")
	resp, err := p.parseResponse([]byte(body), func(string) string { return "input" })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "Hello world" {
		t.Errorf("want %q, got %q", "Hello world", resp.Content)
	}
	if len(resp.ToolCalls) != 0 {
		t.Errorf("expected no tool calls, got %d", len(resp.ToolCalls))
	}
}

func TestParseResponse_ToolCall(t *testing.T) {
	body := `{"choices":[{"message":{"content":null,"tool_calls":[{"id":"c1","type":"function","function":{"name":"web_search","arguments":"{\"query\":\"go lang\"}"}}]}}]}`
	p := NewOpenAIProvider("", "", "")
	resp, err := p.parseResponse([]byte(body), func(string) string { return "query" })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}
	tc := resp.ToolCalls[0]
	if tc.Name != "web_search" {
		t.Errorf("want tool name %q, got %q", "web_search", tc.Name)
	}
	if tc.ID != "c1" {
		t.Errorf("want id %q, got %q", "c1", tc.ID)
	}
}

func TestExtractTextToolCalls_ValidJSON(t *testing.T) {
	content := `<function=run_python>{"code": "print(1)"}</function>`
	firstParam := func(name string) string {
		if name == "run_python" {
			return "code"
		}
		return "query"
	}
	calls := extractTextToolCalls(content, firstParam)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Name != "run_python" {
		t.Errorf("want name %q, got %q", "run_python", calls[0].Name)
	}
	if !strings.Contains(calls[0].Arguments, "print(1)") {
		t.Errorf("arguments should contain code, got %q", calls[0].Arguments)
	}
}

func TestExtractTextToolCalls_InvalidJSON_UsesFirstParam(t *testing.T) {
	content := `<function/web_search>latest AI news</function>`
	firstParam := func(name string) string {
		if name == "web_search" {
			return "query"
		}
		return "code"
	}
	calls := extractTextToolCalls(content, firstParam)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if !strings.Contains(calls[0].Arguments, `"query"`) {
		t.Errorf("expected key 'query' in args, got %q", calls[0].Arguments)
	}
	if !strings.Contains(calls[0].Arguments, "latest AI news") {
		t.Errorf("expected value in args, got %q", calls[0].Arguments)
	}
}

func TestExtractTextToolCalls_MultipleFormats(t *testing.T) {
	content := `
		function=run_python>{"code": "x=1"};</function>
		<function=wikipedia{"query": "Go programming"}></function>
	`
	calls := extractTextToolCalls(content, func(n string) string { return "query" })
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(calls))
	}
}

func TestExtractTextToolCalls_BackslashSeparator(t *testing.T) {
	// Model sometimes emits <function\tool_name{...}></function>
	content := `<function\web_search{"query": "SpaceX launches"}></function>`
	calls := extractTextToolCalls(content, func(n string) string { return "query" })
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Name != "web_search" {
		t.Errorf("want name web_search, got %s", calls[0].Name)
	}
}

func TestConvertMessages_NullContentForAssistantWithToolCalls(t *testing.T) {
	p := NewOpenAIProvider("", "", "")
	msgs := []Message{
		{
			Role:      RoleAssistant,
			Content:   "",
			ToolCalls: []ToolCall{{ID: "x", Name: "web_search", Arguments: "{}"}},
		},
	}
	out := p.convertMessages(msgs)
	if len(out) != 1 {
		t.Fatalf("expected 1 message, got %d", len(out))
	}
	// content must be present and nil (not absent)
	contentVal, exists := out[0]["content"]
	if !exists {
		t.Error("content key must be present for assistant messages")
	}
	if contentVal != nil {
		t.Errorf("content must be nil for empty assistant message, got %v", contentVal)
	}
}

func TestConvertMessages_ContentPresent(t *testing.T) {
	p := NewOpenAIProvider("", "", "")
	msgs := []Message{
		{Role: RoleUser, Content: "hello"},
		{Role: RoleAssistant, Content: "hi there"},
	}
	out := p.convertMessages(msgs)
	if out[0]["content"] != "hello" {
		t.Errorf("user content wrong: %v", out[0]["content"])
	}
	if out[1]["content"] != "hi there" {
		t.Errorf("assistant content wrong: %v", out[1]["content"])
	}
}

func TestRecoverFromFailedGeneration(t *testing.T) {
	body := `{"error":{"message":"failed","failed_generation":"<function=run_python>{\"code\":\"print(42)\"}</function>"}}`
	firstParam := func(n string) string { return "code" }
	resp := recoverFromFailedGeneration([]byte(body), firstParam)
	if resp == nil {
		t.Fatal("expected recovered response, got nil")
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Name != "run_python" {
		t.Errorf("wrong tool name: %s", resp.ToolCalls[0].Name)
	}
}

func TestChatCompletion_HTTPError(t *testing.T) {
	srv := fakeServer(t, 500, `{"error":{"message":"internal error"}}`)
	defer srv.Close()

	p := NewOpenAIProvider(srv.URL, "", "test-model")
	_, err := p.ChatCompletion(context.Background(), []Message{
		{Role: RoleUser, Content: "hi"},
	}, nil)
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should mention HTTP 500, got: %v", err)
	}
}

func TestBuildFirstParamLookup(t *testing.T) {
	tools := []ToolDef{
		{
			"function": map[string]any{
				"name": "web_search",
				"parameters": map[string]any{
					"required": []string{"query"},
				},
			},
		},
		{
			"function": map[string]any{
				"name": "run_python",
				"parameters": map[string]any{
					"required": []string{"code"},
				},
			},
		},
	}

	fn := buildFirstParamLookup(tools)
	if fn("web_search") != "query" {
		t.Errorf("want query, got %s", fn("web_search"))
	}
	if fn("run_python") != "code" {
		t.Errorf("want code, got %s", fn("run_python"))
	}
	if fn("unknown") != "input" {
		t.Errorf("want input fallback, got %s", fn("unknown"))
	}
}
