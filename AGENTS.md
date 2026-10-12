# AGENTS.md

Nexus is an AI gateway library for Go: provider routing, caching, guardrails, tenants, gateway keys, usage and budgets, an OpenAI-compatible proxy and several streaming wire formats, with a Forge extension on top. It's a library. You compose a `nexus.Gateway` from options and mount it on your own router.

This repo holds many Go modules:

| Module | Path | Released by `release.yml` |
|---|---|---|
| `github.com/xraph/nexus` | `.` | yes, tag `vX.Y.Z` |
| `github.com/xraph/nexus/config` | `config/` | yes, tag `config/vX.Y.Z` |
| `github.com/xraph/nexus/providers/<name>` | `providers/<name>/` | 30 of the 32, tag `providers/<name>/vX.Y.Z` |
| `github.com/xraph/nexus/grpcsrv` | `grpcsrv/` | no |
| `providers/geminilive`, `providers/openairealtime` | `providers/` | no |
| `_examples/grpc`, `_examples/live`, `_examples/realtime` | `_examples/` | no (examples) |

The 30 released providers are ai21, anthropic, anyscale, azureopenai, bedrock, cerebras, cohere, deepinfra, deepseek, fireworks, gemini, groq, hyperbolic, jinaai, lepton, lmstudio, mistral, nebius, novita, nvidia, ollama, openai, opencompat, openrouter, perplexity, sambanova, together, vertex, voyageai and xai. `grpcsrv`, `providers/geminilive` and `providers/openairealtime` exist on disk with their own `go.mod`, but they are not in the workflow's module list. None of them has ever been tagged, so nobody outside this repo can `go get` them at a tagged version.

## The workspace

A tracked `go.work` (with its `go.work.sum`) sits at the root. It lists every module in this repo (the root, `config`, `grpcsrv`, all 32 providers and the three example modules) and nothing outside it. With it, you can change the root and see the effect in the providers, `config` and `grpcsrv` straight away, in your editor and in `go` commands, without cutting a release in between. CI checks it out too, so CI builds in workspace mode. When you want to see how one module resolves from its own `go.mod`, run the command with `GOWORK=off` (its in-repo replaces still apply).

Never add a sibling repo (`../grove`, `../../forge`) to it. If you need a local grove or forge while you work, keep that out of anything you commit.

Each nested module also carries `replace` directives pointing back inside the repo (`github.com/xraph/nexus => ../..`, and `providers/openai => ../openai` in the providers built on it). Those are intentional. The `require` next to each replace still has to name a real version, because consumers ignore the replace; the shared CI's Nested modules job fails a `v0.0.0` require and reports one that isn't published yet as a notice. `_examples/` is outside that check.

## Commands

The Makefile walks every directory with a `go.mod` (from `find`) for most targets.

| What | Command |
|---|---|
| Build every module | `make build` |
| Test every module | `make test` (`go test ./...` per module) |
| Race tests, every module | `make test-race` |
| Verbose tests | `make test-verbose` |
| Vet every module | `make vet` |
| Lint every module | `make lint`, or `make lint-fix` |
| Format | `make fmt` (`gofmt -s -w`, then `goimports -local github.com/xraph/nexus`) |
| Format, vet, lint | `make check` |
| Coverage (root module only) | `make coverage`, `make coverage-html` |
| Tidy every module | `./updatemod.sh`, or `make tidy` |
| Docs site | `make docs`, `make docs-build` |

`./updatemod.sh` runs `go mod tidy` in every `go.mod` directory. `make tidy` does the same and then runs `go work sync`, which can push the workspace's resolved versions into the nested `go.mod` files, so read the diff after it. Skip `make build-bin`, `make run`, `make install` and `make dev`. They point at `./cmd/nexus`, which doesn't exist.

The `_examples/forge`, `multi-tenant`, `proxy`, `standalone` and `streaming` programs live in the root module under a `_` directory, which `./...` skips, so no Make target or CI job compiles them. After an API change, build one by path, for example `go build ./_examples/proxy`.

CI (`.github/workflows/ci.yml`) calls the shared `xraph/workflows/.github/workflows/go-ci.yml@v1` on Go 1.26, Ubuntu only. It runs `make test` (so every module), a race-enabled coverage run from the root, golangci-lint, a `gofmt -l` check, `make vet`, a `go mod tidy` diff check on the root `go.mod` and `go.sum`, gosec, govulncheck and the Nested modules check. The repo's own `format` job fails on anything `goimports -l -local github.com/xraph/nexus .` lists, and the `docs` job type-checks, lints and builds the Fumadocs site.

