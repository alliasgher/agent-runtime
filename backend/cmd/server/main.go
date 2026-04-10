package main

import (
	"log/slog"
	"os"

	"github.com/ali-asghar/agent-runtime/internal/llm"
	"github.com/ali-asghar/agent-runtime/internal/server"
	"github.com/ali-asghar/agent-runtime/internal/store"
	"github.com/ali-asghar/agent-runtime/internal/tools"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})))

	port := envOr("PORT", "8080")
	baseURL := envOr("LLM_BASE_URL", "https://api.groq.com/openai/v1")
	apiKey := envOr("LLM_API_KEY", "")
	model := envOr("LLM_MODEL", "llama-3.3-70b-versatile") // Free on Groq, better tool use
	databaseURL := envOr("DATABASE_URL", "")

	if apiKey == "" {
		slog.Error("LLM_API_KEY is required. Get a free key at https://console.groq.com")
		os.Exit(1)
	}

	var db *store.Store
	if databaseURL != "" {
		var err error
		db, err = store.New(databaseURL)
		if err != nil {
			slog.Error("failed to connect to database", "error", err)
			os.Exit(1)
		}
		slog.Info("connected to database")
	} else {
		slog.Info("DATABASE_URL not set — using in-memory session store")
	}

	provider := llm.NewOpenAIProvider(baseURL, apiKey, model)

	registry := tools.NewRegistry()
	registry.Register(tools.NewWebSearchTool())
	registry.Register(tools.NewReadURLTool())
	registry.Register(tools.NewRunPythonTool())
	registry.Register(tools.NewWikipediaTool())

	if err := server.Start(":"+port, provider, registry, db); err != nil {
		slog.Error("server failed", "error", err)
		os.Exit(1)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
