# Nexus migration notes

## v1.9.0

Upgrade the root module, `config` and every provider module you use to v1.9.0 together. This release needs Forge v1.13.2 and Grove v1.10.0 (with `pgdriver`, `sqlitedriver` and `mongodriver` v1.10.0) or later. Run your store's migration before the new version takes traffic, and read the Postgres note under [Usage metadata](#usage-metadata) first if your usage table is large.

The one compile break is the audit recorder's signature. Everything else is new fields, options and errors, so your code builds as it is, and some of it behaves differently. That's what this section is about.

### Routing prefers the provider you named

A completion now goes to the provider the request names in `Provider`, else to the alias target's provider, else to a provider whose catalog lists the model, and only then to your routing strategy. Before, the strategy picked among every provider and ignored all three. With OpenAI and Anthropic both registered, a request for a Claude model now reaches Anthropic.

Things that change for you:

- `CompletionRequest.Provider` was ignored before. It's honoured now, over HTTP too (`"provider"` in the body), and a name that isn't registered is refused with `middlewares.ErrUnknownProvider`, 400 `invalid_request`.
- An alias target whose `Provider` is not registered now refuses the request with the same error. Before, the strategy quietly picked some other provider. Check your aliases.
- When a request names a provider and the alias has several targets, only targets for that provider (or with no provider) are picked.
- Embeddings follow the same idea: `EmbeddingRequest.Provider` (new, with a `"provider"` JSON tag), then the embeddings providers that list the model, then the first one that can embed. A named provider that cannot embed is refused with 400 `invalid_request`, and `errors.Is(err, provider.ErrNotSupported)` still matches.
- A price override makes its provider count as listing the model, so routing follows your overrides.
- Cache keys now include the requested provider, the system prompt, each message's name, tool calls and tool call id, the tool choice, the thinking settings and the response format. Before, requests that differed only in those fields shared an entry. Entries cached before the upgrade miss once and expire by their TTL.

If you relied on a strategy to spread one model across providers that all list it, it still does: the strategy picks among the providers that list the model.

### Streamed output is guarded, and buffered by default

Output guards now check streamed replies. When any guard runs on output (`guard.PhaseOutput` or `guard.PhaseBoth`; `guards.NewPII` and `guards.NewContentFilter` both count), the gateway adds a stream guard stage at priority 155 with `guard.StrategyBuffer`. Your clients then get nothing until the whole reply has ended and passed the guards. That is a big change to time to first token. If you stream chat to people, pick passthrough:

```go
nexus.WithStreamGuardStrategy(guard.StrategyPassthrough)
```

With passthrough, a block replaces `io.EOF` with `*guard.BlockedError` at the end of the stream, and a redaction cannot take effect (the gateway logs a warning). Run tool calls only after a stream ends without an error.

- A blocked stream now returns `*guard.BlockedError` from `Next`, with the usage, model and provider of the refused reply. Code that only expected `io.EOF` or a transport error needs to handle it.
- A buffered reply that a guard rewrote is replayed as a rebuilt stream: the rewritten text, the tool calls, the finish reason and the usage. Reasoning, citations, audio and images from the original are dropped.
- The gateway decides whether to add the stage when `Initialize` runs. An output guard you register later through `gw.Guard().Register` does not reach streams unless the stage was already there. Register guards with `WithGuard`.
- `middlewares.NewStreamGuardrail` with an empty strategy now means buffer. It meant passthrough before. If you added this stage yourself with `WithMiddleware`, set the strategy explicitly, and remove it if the gateway now adds its own, or streams get checked twice.
- `guard.CheckInput.Metadata` is now a copy of the request's `Metadata` in both phases, and `CheckResult.Guard` names the last guard that modified the messages, not only one that blocked.
- `guard/shieldadapter` is deprecated.