### What the tests need

Provider tests run against mock HTTP servers from `testutil`, so you never need an API key. The examples read `OPENAI_API_KEY` or `GEMINI_API_KEY`, but nothing in CI runs them.

`store/storetest.Each` runs one test body against every backend. Memory and SQLite always run. Postgres and Mongo skip silently unless you set:

- `NEXUS_TEST_POSTGRES_DSN`. Each test gets its own schema, dropped afterwards, and Postgres tests across packages are serialized with an advisory lock.
- `NEXUS_TEST_MONGO_URI`. Each test gets its own database, dropped afterwards.

Both refuse the default port (5432 and 27017, spelled or implied), and `mongodb+srv` URIs are refused too. Point them at a throwaway container on another port:

```bash
NEXUS_TEST_POSTGRES_DSN='postgres://nexus:nexus@localhost:55432/nexus?sslmode=disable' \
NEXUS_TEST_MONGO_URI='mongodb://localhost:57017' go test ./store/...
```

CI sets neither, so CI never exercises the Postgres or Mongo stores. `ratelimit/redislimit` skips without `NEXUS_TEST_REDIS_ADDR`. `httpstream/openai_sdk_test.go` is behind the `sdkcompat` tag and only runs with `go test -tags sdkcompat ./httpstream/...`.

## Layout

| Path | What lives there |
|---|---|
| `nexus.go`, `options.go`, `config.go`, `engine.go` | `Gateway`, `New(opts ...Option)`, `Config`, and `Engine` for non-HTTP use |
| `errors.go`, `logger.go`, `exports.go`, `mount.go` | Sentinel errors, the `Logger` interface over go-utils log, re-exports, router mounting |
| `provider/` | `Provider` interface, request and response types, streams, the registry |
| `providers/<name>/` | One module per provider |
| `providertest/` | Shared provider conformance tests (`TestProviderContract`, `TestProviderComplete` and friends) |
| `pipeline/` | Ordered middleware chain every request goes through |
| `router/`, `fallback/`, `model/` | Provider selection strategies, resilience, model aliases |
| `cache/`, `guard/`, `transform/`, `ratelimit/` | Response cache, guardrails, request and response transforms, RPM and TPM windows |
| `auth/`, `key/`, `tenant/`, `usage/` | Gateway key auth, keys, tenants, usage records |
| `money/`, `paging/` | Exact USD amounts, cursor paging rules |
| `store/` | Composite `store.Store`, the in-memory store, and `postgres`, `sqlite`, `mongo` backends plus `storetest` |
| `api/`, `proxy/`, `httpstream/`, `mcp/`, `batch/` | HTTP handlers, the OpenAI-compatible proxy, streaming wire formats, MCP, batch jobs |
| `plugin/`, `audit_hook/`, `relay_hook/`, `observability/` | Lifecycle hooks and the audit, webhook and tracing bridges |
| `extension/`, `extension/contract/` | Forge extension and the dashboard contract contributor (`manifest.yaml`) |
| `config/` | Separate module: YAML and env loading that wires every released provider |
| `grpcsrv/` | Separate module: gRPC streaming server, code generated from `proto/nexus/v1/streaming.proto` |
| `docs/` | Fumadocs site |

## Conventions

The root module never imports a provider. Providers import the root's `provider` package, the OpenAI-compatible ones wrap `providers/openai` (groq, mistral, together, xai and most others), and `config` imports the root plus all 30 released providers. Keep imports flowing that way or you'll build a module cycle.

A new provider gets its own `go.mod` with a `require` and an in-repo `replace` for the root (and for `providers/openai` if it wraps it), a `use` line in `go.work`, and tests that call `providertest.TestProviderContract`. To make it releasable, you also add it to every provider list in `release.yml`: the dispatch `options` and the four `PROVIDERS` strings.

Root sentinel errors live in `errors.go` with a `nexus:` prefix. Subsystems keep their own, such as `key.ErrNotFound` and `tenant.ErrNotFound`, which every store returns for a missing row (`storetest` checks this). Stores wrap with a backend prefix like `nexus/postgres: ...: %w`. A pipeline stage that turns a request away (auth, quota, budget, guardrail) returns `*pipeline.RefusalError`, re-exported as `nexus.RefusalError`, with an HTTP status, a code and, where a limit applies, `Limit` and `RetryAfter`.

