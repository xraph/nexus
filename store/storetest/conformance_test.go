package storetest_test

import (
	"context"
	"testing"

	"github.com/xraph/nexus/store"
	"github.com/xraph/nexus/store/storetest"
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
