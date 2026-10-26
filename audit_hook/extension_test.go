package audit_hook_test

import (
	"context"
	"testing"

	"github.com/xraph/nexus/audit_hook"
	"github.com/xraph/nexus/id"
)

type collector struct{ events []*audit_hook.AuditEvent }

func (c *collector) rec() audit_hook.Recorder {
	return audit_hook.RecorderFunc(func(_ context.Context, ev *audit_hook.AuditEvent) error {
		c.events = append(c.events, ev)
		return nil
	})
}

func TestRecorderGetsAPointerEachTime(t *testing.T) {
	c := &collector{}
	ext := audit_hook.New(c.rec())
	ctx := context.Background()

	if err := ext.OnCircuitOpened(ctx, "openai"); err != nil {
		t.Fatal(err)
	}
	if err := ext.OnKeyRevoked(ctx, id.NewKeyID()); err != nil {
		t.Fatal(err)
	}

	if len(c.events) != 2 {
		t.Fatalf("got %d events, want 2", len(c.events))
	}
	if c.events[0] == c.events[1] {
		t.Fatal("recorder saw the same *AuditEvent twice")
	}
	if c.events[0].Action != audit_hook.ActionCircuitOpened || c.events[1].Action != audit_hook.ActionKeyRevoked {
		t.Fatalf("actions = %q, %q", c.events[0].Action, c.events[1].Action)
	}
	for i, ev := range c.events {
		if ev.Timestamp.IsZero() {
			t.Errorf("event %d has a zero Timestamp", i)
		}
	}
}

func TestTenantIDIsSetWhereKnown(t *testing.T) {
	c := &collector{}
	ext := audit_hook.New(c.rec())
	ctx := context.Background()
	tid := id.NewTenantID()

	if err := ext.OnRequestReceived(ctx, id.NewRequestID(), "m", "tenant_x"); err != nil {
		t.Fatal(err)
	}
	if err := ext.OnBudgetExceeded(ctx, tid); err != nil {
		t.Fatal(err)
	}
	if err := ext.OnKeyCreated(ctx, id.NewKeyID(), tid); err != nil {
		t.Fatal(err)
	}
	if err := ext.OnCircuitOpened(ctx, "openai"); err != nil {
		t.Fatal(err)
	}

	if got := c.events[0].TenantID; got != "tenant_x" {
		t.Errorf("request TenantID = %q, want tenant_x", got)
	}
	if got := c.events[1].TenantID; got != tid.String() {
		t.Errorf("budget TenantID = %q, want %q", got, tid.String())
	}
	if got := c.events[2].TenantID; got != tid.String() {
		t.Errorf("key TenantID = %q, want %q", got, tid.String())
	}
	if got := c.events[3].TenantID; got != "" {
		t.Errorf("provider TenantID = %q, want empty", got)
	}
}

func TestActionFilterStillApplies(t *testing.T) {
	c := &collector{}
	ext := audit_hook.New(c.rec(), audit_hook.WithActions(audit_hook.ActionKeyRevoked))
	ctx := context.Background()

	_ = ext.OnCircuitOpened(ctx, "openai")
	_ = ext.OnBudgetExceeded(ctx, id.NewTenantID())
	_ = ext.OnKeyRevoked(ctx, id.NewKeyID())

	if len(c.events) != 1 || c.events[0].Action != audit_hook.ActionKeyRevoked {
		t.Fatalf("events = %+v, want only key.revoked", c.events)
	}
}
