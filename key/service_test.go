package key_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/key"
	"github.com/xraph/nexus/store"
	"github.com/xraph/nexus/tenant"
)

type events struct {
	mu      sync.Mutex
	created []id.KeyID
	revoked []id.KeyID
}

func (e *events) EmitKeyCreated(_ context.Context, k id.KeyID, _ id.TenantID) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.created = append(e.created, k)
}
func (e *events) EmitKeyRevoked(_ context.Context, k id.KeyID) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.revoked = append(e.revoked, k)
}

func setup(t *testing.T, now func() time.Time) (key.Service, *tenant.Tenant, store.Store, *events) {
	t.Helper()
	s := store.NewMemory()
	tn, err := tenant.NewService(s.Tenants()).Create(context.Background(), &tenant.CreateInput{Name: "Acme", Slug: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	ev := &events{}
	opts := []key.Option{key.WithTenants(s.Tenants()), key.WithEvents(ev)}
	if now != nil {
		opts = append(opts, key.WithClock(now))
	}
	return key.NewService(s.Keys(), opts...), tn, s, ev
}

func TestValidateFindsTheRightKeyAmongSharedPrefixes(t *testing.T) {
	svc, tn, s, _ := setup(t, nil)
	ctx := context.Background()
	a, rawA, err := svc.Create(ctx, &key.CreateInput{TenantID: tn.ID.String(), Name: "a"})
	if err != nil {
		t.Fatal(err)
	}
	// A second key whose raw value starts with a's 12-character prefix: a
	// real 32-bit collision, stored the way Create stores keys (hex SHA-256).
	rawB := a.Prefix + strings.Repeat("b", 56)
	sum := sha256.Sum256([]byte(rawB))
	b := &key.APIKey{ID: id.NewKeyID(), TenantID: tn.ID, Name: "b", Prefix: a.Prefix,
		Hash: hex.EncodeToString(sum[:]), Scopes: []string{"completions"}, Status: key.KeyActive, CreatedAt: time.Now()}
	if err := s.Keys().Insert(ctx, b); err != nil {
		t.Fatal(err)
	}
	for raw, want := range map[string]id.KeyID{rawA: a.ID, rawB: b.ID} {
		got, err := svc.Validate(ctx, raw)
		if err != nil || got.ID != want {
			t.Fatalf("validate = %v; want key %s", err, want)
		}
	}
	forged := a.Prefix + strings.Repeat("c", 56)
	if _, err := svc.Validate(ctx, forged); !errors.Is(err, key.ErrNotFound) {
		t.Fatalf("validate a forged key = %v, want ErrNotFound", err)
	}
}

func TestAnUnknownKeyAndAWrongKeyLookTheSame(t *testing.T) {
	svc, tn, _, _ := setup(t, nil)
	ctx := context.Background()
	_, raw, err := svc.Create(ctx, &key.CreateInput{TenantID: tn.ID.String(), Name: "a"})
	if err != nil {
		t.Fatal(err)
	}
	wrong := raw[:12] + strings.Repeat("0", len(raw)-12)
	for _, k := range []string{"nxs_ffffffff" + strings.Repeat("0", 56), wrong, "short", ""} {
		if _, err := svc.Validate(ctx, k); !errors.Is(err, key.ErrNotFound) {
			t.Errorf("Validate(%q...) = %v, want ErrNotFound", k[:min(len(k), 6)], err)
		}
	}
}

func TestRevokedAndExpiredAreReportedOnlyForTheRightKey(t *testing.T) {
	now := time.Now()
	clock := func() time.Time { return now }
	svc, tn, _, ev := setup(t, clock)
	ctx := context.Background()
	exp := now.Add(time.Hour)
	k, raw, err := svc.Create(ctx, &key.CreateInput{TenantID: tn.ID.String(), Name: "a", ExpiresAt: &exp})
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Hour)
	if _, err = svc.Validate(ctx, raw); !errors.Is(err, key.ErrExpired) {
		t.Fatalf("validate expired = %v", err)
	}
	got, err := svc.Get(ctx, k.ID.String())
	if err != nil || got.Status != key.KeyExpired {
		t.Fatalf("Get = %v; an expired key reads expired without a background job", err)
	}
	k2, raw2, _ := svc.Create(ctx, &key.CreateInput{TenantID: tn.ID.String(), Name: "b"})
	if err := svc.Revoke(ctx, k2.ID.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Validate(ctx, raw2); !errors.Is(err, key.ErrRevoked) {
		t.Fatalf("validate revoked = %v", err)
	}
	if len(ev.created) != 2 || len(ev.revoked) != 1 || ev.revoked[0] != k2.ID {
		t.Fatalf("events created %v revoked %v", ev.created, ev.revoked)
	}
}

func TestLastUsedIsWrittenAtMostOnceAMinute(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	clock := func() time.Time { return now }
	svc, tn, s, _ := setup(t, clock)
	ctx := context.Background()
	k, raw, _ := svc.Create(ctx, &key.CreateInput{TenantID: tn.ID.String(), Name: "a"})
	if _, err := svc.Validate(ctx, raw); err != nil {
		t.Fatal(err)
	}
	first := now
	now = now.Add(30 * time.Second)
	_, _ = svc.Validate(ctx, raw)
	got, _ := s.Keys().FindByID(ctx, k.ID.String())
	if got.LastUsedAt == nil || !got.LastUsedAt.Equal(first) {
		t.Fatalf("last used = %v, want %v (no second write inside a minute)", got.LastUsedAt, first)
	}
	now = now.Add(31 * time.Second)
	_, _ = svc.Validate(ctx, raw)
	got, _ = s.Keys().FindByID(ctx, k.ID.String())
	if !got.LastUsedAt.Equal(now) {
		t.Fatalf("last used = %v, want %v", got.LastUsedAt, now)
	}
}

func TestCreateRefusesBadInputWithoutPanicking(t *testing.T) {
	svc, tn, _, _ := setup(t, nil)
	ctx := context.Background()
	past := time.Now().Add(-time.Minute)
	for name, in := range map[string]*key.CreateInput{
		"malformed tenant": {TenantID: "not-a-tenant", Name: "a"},
		"unknown tenant":   {TenantID: id.NewTenantID().String(), Name: "a"},
		"no name":          {TenantID: tn.ID.String()},
		"expired already":  {TenantID: tn.ID.String(), Name: "a", ExpiresAt: &past},
	} {
		_, _, err := svc.Create(ctx, in)
		if err == nil {
			t.Errorf("%s: created", name)
			continue
		}
		if (name == "no name" || name == "expired already" || name == "malformed tenant") && !errors.Is(err, key.ErrInvalid) {
			t.Errorf("%s = %v, want key.ErrInvalid", name, err)
		}
	}
	if _, _, err := svc.Create(ctx, &key.CreateInput{TenantID: id.NewTenantID().String(), Name: "a"}); !errors.Is(err, tenant.ErrNotFound) {
		t.Errorf("unknown tenant = %v, want tenant.ErrNotFound", err)
	}
	if _, _, err := svc.Create(ctx, &key.CreateInput{TenantID: "nope", Name: "a"}); !errors.Is(err, key.ErrInvalid) {
		t.Errorf("malformed tenant = %v, want key.ErrInvalid", err)
	}
}

func TestRotateRevokesTheOldKeyAndKeepsTheName(t *testing.T) {
	svc, tn, _, _ := setup(t, nil)
	ctx := context.Background()
	old, oldRaw, _ := svc.Create(ctx, &key.CreateInput{TenantID: tn.ID.String(), Name: "indexer", Scopes: []string{"completions"}})
	n, newRaw, err := svc.Rotate(ctx, old.ID.String())
	if err != nil {
		t.Fatal(err)
	}
	n2, _, err := svc.Rotate(ctx, n.ID.String())
	if err != nil {
		t.Fatal(err)
	}
	if n2.Name != "indexer (rotated)" {
		t.Fatalf("name after two rotations = %q", n2.Name)
	}
	if _, err := svc.Validate(ctx, oldRaw); !errors.Is(err, key.ErrRevoked) {
		t.Fatalf("old key = %v", err)
	}
	if _, err := svc.Validate(ctx, newRaw); !errors.Is(err, key.ErrRevoked) {
		t.Fatalf("first rotation's key = %v, want revoked by the second", err)
	}
	if len(n2.Scopes) != 1 || n2.Scopes[0] != "completions" {
		t.Fatalf("scopes = %v", n2.Scopes)
	}
	if _, _, err := svc.Rotate(ctx, old.ID.String()); !errors.Is(err, key.ErrInvalid) {
		t.Fatalf("rotating a revoked key = %v, want key.ErrInvalid", err)
	}
}

func TestRevokingTwiceEmitsOneEvent(t *testing.T) {
	svc, tn, _, ev := setup(t, nil)
	ctx := context.Background()
	k, _, _ := svc.Create(ctx, &key.CreateInput{TenantID: tn.ID.String(), Name: "a"})
	for range 2 {
		if err := svc.Revoke(ctx, k.ID.String()); err != nil {
			t.Fatal(err)
		}
	}
	if len(ev.revoked) != 1 {
		t.Fatalf("revoked events = %v, want exactly one", ev.revoked)
	}
}

// failingUpdates is a key.Store whose Update fails for the old key and, when
// replacementToo is set, for every other key as well (the replacement's id
// does not exist until Rotate creates it).
type failingUpdates struct {
	key.Store
	old            string
	replacementToo bool
}

var errStore = errors.New("store unavailable")

func (f *failingUpdates) Update(ctx context.Context, k *key.APIKey) error {
	if k.ID.String() == f.old || f.replacementToo {
		return errStore
	}
	return f.Store.Update(ctx, k)
}

// rotateWithFailures rotates a fresh key while Update fails for the old key,
// and for the replacement too when replacementToo is set.
func rotateWithFailures(t *testing.T, replacementToo bool) (*failingUpdates, *key.APIKey, error) {
	t.Helper()
	s := store.NewMemory()
	ctx := context.Background()
	tn, err := tenant.NewService(s.Tenants()).Create(ctx, &tenant.CreateInput{Name: "Acme", Slug: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	f := &failingUpdates{Store: s.Keys()}
	svc := key.NewService(f, key.WithTenants(s.Tenants()))
	old, _, err := svc.Create(ctx, &key.CreateInput{TenantID: tn.ID.String(), Name: "a"})
	if err != nil {
		t.Fatal(err)
	}
	f.old, f.replacementToo = old.ID.String(), replacementToo
	_, _, rotErr := svc.Rotate(ctx, old.ID.String())
	return f, old, rotErr
}

func activeKeys(t *testing.T, st key.Store, tenantID id.TenantID) []*key.APIKey {
	t.Helper()
	all, err := st.ListByTenant(context.Background(), tenantID.String())
	if err != nil {
		t.Fatal(err)
	}
	var out []*key.APIKey
	for _, k := range all {
		if k.Status == key.KeyActive {
			out = append(out, k)
		}
	}
	return out
}

func TestRotateRollsTheReplacementBackWhenTheOldRevokeFails(t *testing.T) {
	f, old, err := rotateWithFailures(t, false)
	if err == nil || !errors.Is(err, errStore) {
		t.Fatalf("rotate = %v, want the store error", err)
	}
	if strings.Contains(err.Error(), "still active") {
		t.Fatalf("rollback succeeded, so the error must not say a key is still active: %v", err)
	}
	live := activeKeys(t, f.Store, old.TenantID)
	if len(live) != 1 || live[0].ID != old.ID {
		t.Fatalf("active keys after a rolled-back rotation = %d, want only the old key", len(live))
	}
}

func TestRotateNamesTheReplacementWhenTheRollbackFailsToo(t *testing.T) {
	f, old, err := rotateWithFailures(t, true)
	if err == nil || !errors.Is(err, errStore) {
		t.Fatalf("rotate = %v, want the store error", err)
	}
	live := activeKeys(t, f.Store, old.TenantID)
	if len(live) != 2 {
		t.Fatalf("active keys = %d, want the old key and the stuck replacement", len(live))
	}
	for _, k := range live {
		if k.ID != old.ID && !strings.Contains(err.Error(), k.ID.String()) {
			t.Fatalf("error does not name the replacement key %s: %v", k.ID, err)
		}
	}
	if !strings.Contains(err.Error(), "still active") {
		t.Fatalf("error does not say the replacement is still active: %v", err)
	}
}

// untouchedStore fails the test if Validate reaches the store.
type untouchedStore struct {
	key.Store
	t *testing.T
}

func (u untouchedStore) FindByPrefix(context.Context, string) ([]*key.APIKey, error) {
	u.t.Error("Validate called FindByPrefix for a malformed key")
	return nil, errStore
}

func TestAMalformedKeyNeverReachesTheStore(t *testing.T) {
	svc := key.NewService(untouchedStore{Store: store.NewMemory().Keys(), t: t})
	good := "nxs_" + strings.Repeat("a1", 32)
	for name, raw := range map[string]string{
		"non-utf8":      "nxs_\xff\xfe" + strings.Repeat("a", 62),
		"non-utf8 long": "\xff\xfe\xfd\xfc\xfb\xfa\xf9\xf8\xf7\xf6\xf5\xf4\xf3",
		"uppercase hex": "nxs_" + strings.Repeat("A1", 32),
		"non-hex":       "nxs_" + strings.Repeat("g", 64),
		"too short":     good[:67],
		"too long":      good + "a",
		"wrong prefix":  "abc_" + strings.Repeat("a", 64),
		"empty":         "",
	} {
		if _, err := svc.Validate(context.Background(), raw); !errors.Is(err, key.ErrNotFound) {
			t.Errorf("%s: Validate = %v, want ErrNotFound", name, err)
		}
	}
}

func TestCreateRefusesAnUnknownScope(t *testing.T) {
	ctx := context.Background()
	s := store.NewMemory()
	tn, err := tenant.NewService(s.Tenants()).Create(ctx, &tenant.CreateInput{Name: "A", Slug: "a"})
	if err != nil {
		t.Fatal(err)
	}
	svc := key.NewService(s.Keys(), key.WithTenants(s.Tenants()))
	for _, bad := range [][]string{{"completion"}, {key.ScopeCompletions, "Admin"}, {""}} {
		if _, _, cerr := svc.Create(ctx, &key.CreateInput{TenantID: tn.ID.String(), Name: "k", Scopes: bad}); !errors.Is(cerr, key.ErrInvalid) {
			t.Fatalf("scopes %q = %v; want key.ErrInvalid", bad, cerr)
		}
	}
	all := []string{key.ScopeCompletions, key.ScopeEmbeddings, key.ScopeModels, key.ScopeAdmin}
	k, _, err := svc.Create(ctx, &key.CreateInput{TenantID: tn.ID.String(), Name: "k", Scopes: all})
	if err != nil || len(k.Scopes) != 4 {
		t.Fatalf("every known scope = %v; want a key with all four", err)
	}
}

func TestEffectiveDerivesExpiryAtTheBoundary(t *testing.T) {
	now := time.Now()
	past, edge, future := now.Add(-time.Hour), now, now.Add(time.Hour)
	cases := []struct {
		name   string
		status key.Status
		expiry *time.Time
		want   key.Status
	}{
		{"active without expiry", key.KeyActive, nil, key.KeyActive},
		{"active before expiry", key.KeyActive, &future, key.KeyActive},
		{"active at expiry", key.KeyActive, &edge, key.KeyExpired},
		{"active after expiry", key.KeyActive, &past, key.KeyExpired},
		{"revoked after expiry stays revoked", key.KeyRevoked, &past, key.KeyRevoked},
		{"stored expired stays expired", key.KeyExpired, &future, key.KeyExpired},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := key.Effective(&key.APIKey{Status: c.status, ExpiresAt: c.expiry}, now); got != c.want {
				t.Fatalf("Effective = %s, want %s", got, c.want)
			}
		})
	}
}

func TestListPageAndCountGoThroughTheStoreWithExpiryDerived(t *testing.T) {
	svc, tn, _, _ := setup(t, nil)
	ctx := context.Background()
	soon := time.Now().Add(50 * time.Millisecond)
	if _, _, err := svc.Create(ctx, &key.CreateInput{TenantID: tn.ID.String(), Name: "short", ExpiresAt: &soon}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.Create(ctx, &key.CreateInput{TenantID: tn.ID.String(), Name: "long"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)

	opts := &key.ListOptions{TenantID: tn.ID.String(), Status: key.KeyActive}
	page, err := svc.ListPage(ctx, opts)
	if err != nil || len(page.Items) != 1 || page.Items[0].Name != "long" {
		t.Fatalf("active page = %+v, %v; want only the key without an expiry", page, err)
	}
	n, err := svc.Count(ctx, opts)
	if err != nil || n != 1 {
		t.Fatalf("active count = %d, %v; want 1", n, err)
	}
	opts.Status = key.KeyExpired
	page, err = svc.ListPage(ctx, opts)
	if err != nil || len(page.Items) != 1 || page.Items[0].Name != "short" || page.Items[0].Status != key.KeyExpired {
		t.Fatalf("expired page = %+v, %v; want the lapsed key reading expired", page, err)
	}
	if n, err = svc.Count(ctx, opts); err != nil || n != 1 {
		t.Fatalf("expired count = %d, %v; want 1", n, err)
	}
}
