package anthropic

import (
	"github.com/xraph/nexus/money"
	"github.com/xraph/nexus/provider"
)

// Prices are USD per million tokens, read from
// https://platform.claude.com/docs/en/about-claude/pricing on 2026-10-24.
// Context windows and output limits come from Anthropic's models overview
// (https://platform.claude.com/docs/en/about-claude/models/overview) and the
// per-model pages it links. Do not change a number without re-reading them.

// claudePrice builds a Pricing from the five per-million prices on the
// pricing page: input, output, 5-minute cache write, 1-hour cache write and
// cache read.
func claudePrice(in, out, write5m, write1h, read string) provider.Pricing {
	return provider.Pricing{
		InputPerMillion:          money.MustParse(in),
		OutputPerMillion:         money.MustParse(out),
		CacheWritePerMillion:     money.MustParse(write5m),
		CacheWriteLongPerMillion: money.MustParse(write1h),
		CacheReadPerMillion:      money.MustParse(read),
	}
}

func claude(id, name string, contextWindow, maxOutput int, pricing provider.Pricing) provider.Model {
	return provider.Model{
		ID:       id,
		Provider: "anthropic",
		Name:     name,
		Capabilities: provider.Capabilities{
			Chat: true, Streaming: true, Vision: true, Tools: true, JSON: true, Thinking: true,
		},
		ContextWindow: contextWindow,
		MaxOutput:     maxOutput,
		Pricing:       pricing,
	}
}

// anthropicModels returns the known Anthropic model catalog. Dated ids are
// listed next to their alias with the same limits and prices.
func anthropicModels() []provider.Model {
	haiku55 := claudePrice("0.10", "0.50", "0.125", "0.20", "0.01")
	haiku55LongContext := claudePrice("0.50", "2.50", "0.625", "1", "0.05")
	haiku55.LongContextAbove = 100_000
	haiku55.LongContext = &haiku55LongContext

	opus5 := claudePrice("5", "25", "6.25", "10", "0.50")
	sonnet46 := claudePrice("3", "15", "3.75", "6", "0.30")
	haiku45 := claudePrice("1", "5", "1.25", "2", "0.10")

	return []provider.Model{
		claude("claude-opus-5-5", "Claude Opus 5.5", 1_000_000, 128_000, claudePrice("4", "20", "5", "8", "0.20")),
		claude("claude-sonnet-5-5", "Claude Sonnet 5.5", 1_000_000, 128_000, claudePrice("2", "10", "2.50", "4", "0.10")),
		claude("claude-haiku-5-5", "Claude Haiku 5.5", 1_000_000, 128_000, haiku55),
		claude("claude-fable-5-1", "Claude Fable 5.1", 1_000_000, 128_000, claudePrice("10", "50", "12.50", "20", "0.25")),
		claude("claude-fable-5", "Claude Fable 5", 1_000_000, 128_000, claudePrice("10", "50", "12.50", "20", "1")),
		claude("claude-opus-5", "Claude Opus 5", 1_000_000, 128_000, opus5),
		claude("claude-sonnet-5", "Claude Sonnet 5", 1_000_000, 128_000, claudePrice("2", "10", "2.50", "4", "0.20")),
		claude("claude-opus-4-8", "Claude Opus 4.8", 1_000_000, 128_000, opus5),
		claude("claude-opus-4-7", "Claude Opus 4.7", 1_000_000, 128_000, opus5),
		claude("claude-opus-4-6", "Claude Opus 4.6", 1_000_000, 128_000, opus5),
		claude("claude-sonnet-4-6", "Claude Sonnet 4.6", 1_000_000, 128_000, sonnet46),
		claude("claude-haiku-4-5", "Claude Haiku 4.5", 200_000, 64_000, haiku45),
		claude("claude-haiku-4-5-20251001", "Claude Haiku 4.5", 200_000, 64_000, haiku45),
		claude("claude-opus-4-5", "Claude Opus 4.5", 200_000, 64_000, opus5),
		claude("claude-opus-4-5-20251101", "Claude Opus 4.5", 200_000, 64_000, opus5),
		// Deprecated: retires 2026-11-30.
		claude("claude-sonnet-4-5", "Claude Sonnet 4.5", 200_000, 64_000, sonnet46),
		claude("claude-sonnet-4-5-20250929", "Claude Sonnet 4.5", 200_000, 64_000, sonnet46),
	}
}

// catalog indexes anthropicModels by id for request shaping. Models returns
// fresh copies, so callers can never change what this map holds.
var catalog = func() map[string]provider.Model {
	models := anthropicModels()
	byID := make(map[string]provider.Model, len(models))
	for _, m := range models {
		byID[m.ID] = m
	}
	return byID
}()

// catalogModel returns the catalog entry for id.
func catalogModel(id string) (provider.Model, bool) {
	m, ok := catalog[id]
	return m, ok
}
