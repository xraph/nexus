package postgres_test

import (
	"context"
	"errors"
	"testing"

	"github.com/xraph/nexus/store"
	pgstore "github.com/xraph/nexus/store/postgres"
	"github.com/xraph/nexus/store/storetest"
	"github.com/xraph/nexus/tenant"
)

func migratedStore(t *testing.T) store.Store {
	t.Helper()
	s := pgstore.New(storetest.OpenPostgresDB(t))
	if err := s.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return s
}

// A usage row references its tenant, so Postgres refuses the delete and the
// store says the tenant is in use.
func TestDeletingATenantWithOnlyUsageHistoryIsRefused(t *testing.T) {
	ctx := context.Background()
	s := migratedStore(t)
	tn := storetest.InsertTenant(t, s)
	storetest.InsertRecord(t, s, storetest.Record(tn.ID, "0.01"))

	if err := s.Tenants().Delete(ctx, tn.ID.String()); !errors.Is(err, tenant.ErrInUse) {
		t.Fatalf("delete = %v, want tenant.ErrInUse", err)
	}
	if _, err := s.Tenants().FindByID(ctx, tn.ID.String()); err != nil {
		t.Fatalf("the refused delete removed the tenant: %v", err)
	}
}

func TestDeletingATenantWithAKeyIsRefused(t *testing.T) {
	ctx := context.Background()
	s := migratedStore(t)
	tn := storetest.InsertTenant(t, s)
	if err := s.Keys().Insert(ctx, storetest.Key(tn.ID, "k")); err != nil {
		t.Fatal(err)
	}
	if err := s.Tenants().Delete(ctx, tn.ID.String()); !errors.Is(err, tenant.ErrInUse) {
		t.Fatalf("delete = %v, want tenant.ErrInUse", err)
	}
}

func TestDeletingAnUnusedTenantWorks(t *testing.T) {
	ctx := context.Background()
	s := migratedStore(t)
	tn := storetest.InsertTenant(t, s)
	if err := s.Tenants().Delete(ctx, tn.ID.String()); err != nil {
		t.Fatalf("delete = %v", err)
	}
	if _, err := s.Tenants().FindByID(ctx, tn.ID.String()); !errors.Is(err, tenant.ErrNotFound) {
		t.Fatalf("find after delete = %v, want ErrNotFound", err)
	}
}
