package extension

import (
	"context"
	"slices"
	"testing"

	"github.com/xraph/forge"

	nexus "github.com/xraph/nexus"
)

func TestConfigReachesTheGateway(t *testing.T) {
	off := false
	e := New()
	e.SetLogger(forge.NewNoopLogger())
	e.config = Config{EnableUsage: &off, EnableCache: true, LogLevel: "warn"}
	e.applyConfigToGatewayOpts()
	gw := nexus.New(e.gatewayOpts...)
	if err := gw.Initialize(context.Background()); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	stages := gw.PipelineStages()
	names := make([]string, 0, len(stages))
	for _, s := range stages {
		names = append(names, s.Name)
	}
	if slices.Contains(names, "usage") || !slices.Contains(names, "cache") {
		t.Fatalf("enable_usage=false and enable_cache=true must reach the gateway: %v", names)
	}
	if gw.Config().LogLevel != "warn" {
		t.Fatalf("log level = %q", gw.Config().LogLevel)
	}
}