`store.Store` exposes `Tenants()`, `Keys()` and `Usage()` plus `Migrate()` and `Close()`. Audit data isn't stored there: it flows through `audit_hook` to Chronicle. Each grove backend keeps a `migrate.NewGroup("nexus")` group in its `migrations.go`, and a behaviour every backend must share belongs in `store/storetest`.

Options are functional (`type Option func(*Gateway)`). `WithConfig` replaces the whole config, so start from `DefaultConfig()`. A zero `Config` records no usage and has no timeout or retries.

Money is `money.USD`, an exact decimal that never rounds. Compare with `Equal` or `IsZero`, never `==`. Lists page newest first by TypeID (prefixes `tenant`, `key`, `usage`, `req`) and return no totals.

The Forge extension reads YAML under `extensions.nexus` or `nexus`, takes a grove DB by name or auto-discovers a `*grove.DB` from the vessel container, and provides `*nexus.Gateway`. `NEXUS_BOOTSTRAP_ADMIN_KEY` supplies the bootstrap admin key when config sets none; only its hash is stored.

Lint rules that bite (one `.golangci.yml` at the root covers every module):

- `govet` runs every analyzer except `fieldalignment`, shadow included. A shadowed `err` fails.
- `errcheck` runs with `check-blank` and `check-type-assertions`; ignored errors need `//nolint:errcheck // reason`, and `nolintlint` is on.
- `gosec` excludes G104, G117, G118, G304, G402, G704 and G705, with the reasons in the config. Everything else counts outside tests and `_examples/`.
- `errorlint`, `noctx`, `bodyclose`, `rowserrcheck` and `sqlclosecheck` are on.

Commit subjects follow conventional commits with a scope, as in `feat(nexus): ...`, `fix(sqlite): ...` and `chore(deps): ...`.

## Dependencies

The root depends on these xraph modules (versions in `go.mod`): `grove` with `drivers/mongodriver`, `drivers/pgdriver` and `drivers/sqlitedriver`, `forge`, `go-utils` and `vessel`. `config` and the providers depend on the root, and the OpenAI-compatible providers also on `providers/openai`. Downstream, `cortex` and `cortex/extension` require the root.

To bump a dependency, run `go get` in the module that needs it, then tidy everything:

```bash
go get github.com/xraph/grove@vX.Y.Z
go get github.com/xraph/grove/drivers/pgdriver@vX.Y.Z
./updatemod.sh
```

Grove tags its drivers in lockstep, so move grove and all three drivers to the same version. The root `go.mod` and `go.sum` must be tidy or CI's Verify job fails. Never commit a `replace` pointing at a sibling repo; the in-repo replaces stay as they are.

## Releasing

You release by dispatching `release.yml`. Never push a tag by hand. Wait for green CI on `main` first, and for any upstream xraph release the change depends on.

Before a release of everything, set each nested module's `require` of `github.com/xraph/nexus` (and of sibling providers) to the version you're about to cut. The replaces keep local builds on the working tree, and the Nested modules check tolerates a version that isn't published yet. Without that, a provider fetched at the new tag still asks for the old root.

```bash
gh workflow run release.yml --ref main -f module=all -f version=X.Y.Z
```

That tags the root `vX.Y.Z`, `config/vX.Y.Z` and `providers/<name>/vX.Y.Z` for each of the 30 listed providers. For one module, pass `-f module=nexus` (root only), `-f module=config` or `-f module=<provider>`, for example `-f module=openai`. The version has no leading `v`. `-f dry_run=true` builds without tagging or publishing, `-f prerelease=true` marks a pre-release (a `-suffix` version is marked automatically), and `-f skip_tests=true` exists for hotfixes on a commit that already passed.

The `detect` job creates and pushes the tags before `test` runs, and `test` runs `go test -race` on Ubuntu and macOS (every `go.mod` directory in `all` mode, otherwise just the one module). A failing test leaves the tags pushed and no GitHub release. Fix forward with the next patch. `release-module` creates the GitHub releases and asks the Go proxy to fetch each tag.

`scripts/release.sh` tags on your machine and leaves you to push the tags. Don't use it.

After a root release, `cortex` and `cortex/extension` re-pin with `go get github.com/xraph/nexus@vX.Y.Z`.

## Branches

`main` has no branch protection or rulesets, so a push lands directly. CI runs on pushes to `main` and on pull requests against it.
