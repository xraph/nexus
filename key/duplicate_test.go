package key_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/key"
	"github.com/xraph/nexus/store"
	"github.com/xraph/nexus/tenant"
)

// rowsStore is a key store that returns fixed rows for every prefix lookup,
// in the order given: the shape a race between two replicas leaves behind
// (two rows, one hash) in an order the backend does not promise.
type rowsStore struct {
	key.Store
	rows []*key.APIKey
}

func (r *rowsStore) FindByPrefix(context.Context, string) ([]*key.APIKey, error) {
	out := make([]*key.APIKey, len(r.rows))
	for i, k := range r.rows {
		c := *k
		out[i] = &c
	}
	return out, nil
}

func (r *rowsStore) TouchLastUsed(context.Context, string, time.Time) error { return nil }

func dupRow(tn id.TenantID, status key.Status, expires *time.Time) *key.APIKey {
	return &key.APIKey{ID: id.NewKeyID(), TenantID: tn, Name: "bootstrap admin", Prefix: operatorKey[:12],
		Hash: hashOf(operatorKey), Scopes: []string{key.ScopeAdmin}, Status: status, ExpiresAt: expires,
		CreatedAt: time.Now().Add(-time.Hour)}
}

func TestADuplicateHashFailsClosedInEveryOrder(t *testing.T) {
	tn := id.NewTenantID()
	past := time.Now().Add(-time.Minute)
	cases := []struct {
		name    string
		bad     func() *key.APIKey
		wantErr error
	}{
		{"revoked", func() *key.APIKey { return dupRow(tn, key.KeyRevoked, nil) }, key.ErrRevoked},
		{"expired", func() *key.APIKey { return dupRow(tn, key.KeyActive, &past) }, key.ErrExpired},
	}
	for _, tc := range cases {
		for _, badFirst := range []bool{true, false} {
			name := tc.name + " second"
			if badFirst {
				name = tc.name + " first"
			}
			t.Run(name, func(t *testing.T) {
				bad, live := tc.bad(), dupRow(tn, key.KeyActive, nil)
				rows := []*key.APIKey{live, bad}
				if badFirst {
					rows = []*key.APIKey{bad, live}
				}
				svc := key.NewService(&rowsStore{rows: rows})
				ctx := context.Background()
				if _, err := svc.Validate(ctx, operatorKey); !errors.Is(err, tc.wantErr) {
					t.Fatalf("validate = %v, want %v: an active duplicate must not outvote a %s row", err, tc.wantErr, tc.name)
				}
				k, created, err := svc.Ensure(ctx, operatorKey, &key.CreateInput{TenantID: tn.String(), Name: "bootstrap admin"})
				if err != nil || created {
					t.Fatalf("ensure = created %v, err %v; want the existing row", created, err)
				}
				if k.ID != bad.ID || k.Status == key.KeyActive {
					t.Fatalf("ensure returned key %s status %s, want the %s row %s", k.ID, k.Status, tc.name, bad.ID)
				}
			})
		}
	}
}

// racingStore shows an empty store to the first lookup and the stored row to
// every later one, and refuses the insert as a unique index would: the
// replica that lost the race.
type racingStore struct {
	key.Store
	row   *key.APIKey
	calls atomic.Int32
}

func (r *racingStore) FindByPrefix(context.Context, string) ([]*key.APIKey, error) {
	if r.calls.Add(1) == 1 {
		return []*key.APIKey{}, nil
	}
	c := *r.row
	return []*key.APIKey{&c}, nil
}

func (r *racingStore) Insert(context.Context, *key.APIKey) error { return key.ErrDuplicate }

func TestEnsureReturnsTheWinnerWhenItLosesTheInsertRace(t *testing.T) {
	tn := id.NewTenantID()
	winner := dupRow(tn, key.KeyActive, nil)
	ev := &events{}
	svc := key.NewService(&racingStore{row: winner}, key.WithEvents(ev))
	k, created, err := svc.Ensure(context.Background(), operatorKey, &key.CreateInput{TenantID: tn.String(), Name: "bootstrap admin"})
	if err != nil || created || k.ID != winner.ID {
		t.Fatalf("ensure = key %v, created %v, err %v; want the winner %s", k, created, err, winner.ID)
	}
	if len(ev.created) != 0 {
		t.Fatalf("the loser reported %d created events, want none", len(ev.created))
	}
}

func TestConcurrentEnsureMakesExactlyOneRow(t *testing.T) {
	s := store.NewMemory()
	ctx := context.Background()
	tn, err := tenant.NewService(s.Tenants()).Create(ctx, &tenant.CreateInput{Name: "Operator", Slug: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	ev := &events{}
	svc := key.NewService(s.Keys(), key.WithTenants(s.Tenants()), key.WithEvents(ev))
	const n = 32
	var (
		wg      sync.WaitGroup
		start   = make(chan struct{})
		made    atomic.Int32
		failure atomic.Value
	)
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, created, ensureErr := svc.Ensure(ctx, operatorKey, &key.CreateInput{TenantID: tn.ID.String(), Name: "bootstrap admin", Scopes: []string{key.ScopeAdmin}})
			if ensureErr != nil {
				failure.Store(ensureErr)
				return
			}
			if created {
				made.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()
	if e := failure.Load(); e != nil {
		t.Fatalf("an ensure failed: %v", e)
	}
	keys, err := svc.List(ctx, tn.ID.String())
	if err != nil || len(keys) != 1 {
		t.Fatalf("%d rows (err %v), want exactly 1", len(keys), err)
	}
	if made.Load() != 1 || len(ev.created) != 1 {
		t.Fatalf("%d callers created it and %d events fired, want 1 and 1", made.Load(), len(ev.created))
	}
}
