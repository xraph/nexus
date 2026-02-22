# Nexus

Composable AI gateway library for Go. Route, cache, guard, and observe LLM traffic at scale.

[![Go Reference](https://pkg.go.dev/badge/github.com/xraph/nexus.svg)](https://pkg.go.dev/github.com/xraph/nexus)
[![Go Version](https://img.shields.io/github/go-mod/go-version/xraph/nexus)](https://go.dev)

---

Nexus is a **library**, not a SaaS. Import it, compose your AI gateway, and own your infrastructure. You bring your own providers, database, and HTTP server — Nexus provides the gateway orchestration plumbing.

## Features

**Multi-Provider Routing** — Route to OpenAI, Anthropic, or any OpenAI-compatible API. Priority, cost-optimized, round-robin, and latency-based strategies.

**Content Guardrails** — PII detection, prompt injection blocking, and content filtering with block/redact/warn actions.

**Response Caching** — Deterministic cache keys with optional semantic matching. Memory and Redis backends.

**Multi-Tenant Isolation** — Tenant-scoped requests, API keys, rate limits, usage tracking, budget enforcement, and per-tenant model aliases.

**Model Aliases** — Map virtual model names to provider targets with per-tenant overrides and weighted routing.

**Input/Output Transforms** — System prompt injection, RAG context augmentation, data anonymization, and output normalization.

**Plugin System** — 15 lifecycle hooks for metrics, audit trails, webhooks, and custom processing.

**OpenAI-Compatible Proxy** — Drop-in replacement for the OpenAI API. Point any SDK at your gateway.

**Three Storage Backends** — PostgreSQL, SQLite, and in-memory.

**Forge Integration** — Drop-in `forge.Extension` with auto-discovery and DI-registered Gateway.

## Quick Start

```bash
go get github.com/xraph/nexus
```

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    "github.com/xraph/nexus"
    "github.com/xraph/nexus/cache/stores"
    "github.com/xraph/nexus/guard/guards"
    "github.com/xraph/nexus/model"
    "github.com/xraph/nexus/provider"
    "github.com/xraph/nexus/providers/openai"
    "github.com/xraph/nexus/router/strategies"
)

func main() {
    gw := nexus.New(
        nexus.WithProvider(openai.New(os.Getenv("OPENAI_API_KEY"))),
        nexus.WithRouter(strategies.NewCostOptimized()),
        nexus.WithCache(stores.NewMemory()),
        nexus.WithGuard(guards.NewPII("redact")),
        nexus.WithAlias("fast", model.AliasTarget{
            Provider: "openai",
            Model:    "gpt-4o-mini",
        }),
    )

    ctx := context.Background()
    if err := gw.Initialize(ctx); err != nil {
        log.Fatal(err)
    }
    defer gw.Shutdown(ctx)

    resp, err := gw.Engine().Complete(ctx, &provider.CompletionRequest{
        Model:    "fast",
        Messages: []provider.Message{{Role: "user", Content: "Hello!"}},
    })
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(resp.Choices[0].Message.Content)
}
```

## Examples

| Example | Description |
|---------|-------------|
| [`_examples/standalone`](./_examples/standalone) | Standalone gateway — no framework, just Go |
| [`_examples/proxy`](./_examples/proxy) | OpenAI-compatible proxy server |
| [`_examples/multi-tenant`](./_examples/multi-tenant) | Multi-tenant with API keys and usage tracking |
| [`_examples/forge`](./_examples/forge) | Integration with the Forge framework |

## Architecture

Every request flows through a priority-sorted middleware pipeline:

```
Request
  │
  ├─ Tracing         (priority 10)
  ├─ Timeout          (priority 20)
  ├─ Guardrails       (priority 150)
  ├─ Transforms       (priority 200)
  ├─ Alias Resolution (priority 250)
  ├─ Cache            (priority 280)
  ├─ Retry            (priority 340)
  ├─ Provider Call    (priority 350)   ← core routing + LLM call
  ├─ Headers          (priority 500)
  └─ Usage Tracking   (priority 550)
  │
Response
```

Middleware is added or removed via functional options. The pipeline is assembled at `Initialize()` time.

### Design Principles

- **Library, not service.** You control `main`, the database, and the process lifecycle.
- **Interfaces over implementations.** Every subsystem defines a Go interface. Swap any component with a single type change.
- **Tenant-scoped by design.** Context-injected tenant isolation enforced at every layer.
- **Pipeline-driven.** Priority-sorted middleware chain — add, remove, or reorder without touching engine code.

## Plugin System

Extensions implement one or more hook interfaces and are registered at gateway creation:

```go
nexus.New(
    nexus.WithExtension(myAuditPlugin),
)
```

| Category | Hooks |
|----------|-------|
| **Request** | `OnRequestReceived`, `OnRequestCompleted`, `OnRequestFailed`, `OnRequestCached` |
| **Provider** | `OnProviderFailed`, `OnCircuitOpened`, `OnFallbackTriggered` |
| **Guardrail** | `OnGuardrailBlocked`, `OnGuardrailRedacted` |
| **Tenant** | `OnTenantCreated`, `OnTenantDisabled` |
| **Key** | `OnKeyCreated`, `OnKeyRevoked` |
| **Budget** | `OnBudgetWarning`, `OnBudgetExceeded` |

The registry type-caches hooks at registration time — emit calls iterate only over extensions that implement the relevant interface.

## Documentation

Full documentation is available at [nexus.xraph.com](https://nexus.xraph.com).

## Contributing

Contributions are welcome. Please open an issue to discuss larger changes before submitting a PR.

```bash
# Run tests
go test ./...

# Lint
golangci-lint run ./...

# Build docs
cd docs && pnpm install && pnpm dev
```

## License

See [LICENSE](./LICENSE) for details.
