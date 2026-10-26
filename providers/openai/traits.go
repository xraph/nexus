package openai

import "regexp"

// modelTraits is how one OpenAI model changes the request this client sends.
type modelTraits struct {
	// reasoning: the model reasons at its default effort and rejects a
	// non-default temperature or top_p, so both are left out. This client
	// sends no reasoning_effort, so a model whose default effort is "none"
	// (GPT-5.1, 5.2, 5.4) takes sampling and is not marked.
	reasoning bool
	// embeddingDimensions: the model can shorten its vectors to the
	// requested dimensions.
	embeddingDimensions bool
}

// traits is filled from the same pages as the catalog, read on 2026-10-25:
// each model's page for its default reasoning effort, and
// https://developers.openai.com/api/docs/guides/latest-model ("When
// reasoning effort is not none, remove temperature, top_p ...").
var traits = map[string]modelTraits{
	// verify-at-implementation: https://developers.openai.com/api/docs/models/gpt-6-astra, 2026-10-25 (no "none" effort)
	"gpt-6-astra": {reasoning: true},
	// verify-at-implementation: https://developers.openai.com/api/docs/models/gpt-6.1-sol, 2026-10-25 (no "none" effort)
	"gpt-6.1-sol": {reasoning: true},
	// verify-at-implementation: https://developers.openai.com/api/docs/models/gpt-6-sol, 2026-10-25 (default medium)
	"gpt-6-sol": {reasoning: true},
	// verify-at-implementation: https://developers.openai.com/api/docs/models/gpt-6-luna, 2026-10-25 (default medium)
	"gpt-6-luna": {reasoning: true},
	// verify-at-implementation: https://developers.openai.com/api/docs/models/gpt-5.6-sol, 2026-10-25 (default medium)
	"gpt-5.6-sol": {reasoning: true},
	// verify-at-implementation: https://developers.openai.com/api/docs/models/gpt-5.6-terra, 2026-10-25 (default medium)
	"gpt-5.6-terra": {reasoning: true},
	// verify-at-implementation: https://developers.openai.com/api/docs/models/gpt-5.6-luna, 2026-10-25 (default medium)
	"gpt-5.6-luna": {reasoning: true},
	// verify-at-implementation: https://developers.openai.com/api/docs/models/gpt-5.5, 2026-10-25 (default medium)
	"gpt-5.5": {reasoning: true},
	// verify-at-implementation: https://developers.openai.com/api/docs/models/gpt-5.4, 2026-10-25 (default none)
	"gpt-5.4": {},
	// verify-at-implementation: https://developers.openai.com/api/docs/models/gpt-5.4-mini, 2026-10-25 (default none)
	"gpt-5.4-mini": {},
	// verify-at-implementation: https://developers.openai.com/api/docs/models/gpt-5.4-nano, 2026-10-25 (default none)
	"gpt-5.4-nano": {},
	// verify-at-implementation: https://developers.openai.com/api/docs/models/gpt-5.2, 2026-10-25 (default none)
	"gpt-5.2": {},
	// verify-at-implementation: https://developers.openai.com/api/docs/models/gpt-5.1, 2026-10-25 (default none)
	"gpt-5.1": {},
	// verify-at-implementation: https://developers.openai.com/api/docs/models/gpt-5, 2026-10-25 (minimal to high, no none)
	"gpt-5": {reasoning: true},
	// verify-at-implementation: https://developers.openai.com/api/docs/models/gpt-5-mini, 2026-10-25 (reasoning model)
	"gpt-5-mini": {reasoning: true},
	// verify-at-implementation: https://developers.openai.com/api/docs/models/gpt-5-nano, 2026-10-25 (reasoning model)
	"gpt-5-nano": {reasoning: true},
	// verify-at-implementation: https://developers.openai.com/api/docs/models/gpt-4.1, 2026-10-25
	"gpt-4.1": {},
	// verify-at-implementation: https://developers.openai.com/api/docs/models/gpt-4.1-mini, 2026-10-25
	"gpt-4.1-mini": {},
	// verify-at-implementation: https://developers.openai.com/api/docs/models/gpt-4o, 2026-10-25
	"gpt-4o": {},
	// verify-at-implementation: https://developers.openai.com/api/docs/models/gpt-4o-mini, 2026-10-25
	"gpt-4o-mini": {},
	// verify-at-implementation: https://developers.openai.com/api/docs/models/o3, 2026-10-25 (o-series reasoning)
	"o3": {reasoning: true},
	// verify-at-implementation: https://developers.openai.com/api/docs/models/chat-latest, 2026-10-25 (no reasoning tokens)
	"chat-latest": {},
	// verify-at-implementation: https://developers.openai.com/api/docs/guides/embeddings ("dimensions" on the v3 models), 2026-10-25
	"text-embedding-3-small": {embeddingDimensions: true},
	"text-embedding-3-large": {embeddingDimensions: true},
	"text-embedding-ada-002": {},
}

// snapshotSuffix is the date OpenAI appends to a model id to name a
// snapshot, as in gpt-4o-2024-08-06.
var snapshotSuffix = regexp.MustCompile(`-\d{4}-\d{2}-\d{2}$`)

// modelTraitsFor returns the traits of model, or of the model a dated
// snapshot belongs to. ok is false for a model this catalog does not know.
func modelTraitsFor(model string) (t modelTraits, ok bool) {
	if t, ok = traits[model]; ok {
		return t, true
	}
	t, ok = traits[snapshotSuffix.ReplaceAllString(model, "")]
	return t, ok
}
