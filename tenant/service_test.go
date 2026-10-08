package tenant_test

import (
	"context"
	"errors"
	"testing"

	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/store"
	"github.com/xraph/nexus/tenant"
)

type events struct {
	created  []id.TenantID
	disabled []id.TenantID
}

func (e *events) EmitTenantCreated(_ context.Context, t id.TenantID) {
	e.created = append(e.created, t)
}
func (e *events) EmitTenantDisabled(_ context.Context, t id.TenantID) {
	e.disabled = append(e.disabled, t)
}

func TestTenantEventsFireOnCreateAndOnLeavingActive(t *testing.T) {
	ctx := context.Background()
	ev := &events{}
	svc := tenant.NewService(store.NewMemory().Tenants(), tenant.WithEvents(ev))
	tn, err := svc.Create(ctx, &tenant.CreateInput{Name: "Acme", Slug: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	if len(ev.created) != 1 || ev.created[0] != tn.ID {
		t.Fatalf("created events = %v, want one for %s", ev.created, tn.ID)
	}
	step := func(status tenant.Status, wantDisabled int) {
		t.Helper()
		if err := svc.SetStatus(ctx, tn.ID.String(), status); err != nil {
			t.Fatal(err)
		}
		if len(ev.disabled) != wantDisabled {
			t.Fatalf("after SetStatus(%s): %d disabled events, want %d", status, len(ev.disabled), wantDisabled)
		}
	}
	step(tenant.StatusDisabled, 1)  // active -> disabled
	step(tenant.StatusSuspended, 1) // disabled -> suspended: already out of active
	step(tenant.StatusActive, 1)    // back to active
	step(tenant.StatusSuspended, 2) // active -> suspended
	if ev.disabled[0] != tn.ID || ev.disabled[1] != tn.ID {
		t.Fatalf("disabled events = %v", ev.disabled)
	}
}

func TestTenantServiceWorksWithoutEvents(t *testing.T) {
	ctx := context.Background()
	svc := tenant.NewService(store.NewMemory().Tenants())
	tn, err := svc.Create(ctx, &tenant.CreateInput{Name: "Acme", Slug: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.SetStatus(ctx, tn.ID.String(), tenant.StatusDisabled); err != nil {
		t.Fatal(err)
	}
}

func TestTheModelListsAreTrimmedAndChecked(t *testing.T) {
	ctx := context.Background()
	svc := tenant.NewService(store.NewMemory().Tenants())

	tn, err := svc.Create(ctx, &tenant.CreateInput{Name: "Acme", Slug: "acme", Config: &tenant.Config{
		AllowedModels: []string{" gpt-4o ", "o1"},
		BlockedModels: []string{"  gpt-3.5-turbo"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got := tn.Config.AllowedModels; len(got) != 2 || got[0] != "gpt-4o" || got[1] != "o1" {
		t.Fatalf("allowed = %q; want the names trimmed", got)
	}
	if got := tn.Config.BlockedModels; len(got) != 1 || got[0] != "gpt-3.5-turbo" {
		t.Fatalf("blocked = %q; want the name trimmed", got)
	}

	updated, err := svc.Update(ctx, tn.ID.String(), &tenant.UpdateInput{Config: &tenant.Config{AllowedModels: []string{" o1 "}}})
	if err != nil {
		t.Fatal(err)
	}
	if got := updated.Config.AllowedModels; len(got) != 1 || got[0] != "o1" {
		t.Fatalf("updated allowed = %q; want the name trimmed", got)
	}

	bad := map[string]*tenant.Config{
		"an empty allowed name":   {AllowedModels: []string{"gpt-4o", ""}},
		"a blank blocked name":    {BlockedModels: []string{"   "}},
		"a name in both lists":    {AllowedModels: []string{"gpt-4o"}, BlockedModels: []string{"gpt-4o"}},
		"both lists after a trim": {AllowedModels: []string{" gpt-4o"}, BlockedModels: []string{"gpt-4o "}},
	}
	for name, cfg := range bad {
		_, err = svc.Create(ctx, &tenant.CreateInput{Name: "Bad", Slug: "bad", Config: cfg})
		if !errors.Is(err, tenant.ErrInvalid) {
			t.Fatalf("create with %s = %v; want ErrInvalid", name, err)
		}
		_, err = svc.Update(ctx, tn.ID.String(), &tenant.UpdateInput{Config: cfg})
		if !errors.Is(err, tenant.ErrInvalid) {
			t.Fatalf("update with %s = %v; want ErrInvalid", name, err)
		}
	}
	kept, err := svc.Get(ctx, tn.ID.String())
	if err != nil || len(kept.Config.AllowedModels) != 1 || kept.Config.AllowedModels[0] != "o1" {
		t.Fatalf("after refused updates: %+v, %v; want the config unchanged", kept, err)
	}
}
