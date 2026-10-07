package sqlite_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/xraph/grove"
	"github.com/xraph/grove/drivers/sqlitedriver"

	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/money"
	sqlitestore "github.com/xraph/nexus/store/sqlite"
	"github.com/xraph/nexus/store/storetest"
	"github.com/xraph/nexus/usage"
)

const legacyUsageSchema = `CREATE TABLE usage_records (
    id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL DEFAULT '', key_id TEXT NOT NULL DEFAULT '',
    request_id TEXT NOT NULL DEFAULT '', provider TEXT NOT NULL DEFAULT '', model TEXT NOT NULL DEFAULT '',
    prompt_tokens INTEGER NOT NULL DEFAULT 0, completion_tokens INTEGER NOT NULL DEFAULT 0,
    total_tokens INTEGER NOT NULL DEFAULT 0, cost_usd REAL NOT NULL DEFAULT 0,
    latency_ns INTEGER NOT NULL DEFAULT 0, cached INTEGER NOT NULL DEFAULT 0,
    status_code INTEGER NOT NULL DEFAULT 200, created_at TEXT NOT NULL DEFAULT (datetime('now')))`

func insertLegacyRow(t *testing.T, db *grove.DB, rowID, created string, cost float64) {
	t.Helper()
	_, err := sqlitedriver.Unwrap(db).Exec(context.Background(),
		`INSERT INTO usage_records (id, provider, model, total_tokens, cost_usd, created_at) VALUES (?, 'openai', 'gpt-4o', 10, ?, ?)`,
		rowID, cost, created)
	if err != nil {
		t.Fatalf("insert legacy row: %v", err)
	}
}

// A row written before exact money stored cost 0 because nothing priced
// it. After migrating it must read as unknown, not as free. Its time was
// written by the old store in whatever form modernc chose, and must come
// back as the same instant.
func TestLegacyUsageRowMigratesToUnpriced(t *testing.T) {
	ctx := context.Background()
	db := storetest.OpenSQLiteDB(t)
	if _, err := sqlitedriver.Unwrap(db).Exec(ctx, legacyUsageSchema); err != nil {
		t.Fatalf("legacy schema: %v", err)
	}
	cdt := time.FixedZone("CDT", -5*3600)
	rows := []struct {
		created string
		want    time.Time
	}{
		{"2026-10-07 10:47:16.805728 -0500 CDT m=+0.028456085", time.Date(2026, 10, 7, 10, 47, 16, 805728000, cdt)},
		{"2026-10-07 15:47:16.805728 +0000 UTC", time.Date(2026, 10, 7, 15, 47, 16, 805728000, time.UTC)},
		{"2026-10-01 10:00:00", time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)},
		{"2026-10-01T10:00:00Z", time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)},
		{"0001-01-01 00:00:00 +0000 UTC", time.Time{}},
	}
	ids := make([]string, len(rows))
	for i, r := range rows {
		ids[i] = id.NewUsageID().String()
		insertLegacyRow(t, db, ids[i], r.created, 0)
	}

	s := sqlitestore.New(db)
	for pass := 1; pass <= 2; pass++ { // the second Migrate must change nothing
		if err := s.Migrate(); err != nil {
			t.Fatalf("migrate #%d: %v", pass, err)
		}
		for i, r := range rows {
			got := storetest.FindRecord(t, s, id.MustParseUsageID(ids[i]))
			if got.CostUSD != nil || got.PricingStatus != usage.PricingUnpricedModel {
				t.Fatalf("legacy row %q = cost %v, status %s; want unknown and unpriced_model", r.created, got.CostUSD, got.PricingStatus)
			}
			if !got.TenantID.IsNil() || got.Outcome != usage.OutcomeOK {
				t.Fatalf("legacy row %q = %+v", r.created, got)
			}
			if !got.CreatedAt.Equal(r.want) {
				t.Fatalf("legacy row %q created_at = %s, want the instant %s", r.created, got.CreatedAt, r.want)
			}
		}
	}
}

