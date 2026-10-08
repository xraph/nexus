package extension

import (
	"context"
	"errors"
	"testing"

	dashboard "github.com/xraph/forge/extensions/dashboard"
	dash "github.com/xraph/forge/extensions/dashboard/contract"
	"github.com/xraph/forge/extensions/dashboard/contract/dispatcher"

	nexus "github.com/xraph/nexus"
)

// Keep the dashboard root out of production imports.
var _ dashboard.ContractContributorAware = (*Extension)(nil)

func TestContractWaitsForSuccessfulStart(t *testing.T) {
	e := New()
	d := dispatcher.New(nil)
	if err := e.RegisterContractContributor(d, dash.NewRegistry(), dash.NewWardenRegistry()); err != nil {
		t.Fatal(err)
	}
	e.gateway = nexus.New()
	if err := e.gateway.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.gateway.Shutdown(context.Background()) })
	req := dash.Request{Contributor: "nexus", Intent: "settings.get", IntentVersion: 1, Kind: dash.KindQuery}
	_, _, err := d.Dispatch(context.Background(), req, dash.Principal{})
	var ce *dash.Error
	if !errors.As(err, &ce) || ce.Code != dash.CodeUnavailable {
		t.Fatal("partially started extension served dashboard reads")
	}
	e.MarkStarted()
	if _, _, err := d.Dispatch(context.Background(), req, dash.Principal{}); err != nil {
		t.Fatal(err)
	}
	e.MarkStopped()
	if _, _, err := d.Dispatch(context.Background(), req, dash.Principal{}); err == nil {
		t.Fatal("stopped extension served dashboard reads")
	}
}
