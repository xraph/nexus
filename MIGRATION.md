# Nexus migration notes

## Upgrade behavior changes

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

## Exact money and storage

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

## Pipeline and API contracts

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

## Dashboard migration status

The Go contract and React read routes are implemented. The React tenant forms and
status controls are implemented and unit tested; key dialogs and charts are in
progress. Browser verification of writes and final templ retirement remain open.
The legacy contributor is disconnected and build-ignored for Forge v1.12.3, but
the templ sources remain until the replacement passes its browser gate.

The following inventory was taken from all 25 templ sources while they existed.
A moved field remains available through the named replacement page. Pending items
must be verified before deleting the legacy directory.

| Legacy surface | Columns, actions, filters, badges and empty states | Replacement and status |
|---|---|---|
| Overview | Tenant count, active keys, monthly spend, monthly requests, cache hit rate, provider count, recent request table | Read pages verified. Overview carries posture, counts and spend; Usage carries period requests and cache hit rate; Models lists providers; Request log carries recent records. |
| Tenant list | Name, slug, status, RPM, monthly budget, created; name search; create and row navigation; tenant count caption; no tenants | Read page verified with search, real status filter, cursor paging and visible-page count. Create form implemented; browser write check pending. |
| Tenant detail | ID, name, slug, status, created, updated; enable/disable/suspend confirmations; edit; five quotas; six config fields; metadata; keys; monthly requests/tokens/cost/cache/latency | Read page verified for quotas/config/metadata/keys. Usage provides monthly aggregates with tenant selection. Status/edit implemented; browser checks and updated timestamp parity pending. Streaming quotas and config metadata are added. |
| Tenant form | Name, immutable edit slug, RPM, TPM, daily requests, budget, max tokens/request, default model, routing/guard policy, allow/block model lists; create/save/cancel | Implemented with explicit No limit controls and dirty patches, including streaming limits, cache inheritance and both metadata maps. Browser checks pending. |
| Key list | Name, prefix, scopes, status, last used, created; tenant filter, create, row navigation; no keys | Read page verified; created is in detail. Expiry and customer names added. Create dialog and browser write checks pending. |
| Key detail | ID, tenant ID, prefix, status, created, last used or Never, expires or Never, scope badges, metadata; rotate/revoke confirmation; new raw key; recent usage | Read page verified. Real key-filtered Request log replaces the old tenant-filtered recent usage. Protected reveal, rotation and revocation browser checks pending. |
| Key form | Tenant, name, checked scopes, create/cancel; one-time generated key and copy affordance | Two-step create/reveal dialog in progress. Every checked scope must survive, and admin must carry its cross-tenant warning. |
| Usage | Day/week/month selection; requests, tokens, cost, cache hit rate, average latency; provider/model tables of requests/tokens/cost; no usage | Exact tables verified with tenant/period filters and explicit collection-off state. Separate spend/request charts pending. |
| Request log | Tenant/provider/model filters; provider, model, tokens, cost, latency, cached, status code, created; no records | Verified with tenant/key/provider/model/outcome/time filters, correlation IDs and cursor load-more. Cached is an outcome badge; failing HTTP status is shown with the outcome. |
| Models | Model ID, provider, name, context window, max output, input/output price; no models | Verified. Adds embedding price, capability fields and priced/free/unpriced distinction. |
| Providers | Name, model count, healthy/unhealthy badge | Verified traffic counts over a stated 15-minute window replace the unsupported health verdict. Zero traffic does not imply healthy. |
| Settings | Base path, timeout, retries, rate limit, log level, usage/cache toggles, provider/extension counts | Effective configuration verified. Providers remain inspectable on Models. The hook-extension count is deliberately dropped: it implied no actionable runtime health and has no equivalent in the safe settings contract. |
| Status badges | Tenant active/disabled/suspended; key active/revoked/expired; cached/live; provider healthy/unhealthy | Tenant/key/outcome badges migrated with revised visual weight. Unsupported provider health is replaced by observed traffic. |
| Empty states | No tenants, keys, records, usage or models | Shared ZeroState distinguishes empty, filtered-empty, loading, failure, denial and unavailable collection. No Nexus walkthrough is registered. |
| Three widgets | Aggregate stats, monthly spend, recent activity | Widget slots deliberately dropped because the React shell has no equivalent slot. Their data remains on Overview, Usage and Request log. |
| Topbar API Docs | Link to a route Nexus did not mount | Deliberately dropped. |

## Known limits

- Routing has no per-request decision explanation. Cost and latency strategies
  select the first healthy provider while candidates have no cost/latency data.
- Streaming output is not guarded; non-text guard content is skipped. A recorded
  block names its guard, but absence of a block does not prove every guard ran.
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

## Verification

Before the repository-wide formatting pass, the root race suite passed with real
Postgres, MongoDB and Redis test containers, plus memory and SQLite coverage.
All 37 nested modules passed standalone vet and tests. Contract transport checks
covered CSRF, invalidations, secret replay and concurrent retry behavior. The
React read plugin passed 28 tests and desktop/narrow fixture checks on all ten
routes; tenant writes added five UI tests and six model tests, bringing the suite
to 39. These checks do not qualify live provider credentials, installed-host
permissions or distributed durable idempotency configuration.

After `make f`, `make l`, `make test-race` and `make b` passed across the Go
workspace. The race run used the same Postgres, MongoDB and Redis containers.
The linter used a task-specific temporary directory to avoid the shared linter
lock held by another repository. The migration is not yet complete.
