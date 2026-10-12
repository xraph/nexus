package storetest_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/key"
	"github.com/xraph/nexus/store"
	"github.com/xraph/nexus/store/storetest"
)

// statusKeys inserts, for one tenant, an active key with no expiry, an active
// key expiring in an hour, an active key that expired an hour ago, and a
// revoked key.
type statusKeys struct {
	tenant                            string
	noExpiry, future, lapsed, revoked *key.APIKey
}

func insertStatusKeys(t *testing.T, s store.Store) statusKeys {
	t.Helper()
	ctx := context.Background()
	tn := storetest.InsertTenant(t, s)
	now := time.Now().UTC()
	in, ago := now.Add(time.Hour), now.Add(-time.Hour)

	noExpiry := storetest.Key(tn.ID, "no-expiry")
	noExpiry.ExpiresAt = nil
	future := storetest.Key(tn.ID, "future")
	future.ExpiresAt = &in
	lapsed := storetest.Key(tn.ID, "lapsed")
	lapsed.ExpiresAt = &ago
	revoked := storetest.Key(tn.ID, "revoked")
	revoked.Status = key.KeyRevoked

	for _, k := range []*key.APIKey{noExpiry, future, lapsed, revoked} {
		if err := s.Keys().Insert(ctx, k); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}
	return statusKeys{tenant: tn.ID.String(), noExpiry: noExpiry, future: future, lapsed: lapsed, revoked: revoked}
}

func idsOf(items []*key.APIKey) []string {
	out := make([]string, 0, len(items))
	for _, k := range items {
		out = append(out, k.ID.String())
	}
	slices.Sort(out)
	return out
}

func wantIDs(keys ...*key.APIKey) []string { return idsOf(keys) }

func TestKeyStatusIsDerivedAtTheStore(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		ctx := context.Background()
		ks := insertStatusKeys(t, s)

		cases := []struct {
			name   string
			status key.Status
			want   []string
		}{
			{"active", key.KeyActive, wantIDs(ks.noExpiry, ks.future)},
			{"expired", key.KeyExpired, wantIDs(ks.lapsed)},
			{"revoked", key.KeyRevoked, wantIDs(ks.revoked)},
			{"any", "", wantIDs(ks.noExpiry, ks.future, ks.lapsed, ks.revoked)},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				opts := &key.ListOptions{TenantID: ks.tenant, Status: c.status}
				res, err := s.Keys().List(ctx, opts)
				if err != nil {
					t.Fatalf("list: %v", err)
				}
				if got := idsOf(res.Items); !slices.Equal(got, c.want) {
					t.Fatalf("List(%q) ids:\n got %v\nwant %v", c.status, got, c.want)
				}
				n, err := s.Keys().Count(ctx, opts)
				if err != nil {
					t.Fatalf("count: %v", err)
				}
				if n != len(c.want) {
					t.Fatalf("Count(%q) = %d, want %d", c.status, n, len(c.want))
				}
				for _, k := range res.Items {
					want := key.KeyActive
					switch k.ID.String() {
					case ks.lapsed.ID.String():
						want = key.KeyExpired
					case ks.revoked.ID.String():
						want = key.KeyRevoked
					}
					if k.Status != want {
						t.Fatalf("List(%q) returned %s as %s, want %s", c.status, k.Name, k.Status, want)
					}
				}
			})
		}
	})
}

func TestKeyCountIgnoresPagingAndHonoursTenant(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		ctx := context.Background()
		ks := insertStatusKeys(t, s)
		other := storetest.InsertTenant(t, s)
		if err := s.Keys().Insert(ctx, storetest.Key(other.ID, "elsewhere")); err != nil {
			t.Fatalf("insert: %v", err)
		}
		n, err := s.Keys().Count(ctx, &key.ListOptions{TenantID: ks.tenant, Status: key.KeyActive, Limit: 1, Cursor: id.NewKeyID().String()})
		if err != nil || n != 2 {
			t.Fatalf("Count with a limit and a cursor = %d, %v; want 2 (both ignored)", n, err)
		}
		n, err = s.Keys().Count(ctx, nil)
		if err != nil || n != 5 {
			t.Fatalf("Count(nil) = %d, %v; want every key, 5", n, err)
		}
	})
}

func TestKeyPagingOverActiveSkipsTheExpired(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		ctx := context.Background()
		ks := insertStatusKeys(t, s)
		var got []string
		cursor := ""
		for range 5 {
			res, err := s.Keys().List(ctx, &key.ListOptions{TenantID: ks.tenant, Status: key.KeyActive, Limit: 1, Cursor: cursor})
			if err != nil {
				t.Fatalf("list: %v", err)
			}
			if len(res.Items) > 1 {
				t.Fatalf("page of %d over a limit of 1", len(res.Items))
			}
			got = append(got, idsOf(res.Items)...)
			if res.NextCursor == "" {
				break
			}
			cursor = res.NextCursor
		}
		slices.Sort(got)
		if want := wantIDs(ks.noExpiry, ks.future); !slices.Equal(got, want) {
			t.Fatalf("paged active ids:\n got %v\nwant %v", got, want)
		}
	})
}

func TestAStoredExpiredStatusStaysExpired(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		ctx := context.Background()
		tn := storetest.InsertTenant(t, s)
		k := storetest.Key(tn.ID, "marked")
		k.Status = key.KeyExpired
		if err := s.Keys().Insert(ctx, k); err != nil {
			t.Fatalf("insert: %v", err)
		}
		res, err := s.Keys().List(ctx, &key.ListOptions{TenantID: tn.ID.String(), Status: key.KeyExpired})
		if err != nil || len(res.Items) != 1 || res.Items[0].Status != key.KeyExpired {
			t.Fatalf("expired list = %v, %v; want the stored expired key", res, err)
		}
		if n, err := s.Keys().Count(ctx, &key.ListOptions{TenantID: tn.ID.String(), Status: key.KeyActive}); err != nil || n != 0 {
			t.Fatalf("active count = %d, %v; want 0", n, err)
		}
	})
}
