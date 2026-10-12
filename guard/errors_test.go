package guard_test

import (
	"context"
	"testing"

	"github.com/xraph/nexus/guard"
	"github.com/xraph/nexus/provider"
)

type blocker struct{ name string }

func (b blocker) Name() string       { return b.name }
func (b blocker) Phase() guard.Phase { return guard.PhaseInput }
func (b blocker) Check(context.Context, *guard.CheckInput) (*guard.CheckResult, error) {
	return &guard.CheckResult{Blocked: true, Action: guard.ActionBlock, Reason: "nope"}, nil
}

func TestABlockNamesItsGuard(t *testing.T) {
	svc := guard.NewService()
	svc.Register(blocker{"pii"})
	r, err := svc.CheckPhase(context.Background(), guard.PhaseInput, &guard.CheckInput{Messages: []provider.Message{{Role: "user", Content: "x"}}})
	if err != nil || !r.Blocked || r.Guard != "pii" {
		t.Fatalf("result %+v, %v", r, err)
	}
	r, err = svc.Check(context.Background(), &guard.CheckInput{})
	if err != nil || r.Guard != "pii" {
		t.Fatalf("Check result %+v, %v", r, err)
	}
	e := &guard.BlockedError{Guard: "pii", Phase: guard.PhaseInput, Reason: "nope"}
	if e.Error() != "nexus: blocked by guard pii: nope" {
		t.Fatalf("Error() = %q", e.Error())
	}
}