// A time nothing can read must stop the migration and say which row it was,
// not leave a row that breaks every later listing.
func TestLegacyUsageRowWithJunkTimeFailsMigrate(t *testing.T) {
	db := storetest.OpenSQLiteDB(t)
	if _, err := sqlitedriver.Unwrap(db).Exec(context.Background(), legacyUsageSchema); err != nil {
		t.Fatalf("legacy schema: %v", err)
	}
	rowID := id.NewUsageID().String()
	insertLegacyRow(t, db, rowID, "last tuesday", 0.5)

	err := sqlitestore.New(db).Migrate()
	if err == nil || !strings.Contains(err.Error(), rowID) || !strings.Contains(err.Error(), "last tuesday") {
		t.Fatalf("migrate error = %v, want one naming %s and the value", err, rowID)
	}
}

// A cache hit called no provider, so a legacy cached row is exactly $0, not
// unknown.
func TestLegacyCacheHitMigratesToCachedZero(t *testing.T) {
	db := storetest.OpenSQLiteDB(t)
	if _, err := sqlitedriver.Unwrap(db).Exec(context.Background(), legacyUsageSchema); err != nil {
		t.Fatalf("legacy schema: %v", err)
	}
	cachedID := id.NewUsageID().String()
	_, err := sqlitedriver.Unwrap(db).Exec(context.Background(),
		`INSERT INTO usage_records (id, provider, model, total_tokens, cost_usd, cached, created_at)
         VALUES (?, 'openai', 'gpt-4o', 10, 0, 1, '2026-10-01 10:00:00')`, cachedID)
	if err != nil {
		t.Fatalf("insert legacy cache hit: %v", err)
	}
	uncachedID := id.NewUsageID().String()
	insertLegacyRow(t, db, uncachedID, "2026-10-01 10:00:00", 0)

	s := sqlitestore.New(db)
	if err := s.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	got := storetest.FindRecord(t, s, id.MustParseUsageID(cachedID))
	if got.CostUSD == nil || !got.CostUSD.Equal(money.Zero) || got.PricingStatus != usage.PricingCached || got.Outcome != usage.OutcomeCached {
		t.Fatalf("legacy cache hit = cost %v, status %s, outcome %s; want $0, cached, cached", got.CostUSD, got.PricingStatus, got.Outcome)
	}
	got = storetest.FindRecord(t, s, id.MustParseUsageID(uncachedID))
	if got.CostUSD != nil || got.PricingStatus != usage.PricingUnpricedModel {
		t.Fatalf("legacy row = cost %v, status %s; want unknown and unpriced_model", got.CostUSD, got.PricingStatus)
	}
}

// A binary from before exact money may still be running when the new one
// migrates. It omits pricing_status and writes cost_usd = 0 (a REAL). That
// row must read as unknown, not as a priced $0.
func TestRowFromAnOldBinaryReadsAsUnpriced(t *testing.T) {
	db := storetest.OpenSQLiteDB(t)
	s := sqlitestore.New(db)
	if err := s.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	usageID := id.NewUsageID().String()
	_, err := sqlitedriver.Unwrap(db).Exec(context.Background(),
		`INSERT INTO usage_records (id, tenant_id, key_id, request_id, provider, model, total_tokens, cost_usd, cached, created_at)
         VALUES (?, '', '', '', 'openai', 'gpt-4o', 10, 0.0, 0, '2026-10-01T10:00:00.000000000Z')`, usageID)
	if err != nil {
		t.Fatalf("insert as the old binary: %v", err)
	}
	got := storetest.FindRecord(t, s, id.MustParseUsageID(usageID))
	if got.CostUSD != nil || got.PricingStatus != usage.PricingUnpricedModel {
		t.Fatalf("old binary row = cost %v, status %s; want unknown and unpriced_model", got.CostUSD, got.PricingStatus)
	}
}