The "Streaming output is not guarded" item under [Known limits](#known-limits) no longer applies.

### audit_hook takes the event by pointer

`audit_hook.Recorder.Record` and `audit_hook.RecorderFunc` now take `*AuditEvent` instead of `AuditEvent`, matching Chronicle's emitter and Dispatch's audit hook. Change your recorder to:

```go
func(ctx context.Context, ev *audit_hook.AuditEvent) error
```

`AuditEvent` also gains `TenantID`, set by the request, tenant, key and budget hooks. It is empty for provider, guardrail, key-revoked and stream events, which have no tenant to give.

### Anthropic

The catalog was rebuilt from Anthropic's pricing and models pages, with cache prices on every model. It now lists `claude-opus-5-5`, `claude-sonnet-5-5`, `claude-haiku-5-5`, `claude-fable-5-1`, `claude-fable-5`, `claude-opus-5`, `claude-sonnet-5`, `claude-opus-4-8`, `claude-opus-4-7`, `claude-opus-4-6`, `claude-sonnet-4-6`, `claude-haiku-4-5`, `claude-opus-4-5` and `claude-sonnet-4-5`, with their dated ids.

- These ids are gone: `claude-3-5-haiku-20241022`, `claude-3-5-sonnet-20241022`, `claude-sonnet-4-5-20250514` and `claude-opus-4-5-20250630`. A request for one still goes out, but it records an unknown cost and is not routed to Anthropic by its listing. Move to a current id, or set `Provider: "anthropic"` and a price override.
- The default `max_tokens` was 4096 for every model. It is now `min(16000, max output)` for `Complete`, `min(64000, max output)` for streams, and 8192 for an id the catalog does not list. Replies can run longer and cost more. Set `MaxTokens` on the request if you want the old cap.
- `temperature` and `top_p` are dropped, without an error, on models that reject them (every Claude 5 model, plus Opus 4.7 and 4.8), and on every model when you set `WithAPIVersion(anthropic.APIVersion20261021)`.
- Thinking now maps per model. Adaptive models get `{"type":"adaptive"}`, and turning thinking off sends `disabled` or `between_tools` where the model needs one. Budget models get `budget_tokens` raised to at least 1024. `ThinkingConfig.Effort` is new and goes to `output_config.effort`. An effort the model does not accept fails before the call with an error wrapping `provider.ErrNotSupported`.
- `ToolChoice` used to be ignored by this provider. It is sent now, and `"required"` or a named function on `claude-opus-5-5`, `claude-fable-5-1` or `claude-sonnet-5-5` fails with `anthropic.ErrForcedToolChoice` before the call.
- The HTTP client had a 120 second timeout on the whole request, which cut long streams. It now has a 10 minute response-header timeout (`anthropic.WithTimeout`) and nothing on the body, so a stream runs as long as it needs. A non-streamed call is bounded by `nexus.WithTimeout` (30 seconds by default) and your context.
- A `refusal` stop reason now maps to `FinishReason` `content_filter`. It came through as `refusal` before. `pause_turn` passes through.
- `PromptTokens` now includes cache reads and writes. Anthropic's `input_tokens` leaves them out, so prompt counts for cached requests go up, and costs follow the cache prices.
- The `message_delta` chunk of a stream now carries the usage, so `provider.Accumulate` gets the completion tokens it used to miss.
- A stream with content ends with one more chunk, `provider.EventMessageStop`, carrying `Delta.Native`. The HTTP stream sends it to clients as an empty delta event.
- Replies carry `Message.Native`. Store it with the assistant message and send it back unchanged to keep thinking valid across tool turns. See the [Anthropic page](https://www.xraph.com/docs/nexus/providers/anthropic).
- Prompt caching is new and off by default (`anthropic.WithPromptCaching`).

### OpenAI and compatible providers

The OpenAI catalog lists what OpenAI's Chat Completions API served on 2026-10-25, with cached-input and long-context prices.

- `gpt-4-turbo`, `o1` and `o1-mini` were removed: OpenAI shut them down. Requests for them record an unknown cost.
- On `https://api.openai.com/v1` the output cap is sent as `max_completion_tokens`. Other base URLs keep `max_tokens` unless you set `openai.WithMaxCompletionTokens(true)` (or the same option on opencompat).
- Reasoning models (the GPT-6 and GPT-5.6 families, `gpt-5.5`, `gpt-5`, `gpt-5-mini`, `gpt-5-nano`, `o3`) no longer get `temperature` or `top_p`, on any base URL.
- The 120 second whole-request timeout is gone here too, and in every provider that wraps `providers/openai` (Groq, Together, Fireworks and the rest). Requests wait up to 10 minutes for headers (`openai.WithTimeout`, `opencompat.WithTimeout`) and streams are not cut. Non-streamed calls through the gateway are still bounded by `nexus.WithTimeout`.
- `prompt_tokens_details.cached_tokens` and `cache_write_tokens` now fill `CacheReadTokens` and `CacheWriteTokens`.
- A provider with an empty key no longer sends `Authorization: Bearer ` with nothing after it. It sends no `Authorization` header.
- `EmbeddingRequest.Dimensions` is new. With it set, the gateway refuses a provider that cannot return that width with `middlewares.ErrDimensionsUnsupported` (400, wrapping `provider.ErrNotSupported`). On OpenAI's own endpoint only `text-embedding-3-*` can. It fails a response whose vectors have another length (`middlewares.ErrDimensionsMismatch`), and records the usage it was charged.
- opencompat's default capabilities now include embeddings, tools, streamed tools and JSON output. An opencompat provider registered ahead of OpenAI can now be the first provider that embeds, so an embedding request that names no provider, for a model no catalog lists, can land on it. Name the provider, or pass `WithCapabilities` to keep the old set.

### Gateway and Forge extension lifecycle

- `gw.Engine()` returned nil before `Initialize`. It now returns the engine from `nexus.New` on, and its calls return `nexus.ErrNotInitialized` until `Initialize` finishes.
- `gw.Initialized()` is new. `Shutdown` clears it, after which engine calls return `ErrNotInitialized` again. `Shutdown` on a gateway that was never initialized no longer panics.
- A gateway is not restarted. `Initialize` after `Shutdown` returned nil and left every call failing; it now returns an error that wraps `ErrNotInitialized`, so the Forge extension's `Start` after `Stop` fails too. Create a new gateway instead.
- `gw.Health` returned nil whatever the state. It now returns `ErrNotInitialized` before `Initialize` and after `Shutdown`.
- The Forge extension builds the gateway in `Register` and provides that pointer in the container, so `vessel.Inject[*nexus.Gateway]` works from another extension's `Register`. It was nil until `Start` before. `Start` initializes it, `Health` reports not ready until then, and `Stop` skips a gateway that never started.
- `Config.DisableRoutes`, `Config.BasePath`, `WithDisableRoutes` and `WithBasePath` are deprecated no-ops. `Register` logs a warning when `disable_routes` is true or `base_path` is not `/nexus`.
- `gw.PriceBook()` exists from `nexus.New`, so `WithPriceOverrides` applies before `Initialize`.

### Usage metadata

`CompletionRequest.Metadata` and the new `EmbeddingRequest.Metadata` are stored on usage records, and `usage.QueryOptions` and `usage.SeriesOptions` can filter by them. The gateway now validates request metadata: at most 16 pairs, keys matching `^[A-Za-z0-9_-]{1,64}$`, values up to 256 bytes. A request that breaks a rule is refused with 400 `invalid_request` before any provider call. It was never checked before, so look at what you put there.

The `usage_metadata` migration (version `20261024000001`, group `nexus`) adds the storage:

- Postgres: a `metadata` JSONB column and a GIN index, `idx_nexus_usage_metadata`. The migration's `CREATE INDEX` blocks writes to `nexus_usage_records` while it builds, and on a large table usage inserts back up behind it. Create the column and the index yourself before the upgrade, with `CREATE INDEX CONCURRENTLY` outside a transaction, and the migration skips both. The statements are on the [PostgreSQL page](https://www.xraph.com/docs/nexus/stores/postgres).
- SQLite: a `metadata` text column, no index.
- MongoDB: a `metadata` field and a wildcard index on `metadata.$**`.

### Prices and unpriced models

- `provider.Pricing` gains cache read, 5-minute and 1-hour cache write, and long-context prices. `provider.Usage` gains `CacheWriteLongTokens`. `PromptTokens` includes the cache tokens, and the cache counts are parts of it. If you wrote a provider, report usage that way.
- `nexus.WithPriceOverrides` and `gw.PriceBook().SetOverrides` set or replace prices per provider and model. An override whose long-context tier lacks an input price or an output price (a `Free` price or tier excepted) is rejected with `model.ErrUnpricedLongContext`: `SetOverrides` returns it, and with `WithPriceOverrides` so does `Initialize`. A tier in a provider's own price list with no price never makes a request unpriced; the request is charged at the base price. If a rejected `WithPriceOverrides` is followed by a valid `gw.PriceBook().SetOverrides` before `Initialize`, `Initialize` succeeds.
- An unpriced request is still served by default, but it is now counted in `nexus_usage_unpriced_total{provider}` and logged once per provider and model. `nexus.WithUnpricedPolicy(nexus.RefuseUnpriced)` refuses it with 400 `model_unpriced` before the call, so budgets cannot miss spend on unpriced models. A priced call whose token counts never arrive (a stream that fails or is closed before its usage chunk, a mid-stream `StreamGuard` block, a call that fails after the provider started on it) still records an unknown cost that no budget counts; alert on `Unpriced > 0` in `gw.Usage().Series`.

### Tenant credentials

`nexus.WithCredentialProvider` is new: one gateway can serve each tenant on its own Anthropic, OpenAI or opencompat key, and on opencompat its own base URL. Nothing changes unless you set it, apart from these:

- `provider.ErrCredentialNotFound` is exported (it was unexported), and `StaticCredential` and `EnvCredential` return it.
- A provider error wrapping `provider.ErrMissingAPIKey` or `provider.ErrMissingBaseURL` is now permanent, so the retry stage does not retry it.
- A provider error wrapping `provider.ErrNotSupported` (a forced tool choice the model refuses, an effort it does not take, a `ToolChoice` that cannot be read) used to be retried and recorded as a 500 error. It is now a 400 `invalid_request` refusal: not retried, recorded at $0, and `errors.Is` still matches the provider's error.
- `openai`, `azureopenai` and every provider built on them now read `ToolChoice` and send it in OpenAI's shape: Anthropic's `"any"` goes out as `"required"` and `{"type":"tool","name":...}` as a function choice. An object with a `type` they do not model, such as `{"type":"allowed_tools",...}` or `{"type":"custom",...}`, is forwarded unchanged, as in v1.8.x. Only a malformed value is refused (an unknown string, an object with no `type`, a function with no name), with the 400 above. `bedrock`, `gemini`, `cohere` and `vertex` still drop `ToolChoice`.
- The gateway clears any credential on the caller's context before the provider call, so `provider.WithCredential` on a context you pass to `Engine` has no effect. Only the credential the gateway resolved reaches the provider.

Nexus trusts the base URL a resolver returns and sends the request body and the tenant's key there. If your tenants choose that URL, validate it on your side: allow only the schemes you mean to, refuse private, loopback, link-local and cloud metadata addresses, and check the resolved address at dial time against DNS rebinding.

### Known stale: Bedrock

`providers/bedrock` was not updated in this release. Its catalog still lists an old Claude id, `anthropic.claude-3-5-sonnet-20241022-v2:0`, at its old price. Price the Bedrock models you call with `WithPriceOverrides` until it is.

## Before v1.9.0

### Upgrade behavior changes

This upgrade changes the gateway's money types, authentication, accounting and
quota enforcement. Plan a maintenance window and upgrade the root Nexus module
and provider modules together. These are breaking changes within the v1 module
path. Forge is pinned to **v1.12.3**.

HTTP completion and proxy routes require a gateway key by default. If you choose
open access, those routes accept anonymous requests; `/admin` still requires an
admin key. A valid supplied key attributes traffic to its tenant even in open
mode. Set `bootstrap_admin_key`, or `NEXUS_BOOTSTRAP_ADMIN_KEY`, to bootstrap an
operator key. Only its hash is stored, and a revoked key stays revoked. Protect
the bootstrap configuration as a secret. See the HTTP API guide for key setup.

Tenant status, scopes, request/token rate limits, daily requests, monthly budgets
and stream limits now affect requests. Default models, model allow/block lists
and tenant cache overrides are enforced. Tenant routing strategy and guardrail
policy are stored but not enforced. The dashboard labels those fields accordingly.

A monthly budget is a soft limit based on recorded priced spend. Concurrent
requests, unpriced models and failed usage inserts can exceed it. Disabling usage
collection disables monthly budget enforcement; daily request limits remain.
Memory rate limits apply per replica. Use the Redis limiter when replicas must
share rate counters. Limiter failures fail open and are counted; failures reading
quota data from the store fail closed.

### Exact money and storage

Prices, budgets, record costs and aggregate costs now use `money.USD`. JSON money
values are decimal strings. An unpriced record has null cost and an explicit
pricing status; it is counted separately from priced spend. Update clients that
expect JSON numbers. Go literals use `money.MustParse("0.15")`; use `Equal` or
`Cmp` for comparisons, not `==`.

Amounts accept up to 18 fractional places. Per-million token multiplication
rounds at that storage boundary, half away from zero. Other sums and comparisons
stay decimal. Provider token reporting still determines what can be priced; this
upgrade does not add missing provider-specific cache or thinking-token reporting.

Postgres stores numeric amounts, SQLite stores decimal text, MongoDB uses decimal
storage, and memory accumulates decimal values. The migrations also add outcome,
refusal and guard details, nullable attribution fields, key-hash uniqueness and
usage indexes. Back up the database and stop old writers before migrating.
SQLite's legacy timestamp normalization runs in `Store.Migrate`; an external
migration orchestrator must call it too. The composite usage index can block
writes on a large table. On Postgres, you can create the exact index concurrently
before the upgrade, outside a transaction, so the migration skips that work.

### Pipeline and API contracts

The provider call is terminal and runs after the other stages. Usage and stream
lifecycle wrappers now execute. Custom middleware previously sorted after the
provider call can run for the first time, inside retry and once per attempt.
Cache keys include the tenant. Requests carry a correlation ID, and usage records
include their final outcome, guard/refusal detail, latency and exact known cost.

Lists use cursor continuation and do not promise totals. Blank optional tenant
scope is different from omitted scope: malformed or explicit blank IDs fail
closed. Effective key status includes expiry. Rotation revokes the old key
immediately; there is no grace period. Tenant deletion refuses customers with
keys or usage history, so disable a customer to stop traffic and keep its records.

The React dashboard contract is operator-wide. Its 18 intents use explicit safe
response DTOs and command invalidations. Create and rotate return a raw key once.
Forge's secret-response dispatcher keeps an idempotency tombstone, never a raw
secret response for replay. A replay can return a typed `CONFLICT` inside an HTTP
500 response; clients must read the envelope and its reason. An unresolved retry
must retain its idempotency key. Installed dashboard authorization and durable
shared idempotency storage require deployment verification.

### Dashboard migration status

The Go contract, React read routes, tenant forms, status controls, key dialogs and
usage charts are implemented. All 12 routes passed desktop and narrow fixture
browser checks. Writes, exact chart values, protected key reveals and retry paths
passed their browser gate. The disconnected legacy dashboard is retired. Remove imports of
`github.com/xraph/nexus/dashboard`; the Forge extension registers the active
`extension/contract` contributor.

The following inventory was taken from all 25 templ sources while they existed.
A moved field remains available through the named replacement page. This inventory
was committed before the legacy directory was deleted.

| Legacy surface | Columns, actions, filters, badges and empty states | Replacement and status |
|---|---|---|
| Overview | Tenant count, active keys, monthly spend, monthly requests, cache hit rate, provider count, recent request table | Read pages verified. Overview carries posture, counts and spend; Usage carries period requests and cache hit rate; Models lists providers; Request log carries recent records. |
| Tenant list | Name, slug, status, RPM, monthly budget, created; name search; create and row navigation; tenant count caption; no tenants | Read page verified with search, real status filter, cursor paging and visible-page count. Create form and persisted browser write verified. |
| Tenant detail | ID, name, slug, status, created, updated; enable/disable/suspend confirmations; edit; five quotas; six config fields; metadata; keys; monthly requests/tokens/cost/cache/latency | Read page verified for quotas/config/metadata/keys. Usage provides monthly aggregates with tenant selection. Status/edit browser checks passed; updated timestamp is shown. Streaming quotas and config metadata are added. |
| Tenant form | Name, immutable edit slug, RPM, TPM, daily requests, budget, max tokens/request, default model, routing/guard policy, allow/block model lists; create/save/cancel | Implemented with explicit No limit controls and dirty patches, including streaming limits, cache inheritance and both metadata maps. Create/edit browser checks passed. Untouched stored fields stay omitted; a transient refresh failure preserves the draft. |
| Key list | Name, prefix, scopes, status, last used, created; tenant filter, create, row navigation; no keys | Read page verified; created is in detail. Expiry and customer names added. Global and tenant create dialogs passed browser writes with all scopes and expiry. |
| Key detail | ID, tenant ID, prefix, status, created, last used or Never, expires or Never, scope badges, metadata; rotate/revoke confirmation; new raw key; recent usage | Read page verified. Real key-filtered Request log replaces the old tenant-filtered recent usage. Protected reveal, immediate rotation, revocation and failure/retry passed browser checks. Never is explicit. |
| Key form | Tenant, name, checked scopes, create/cancel; one-time generated key and copy affordance | Two-step create/reveal dialog verified. All checked scopes survive; admin carries its cross-tenant warning. Escape, backdrop, unload and SPA Back protect the reveal. Clipboard fallback and storage acknowledgement are verified. |
| Usage | Day/week/month selection; requests, tokens, cost, cache hit rate, average latency; provider/model tables of requests/tokens/cost; no usage | Exact tables verified with tenant/period filters and explicit collection-off state. Separate spend/request charts, exact tooltips/table view and delayed filter retention passed browser checks. |
| Request log | Tenant/provider/model filters; provider, model, tokens, cost, latency, cached, status code, created; no records | Verified with tenant/key/provider/model/outcome/time filters, correlation IDs and cursor load-more. Cached is an outcome badge; failing HTTP status is shown with the outcome. |
| Models | Model ID, provider, name, context window, max output, input/output price; no models | Verified. Adds embedding price, capability fields and priced/free/unpriced distinction. |
| Providers | Name, model count, healthy/unhealthy badge | Verified traffic counts over a stated 15-minute window replace the unsupported health verdict. Zero traffic does not imply healthy. |
| Settings | Base path, timeout, retries, rate limit, log level, usage/cache toggles, provider/extension counts | Effective configuration verified. Providers remain inspectable on Models. The hook-extension count is deliberately dropped: it implied no actionable runtime health and has no equivalent in the safe settings contract. |
| Status badges | Tenant active/disabled/suspended; key active/revoked/expired; cached/live; provider healthy/unhealthy | Tenant/key/outcome badges migrated with revised visual weight. Unsupported provider health is replaced by observed traffic. |
| Empty states | No tenants, keys, records, usage or models | Shared ZeroState distinguishes empty, filtered-empty, loading, failure, denial and unavailable collection. No Nexus walkthrough is registered. |
| Three widgets | Aggregate stats, monthly spend, recent activity | Widget slots deliberately dropped because the React shell has no equivalent slot. Their data remains on Overview, Usage and Request log. |
| Topbar API Docs | Link to a route Nexus did not mount | Deliberately dropped. |

### Known limits

- Routing has no per-request decision explanation. Cost and latency strategies
  select the first healthy provider while candidates have no cost/latency data.
- Built-in guards skip non-text content. A recorded block names its guard, but
  absence of a block does not prove every guard ran. (Streamed output is guarded
  from v1.9.0 on.)
- Cache entry/byte counts are not tracked. Completion and stream cache backends
  are reported separately. Redis cache Clear is an existing no-op.
- Fallback, circuit breakers, batch and MCP are not wired into this gateway.
- Provider-specific token gaps and model/embedding/alias limitations are listed
  in the dashboard migration design. No live provider billing reconciliation was
  performed by this migration.
- Scope lookup errors are sanitized before the outer contract logger sees them,
  so that log loses the original store cause. Responses remain safe and fail closed.
- Fixture timestamp validation accepts some dates JavaScript normalizes but Go
  rejects. This is a fixture parity gap, not a production parser change.

### Verification

The root race suite passed with real Postgres, MongoDB and Redis test containers,
plus memory and SQLite coverage. All 37 nested modules passed standalone vet and
tests before retirement. Contract transport checks covered CSRF, invalidations,
secret replay and concurrent retry behavior. The final retirement pass repeats
the Go checks after dependency cleanup.

The React plugin passed 66 tests, formatting, lint and typecheck after the final
review fixes. All 12 routes passed 1440 by 1000 and 390 by 844 fixture browser
checks. Tenant create/edit/status, key create/rotate/revoke, exact charts and
error/retry paths were checked. Pending, uncertain and revealed key operations
survived repeated Back, with the Navigation API and its indexed-history fallback.
Tenant drafts survived an offline refresh and retry; denial, missing resources
and a cleared host context discard them. All 18 HTTP intents and 12 spot checks
passed, along with eight fixture tests.

These checks do not qualify live provider credentials, installed-host permissions
or distributed durable idempotency configuration. The React BASELINE records
whole-host build measurements, shared-check outcomes and browser evidence.

After retirement, `make f`, `make l`, `make test-race` and `make b` passed across
all workspace modules. The race suite used the same database containers. No active
Go source imports the retired dashboard, templ or forgeui, and Forge resolves to
v1.12.3 with and without `go.work`.
