package openai

import (
	"github.com/xraph/nexus/money"
	"github.com/xraph/nexus/provider"
)

// The catalog is every model OpenAI's Chat Completions API serves, plus
// the embedding models, as listed on 2026-10-25. Prices are USD per million
// tokens at the Standard tier, from https://developers.openai.com/api/docs/pricing
// (https://openai.com/api/pricing/ answered 403 that day; the platform
// pricing and models pages redirect to developers.openai.com). Context
// windows, output limits and Chat Completions support come from each
// model's page, linked on its row, which also repeats its price. Ids a
// model page marks "Shut down" are left out even where the pricing page
// still prices them (o1, o3-mini, o4-mini, gpt-4.1-nano, gpt-4-turbo,
// gpt-4, gpt-3.5-turbo, gpt-4o-2024-05-13); deprecated ids still served
// keep their rows. Models served by Chat Completions only for audio
// (gpt-audio, gpt-audio-mini, gpt-audio-1.5) are left out: this client
// sends no audio, and their text prices would understate audio tokens.
// Do not change a number without re-reading the pages.

// longContextAbove is where OpenAI's long-context prices start: prompts of
// more than 272K input tokens.
const longContextAbove = 272_000

// price builds a Pricing from the input, output and cached-input prices.
// A cached input of "" means the pricing page lists none.
func price(in, out, cachedIn string) provider.Pricing {
	p := provider.Pricing{
		InputPerMillion:  money.MustParse(in),
		OutputPerMillion: money.MustParse(out),
	}
	if cachedIn != "" {
		p.CacheReadPerMillion = money.MustParse(cachedIn)
	}
	return p
}

// withWrite adds the 30-minute cache-write price GPT-5.6 and later bill
// (Chat Completions only writes with the default 30-minute lifetime).
func withWrite(p provider.Pricing, write30m string) provider.Pricing {
	p.CacheWritePerMillion = money.MustParse(write30m)
	return p
}

// withLongContext adds the prices for prompts above 272K input tokens.
func withLongContext(p, long provider.Pricing) provider.Pricing {
	p.LongContextAbove = longContextAbove
	p.LongContext = &long
	return p
}

// chatCaps are the Chat Completions capabilities of a current OpenAI model:
// text and image input, streaming, structured outputs and, when tools is
// set, function calling.
func chatCaps(tools, thinking bool) provider.Capabilities {
	return provider.Capabilities{
		Chat: true, Streaming: true, Vision: true, JSON: true,
		Tools: tools, StreamingTools: tools, Thinking: thinking,
	}
}

func chat(id, name string, contextWindow, maxOutput int, caps provider.Capabilities, pricing provider.Pricing) provider.Model {
	return provider.Model{
		ID: id, Provider: "openai", Name: name,
		Capabilities:  caps,
		ContextWindow: contextWindow,
		MaxOutput:     maxOutput,
		Pricing:       pricing,
	}
}

func embedding(id, name, perMillion string) provider.Model {
	return provider.Model{
		ID: id, Provider: "openai", Name: name,
		Capabilities: provider.Capabilities{Embeddings: true},
		// verify-at-implementation: https://developers.openai.com/api/docs/guides/embeddings (max input), 2026-10-25
		ContextWindow: 8192,
		Pricing:       provider.Pricing{EmbeddingPerMillion: money.MustParse(perMillion)},
	}
}

