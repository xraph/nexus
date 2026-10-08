package sqlite_test

import (
	"context"
	"database/sql"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/xraph/grove"
	"github.com/xraph/grove/drivers/sqlitedriver"

	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/key"
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

// A run that died after creating usage_records_next must not stop the next
// run, and a run that fails must leave the old table as it was.
func TestRebuildMigrationIsRerunnableAndAtomic(t *testing.T) {
	ctx := context.Background()
	db := storetest.OpenSQLiteDB(t)
	raw := sqlitedriver.Unwrap(db)
	if _, err := raw.Exec(ctx, legacyUsageSchema); err != nil {
		t.Fatalf("legacy schema: %v", err)
	}
	if _, err := raw.Exec(ctx, `CREATE TABLE usage_records_next (junk TEXT)`); err != nil {
		t.Fatalf("leftover table: %v", err)
	}
	rowID := id.NewUsageID().String()
	insertLegacyRow(t, db, rowID, "2026-10-01 10:00:00", 0.5)

	s := sqlitestore.New(db)
	if err := s.Migrate(); err != nil {
		t.Fatalf("migrate with a leftover usage_records_next: %v", err)
	}
	got := storetest.FindRecord(t, s, id.MustParseUsageID(rowID))
	if got.CostUSD == nil || got.CostUSD.String() != "0.5" || got.PricingStatus != usage.PricingPriced {
		t.Fatalf("migrated row = cost %v, status %s", got.CostUSD, got.PricingStatus)
	}
}

func TestFailedRebuildLeavesNoPartialTable(t *testing.T) {
	ctx := context.Background()
	db := storetest.OpenSQLiteDB(t)
	raw := sqlitedriver.Unwrap(db)
	// No cached column: the copy fails after usage_records_next was created.
	if _, err := raw.Exec(ctx, `CREATE TABLE usage_records (id TEXT PRIMARY KEY, tenant_id TEXT, key_id TEXT,
        request_id TEXT, provider TEXT, model TEXT, prompt_tokens INTEGER, completion_tokens INTEGER,
        total_tokens INTEGER, cost_usd REAL, latency_ns INTEGER, status_code INTEGER, created_at TEXT)`); err != nil {
		t.Fatalf("broken legacy schema: %v", err)
	}
	if err := sqlitestore.New(db).Migrate(); err == nil {
		t.Fatalf("migrate over a table with no cached column should fail")
	}
	var n int
	if err := raw.QueryRow(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE name = 'usage_records_next'`).Scan(&n); err != nil {
		t.Fatalf("look for the partial table: %v", err)
	}
	if n != 0 {
		t.Fatalf("a failed rebuild left usage_records_next behind")
	}
	// The connection must not be left inside the failed transaction.
	if _, err := raw.Exec(ctx, `BEGIN; COMMIT;`); err != nil {
		t.Fatalf("connection is stuck in a transaction: %v", err)
	}
}

// Keys written before expires_at became conv.TimeText hold whatever form
// grove bound a time.Time in: time.Time.String, in the zone the caller used.
// Compared as text those do not sort as times, so Migrate rewrites them, and
// the status filter then sees the right instants.
func TestLegacyKeyExpiryIsNormalisedForTheStatusFilter(t *testing.T) {
	ctx := context.Background()
	db := storetest.OpenSQLiteDB(t)
	s := sqlitestore.New(db)
	if err := s.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	tn := storetest.InsertTenant(t, s)
	now := time.Now().UTC()
	lapsed := storetest.Key(tn.ID, "lapsed")
	live := storetest.Key(tn.ID, "live")
	forever := storetest.Key(tn.ID, "forever")
	forever.ExpiresAt = nil
	for _, k := range []*key.APIKey{lapsed, live, forever} {
		if err := s.Keys().Insert(ctx, k); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}
	// 2h ago in a zone ahead of UTC and 1h ahead in one behind it: as text, the
	// zone-local clock readings sort the wrong way round.
	east, west := time.FixedZone("EEST", 3*3600), time.FixedZone("CDT", -5*3600)
	legacy := map[string]string{
		lapsed.ID.String(): now.Add(-2 * time.Hour).In(east).Format("2006-01-02 15:04:05.999999999 -0700 MST"),
		live.ID.String():   now.Add(time.Hour).In(west).Format("2006-01-02 15:04:05.999999999 -0700 MST") + " m=+0.028456085",
	}
	raw := sqlitedriver.Unwrap(db)
	for kid, text := range legacy {
		if _, err := raw.Exec(ctx, `UPDATE api_keys SET expires_at = ? WHERE id = ?`, text, kid); err != nil {
			t.Fatalf("write legacy expiry: %v", err)
		}
	}
	rawExpiry := func() map[string]sql.NullString {
		out := map[string]sql.NullString{}
		for _, k := range []*key.APIKey{lapsed, live, forever} {
			var v sql.NullString
			if err := raw.QueryRow(ctx, `SELECT expires_at FROM api_keys WHERE id = ?`, k.ID.String()).Scan(&v); err != nil {
				t.Fatalf("read raw expires_at: %v", err)
			}
			out[k.Name] = v
		}
		return out
	}
	var first map[string]sql.NullString
	for pass := 1; pass <= 2; pass++ { // the second Migrate must change nothing
		if err := s.Migrate(); err != nil {
			t.Fatalf("migrate #%d: %v", pass, err)
		}
		stored := rawExpiry()
		if pass == 1 {
			first = stored
			for _, name := range []string{"lapsed", "live"} {
				if !stored[name].Valid || len(stored[name].String) != 30 || !strings.HasSuffix(stored[name].String, "Z") {
					t.Fatalf("%s expires_at = %+v; want fixed-width UTC text", name, stored[name])
				}
			}
			if stored["forever"].Valid {
				t.Fatalf("forever expires_at = %+v; a key without an expiry stays NULL", stored["forever"])
			}
		} else if !reflect.DeepEqual(stored, first) {
			t.Fatalf("second Migrate changed the stored text:\n pass 1 %+v\n pass 2 %+v", first, stored)
		}
		res, err := s.Keys().List(ctx, &key.ListOptions{TenantID: tn.ID.String(), Status: key.KeyExpired})
		if err != nil || len(res.Items) != 1 || res.Items[0].ID.String() != lapsed.ID.String() {
			t.Fatalf("pass %d: expired = %v, %v; want only the lapsed key", pass, res, err)
		}
		n, err := s.Keys().Count(ctx, &key.ListOptions{TenantID: tn.ID.String(), Status: key.KeyActive})
		if err != nil || n != 2 {
			t.Fatalf("pass %d: active count = %d, %v; want 2 (live and forever)", pass, n, err)
		}
		got, err := s.Keys().FindByID(ctx, live.ID.String())
		if err != nil || got.ExpiresAt == nil || got.ExpiresAt.Sub(now.Add(time.Hour)).Abs() > time.Second {
			t.Fatalf("pass %d: live key expiry = %v, %v; want the same instant", pass, got, err)
		}
		if got, err = s.Keys().FindByID(ctx, forever.ID.String()); err != nil || got.ExpiresAt != nil {
			t.Fatalf("pass %d: forever key expiry = %v, %v; want none", pass, got, err)
		}
	}
}
