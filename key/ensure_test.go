package key_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/key"
)

// operatorKey is the raw value an operator would supply: "nxs_" and 64 hex digits.
var operatorKey = "nxs_" + strings.Repeat("ab", 32)

func TestEnsureCreatesAnAdminKeyThatValidates(t *testing.T) {
	svc, tn, _, ev := setup(t, nil)
	ctx := context.Background()
	k, created, err := svc.Ensure(ctx, operatorKey, &key.CreateInput{TenantID: tn.ID.String(), Name: "bootstrap admin", Scopes: []string{key.ScopeAdmin}})
	if err != nil || !created {
		t.Fatalf("ensure = created %v, err %v; want created", created, err)
	}
	if k.Status != key.KeyActive || len(k.Scopes) != 1 || k.Scopes[0] != key.ScopeAdmin || k.Prefix != operatorKey[:12] {
		t.Fatalf("key = status %s, scopes %v, prefix %q", k.Status, k.Scopes, k.Prefix)
	}
	got, err := svc.Validate(ctx, operatorKey)
	if err != nil || got.ID != k.ID {
		t.Fatalf("validate = %v; want key %s", err, k.ID)
	}
	if len(ev.created) != 1 || ev.created[0] != k.ID {
		t.Fatalf("created events = %v, want exactly one for %s", ev.created, k.ID)
	}
}

func TestEnsureTwiceMakesOneKey(t *testing.T) {
	svc, tn, _, ev := setup(t, nil)
	ctx := context.Background()
	in := &key.CreateInput{TenantID: tn.ID.String(), Name: "bootstrap admin", Scopes: []string{key.ScopeAdmin}}
	first, _, err := svc.Ensure(ctx, operatorKey, in)
	if err != nil {
		t.Fatal(err)
	}
	second, created, err := svc.Ensure(ctx, operatorKey, in)
	if err != nil || created {
		t.Fatalf("second ensure = created %v, err %v; want found", created, err)
	}
	if second.ID != first.ID {
		t.Fatalf("second ensure returned key %s, want %s", second.ID, first.ID)
	}
	keys, err := svc.List(ctx, tn.ID.String())
	if err != nil || len(keys) != 1 {
		t.Fatalf("%d keys (err %v), want 1", len(keys), err)
	}
	if len(ev.created) != 1 {
		t.Fatalf("%d created events, want 1", len(ev.created))
	}
}

func TestEnsureLeavesARevokedKeyRevoked(t *testing.T) {
	svc, tn, _, _ := setup(t, nil)
	ctx := context.Background()
	in := &key.CreateInput{TenantID: tn.ID.String(), Name: "bootstrap admin", Scopes: []string{key.ScopeAdmin}}
	first, _, err := svc.Ensure(ctx, operatorKey, in)
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.Revoke(ctx, first.ID.String()); err != nil {
		t.Fatal(err)
	}
	again, created, err := svc.Ensure(ctx, operatorKey, in)
	if err != nil || created {
		t.Fatalf("ensure over a revoked key = created %v, err %v; want found", created, err)
	}
	if again.ID != first.ID || again.Status != key.KeyRevoked {
		t.Fatalf("ensure returned key %s status %s; want %s revoked", again.ID, again.Status, first.ID)
	}
	if _, err := svc.Validate(ctx, operatorKey); !errors.Is(err, key.ErrRevoked) {
		t.Fatalf("validate = %v, want ErrRevoked", err)
	}
	if keys, _ := svc.List(ctx, tn.ID.String()); len(keys) != 1 {
		t.Fatalf("%d keys, want 1", len(keys))
	}
}

func TestEnsureRefusesAMalformedKeyWithoutEchoingIt(t *testing.T) {
	svc, tn, _, _ := setup(t, nil)
	in := &key.CreateInput{TenantID: tn.ID.String(), Name: "bootstrap admin", Scopes: []string{key.ScopeAdmin}}
	for _, bad := range []string{
		"",
		"hunter2-swordfish",
		"nxs_tooshort-and-secret",
		"nxs_" + strings.Repeat("AB", 32),
		"nxs_" + strings.Repeat("zz", 32),
		operatorKey + "0",
	} {
		k, created, err := svc.Ensure(context.Background(), bad, in)
		if !errors.Is(err, key.ErrInvalid) || k != nil || created {
			t.Fatalf("ensure of a malformed key = %v, %v, %v; want ErrInvalid", k, created, err)
		}
		if bad != "" && strings.Contains(err.Error(), bad) {
			t.Fatalf("the error echoes the key it was given (length %d)", len(bad))
		}
	}
}

func TestEnsureRunsTheChecksCreateRuns(t *testing.T) {
	svc, tn, _, _ := setup(t, nil)
	ctx := context.Background()
	past := time.Now().Add(-time.Hour)
	for name, in := range map[string]*key.CreateInput{
		"unknown scope": {TenantID: tn.ID.String(), Name: "x", Scopes: []string{"root"}},
		"no name":       {TenantID: tn.ID.String(), Scopes: []string{key.ScopeAdmin}},
		"nil input":     nil,
		"expired":       {TenantID: tn.ID.String(), Name: "x", ExpiresAt: &past},
		"bad tenant id": {TenantID: "nope", Name: "x"},
	} {
		_, created, err := svc.Ensure(ctx, operatorKey, in)
		if !errors.Is(err, key.ErrInvalid) || created {
			t.Fatalf("%s: ensure = created %v, err %v; want ErrInvalid", name, created, err)
		}
		if strings.Contains(err.Error(), operatorKey) {
			t.Fatalf("%s: the error echoes the key", name)
		}
	}
	if keys, _ := svc.List(ctx, tn.ID.String()); len(keys) != 0 {
		t.Fatalf("%d keys after refused ensures, want none", len(keys))
	}
	// A tenant that does not exist is refused too, and the error stays clean.
	_, created, err := svc.Ensure(ctx, operatorKey, &key.CreateInput{TenantID: id.NewTenantID().String(), Name: "x"})
	if err == nil || created || strings.Contains(err.Error(), operatorKey) {
		t.Fatalf("ensure for a missing tenant = created %v, err %v", created, err)
	}
}

func TestEnsureDoesNotTouchLastUsed(t *testing.T) {
	svc, tn, _, _ := setup(t, nil)
	ctx := context.Background()
	in := &key.CreateInput{TenantID: tn.ID.String(), Name: "bootstrap admin", Scopes: []string{key.ScopeAdmin}}
	if _, _, err := svc.Ensure(ctx, operatorKey, in); err != nil {
		t.Fatal(err)
	}
	k, _, err := svc.Ensure(ctx, operatorKey, in)
	if err != nil || k.LastUsedAt != nil {
		t.Fatalf("ensure of an existing key = last used %v, err %v; want unused", k.LastUsedAt, err)
	}
}

func TestWellFormed(t *testing.T) {
	if !key.WellFormed(operatorKey) {
		t.Fatal("a nxs_ key with 64 lowercase hex digits must be well formed")
	}
	for _, bad := range []string{"", "nxs_", operatorKey[:len(operatorKey)-1], "nxs_" + strings.Repeat("AB", 32), "xxx_" + strings.Repeat("ab", 32)} {
		if key.WellFormed(bad) {
			t.Fatalf("%q must not be well formed", bad)
		}
	}
}
