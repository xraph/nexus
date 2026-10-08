package sqlite_test

import (
	"context"
	"testing"
	"time"

	"github.com/xraph/grove/drivers/sqlitedriver"

	"github.com/xraph/nexus/id"
	sqlitestore "github.com/xraph/nexus/store/sqlite"
	"github.com/xraph/nexus/store/storetest"
	"github.com/xraph/nexus/tenant"
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

func TestTenantServiceTimestampsRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := sqlitestore.New(storetest.OpenSQLiteDB(t))
	if err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	svc := tenant.NewService(s.Tenants())
	created, err := svc.Create(ctx, &tenant.CreateInput{Name: "Clock", Slug: "clock"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.Get(ctx, created.ID.String())
	if err != nil {
		t.Fatal(err)
	}
	if !got.CreatedAt.Equal(created.CreatedAt) || !got.UpdatedAt.Equal(created.UpdatedAt) {
		t.Fatal("create timestamps changed")
	}
	name := "Updated"
	updated, err := svc.Update(ctx, created.ID.String(), &tenant.UpdateInput{Name: &name})
	if err != nil {
		t.Fatal(err)
	}
	got, err = svc.Get(ctx, created.ID.String())
	if err != nil || !got.UpdatedAt.Equal(updated.UpdatedAt) {
		t.Fatalf("updated timestamp did not round trip: %v", err)
	}
}

func TestLegacyTenantMonotonicTimestamps(t *testing.T) {
	ctx := context.Background()
	db := storetest.OpenSQLiteDB(t)
	s := sqlitestore.New(db)
	if err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	tid := id.NewTenantID().String()
	legacy := "2026-10-08 12:14:01.104899 -0500 CDT m=+0.019153876"
	_, err := sqlitedriver.Unwrap(db).Exec(ctx,
		`INSERT INTO tenants (id, name, slug, status, quota, config, metadata, created_at, updated_at)
         VALUES (?, 'Clock', 'clock', 'active', '{}', '{}', '{}', ?, ?)`, tid, legacy, legacy)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Tenants().FindByID(ctx, tid)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 10, 8, 17, 14, 1, 104899000, time.UTC)
	if !got.CreatedAt.Equal(want) || !got.UpdatedAt.Equal(want) {
		t.Fatal("legacy timestamp lost its offset or precision")
	}
}
