package anthropic_test

import (
	"context"
	"testing"

	"github.com/xraph/nexus/money"
	"github.com/xraph/nexus/provider"
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
		if !m.Pricing.InputPerMillion.IsPositive() {
			t.Errorf("model %q Pricing.InputPerMillion = %s, want > 0", m.ID, m.Pricing.InputPerMillion)
		}
		if !m.Pricing.OutputPerMillion.IsPositive() {
			t.Errorf("model %q Pricing.OutputPerMillion = %s, want > 0", m.ID, m.Pricing.OutputPerMillion)
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
		"claude-opus-5-5":            false,
		"claude-sonnet-5-5":          false,
		"claude-haiku-5-5":           false,
		"claude-fable-5-1":           false,
		"claude-fable-5":             false,
		"claude-opus-5":              false,
		"claude-sonnet-5":            false,
		"claude-opus-4-8":            false,
		"claude-opus-4-7":            false,
		"claude-opus-4-6":            false,
		"claude-sonnet-4-6":          false,
		"claude-haiku-4-5":           false,
		"claude-haiku-4-5-20251001":  false,
		"claude-opus-4-5":            false,
		"claude-opus-4-5-20251101":   false,
		"claude-sonnet-4-5":          false,
		"claude-sonnet-4-5-20250929": false,
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
	if len(models) != len(known) {
		t.Errorf("catalog lists %d models, want %d", len(models), len(known))
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

// catalogRow is one row of the verified price table: USD per million tokens.
type catalogRow struct {
	ids                                  []string
	name                                 string
	in, out, write5m, write1h, cacheRead string
	context, maxOutput                   int
}

func TestCatalogPricesMatchTheVerifiedTable(t *testing.T) {
	rows := []catalogRow{
		{[]string{"claude-opus-5-5"}, "Claude Opus 5.5", "4", "20", "5", "8", "0.20", 1_000_000, 128_000},
		{[]string{"claude-sonnet-5-5"}, "Claude Sonnet 5.5", "2", "10", "2.50", "4", "0.10", 1_000_000, 128_000},
		{[]string{"claude-haiku-5-5"}, "Claude Haiku 5.5", "0.10", "0.50", "0.125", "0.20", "0.01", 1_000_000, 128_000},
		{[]string{"claude-fable-5-1"}, "Claude Fable 5.1", "10", "50", "12.50", "20", "0.25", 1_000_000, 128_000},
		{[]string{"claude-fable-5"}, "Claude Fable 5", "10", "50", "12.50", "20", "1", 1_000_000, 128_000},
		{[]string{"claude-opus-5"}, "Claude Opus 5", "5", "25", "6.25", "10", "0.50", 1_000_000, 128_000},
		{[]string{"claude-sonnet-5"}, "Claude Sonnet 5", "2", "10", "2.50", "4", "0.20", 1_000_000, 128_000},
		{[]string{"claude-opus-4-8"}, "Claude Opus 4.8", "5", "25", "6.25", "10", "0.50", 1_000_000, 128_000},
		{[]string{"claude-opus-4-7"}, "Claude Opus 4.7", "5", "25", "6.25", "10", "0.50", 1_000_000, 128_000},
		{[]string{"claude-opus-4-6"}, "Claude Opus 4.6", "5", "25", "6.25", "10", "0.50", 1_000_000, 128_000},
		{[]string{"claude-sonnet-4-6"}, "Claude Sonnet 4.6", "3", "15", "3.75", "6", "0.30", 1_000_000, 128_000},
		{[]string{"claude-haiku-4-5", "claude-haiku-4-5-20251001"}, "Claude Haiku 4.5", "1", "5", "1.25", "2", "0.10", 200_000, 64_000},
		{[]string{"claude-opus-4-5", "claude-opus-4-5-20251101"}, "Claude Opus 4.5", "5", "25", "6.25", "10", "0.50", 200_000, 64_000},
		{[]string{"claude-sonnet-4-5", "claude-sonnet-4-5-20250929"}, "Claude Sonnet 4.5", "3", "15", "3.75", "6", "0.30", 200_000, 64_000},
	}

	models, err := anthropic.New("test-key").Models(context.Background())
	if err != nil {
		t.Fatalf("Models() error: %v", err)
	}
	byID := make(map[string]provider.Model, len(models))
	for _, m := range models {
		byID[m.ID] = m
	}

	for _, row := range rows {
		for _, id := range row.ids {
			t.Run(id, func(t *testing.T) {
				m, ok := byID[id]
				if !ok {
					t.Fatalf("model %q not in catalog", id)
				}
				if m.Name != row.name {
					t.Errorf("Name = %q, want %q", m.Name, row.name)
				}
				checkPrice(t, "InputPerMillion", m.Pricing.InputPerMillion, row.in)
				checkPrice(t, "OutputPerMillion", m.Pricing.OutputPerMillion, row.out)
				checkPrice(t, "CacheWritePerMillion", m.Pricing.CacheWritePerMillion, row.write5m)
				checkPrice(t, "CacheWriteLongPerMillion", m.Pricing.CacheWriteLongPerMillion, row.write1h)
				checkPrice(t, "CacheReadPerMillion", m.Pricing.CacheReadPerMillion, row.cacheRead)
				if m.ContextWindow != row.context {
					t.Errorf("ContextWindow = %d, want %d", m.ContextWindow, row.context)
				}
				if m.MaxOutput != row.maxOutput {
					t.Errorf("MaxOutput = %d, want %d", m.MaxOutput, row.maxOutput)
				}
				if id != "claude-haiku-5-5" && (m.Pricing.LongContextAbove != 0 || m.Pricing.LongContext != nil) {
					t.Errorf("unexpected long-context tier: above %d, tier %v", m.Pricing.LongContextAbove, m.Pricing.LongContext)
				}
			})
		}
	}

	t.Run("claude-haiku-5-5 long-context tier", func(t *testing.T) {
		p := byID["claude-haiku-5-5"].Pricing
		if p.LongContextAbove != 100_000 {
			t.Fatalf("LongContextAbove = %d, want 100000", p.LongContextAbove)
		}
		if p.LongContext == nil {
			t.Fatal("LongContext tier is nil")
		}
		checkPrice(t, "LongContext.InputPerMillion", p.LongContext.InputPerMillion, "0.50")
		checkPrice(t, "LongContext.OutputPerMillion", p.LongContext.OutputPerMillion, "2.50")
		checkPrice(t, "LongContext.CacheWritePerMillion", p.LongContext.CacheWritePerMillion, "0.625")
		checkPrice(t, "LongContext.CacheWriteLongPerMillion", p.LongContext.CacheWriteLongPerMillion, "1")
		checkPrice(t, "LongContext.CacheReadPerMillion", p.LongContext.CacheReadPerMillion, "0.05")
	})
}

func checkPrice(t *testing.T, field string, got money.USD, want string) {
	t.Helper()
	if !got.Equal(money.MustParse(want)) {
		t.Errorf("%s = %s, want %s", field, got, want)
	}
}

func TestCatalogDropsRetiredAndInvalidIDs(t *testing.T) {
	models, err := anthropic.New("test-key").Models(context.Background())
	if err != nil {
		t.Fatalf("Models() error: %v", err)
	}
	dropped := map[string]bool{
		"claude-3-5-haiku-20241022":  true,
		"claude-3-5-sonnet-20241022": true,
		"claude-sonnet-4-5-20250514": true,
		"claude-opus-4-5-20250630":   true,
	}
	for _, m := range models {
		if dropped[m.ID] {
			t.Errorf("catalog still lists %q", m.ID)
		}
	}
}
