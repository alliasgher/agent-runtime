package main

import (
	"log"
	"os"

	"github.com/ali-asghar/agent-runtime/internal/llm"
	"github.com/ali-asghar/agent-runtime/internal/server"
	"github.com/ali-asghar/agent-runtime/internal/tools"
)

func main() {
	// Configuration from environment
	port := envOr("PORT", "8080")
	baseURL := envOr("LLM_BASE_URL", "https://api.groq.com/openai/v1") // Groq free tier
	apiKey := envOr("LLM_API_KEY", "")
	model := envOr("LLM_MODEL", "llama-3.1-8b-instant") // Free on Groq

	if apiKey == "" {
		log.Fatal("LLM_API_KEY is required. Get a free key at https://console.groq.com")
	}

	// Create LLM provider
	provider := llm.NewOpenAIProvider(baseURL, apiKey, model)

	// Create tool registry with built-in tools
	registry := tools.NewRegistry()
	registry.Register(tools.NewWebSearchTool())
	registry.Register(tools.NewReadURLTool())
	registry.Register(tools.NewRunPythonTool())
	registry.Register(tools.NewWikipediaTool())

	// Start server
	if err := server.Start(":"+port, provider, registry); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
