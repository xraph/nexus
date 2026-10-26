package openai

import (
	"testing"

	"github.com/xraph/nexus/money"
	"github.com/xraph/nexus/provider"
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
			hasChatPricing := m.Pricing.InputPerMillion.IsPositive() || m.Pricing.OutputPerMillion.IsPositive()
			hasEmbedPricing := m.Pricing.EmbeddingPerMillion.IsPositive()

			if !hasChatPricing && !hasEmbedPricing {
				t.Errorf("model %q must have pricing set (chat or embedding)", m.ID)
			}

			// Ensure no negative pricing values.
			if m.Pricing.InputPerMillion.IsNegative() {
				t.Errorf("model %q InputPerMillion = %s, must not be negative", m.ID, m.Pricing.InputPerMillion)
			}
			if m.Pricing.OutputPerMillion.IsNegative() {
				t.Errorf("model %q OutputPerMillion = %s, must not be negative", m.ID, m.Pricing.OutputPerMillion)
			}
			if m.Pricing.EmbeddingPerMillion.IsNegative() {
				t.Errorf("model %q EmbeddingPerMillion = %s, must not be negative", m.ID, m.Pricing.EmbeddingPerMillion)
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
				if !m.Pricing.EmbeddingPerMillion.IsPositive() {
					t.Errorf("embedding model %q should have EmbeddingPerMillion > 0", m.ID)
				}
			})
		}
	}
	if embeddingCount == 0 {
		t.Error("expected at least one embedding model in the catalog")
	}
}

// catalogIDs is every id the catalog lists, read from OpenAI's pricing and
// models pages on 2026-10-25. Shut-down ids (o1, o1-mini, o3-mini, o4-mini,
// gpt-4-turbo, gpt-4, gpt-3.5-turbo, gpt-4.1-nano, ...) are left out, even
// where the pricing page still prices them.
var catalogIDs = []string{
	"gpt-6-astra", "gpt-6.1-sol", "gpt-6-sol", "gpt-6-luna",
	"gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna",
	"gpt-5.5", "gpt-5.5-2026-04-23",
	"gpt-5.4", "gpt-5.4-2026-03-05",
	"gpt-5.4-mini", "gpt-5.4-mini-2026-03-17",
	"gpt-5.4-nano", "gpt-5.4-nano-2026-03-17",
	"gpt-5.2", "gpt-5.2-2025-12-11",
	"gpt-5.1", "gpt-5.1-2025-11-13",
	"gpt-5", "gpt-5-2025-08-07",
	"gpt-5-mini", "gpt-5-mini-2025-08-07",
	"gpt-5-nano", "gpt-5-nano-2025-08-07",
	"gpt-4.1", "gpt-4.1-2025-04-14",
	"gpt-4.1-mini", "gpt-4.1-mini-2025-04-14",
	"gpt-4o", "gpt-4o-2024-11-20", "gpt-4o-2024-08-06",
	"gpt-4o-mini", "gpt-4o-mini-2024-07-18",
	"o3", "o3-2025-04-16",
	"chat-latest",
	"text-embedding-3-small", "text-embedding-3-large", "text-embedding-ada-002",
}

func TestOpenAICatalogRowsAreComplete(t *testing.T) {
	models := openAIModels()
	got := make(map[string]bool, len(models))
	for _, m := range models {
		if got[m.ID] {
			t.Errorf("duplicate model ID %q", m.ID)
		}
		got[m.ID] = true
		if m.Name == "" || m.ContextWindow <= 0 {
			t.Errorf("%s: Name %q, ContextWindow %d; both must be set", m.ID, m.Name, m.ContextWindow)
		}
		if m.Capabilities.Chat {
			if m.MaxOutput <= 0 || !m.Pricing.InputPerMillion.IsPositive() || !m.Pricing.OutputPerMillion.IsPositive() {
				t.Errorf("%s: chat model needs MaxOutput, input and output prices, got %d %s %s",
					m.ID, m.MaxOutput, m.Pricing.InputPerMillion, m.Pricing.OutputPerMillion)
			}
			if lc := m.Pricing.LongContext; lc != nil {
				if m.Pricing.LongContextAbove != 272_000 || !lc.InputPerMillion.IsPositive() || !lc.OutputPerMillion.IsPositive() {
					t.Errorf("%s: long-context tier must start above 272K and carry input and output prices", m.ID)
				}
			}
		} else if !m.Pricing.EmbeddingPerMillion.IsPositive() {
			t.Errorf("%s: embedding model needs an embedding price", m.ID)
		}
	}
	for _, id := range catalogIDs {
		if !got[id] {
			t.Errorf("catalog is missing %q", id)
		}
	}
	if len(models) != len(catalogIDs) {
		t.Errorf("catalog has %d models, want exactly the %d listed here", len(models), len(catalogIDs))
	}
	for _, gone := range []string{"gpt-4-turbo", "o1-mini", "o1", "o3-mini", "o4-mini", "gpt-4", "gpt-3.5-turbo", "gpt-4.1-nano"} {
		if got[gone] {
			t.Errorf("%q is shut down and must not be listed", gone)
		}
	}
}

func TestOpenAICachePricesAreTheirOwn(t *testing.T) {
	byID := make(map[string]provider.Model)
	for _, m := range openAIModels() {
		byID[m.ID] = m
	}
	cases := []struct {
		id, read, write string
	}{
		{"gpt-4o", "1.25", "0"},         // older models: cached input only
		{"gpt-6.1-sol", "0.10", "2.50"}, // GPT-5.6 and later bill 30-minute writes
		{"gpt-5.5", "0.50", "0"},
	}
	for _, tc := range cases {
		p := byID[tc.id].Pricing
		if !p.CacheReadPerMillion.Equal(money.MustParse(tc.read)) || !p.CacheWritePerMillion.Equal(money.MustParse(tc.write)) {
			t.Errorf("%s: cache read %s write %s, want %s and %s", tc.id, p.CacheReadPerMillion, p.CacheWritePerMillion, tc.read, tc.write)
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
