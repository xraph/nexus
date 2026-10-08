package sqlite_test

import (
	"context"
	"strings"
	"testing"

	"github.com/xraph/grove/driver"
	"github.com/xraph/grove/drivers/sqlitedriver"
	"github.com/xraph/grove/migrate"

	sqlitestore "github.com/xraph/nexus/store/sqlite"
	"github.com/xraph/nexus/store/storetest"
)

// execOnly runs a migration's Down by hand. Down only calls Exec.
type execOnly struct {
	migrate.Executor
	db *sqlitedriver.SqliteDB
}

func (e execOnly) Exec(ctx context.Context, query string, args ...any) (driver.Result, error) {
	return e.db.Exec(ctx, query, args...)
}

// The quota stage reads a tenant's daily count and monthly spend on every
// request it checks, so usage needs an index on (tenant_id, created_at).
func TestUsageIsIndexedByTenantAndTime(t *testing.T) {
	ctx := context.Background()
	db := storetest.OpenSQLiteDB(t)
	if err := sqlitestore.New(db).Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	raw := sqlitedriver.Unwrap(db)
	sql := func() string {
		var def string
		err := raw.QueryRow(ctx, `SELECT COALESCE(MAX(sql), '') FROM sqlite_master
			WHERE type = 'index' AND name = 'idx_usage_tenant_created'`).Scan(&def)
		if err != nil {
			t.Fatalf("read index: %v", err)
		}
		return def
	}
	if def := sql(); !strings.Contains(def, "usage_records (tenant_id, created_at)") {
		t.Fatalf("index = %q; want one on usage_records (tenant_id, created_at)", def)
	}
	var m *migrate.Migration
	for _, mig := range sqlitestore.Migrations.Migrations() {
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
	if def := sql(); def != "" {
		t.Fatalf("after Down the index is still %q", def)
	}
}

// A key list filters on tenant and status, and derives expiry from
// expires_at, so keys need (tenant_id, status) and (status, expires_at).
func TestKeysAreIndexedByStatusAndExpiry(t *testing.T) {
	ctx := context.Background()
	db := storetest.OpenSQLiteDB(t)
	if err := sqlitestore.New(db).Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	raw := sqlitedriver.Unwrap(db)
	def := func(name string) string {
		var d string
		err := raw.QueryRow(ctx, `SELECT COALESCE(MAX(sql), '') FROM sqlite_master WHERE type = 'index' AND name = ?`, name).Scan(&d)
		if err != nil {
			t.Fatalf("read index %s: %v", name, err)
		}
		return d
	}
	want := map[string]string{
		"idx_api_keys_tenant_status":  "api_keys (tenant_id, status)",
		"idx_api_keys_status_expires": "api_keys (status, expires_at)",
	}
	for name, cols := range want {
		if d := def(name); !strings.Contains(d, cols) {
			t.Fatalf("index %s = %q; want one on %s", name, d, cols)
		}
	}
	var m *migrate.Migration
	for _, mig := range sqlitestore.Migrations.Migrations() {
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