// openAIModels returns the known OpenAI model catalog. Dated snapshots the
// model pages list as served sit next to their model with its limits and
// prices.
func openAIModels() []provider.Model {
	// GPT-6 Astra and GPT-6.1 Sol take tools only through the Responses
	// API; GPT-6 Sol and Luna only with reasoning_effort "none", which this
	// client does not send. So none of them calls tools here.
	gpt6 := chatCaps(false, true)
	reasoning := chatCaps(true, true)
	plain := chatCaps(true, false)

	// verify-at-implementation: https://developers.openai.com/api/docs/pricing, 2026-10-25
	gpt55 := withLongContext(price("5.00", "30.00", "0.50"), price("10.00", "45.00", "1.00"))
	gpt54 := withLongContext(price("2.50", "15.00", "0.25"), price("5.00", "22.50", "0.50"))
	gpt54mini := price("0.75", "4.50", "0.075")
	gpt54nano := price("0.20", "1.25", "0.02")
	gpt52 := price("1.75", "14.00", "0.175")
	gpt51 := price("1.25", "10.00", "0.125")
	gpt5 := price("1.25", "10.00", "0.125")
	gpt5mini := price("0.25", "2.00", "0.025")
	gpt5nano := price("0.05", "0.40", "0.005")
	gpt41 := price("2.00", "8.00", "0.50")
	gpt41mini := price("0.40", "1.60", "0.10")
	gpt4o := price("2.50", "10.00", "1.25")
	gpt4omini := price("0.15", "0.60", "0.075")
	o3 := price("2.00", "8.00", "0.50")

	return []provider.Model{
		// verify-at-implementation: https://developers.openai.com/api/docs/models/gpt-6-astra, 2026-10-25
		chat("gpt-6-astra", "GPT-6 Astra", 1_050_000, 128_000, gpt6,
			withLongContext(withWrite(price("10.00", "50.00", "1.00"), "12.50"), withWrite(price("20.00", "75.00", "2.00"), "25.00"))),
		// verify-at-implementation: https://developers.openai.com/api/docs/models/gpt-6.1-sol, 2026-10-25
		chat("gpt-6.1-sol", "GPT-6.1 Sol", 1_050_000, 128_000, gpt6,
			withLongContext(withWrite(price("2.00", "10.00", "0.10"), "2.50"), withWrite(price("4.00", "15.00", "0.20"), "5.00"))),
		// verify-at-implementation: https://developers.openai.com/api/docs/models/gpt-6-sol, 2026-10-25
		chat("gpt-6-sol", "GPT-6 Sol", 1_050_000, 128_000, gpt6,
			withLongContext(withWrite(price("2.00", "10.00", "0.20"), "2.50"), withWrite(price("4.00", "15.00", "0.40"), "5.00"))),
		// verify-at-implementation: https://developers.openai.com/api/docs/models/gpt-6-luna, 2026-10-25
		chat("gpt-6-luna", "GPT-6 Luna", 1_050_000, 128_000, gpt6,
			withLongContext(withWrite(price("0.10", "0.50", "0.01"), "0.125"), withWrite(price("0.20", "0.75", "0.02"), "0.25"))),
		// Promotional price, offered at least through 2026-11-21.
		// verify-at-implementation: https://developers.openai.com/api/docs/models/gpt-5.6-sol, 2026-10-25
		chat("gpt-5.6-sol", "GPT-5.6 Sol", 1_050_000, 128_000, reasoning,
			withLongContext(withWrite(price("4.00", "20.00", "0.40"), "5.00"), withWrite(price("8.00", "30.00", "0.80"), "10.00"))),
		// verify-at-implementation: https://developers.openai.com/api/docs/models/gpt-5.6-terra, 2026-10-25
		chat("gpt-5.6-terra", "GPT-5.6 Terra", 1_050_000, 128_000, reasoning,
			withLongContext(withWrite(price("2.00", "12.00", "0.20"), "2.50"), withWrite(price("4.00", "18.00", "0.40"), "5.00"))),
		// verify-at-implementation: https://developers.openai.com/api/docs/models/gpt-5.6-luna, 2026-10-25
		chat("gpt-5.6-luna", "GPT-5.6 Luna", 1_050_000, 128_000, reasoning,
			withLongContext(withWrite(price("0.20", "1.20", "0.02"), "0.25"), withWrite(price("0.40", "1.80", "0.04"), "0.50"))),
		// verify-at-implementation: https://developers.openai.com/api/docs/models/gpt-5.5, 2026-10-25
		chat("gpt-5.5", "GPT-5.5", 1_050_000, 128_000, reasoning, gpt55),
		chat("gpt-5.5-2026-04-23", "GPT-5.5", 1_050_000, 128_000, reasoning, gpt55),
		// verify-at-implementation: https://developers.openai.com/api/docs/models/gpt-5.4, 2026-10-25
		chat("gpt-5.4", "GPT-5.4", 1_050_000, 128_000, reasoning, gpt54),
		chat("gpt-5.4-2026-03-05", "GPT-5.4", 1_050_000, 128_000, reasoning, gpt54),
		// verify-at-implementation: https://developers.openai.com/api/docs/models/gpt-5.4-mini, 2026-10-25
		chat("gpt-5.4-mini", "GPT-5.4 Mini", 400_000, 128_000, reasoning, gpt54mini),
		chat("gpt-5.4-mini-2026-03-17", "GPT-5.4 Mini", 400_000, 128_000, reasoning, gpt54mini),
		// Deprecated. verify-at-implementation: https://developers.openai.com/api/docs/models/gpt-5.4-nano, 2026-10-25
		chat("gpt-5.4-nano", "GPT-5.4 nano", 400_000, 128_000, reasoning, gpt54nano),
		chat("gpt-5.4-nano-2026-03-17", "GPT-5.4 nano", 400_000, 128_000, reasoning, gpt54nano),
		// verify-at-implementation: https://developers.openai.com/api/docs/models/gpt-5.2, 2026-10-25
		chat("gpt-5.2", "GPT-5.2", 400_000, 128_000, reasoning, gpt52),
		chat("gpt-5.2-2025-12-11", "GPT-5.2", 400_000, 128_000, reasoning, gpt52),
		// Deprecated. verify-at-implementation: https://developers.openai.com/api/docs/models/gpt-5.1, 2026-10-25
		chat("gpt-5.1", "GPT-5.1", 400_000, 128_000, reasoning, gpt51),
		chat("gpt-5.1-2025-11-13", "GPT-5.1", 400_000, 128_000, reasoning, gpt51),
		// Deprecated. verify-at-implementation: https://developers.openai.com/api/docs/models/gpt-5, 2026-10-25
		chat("gpt-5", "GPT-5", 400_000, 128_000, reasoning, gpt5),
		chat("gpt-5-2025-08-07", "GPT-5", 400_000, 128_000, reasoning, gpt5),
		// Deprecated. verify-at-implementation: https://developers.openai.com/api/docs/models/gpt-5-mini, 2026-10-25
		chat("gpt-5-mini", "GPT-5 Mini", 400_000, 128_000, reasoning, gpt5mini),
		chat("gpt-5-mini-2025-08-07", "GPT-5 Mini", 400_000, 128_000, reasoning, gpt5mini),
		// Deprecated. verify-at-implementation: https://developers.openai.com/api/docs/models/gpt-5-nano, 2026-10-25
		chat("gpt-5-nano", "GPT-5 nano", 400_000, 128_000, reasoning, gpt5nano),
		chat("gpt-5-nano-2025-08-07", "GPT-5 nano", 400_000, 128_000, reasoning, gpt5nano),
		// Deprecated. verify-at-implementation: https://developers.openai.com/api/docs/models/gpt-4.1, 2026-10-25
		chat("gpt-4.1", "GPT-4.1", 1_047_576, 32_768, plain, gpt41),
		chat("gpt-4.1-2025-04-14", "GPT-4.1", 1_047_576, 32_768, plain, gpt41),
		// Deprecated. verify-at-implementation: https://developers.openai.com/api/docs/models/gpt-4.1-mini, 2026-10-25
		chat("gpt-4.1-mini", "GPT-4.1 Mini", 1_047_576, 32_768, plain, gpt41mini),
		chat("gpt-4.1-mini-2025-04-14", "GPT-4.1 Mini", 1_047_576, 32_768, plain, gpt41mini),
		// Deprecated. verify-at-implementation: https://developers.openai.com/api/docs/models/gpt-4o, 2026-10-25
		chat("gpt-4o", "GPT-4o", 128_000, 16_384, plain, gpt4o),
		chat("gpt-4o-2024-11-20", "GPT-4o", 128_000, 16_384, plain, gpt4o),
		chat("gpt-4o-2024-08-06", "GPT-4o", 128_000, 16_384, plain, gpt4o),
		// Deprecated. verify-at-implementation: https://developers.openai.com/api/docs/models/gpt-4o-mini, 2026-10-25
		chat("gpt-4o-mini", "GPT-4o Mini", 128_000, 16_384, plain, gpt4omini),
		chat("gpt-4o-mini-2024-07-18", "GPT-4o Mini", 128_000, 16_384, plain, gpt4omini),
		// Deprecated. verify-at-implementation: https://developers.openai.com/api/docs/models/o3, 2026-10-25
		chat("o3", "o3", 200_000, 100_000, reasoning, o3),
		chat("o3-2025-04-16", "o3", 200_000, 100_000, reasoning, o3),
		// Priced under "Specialized models".
		// verify-at-implementation: https://developers.openai.com/api/docs/models/chat-latest, 2026-10-25
		chat("chat-latest", "Chat Latest", 400_000, 128_000, plain, price("5.00", "30.00", "0.50")),

		// verify-at-implementation: https://developers.openai.com/api/docs/models/text-embedding-3-small, 2026-10-25
		embedding("text-embedding-3-small", "Embedding 3 Small", "0.02"),
		// verify-at-implementation: https://developers.openai.com/api/docs/models/text-embedding-3-large, 2026-10-25
		embedding("text-embedding-3-large", "Embedding 3 Large", "0.13"),
		// verify-at-implementation: https://developers.openai.com/api/docs/models/text-embedding-ada-002, 2026-10-25
		embedding("text-embedding-ada-002", "Embedding Ada 002", "0.10"),
	}
}
