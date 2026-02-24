// Package providertest provides shared test utilities for provider conformance testing.
package providertest

import (
	"context"
	"errors"
	"testing"

	"github.com/xraph/nexus/provider"
)

// TestProviderContract runs all conformance checks on a provider.Provider implementation.
// Every provider must pass these basic contract tests.
func TestProviderContract(t *testing.T, p provider.Provider) {
	t.Helper()

	t.Run("Name", func(t *testing.T) {
		name := p.Name()
		if name == "" {
			t.Fatal("provider name must not be empty")
		}
	})

	t.Run("Capabilities", func(t *testing.T) {
		caps := p.Capabilities()
		// At minimum, a provider should support chat OR embeddings.
		if !caps.Chat && !caps.Embeddings {
			t.Fatal("provider must support at least chat or embeddings")
		}
	})

	t.Run("Models", func(t *testing.T) {
		ctx := context.Background()
		models, err := p.Models(ctx)
		if err != nil {
			t.Fatalf("Models() returned error: %v", err)
		}
		if len(models) == 0 {
			t.Fatal("Models() must return at least one model")
		}

		providerName := p.Name()
		for _, m := range models {
			if m.ID == "" {
				t.Errorf("model ID must not be empty")
			}
			if m.Provider != providerName {
				t.Errorf("model %q has Provider=%q, want %q", m.ID, m.Provider, providerName)
			}
			if m.Name == "" {
				t.Errorf("model %q Name must not be empty", m.ID)
			}
			if m.ContextWindow <= 0 {
				t.Errorf("model %q ContextWindow must be positive, got %d", m.ID, m.ContextWindow)
			}

			// Models should have pricing set (except embeddings-only which use EmbeddingPerMillion).
			hasChatPricing := m.Pricing.InputPerMillion > 0 || m.Pricing.OutputPerMillion > 0
			hasEmbedPricing := m.Pricing.EmbeddingPerMillion > 0
			if !hasChatPricing && !hasEmbedPricing {
				t.Errorf("model %q must have pricing set", m.ID)
			}
		}
	})
}

// TestProviderComplete runs a basic non-streaming completion test using a mock.
// The provider must be configured to use a mock server.
func TestProviderComplete(t *testing.T, p provider.Provider) {
	t.Helper()

	ctx := context.Background()
	resp, err := p.Complete(ctx, &provider.CompletionRequest{
		Model: "test-model",
		Messages: []provider.Message{
			{Role: "user", Content: "Hello"},
		},
		MaxTokens: 100,
	})
	if err != nil {
		t.Fatalf("Complete() returned error: %v", err)
	}
	if resp == nil {
		t.Fatal("Complete() returned nil response")
	}
	if resp.Provider == "" {
		t.Error("response Provider must not be empty")
	}
	if len(resp.Choices) == 0 {
		t.Error("response must have at least one choice")
	}
}

// TestProviderEmbed runs a basic embedding test using a mock.
func TestProviderEmbed(t *testing.T, p provider.Provider) {
	t.Helper()

	ctx := context.Background()
	resp, err := p.Embed(ctx, &provider.EmbeddingRequest{
		Model: "test-embed-model",
		Input: []string{"Hello world"},
	})
	if err != nil {
		t.Fatalf("Embed() returned error: %v", err)
	}
	if resp == nil {
		t.Fatal("Embed() returned nil response")
	}
	if len(resp.Embeddings) == 0 {
		t.Error("response must have at least one embedding")
	}
}

// TestProviderEmbedNotSupported verifies that a provider correctly returns ErrNotSupported.
func TestProviderEmbedNotSupported(t *testing.T, p provider.Provider) {
	t.Helper()

	ctx := context.Background()
	_, err := p.Embed(ctx, &provider.EmbeddingRequest{
		Model: "any-model",
		Input: []string{"Hello"},
	})
	if !errors.Is(err, provider.ErrNotSupported) {
		t.Fatalf("Embed() should return ErrNotSupported, got: %v", err)
	}
}
