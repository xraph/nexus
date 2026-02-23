package anthropic_test

import (
	"context"
	"testing"

	"github.com/xraph/nexus/providers/anthropic"
)

// --------------------------------------------------------------------
// Model catalog validation
// --------------------------------------------------------------------

func TestModels_AllValid(t *testing.T) {
	p := anthropic.New("test-key")
	ctx := context.Background()

	models, err := p.Models(ctx)
	if err != nil {
		t.Fatalf("Models() error: %v", err)
	}

	if len(models) == 0 {
		t.Fatal("Models() returned empty list")
	}

	for _, m := range models {
		t.Run(m.ID, func(t *testing.T) {
			if m.ID == "" {
				t.Error("model ID must not be empty")
			}
			if m.Name == "" {
				t.Errorf("model %q Name must not be empty", m.ID)
			}
		})
	}
}

func TestModels_ProviderIsAnthropic(t *testing.T) {
	p := anthropic.New("test-key")
	ctx := context.Background()

	models, err := p.Models(ctx)
	if err != nil {
		t.Fatalf("Models() error: %v", err)
	}

	for _, m := range models {
		if m.Provider != "anthropic" {
			t.Errorf("model %q Provider = %q, want %q", m.ID, m.Provider, "anthropic")
		}
	}
}

func TestModels_ContextWindowPositive(t *testing.T) {
	p := anthropic.New("test-key")
	ctx := context.Background()

	models, err := p.Models(ctx)
	if err != nil {
		t.Fatalf("Models() error: %v", err)
	}

	for _, m := range models {
		if m.ContextWindow <= 0 {
			t.Errorf("model %q ContextWindow = %d, want > 0", m.ID, m.ContextWindow)
		}
	}
}

func TestModels_MaxOutputPositive(t *testing.T) {
	p := anthropic.New("test-key")
	ctx := context.Background()

	models, err := p.Models(ctx)
	if err != nil {
		t.Fatalf("Models() error: %v", err)
	}

	for _, m := range models {
		if m.MaxOutput <= 0 {
			t.Errorf("model %q MaxOutput = %d, want > 0", m.ID, m.MaxOutput)
		}
	}
}

func TestModels_PricingSet(t *testing.T) {
	p := anthropic.New("test-key")
	ctx := context.Background()

	models, err := p.Models(ctx)
	if err != nil {
		t.Fatalf("Models() error: %v", err)
	}

	for _, m := range models {
		if m.Pricing.InputPerMillion <= 0 {
			t.Errorf("model %q Pricing.InputPerMillion = %f, want > 0", m.ID, m.Pricing.InputPerMillion)
		}
		if m.Pricing.OutputPerMillion <= 0 {
			t.Errorf("model %q Pricing.OutputPerMillion = %f, want > 0", m.ID, m.Pricing.OutputPerMillion)
		}
	}
}

func TestModels_ChatCapability(t *testing.T) {
	p := anthropic.New("test-key")
	ctx := context.Background()

	models, err := p.Models(ctx)
	if err != nil {
		t.Fatalf("Models() error: %v", err)
	}

	for _, m := range models {
		if !m.Capabilities.Chat {
			t.Errorf("model %q should have Chat capability", m.ID)
		}
		if !m.Capabilities.Streaming {
			t.Errorf("model %q should have Streaming capability", m.ID)
		}
	}
}

func TestModels_KnownModels(t *testing.T) {
	p := anthropic.New("test-key")
	ctx := context.Background()

	models, err := p.Models(ctx)
	if err != nil {
		t.Fatalf("Models() error: %v", err)
	}

	known := map[string]bool{
		"claude-sonnet-4-5-20250514":  false,
		"claude-opus-4-5-20250630":    false,
		"claude-3-5-haiku-20241022":   false,
		"claude-3-5-sonnet-20241022":  false,
	}

	for _, m := range models {
		if _, ok := known[m.ID]; ok {
			known[m.ID] = true
		}
	}

	for id, found := range known {
		if !found {
			t.Errorf("expected model %q not found in catalog", id)
		}
	}
}

func TestModels_NoDuplicateIDs(t *testing.T) {
	p := anthropic.New("test-key")
	ctx := context.Background()

	models, err := p.Models(ctx)
	if err != nil {
		t.Fatalf("Models() error: %v", err)
	}

	seen := make(map[string]bool)
	for _, m := range models {
		if seen[m.ID] {
			t.Errorf("duplicate model ID: %q", m.ID)
		}
		seen[m.ID] = true
	}
}

func TestModels_NoEmbeddingCapability(t *testing.T) {
	p := anthropic.New("test-key")
	ctx := context.Background()

	models, err := p.Models(ctx)
	if err != nil {
		t.Fatalf("Models() error: %v", err)
	}

	for _, m := range models {
		if m.Capabilities.Embeddings {
			t.Errorf("model %q should NOT have Embeddings capability (Anthropic does not support embeddings)", m.ID)
		}
	}
}
