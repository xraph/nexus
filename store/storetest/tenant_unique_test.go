package storetest_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/store"
	"github.com/xraph/nexus/store/storetest"
	"github.com/xraph/nexus/tenant"
)

func TestTenantSlugIsUniqueOnInsertAndUpdate(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		ctx := context.Background()
		first := storetest.InsertTenant(t, s)
		other := storetest.InsertTenant(t, s)
		other.Slug = first.Slug
		if err := s.Tenants().Update(ctx, other); !errors.Is(err, tenant.ErrDuplicate) {
			t.Fatal("duplicate slug update must return ErrDuplicate")
		}
		other.ID = id.NewTenantID()
		if err := s.Tenants().Insert(ctx, other); !errors.Is(err, tenant.ErrDuplicate) {
			t.Fatal("duplicate slug insert must return ErrDuplicate")
		}
		got, err := s.Tenants().FindBySlug(ctx, first.Slug)
		if err != nil || got.ID != first.ID {
			t.Fatal("duplicate write replaced the original tenant")
		}
	})
}

func TestConcurrentMemoryTenantCreatesKeepOneSlug(t *testing.T) {
	s := store.NewMemory()
	var wg sync.WaitGroup
	results := make(chan error, 20)
	for range 20 {
		wg.Go(func() {
			results <- s.Tenants().Insert(context.Background(), &tenant.Tenant{ID: id.NewTenantID(), Slug: "same"})
		})
	}
	wg.Wait()
	close(results)
	created := 0
	for err := range results {
		if err == nil {
			created++
		} else if !errors.Is(err, tenant.ErrDuplicate) {
			t.Error("unexpected create error")
		}
	}
	if created != 1 {
		t.Fatalf("created %d tenants for one slug", created)
	}
}
