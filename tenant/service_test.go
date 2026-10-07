package tenant_test

import (
	"context"
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
