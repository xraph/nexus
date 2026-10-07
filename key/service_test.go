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
			t.Fatalf("validate = %v, %v; want %s", got, err, want)
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
		t.Fatalf("Get = %v, %v; an expired key reads expired without a background job", got, err)
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
		if _, _, err := svc.Create(ctx, in); err == nil {
			t.Errorf("%s: created", name)
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
	if _, _, err := svc.Rotate(ctx, old.ID.String()); err == nil {
		t.Fatal("rotating a revoked key must fail")
	}
}
