// Example: standalone Nexus gateway usage (no Forge).
//
// This demonstrates using Nexus as a Go library: register providers,
// configure routing, cache, guardrails, and send completions.
//
//	go run ./_examples/standalone/main.go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	nexus "github.com/xraph/nexus"
	"github.com/xraph/nexus/cache/stores"
	"github.com/xraph/nexus/guard/guards"
	"github.com/xraph/nexus/model"
	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/providers/openai"
	"github.com/xraph/nexus/router/strategies"
)

func main() {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY environment variable is required")
	}

	ctx := context.Background()

	// Build the gateway with functional options.
	gw := nexus.New(
		// Providers
		nexus.WithProvider(openai.New(apiKey)),

		// Routing: cost-optimized
		nexus.WithRouter(strategies.NewCostOptimized()),

		// Cache: in-memory with defaults
		nexus.WithCache(stores.NewMemory()),

		// Guardrails
		nexus.WithGuard(guards.NewPII("redact")),
		nexus.WithGuard(guards.NewInjection()),

		// Model alias: "fast" → gpt-4o-mini
		nexus.WithAlias("fast", model.AliasTarget{
			Provider: "openai",
			Model:    "gpt-4o-mini",
		}),
	)

	// Initialize (applies defaults, builds pipeline).
	if err := gw.Initialize(ctx); err != nil {
		log.Fatalf("failed to initialize: %v", err)
	}
	defer gw.Shutdown(ctx)

	// Send a completion request via the engine.
	resp, err := gw.Engine().Complete(ctx, &provider.CompletionRequest{
		Model: "fast", // resolved via alias
		Messages: []provider.Message{
			{Role: "user", Content: "What is the capital of France?"},
		},
		MaxTokens: 100,
	})
	if err != nil {
		log.Fatalf("completion failed: %v", err)
	}

	// Print response.
	data, _ := json.MarshalIndent(resp, "", "  ")
	fmt.Println(string(data))
}
