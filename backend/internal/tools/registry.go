package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
)

type ParameterDef struct {
	Type        string `json:"type"`
	Description string `json:"description"`
	Required    bool   `json:"-"`
}

type Tool struct {
	Name        string                                                         `json:"name"`
	Description string                                                         `json:"description"`
	Parameters  map[string]ParameterDef                                        `json:"parameters"`
	Execute     func(ctx context.Context, args map[string]any) (string, error) `json:"-"`
}

func (t *Tool) OpenAIDef() map[string]any {
	properties := make(map[string]any)
	required := []string{}

	for name, param := range t.Parameters {
		properties[name] = map[string]any{
			"type":        param.Type,
			"description": param.Description,
		}
		if param.Required {
			required = append(required, name)
		}
	}

	// Sort required for stable ordering (helps buildFirstParamLookup pick a deterministic first param)
	sort.Strings(required)

	return map[string]any{
		"type": "function",
		"function": map[string]any{
			"name":        t.Name,
			"description": t.Description,
			"parameters": map[string]any{
				"type":       "object",
				"properties": properties,
				"required":   required,
			},
		},
	}
}

type Registry struct {
	tools map[string]*Tool
	mu    sync.RWMutex
}

func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]*Tool)}
}

func (r *Registry) Register(tool *Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[tool.Name] = tool
}

func (r *Registry) Get(name string) (*Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

func (r *Registry) List() []*Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Tool, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t)
	}
	return out
}

func (r *Registry) OpenAIDefs() []map[string]any {
	list := r.List()
	defs := make([]map[string]any, len(list))
	for i, t := range list {
		defs[i] = t.OpenAIDef()
	}
	return defs
}

func (r *Registry) Execute(ctx context.Context, name, argsJSON string) (string, error) {
	tool, ok := r.Get(name)
	if !ok {
		return "", fmt.Errorf("tool %q not found", name)
	}

	var args map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("invalid tool arguments: %w", err)
	}

	// Validate required parameters before invoking the tool so the model gets
	// a clear, actionable error rather than a tool-specific message.
	var missing []string
	for paramName, paramDef := range tool.Parameters {
		if !paramDef.Required {
			continue
		}
		val, exists := args[paramName]
		if !exists || val == nil {
			missing = append(missing, paramName)
			continue
		}
		if s, ok := val.(string); ok && strings.TrimSpace(s) == "" {
			missing = append(missing, paramName)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return "", fmt.Errorf("missing required parameter(s) for %s: %s", name, strings.Join(missing, ", "))
	}

	return tool.Execute(ctx, args)
}
