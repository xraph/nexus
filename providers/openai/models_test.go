package openai

import (
	"testing"
)

// ---------------------------------------------------------------------------
// Model catalog validation
// ---------------------------------------------------------------------------

func TestOpenAIModels_NonEmpty(t *testing.T) {
	models := openAIModels()
	if len(models) == 0 {
		t.Fatal("openAIModels() returned empty slice")
	}
}

func TestOpenAIModels_AllFieldsValid(t *testing.T) {
	models := openAIModels()
	for _, m := range models {
		t.Run(m.ID, func(t *testing.T) {
			if m.ID == "" {
				t.Error("model ID must not be empty")
			}
			if m.Name == "" {
				t.Errorf("model %q Name must not be empty", m.ID)
			}
			if m.ContextWindow <= 0 {
				t.Errorf("model %q ContextWindow must be positive, got %d", m.ID, m.ContextWindow)
			}
		})
	}
}

func TestOpenAIModels_ProviderIsOpenAI(t *testing.T) {
	models := openAIModels()
	for _, m := range models {
		t.Run(m.ID, func(t *testing.T) {
			if m.Provider != "openai" {
				t.Errorf("model %q Provider = %q, want %q", m.ID, m.Provider, "openai")
			}
		})
	}
}

func TestOpenAIModels_PositivePricing(t *testing.T) {
	models := openAIModels()
	for _, m := range models {
		t.Run(m.ID, func(t *testing.T) {
			hasChatPricing := m.Pricing.InputPerMillion > 0 || m.Pricing.OutputPerMillion > 0
			hasEmbedPricing := m.Pricing.EmbeddingPerMillion > 0

			if !hasChatPricing && !hasEmbedPricing {
				t.Errorf("model %q must have pricing set (chat or embedding)", m.ID)
			}

			// Ensure no negative pricing values.
			if m.Pricing.InputPerMillion < 0 {
				t.Errorf("model %q InputPerMillion = %f, must not be negative", m.ID, m.Pricing.InputPerMillion)
			}
			if m.Pricing.OutputPerMillion < 0 {
				t.Errorf("model %q OutputPerMillion = %f, must not be negative", m.ID, m.Pricing.OutputPerMillion)
			}
			if m.Pricing.EmbeddingPerMillion < 0 {
				t.Errorf("model %q EmbeddingPerMillion = %f, must not be negative", m.ID, m.Pricing.EmbeddingPerMillion)
			}
		})
	}
}

func TestOpenAIModels_UniqueIDs(t *testing.T) {
	models := openAIModels()
	seen := make(map[string]bool, len(models))
	for _, m := range models {
		if seen[m.ID] {
			t.Errorf("duplicate model ID: %q", m.ID)
		}
		seen[m.ID] = true
	}
}

func TestOpenAIModels_ChatModelsHaveChatCapability(t *testing.T) {
	models := openAIModels()
	for _, m := range models {
		// Skip embedding-only models.
		if m.Capabilities.Embeddings && !m.Capabilities.Chat {
			continue
		}
		t.Run(m.ID, func(t *testing.T) {
			if !m.Capabilities.Chat {
				t.Errorf("non-embedding model %q should have Chat capability", m.ID)
			}
		})
	}
}

func TestOpenAIModels_ChatModelsHaveMaxOutput(t *testing.T) {
	models := openAIModels()
	for _, m := range models {
		if !m.Capabilities.Chat {
			continue
		}
		t.Run(m.ID, func(t *testing.T) {
			if m.MaxOutput <= 0 {
				t.Errorf("chat model %q MaxOutput must be positive, got %d", m.ID, m.MaxOutput)
			}
		})
	}
}

func TestOpenAIModels_EmbeddingModelsHaveEmbeddingCapability(t *testing.T) {
	models := openAIModels()
	embeddingCount := 0
	for _, m := range models {
		if m.Capabilities.Embeddings {
			embeddingCount++
			t.Run(m.ID, func(t *testing.T) {
				if m.Pricing.EmbeddingPerMillion <= 0 {
					t.Errorf("embedding model %q should have EmbeddingPerMillion > 0", m.ID)
				}
			})
		}
	}
	if embeddingCount == 0 {
		t.Error("expected at least one embedding model in the catalog")
	}
}

func TestOpenAIModels_KnownModelsPresent(t *testing.T) {
	models := openAIModels()
	ids := make(map[string]bool, len(models))
	for _, m := range models {
		ids[m.ID] = true
	}

	expected := []string{
		"gpt-4o",
		"gpt-4o-mini",
		"gpt-4-turbo",
		"o1",
		"o1-mini",
		"text-embedding-3-small",
		"text-embedding-3-large",
	}
	for _, id := range expected {
		if !ids[id] {
			t.Errorf("expected model %q to be in the catalog", id)
		}
	}
}

func TestOpenAIModels_StreamingConsistency(t *testing.T) {
	// All chat models should have streaming support.
	models := openAIModels()
	for _, m := range models {
		if !m.Capabilities.Chat {
			continue
		}
		t.Run(m.ID, func(t *testing.T) {
			if !m.Capabilities.Streaming {
				t.Errorf("chat model %q should support streaming", m.ID)
			}
		})
	}
}
