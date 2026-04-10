package tools

import (
	"context"
	"strings"
	"testing"
)

func TestRegistry_Execute_ToolNotFound(t *testing.T) {
	r := NewRegistry()
	_, err := r.Execute(context.Background(), "nonexistent", "{}")
	if err == nil {
		t.Fatal("expected error for missing tool")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found', got: %v", err)
	}
}

func TestRegistry_Execute_InvalidJSON(t *testing.T) {
	r := NewRegistry()
	r.Register(&Tool{
		Name:       "dummy",
		Parameters: map[string]ParameterDef{},
		Execute:    func(_ context.Context, _ map[string]any) (string, error) { return "ok", nil },
	})
	_, err := r.Execute(context.Background(), "dummy", "not-json")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestRegistry_Execute_MissingRequiredParam(t *testing.T) {
	r := NewRegistry()
	r.Register(&Tool{
		Name: "search",
		Parameters: map[string]ParameterDef{
			"query": {Type: "string", Required: true},
		},
		Execute: func(_ context.Context, _ map[string]any) (string, error) { return "ok", nil },
	})

	// Empty args — should fail with clear message
	_, err := r.Execute(context.Background(), "search", "{}")
	if err == nil {
		t.Fatal("expected error for missing required param")
	}
	if !strings.Contains(err.Error(), "query") {
		t.Errorf("error should mention missing param 'query', got: %v", err)
	}

	// Blank string value — should also fail
	_, err = r.Execute(context.Background(), "search", `{"query":""}`)
	if err == nil {
		t.Fatal("expected error for blank required param")
	}

	// Whitespace-only — should also fail
	_, err = r.Execute(context.Background(), "search", `{"query":"   "}`)
	if err == nil {
		t.Fatal("expected error for whitespace-only required param")
	}
}

func TestRegistry_Execute_AllRequiredParamsProvided(t *testing.T) {
	r := NewRegistry()
	r.Register(&Tool{
		Name: "search",
		Parameters: map[string]ParameterDef{
			"query": {Type: "string", Required: true},
		},
		Execute: func(_ context.Context, args map[string]any) (string, error) {
			return args["query"].(string), nil
		},
	})

	out, err := r.Execute(context.Background(), "search", `{"query":"hello"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "hello" {
		t.Errorf("want 'hello', got %q", out)
	}
}

func TestRegistry_OpenAIDefs(t *testing.T) {
	r := NewRegistry()
	r.Register(&Tool{
		Name:        "my_tool",
		Description: "does stuff",
		Parameters: map[string]ParameterDef{
			"input": {Type: "string", Description: "the input", Required: true},
		},
		Execute: func(_ context.Context, _ map[string]any) (string, error) { return "", nil },
	})

	defs := r.OpenAIDefs()
	if len(defs) != 1 {
		t.Fatalf("expected 1 def, got %d", len(defs))
	}

	fn, ok := defs[0]["function"].(map[string]any)
	if !ok {
		t.Fatal("function key missing in def")
	}
	if fn["name"] != "my_tool" {
		t.Errorf("wrong name: %v", fn["name"])
	}

	params := fn["parameters"].(map[string]any)
	required := params["required"].([]string)
	if len(required) != 1 || required[0] != "input" {
		t.Errorf("required should be [input], got %v", required)
	}
}

func TestWebSearchTool_MissingQuery(t *testing.T) {
	r := NewRegistry()
	r.Register(NewWebSearchTool())

	_, err := r.Execute(context.Background(), "web_search", `{}`)
	if err == nil {
		t.Fatal("expected error for missing query")
	}
	if !strings.Contains(err.Error(), "query") {
		t.Errorf("error should mention query, got: %v", err)
	}
}

func TestWikipediaTool_MissingQuery(t *testing.T) {
	r := NewRegistry()
	r.Register(NewWikipediaTool())

	_, err := r.Execute(context.Background(), "wikipedia", `{}`)
	if err == nil {
		t.Fatal("expected error for missing query")
	}
}
