package postgres_test

import (
	"context"
	"strings"
	"testing"

	"github.com/xraph/grove/driver"
	"github.com/xraph/grove/drivers/pgdriver"
	"github.com/xraph/grove/migrate"

	pgstore "github.com/xraph/nexus/store/postgres"
	"github.com/xraph/nexus/store/storetest"
)

// execOnly runs a migration's Down by hand. Down only calls Exec.
type execOnly struct {
	migrate.Executor
	db *pgdriver.PgDB
}

func (e execOnly) Exec(ctx context.Context, query string, args ...any) (driver.Result, error) {
	return e.db.Exec(ctx, query, args...)
}

// The quota stage reads a tenant's daily count and monthly spend on every
// request it checks, so usage needs an index on (tenant_id, created_at).
func TestUsageIsIndexedByTenantAndTime(t *testing.T) {
	ctx := context.Background()
	db := storetest.OpenPostgresDB(t)
	if err := pgstore.New(db).Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	raw := pgdriver.Unwrap(db)
	columns := func() string {
		var def string
		err := raw.QueryRow(ctx, `SELECT COALESCE(MAX(indexdef), '') FROM pg_indexes
			WHERE schemaname = current_schema() AND indexname = 'idx_nexus_usage_tenant_created'`).Scan(&def)
		if err != nil {
			t.Fatalf("read index: %v", err)
		}
		return def
	}
	if def := columns(); !strings.Contains(def, "(tenant_id, created_at)") {
		t.Fatalf("index = %q; want one on nexus_usage_records (tenant_id, created_at)", def)
	}
	var m *migrate.Migration
	for _, mig := range pgstore.Migrations.Migrations() {
		if mig.Name == "index_usage_by_tenant_and_time" {
			m = mig
		}
	}
	if m == nil {
		t.Fatal("no index_usage_by_tenant_and_time migration")
	}
	if err := m.Down(ctx, execOnly{db: raw}); err != nil {
		t.Fatalf("down: %v", err)
	}
	if def := columns(); def != "" {
		t.Fatalf("after Down the index is still %q", def)
	}
}

// A key list filters on tenant and status, and derives expiry from
// expires_at, so keys need (tenant_id, status) and (status, expires_at).
func TestKeysAreIndexedByStatusAndExpiry(t *testing.T) {
	ctx := context.Background()
	db := storetest.OpenPostgresDB(t)
	if err := pgstore.New(db).Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	raw := pgdriver.Unwrap(db)
	def := func(name string) string {
		var d string
		err := raw.QueryRow(ctx, `SELECT COALESCE(MAX(indexdef), '') FROM pg_indexes
			WHERE schemaname = current_schema() AND indexname = $1`, name).Scan(&d)
		if err != nil {
			t.Fatalf("read index %s: %v", name, err)
		}
		return d
	}
	want := map[string]string{
		"idx_nexus_api_keys_tenant_status":  "(tenant_id, status)",
		"idx_nexus_api_keys_status_expires": "(status, expires_at)",
	}
	for name, cols := range want {
		if d := def(name); !strings.Contains(d, cols) {
			t.Fatalf("index %s = %q; want one on nexus_api_keys %s", name, d, cols)
		}
	}
	var m *migrate.Migration
	for _, mig := range pgstore.Migrations.Migrations() {
		if mig.Name == "index_keys_by_status_and_expiry" {
			m = mig
		}
	}
	if m == nil {
		t.Fatal("no index_keys_by_status_and_expiry migration")
	}
	if err := m.Down(ctx, execOnly{db: raw}); err != nil {
		t.Fatalf("down: %v", err)
	}
	for name := range want {
		if d := def(name); d != "" {
			t.Fatalf("after Down index %s is still %q", name, d)
		}
	}
}
