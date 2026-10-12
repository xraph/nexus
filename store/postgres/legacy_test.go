package postgres_test

import (
	"context"
	"testing"

	"github.com/xraph/grove/drivers/pgdriver"

	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/money"
	pgstore "github.com/xraph/nexus/store/postgres"
	"github.com/xraph/nexus/store/storetest"
	"github.com/xraph/nexus/usage"
)

func TestLegacyUsageRowsMigrateToUnpricedOrCached(t *testing.T) {
	ctx := context.Background()
	db := storetest.OpenPostgresDB(t)
	raw := pgdriver.Unwrap(db)
	tenantID := id.NewTenantID().String()
	cachedID, uncachedID := id.NewUsageID().String(), id.NewUsageID().String()
	for _, stmt := range []string{
		`CREATE TABLE nexus_tenants (id TEXT PRIMARY KEY, name TEXT NOT NULL, slug TEXT UNIQUE NOT NULL,
    status TEXT NOT NULL DEFAULT 'active', quota JSONB NOT NULL DEFAULT '{}', config JSONB NOT NULL DEFAULT '{}',
    metadata JSONB NOT NULL DEFAULT '{}', created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`CREATE TABLE nexus_usage_records (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL REFERENCES nexus_tenants(id),
    key_id TEXT NOT NULL, request_id TEXT NOT NULL, provider TEXT NOT NULL, model TEXT NOT NULL,
    prompt_tokens INTEGER NOT NULL DEFAULT 0, completion_tokens INTEGER NOT NULL DEFAULT 0,
    total_tokens INTEGER NOT NULL DEFAULT 0, cost_usd DOUBLE PRECISION NOT NULL DEFAULT 0,
    latency_ns BIGINT NOT NULL DEFAULT 0, cached BOOLEAN NOT NULL DEFAULT FALSE,
    status_code INTEGER NOT NULL DEFAULT 200, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`INSERT INTO nexus_tenants (id, name, slug) VALUES ('` + tenantID + `', 'Legacy', 'legacy')`,
		`INSERT INTO nexus_usage_records (id, tenant_id, key_id, request_id, provider, model, total_tokens, cost_usd, cached)
         VALUES ('` + cachedID + `', '` + tenantID + `', '', '', 'openai', 'gpt-4o', 10, 0, TRUE),
                ('` + uncachedID + `', '` + tenantID + `', '', '', 'openai', 'gpt-4o', 10, 0, FALSE)`,
	} {
		if _, err := raw.Exec(ctx, stmt); err != nil {
			t.Fatalf("legacy schema: %v", err)
		}
	}

	s := pgstore.New(db)
	if err := s.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// A cache hit called no provider, so it cost exactly $0.
	got := storetest.FindRecord(t, s, id.MustParseUsageID(cachedID))
	if got.CostUSD == nil || !got.CostUSD.Equal(money.Zero) || got.PricingStatus != usage.PricingCached || got.Outcome != usage.OutcomeCached {
		t.Fatalf("legacy cache hit = cost %v, status %s, outcome %s; want $0, cached, cached", got.CostUSD, got.PricingStatus, got.Outcome)
	}
	// Anything else that stored 0 never had a price.
	got = storetest.FindRecord(t, s, id.MustParseUsageID(uncachedID))
	if got.CostUSD != nil || got.PricingStatus != usage.PricingUnpricedModel || got.Outcome != usage.OutcomeOK {
		t.Fatalf("legacy row = cost %v, status %s, outcome %s; want unknown, unpriced_model, ok", got.CostUSD, got.PricingStatus, got.Outcome)
	}
	if !got.KeyID.IsNil() || !got.RequestID.IsNil() {
		t.Fatalf("legacy empty attribution should read as nil, got %+v", got)
	}
}

// A binary from before exact money may still be running when the new one
// migrates. It omits pricing_status and writes cost_usd = 0. That row must
// read as unknown, not as a priced $0.
func TestRowFromAnOldBinaryReadsAsUnpriced(t *testing.T) {
	ctx := context.Background()
	db := storetest.OpenPostgresDB(t)
	s := pgstore.New(db)
	if err := s.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	tn := storetest.InsertTenant(t, s)
	usageID := id.NewUsageID().String()
	_, err := pgdriver.Unwrap(db).Exec(ctx,
		`INSERT INTO nexus_usage_records (id, tenant_id, key_id, request_id, provider, model, total_tokens, cost_usd, cached)
         VALUES ($1, $2, '', '', 'openai', 'gpt-4o', 10, 0, FALSE)`, usageID, tn.ID.String())
	if err != nil {
		t.Fatalf("insert as the old binary: %v", err)
	}
	got := storetest.FindRecord(t, s, id.MustParseUsageID(usageID))
	if got.CostUSD != nil || got.PricingStatus != usage.PricingUnpricedModel {
		t.Fatalf("old binary row = cost %v, status %s; want unknown and unpriced_model", got.CostUSD, got.PricingStatus)
	}
}
