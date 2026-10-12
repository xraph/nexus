package postgres_test

import (
	"context"
	"testing"

	"github.com/xraph/grove/drivers/pgdriver"

	"github.com/xraph/nexus/id"
	pgstore "github.com/xraph/nexus/store/postgres"
	"github.com/xraph/nexus/store/storetest"
)

func TestLegacyTenantBudgetReadsExactly(t *testing.T) {
	ctx := context.Background()
	db := storetest.OpenPostgresDB(t)
	s := pgstore.New(db)
	if err := s.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	tid := id.NewTenantID().String()
	_, err := pgdriver.Unwrap(db).Exec(ctx,
		`INSERT INTO nexus_tenants (id, name, slug, status, quota, config, metadata)
         VALUES ($1, 'Legacy', 'legacy', 'active', '{"rpm":60,"monthly_budget_usd":12.5}'::jsonb, '{}'::jsonb, '{}'::jsonb)`, tid)
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
