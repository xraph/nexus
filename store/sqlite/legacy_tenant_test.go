package sqlite_test

import (
	"context"
	"testing"

	"github.com/xraph/grove/drivers/sqlitedriver"

	"github.com/xraph/nexus/id"
	sqlitestore "github.com/xraph/nexus/store/sqlite"
	"github.com/xraph/nexus/store/storetest"
)

// Budgets were float JSON numbers before they were exact strings. A tenant
// stored then must still load, with the budget exactly as written.
func TestLegacyTenantBudgetReadsExactly(t *testing.T) {
	ctx := context.Background()
	db := storetest.OpenSQLiteDB(t)
	s := sqlitestore.New(db)
	if err := s.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	tid := id.NewTenantID().String()
	_, err := sqlitedriver.Unwrap(db).Exec(ctx,
		`INSERT INTO tenants (id, name, slug, status, quota, config, metadata, created_at, updated_at)
         VALUES (?, 'Legacy', 'legacy', 'active', '{"rpm":60,"monthly_budget_usd":12.5}', '{}', '{}', '2026-10-01T10:00:00Z', '2026-10-01T10:00:00Z')`, tid)
	if err != nil {
		t.Fatalf("insert legacy tenant: %v", err)
	}
	got, err := s.Tenants().FindByID(ctx, tid)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if got.Quota.MonthlyBudgetUSD.String() != "12.5" || got.Quota.RPM != 60 {
		t.Fatalf("legacy quota = %+v", got.Quota)
	}
}
