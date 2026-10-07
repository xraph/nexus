package storetest_test

import (
	"context"
	"errors"
	"testing"

	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/key"
	"github.com/xraph/nexus/store"
	"github.com/xraph/nexus/store/storetest"
	"github.com/xraph/nexus/tenant"
)

func TestTenantRoundTripsEveryField(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		want := storetest.InsertTenant(t, s)
		got, err := s.Tenants().FindByID(context.Background(), want.ID.String())
		if err != nil {
			t.Fatalf("find: %v", err)
		}
		storetest.SameTenant(t, got, want)

		bySlug, err := s.Tenants().FindBySlug(context.Background(), want.Slug)
		if err != nil {
			t.Fatalf("find by slug: %v", err)
		}
		storetest.SameTenant(t, bySlug, want)
	})
}

func TestKeyRoundTripsEveryField(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		ctx := context.Background()
		tn := storetest.InsertTenant(t, s)
		want := storetest.Key(tn.ID, "search-indexer")
		if err := s.Keys().Insert(ctx, want); err != nil {
			t.Fatalf("insert key: %v", err)
		}
		got, err := s.Keys().FindByID(ctx, want.ID.String())
		if err != nil {
			t.Fatalf("find: %v", err)
		}
		storetest.SameKey(t, got, want)
	})
}

func TestMissingRowsAreErrNotFound(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		ctx := context.Background()
		if _, err := s.Tenants().FindByID(ctx, id.NewTenantID().String()); !errors.Is(err, tenant.ErrNotFound) {
			t.Errorf("tenant FindByID = %v, want tenant.ErrNotFound", err)
		}
		if _, err := s.Tenants().FindBySlug(ctx, "no-such-slug"); !errors.Is(err, tenant.ErrNotFound) {
			t.Errorf("tenant FindBySlug = %v, want tenant.ErrNotFound", err)
		}
		if err := s.Tenants().Update(ctx, storetest.Tenant("ghost")); !errors.Is(err, tenant.ErrNotFound) {
			t.Errorf("tenant Update of a missing tenant = %v, want tenant.ErrNotFound", err)
		}
		if _, err := s.Keys().FindByID(ctx, id.NewKeyID().String()); !errors.Is(err, key.ErrNotFound) {
			t.Errorf("key FindByID = %v, want key.ErrNotFound", err)
		}
		if _, err := s.Keys().FindByPrefix(ctx, "nxs_00000000"); !errors.Is(err, key.ErrNotFound) {
			t.Errorf("key FindByPrefix = %v, want key.ErrNotFound", err)
		}
		tn := storetest.InsertTenant(t, s)
		if err := s.Keys().Update(ctx, storetest.Key(tn.ID, "ghost")); !errors.Is(err, key.ErrNotFound) {
			t.Errorf("key Update of a missing key = %v, want key.ErrNotFound", err)
		}
	})
}

func TestChangingAReturnedRowDoesNotChangeTheStore(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		ctx := context.Background()
		want := storetest.InsertTenant(t, s)
		got, err := s.Tenants().FindByID(ctx, want.ID.String())
		if err != nil {
			t.Fatalf("find: %v", err)
		}
		got.Name = "changed without Update"
		got.Config.AllowedModels[0] = "changed"
		got.Metadata["owner"] = "changed"

		again, err := s.Tenants().FindByID(ctx, want.ID.String())
		if err != nil {
			t.Fatalf("find again: %v", err)
		}
		// A fresh fixture with the stored identity is what the row should
		// still hold.
		fresh := storetest.Tenant(want.Slug)
		fresh.ID, fresh.CreatedAt, fresh.UpdatedAt = want.ID, want.CreatedAt, want.UpdatedAt
		storetest.SameTenant(t, again, fresh)
	})
}
