package nexus_test

import (
	"context"
	"slices"
	"testing"

	nexus "github.com/xraph/nexus"
	"github.com/xraph/nexus/model"
	"github.com/xraph/nexus/router/strategies"
	"github.com/xraph/nexus/store"
)

func stageNames(gw *nexus.Gateway) []string {
	stages := gw.PipelineStages()
	names := make([]string, 0, len(stages))
	for _, s := range stages {
		names = append(names, s.Name)
	}
	return names
}

func TestInitializeBuildsTheDeclaredServices(t *testing.T) {
	gw := nexus.New(nexus.WithDatabase(store.NewMemory()))
	if err := gw.Initialize(context.Background()); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if gw.Tenants() == nil || gw.Keys() == nil || gw.Usage() == nil {
		t.Fatalf("tenant %v, key %v, usage %v: all must be built", gw.Tenants(), gw.Keys(), gw.Usage())
	}
	want := []string{"request_id", "usage", "timeout", "identity", "stream_lifecycle", "retry", "provider_call"}
	if got := stageNames(gw); !slices.Equal(got, want) {
		t.Fatalf("stages %v, want %v", got, want)
	}
	if gw.RoutingStrategy() != "priority" || gw.PriceBook() == nil {
		t.Fatalf("strategy %q, price book %v", gw.RoutingStrategy(), gw.PriceBook())
	}
}

func TestUsageCanBeTurnedOff(t *testing.T) {
	gw := nexus.New(nexus.WithUsageEnabled(false))
	if err := gw.Initialize(context.Background()); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if slices.Contains(stageNames(gw), "usage") {
		t.Fatalf("usage stage present with usage disabled: %v", stageNames(gw))
	}
	if gw.Usage() == nil {
		t.Fatalf("the usage service is still built for reading")
	}
}

func TestEnableCacheBuildsAMemoryCacheAndAccessorsReport(t *testing.T) {
	gw := nexus.New(
		nexus.WithCacheEnabled(true),
		nexus.WithRouter(strategies.NewRoundRobin()),
		nexus.WithAlias("fast", model.AliasTarget{Provider: "openai", Model: "gpt-4o-mini"}),
	)
	if err := gw.Initialize(context.Background()); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if gw.Cache() == nil || !slices.Contains(stageNames(gw), "cache") {
		t.Fatalf("enable_cache must build a cache stage: %v", stageNames(gw))
	}
	if gw.RoutingStrategy() != strategies.NewRoundRobin().Name() {
		t.Fatalf("strategy = %q", gw.RoutingStrategy())
	}
	if len(gw.Aliases()) != 1 || gw.Aliases()[0].Name != "fast" {
		t.Fatalf("aliases = %+v", gw.Aliases())
	}
}

func TestLevelLoggerDropsBelowItsLevel(t *testing.T) {
	var got []string
	l := nexus.NewLevelLogger(recordLogger{&got}, "warn")
	l.Debug("d")
	l.Info("i")
	l.Warn("w")
	l.Error("e")
	if !slices.Equal(got, []string{"w", "e"}) {
		t.Fatalf("logged %v", got)
	}
}

type recordLogger struct{ got *[]string }

func (r recordLogger) Debug(m string, _ ...any) { *r.got = append(*r.got, m) }
func (r recordLogger) Info(m string, _ ...any)  { *r.got = append(*r.got, m) }
func (r recordLogger) Warn(m string, _ ...any)  { *r.got = append(*r.got, m) }
func (r recordLogger) Error(m string, _ ...any) { *r.got = append(*r.got, m) }
