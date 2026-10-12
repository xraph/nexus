// Example: multi-tenant SaaS with API keys and usage tracking.
//
// This demonstrates Nexus as a multi-tenant AI gateway with:
//
//   - Tenant management (create, quota)
//
//   - API key issuance and validation
//
//   - Usage tracking per tenant
//
//   - Per-tenant model aliases
//
//     go run ./_examples/multi-tenant/main.go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	nexus "github.com/xraph/nexus"
	"github.com/xraph/nexus/key"
	"github.com/xraph/nexus/model"
	"github.com/xraph/nexus/money"
	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/providers/openai"
	"github.com/xraph/nexus/tenant"
)

func main() {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY environment variable is required")
	}

	ctx := context.Background()

	// Build gateway with multi-tenant support.
	gw := nexus.New(
		nexus.WithProvider(openai.New(apiKey)),

		// Per-tenant alias overrides
		nexus.WithAlias("default", model.AliasTarget{
			Provider: "openai",
			Model:    "gpt-4o-mini",
		}),
	)

	if err := gw.Initialize(ctx); err != nil {
		log.Fatalf("failed to initialize: %v", err)
	}
	defer gw.Shutdown(ctx)

	// Create a tenant through the gateway's own service, so the gateway sees it
	// (the access stage refuses a request for a tenant it cannot find).
	tenantSvc := gw.Tenants()
	t, err := tenantSvc.Create(ctx, &tenant.CreateInput{
		Name: "Acme Corp",
		Slug: "acme",
		Quota: &tenant.Quota{
			RPM:              60,
			TPM:              100000,
			DailyRequests:    1000,
			MonthlyBudgetUSD: money.MustParse("100"),
		},
	})
	if err != nil {
		log.Fatalf("failed to create tenant: %v", err)
	}
	fmt.Printf("Created tenant: %s (%s)\n", t.Name, t.ID)

	// Issue a gateway key for the tenant. A key carries scopes: this one may
	// call completions and embeddings, but not list models or use the admin API.
	// Over HTTP the key goes in "Authorization: Bearer nxs_..." or "x-api-key",
	// and the api and proxy packages refuse a request without one.
	keySvc := gw.Keys()
	apiKeyRecord, rawKey, err := keySvc.Create(ctx, &key.CreateInput{
		TenantID: t.ID.String(),
		Name:     "production-key",
		Scopes:   []string{"completions", "embeddings"},
	})
	if err != nil {
		log.Fatalf("failed to create API key: %v", err)
	}
	// Examples may print the key. It is shown once; never log a key in a real
	// service.
	fmt.Printf("API Key: %s (save this!)\n", rawKey)
	fmt.Printf("Key ID: %s, Prefix: %s\n", apiKeyRecord.ID, apiKeyRecord.Prefix)

	// Validate the key.
	validated, err := keySvc.Validate(ctx, rawKey)
	if err != nil {
		log.Fatalf("key validation failed: %v", err)
	}
	fmt.Printf("Validated key for tenant: %s\n", validated.TenantID)

	// Send a request scoped to the tenant. Naming the key makes the access
	// stage check its status and scopes; the tenant's quota applies too.
	resp, err := gw.Engine().Complete(ctx, &provider.CompletionRequest{
		Model:    "default",
		TenantID: t.ID.String(),
		KeyID:    apiKeyRecord.ID.String(),
		Messages: []provider.Message{
			{Role: "user", Content: "Summarize quantum computing in 2 sentences."},
		},
		MaxTokens: 200,
	})
	if err != nil {
		log.Fatalf("completion failed: %v", err)
	}

	data, _ := json.MarshalIndent(resp, "", "  ")
	fmt.Println(string(data))
}
