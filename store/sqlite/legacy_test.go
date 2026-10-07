package sqlite_test

import (
	"context"
	"testing"

	"github.com/xraph/grove/drivers/sqlitedriver"

	"github.com/xraph/nexus/id"
	sqlitestore "github.com/xraph/nexus/store/sqlite"
	"github.com/xraph/nexus/store/storetest"
	"github.com/xraph/nexus/usage"
)

// A row written before exact money stored cost 0 because nothing priced
// it. After migrating it must read as unknown, not as free.
func TestLegacyUsageRowMigratesToUnpriced(t *testing.T) {
	ctx := context.Background()
	db := storetest.OpenSQLiteDB(t)
	raw := sqlitedriver.Unwrap(db)
	legacyID := id.NewUsageID().String()
	for _, stmt := range []string{
		`CREATE TABLE usage_records (
    id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL DEFAULT '', key_id TEXT NOT NULL DEFAULT '',
    request_id TEXT NOT NULL DEFAULT '', provider TEXT NOT NULL DEFAULT '', model TEXT NOT NULL DEFAULT '',
    prompt_tokens INTEGER NOT NULL DEFAULT 0, completion_tokens INTEGER NOT NULL DEFAULT 0,
    total_tokens INTEGER NOT NULL DEFAULT 0, cost_usd REAL NOT NULL DEFAULT 0,
    latency_ns INTEGER NOT NULL DEFAULT 0, cached INTEGER NOT NULL DEFAULT 0,
    status_code INTEGER NOT NULL DEFAULT 200, created_at TEXT NOT NULL DEFAULT (datetime('now')))`,
		`INSERT INTO usage_records (id, provider, model, total_tokens, cost_usd, created_at)
         VALUES ('` + legacyID + `', 'openai', 'gpt-4o', 10, 0, '2026-10-01T10:00:00Z')`,
	} {
		if _, err := raw.Exec(ctx, stmt); err != nil {
			t.Fatalf("legacy schema: %v", err)
		}
	}

	s := sqlitestore.New(db)
	if err := s.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	got := storetest.FindRecord(t, s, id.MustParseUsageID(legacyID))
	if got.CostUSD != nil || got.PricingStatus != usage.PricingUnpricedModel {
		t.Fatalf("legacy row = cost %v, status %s; want unknown and unpriced_model", got.CostUSD, got.PricingStatus)
	}
	if !got.TenantID.IsNil() || got.Outcome != usage.OutcomeOK || got.CreatedAt.Format("2006-01-02T15:04:05Z") != "2026-10-01T10:00:00Z" {
		t.Fatalf("legacy row = %+v", got)
	}
}
