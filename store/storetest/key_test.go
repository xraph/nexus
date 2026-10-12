package storetest_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/key"
	"github.com/xraph/nexus/store"
	"github.com/xraph/nexus/store/storetest"
)

func TestKeysSharingAPrefixAreAllFound(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		ctx := context.Background()
		tn := storetest.InsertTenant(t, s)
		a, b := storetest.Key(tn.ID, "a"), storetest.Key(tn.ID, "b")
		b.Prefix = a.Prefix
		b.Status = key.KeyRevoked
		other := storetest.Key(tn.ID, "other")
		for _, k := range []*key.APIKey{a, b, other} {
			if err := s.Keys().Insert(ctx, k); err != nil {
				t.Fatalf("insert: %v", err)
			}
		}
		got, err := s.Keys().FindByPrefix(ctx, a.Prefix)
		if err != nil {
			t.Fatalf("find: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("found %d keys for a shared prefix, want 2 (a revoked key is found too)", len(got))
		}
		seen := map[string]bool{}
		for _, k := range got {
			seen[k.ID.String()] = true
		}
		if !seen[a.ID.String()] || !seen[b.ID.String()] {
			t.Fatalf("found %v, want a and b", seen)
		}
	})
}

func TestAnUnknownPrefixFindsNothing(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		got, err := s.Keys().FindByPrefix(context.Background(), "nxs_00000000")
		if err != nil || len(got) != 0 {
			t.Fatalf("FindByPrefix(unknown) = %v, %v; want none and no error", got, err)
		}
	})
}

func TestTouchingAKeyNeverChangesItsStatus(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		ctx := context.Background()
		tn := storetest.InsertTenant(t, s)
		k := storetest.Key(tn.ID, "k")
		if err := s.Keys().Insert(ctx, k); err != nil {
			t.Fatalf("insert: %v", err)
		}
		k.Status = key.KeyRevoked
		if err := s.Keys().Update(ctx, k); err != nil {
			t.Fatalf("revoke: %v", err)
		}
		at := storetest.Now().Add(time.Minute)
		if err := s.Keys().TouchLastUsed(ctx, k.ID.String(), at); err != nil {
			t.Fatalf("touch: %v", err)
		}
		got, err := s.Keys().FindByID(ctx, k.ID.String())
		if err != nil {
			t.Fatalf("find: %v", err)
		}
		if got.Status != key.KeyRevoked {
			t.Fatalf("status after touch = %s, want revoked", got.Status)
		}
		if got.LastUsedAt == nil || !got.LastUsedAt.Equal(at) {
			t.Fatalf("last used = %v, want %v", got.LastUsedAt, at)
		}
		if got.Name != k.Name || got.Hash != k.Hash || got.TenantID != k.TenantID || !slices.Equal(got.Scopes, k.Scopes) {
			t.Fatalf("touch changed more than last_used_at: name %q hash %q tenant %s scopes %v", got.Name, got.Hash, got.TenantID, got.Scopes)
		}
		if err := s.Keys().TouchLastUsed(ctx, id.NewKeyID().String(), at); !errors.Is(err, key.ErrNotFound) {
			t.Fatalf("touch of a missing key = %v, want key.ErrNotFound", err)
		}
	})
}

func TestAKeyHashIsUnique(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		ctx := context.Background()
		tn := storetest.InsertTenant(t, s)
		first := storetest.Key(tn.ID, "first")
		if err := s.Keys().Insert(ctx, first); err != nil {
			t.Fatalf("insert: %v", err)
		}
		// A different id, name and prefix, the same hash.
		again := storetest.Key(tn.ID, "again")
		again.Hash = first.Hash
		if err := s.Keys().Insert(ctx, again); !errors.Is(err, key.ErrDuplicate) {
			t.Fatalf("a second insert with the same hash = %v, want ErrDuplicate", err)
		}
		if _, err := s.Keys().FindByID(ctx, again.ID.String()); !errors.Is(err, key.ErrNotFound) {
			t.Fatalf("the refused key was stored anyway: %v", err)
		}
		// A hash can be stored again once its key is deleted.
		if err := s.Keys().Delete(ctx, first.ID.String()); err != nil {
			t.Fatal(err)
		}
		if err := s.Keys().Insert(ctx, again); err != nil {
			t.Fatalf("insert after the delete: %v", err)
		}
	})
}
