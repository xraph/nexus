# Nexus dashboard contract

Register this contributor through the Nexus extension. You get 18 version-1
intents after the gateway starts successfully. During startup, or after a
failed start, requests return retryable `UNAVAILABLE`.

The contract requires Forge v1.12.3. `nexus` is the contributor name.

## Queries and commands

Queries are `overview.get`, `tenants.list`, `tenants.get`, `keys.list`,
`keys.get`, `usage.summary`, `usage.series`, `usage.records`, `models.list`,
`providers.list`, `gateway.get` and `settings.get`.

Commands are `tenants.create`, `tenants.update`, `tenants.setStatus`,
`keys.create`, `keys.rotate` and `keys.revoke`. Each declares its query
invalidations in `manifest.yaml`. There is no tenant-delete or cache-clear
command.

You can pass query filters in the envelope's `params`. Commands use `payload`.
The dashboard host supplies authentication, authorization, CSRF validation and
an idempotency store. Gateway API keys authenticate Nexus HTTP api and proxy
routes; they do not grant dashboard access.

## Scope and values

An omitted `tenantId` requests the operator-wide view, including unattributed
usage. Explicit null, blank, malformed or wrong-type tenant values return
`BAD_REQUEST`. A valid but unknown ID returns `NOT_FOUND`. Principal claims
never substitute a tenant filter. If you supply both a tenant and key filter,
the key must belong to that tenant.

Money is an exact decimal string. Unknown record cost is `null`; known zero
cost is `"0"`. Summary costs add priced records only, with an unpriced count
beside the total. When usage collection is off, measurements are null and
usage lists are empty with `usageEnabled: false`. Daily request counts exclude
refusals. Collection being off prevents monthly budget enforcement but leaves
daily, RPM, TPM and token limits active.

Durations and latency use fields ending in `Ms`. Timestamps are UTC RFC3339.
Lists are arrays, including empty lists. Paged lists return `nextCursor`, empty
on the last page. Periods are `day`, `week` or `month`; series buckets are `hour`
or `day`. Week means the preceding seven days; day and month start in UTC.
Request-log `from` is inclusive and `to` is exclusive.

Tenant patches preserve omitted quota and config fields. In a config patch,
`cacheEnabled: null` resets cache inheritance; omission preserves it. Routing
strategy and guardrail policy are stored tenant settings whose enforcement
flags remain false. Commands return their committed entity without a second
usage read. `tenants.setStatus` leaves `updatedAt` null until the invalidated
query reads the service's timestamp.

## One-time keys

Create and rotate return a raw key once. Copy it before dismissing the result.
Read responses contain the display prefix and metadata, never the hash or raw
key. Omitting scopes uses the key service's defaults; an explicit empty or null
scope list is refused. The `admin` scope grants cross-tenant API administration.

Both commands register with `dispatcher.SecretResponse()`. Forge retains a
24-hour tombstone with no response body, so replay returns `CONFLICT` with
reason `idempotency.already_ran` and cannot recover the key. Concurrent retries
with the same idempotency key mint only once when the host uses Forge's claim
adapter. A host that omits the idempotency store cannot deduplicate commands.

Forge v1.12.3 sends dispatcher errors, including this conflict, with HTTP 500.
Read the contract error code and reason. The shared React client already does
this; HTTP-status-only monitoring can count a harmless replay as a server error.

## Inspection limits

Provider traffic covers the last 15 minutes and reads every usage page. No
handler calls a provider health probe. Model catalogs are read from the
registered providers; a provider catalog error fails the query rather than
silently hiding that provider's models.

Cache kinds are optional inspection capabilities. An unidentified custom
backend is `unknown`. Completion and stream kinds remain separate, cache hit
statistics cover completions, and untracked size and bytes are null. A custom
pipeline without inspection support reports `stagesAvailable: false`.

The gateway response includes the default pipeline's guard coverage and
soft-budget caveats. These describe configured behavior; they are not a safety
verdict or proof of production capacity.
