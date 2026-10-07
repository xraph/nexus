package extension

import (
	"context"
	"slices"
	"testing"
	"time"

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

type markerLogger struct{ lines int }

func (m *markerLogger) Debug(string, ...any) { m.lines++ }
func (m *markerLogger) Info(string, ...any)  { m.lines++ }
func (m *markerLogger) Warn(string, ...any)  { m.lines++ }
func (m *markerLogger) Error(string, ...any) { m.lines++ }

func TestExplicitGatewayOptionsBeatConfig(t *testing.T) {
	on := true
	mine := &markerLogger{}
	e := New(
		WithGatewayOption(nexus.WithUsageEnabled(false)),
		WithGatewayOption(nexus.WithLogger(mine)),
		WithGatewayOption(nexus.WithTimeout(7*time.Second)),
	)
	e.SetLogger(forge.NewNoopLogger())
	e.config = Config{EnableUsage: &on, LogLevel: "info", DefaultTimeout: time.Second}
	e.applyConfigToGatewayOpts()
	gw := nexus.New(e.gatewayOpts...)
	if err := gw.Initialize(context.Background()); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	for _, s := range gw.PipelineStages() {
		if s.Name == "usage" {
			t.Fatalf("WithGatewayOption(WithUsageEnabled(false)) was overridden by config")
		}
	}
	if gw.Logger() != nexus.Logger(mine) {
		t.Fatalf("WithGatewayOption(WithLogger) was replaced by the config logger: %T", gw.Logger())
	}
	if mine.lines == 0 {
		t.Fatalf("the user's logger received nothing")
	}
	if gw.Config().DefaultTimeout != 7*time.Second {
		t.Fatalf("timeout = %v, the explicit option must win", gw.Config().DefaultTimeout)
	}
}
