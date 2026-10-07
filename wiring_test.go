package nexus_test

import (
	"context"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	nexus "github.com/xraph/nexus"
	"github.com/xraph/nexus/model"
	"github.com/xraph/nexus/pipeline"
	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/router/strategies"
	"github.com/xraph/nexus/store"
	"github.com/xraph/nexus/usage"
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

// gatedUsage is a usage service whose Record blocks until release is closed.
type gatedUsage struct {
	usage.Service
	started chan struct{}
	release chan struct{}
	landed  atomic.Int32
}

func newGatedUsage() *gatedUsage {
	return &gatedUsage{started: make(chan struct{}, 8), release: make(chan struct{})}
}

func (g *gatedUsage) Record(context.Context, *usage.Record) error {
	g.started <- struct{}{}
	<-g.release
	g.landed.Add(1)
	return nil
}

// oneRequest sends a request that fails (no provider is registered). The
// usage stage records failures too, so exactly one record is in flight.
func oneRequest(t *testing.T, gw *nexus.Gateway, g *gatedUsage) {
	t.Helper()
	_, _ = gw.Engine().Complete(context.Background(), &provider.CompletionRequest{Model: "gpt-4o"})
	select {
	case <-g.started:
	case <-time.After(5 * time.Second):
		t.Fatalf("the usage record never started")
	}
}

func TestShutdownWaitsForAnInFlightUsageInsert(t *testing.T) {
	g := newGatedUsage()
	gw := nexus.New(nexus.WithUsageService(g))
	if err := gw.Initialize(context.Background()); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	oneRequest(t, gw, g)

	done := make(chan error, 1)
	go func() { done <- gw.Shutdown(context.Background()) }()
	select {
	case err := <-done:
		t.Fatalf("Shutdown returned (%v) while an insert was still in flight", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(g.release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("Shutdown did not return after the insert finished")
	}
	if g.landed.Load() != 1 {
		t.Fatalf("the record had not landed when Shutdown returned")
	}
}

func TestShutdownIsBoundedByItsContext(t *testing.T) {
	g := newGatedUsage()
	gw := nexus.New(nexus.WithUsageService(g))
	if err := gw.Initialize(context.Background()); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	oneRequest(t, gw, g)
	defer close(g.release)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	done := make(chan struct{})
	go func() {
		_ = gw.Shutdown(ctx)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("Shutdown hung on a stuck insert past its context")
	}
}

// bare is a pipeline with no usage stage.
type bare struct{}

func (bare) Execute(context.Context, *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	return nil, nil //nolint:nilnil // not used
}
func (bare) ExecuteStream(context.Context, *provider.CompletionRequest) (provider.Stream, error) {
	return nil, nil //nolint:nilnil // not used
}
func (bare) ExecuteEmbedding(context.Context, *provider.EmbeddingRequest) (*provider.EmbeddingResponse, error) {
	return nil, nil //nolint:nilnil // not used
}

var _ pipeline.Service = bare{}

func TestShutdownWithACustomPipelineIsClean(t *testing.T) {
	gw := nexus.New(nexus.WithPipeline(bare{}))
	if err := gw.Initialize(context.Background()); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if gw.PipelineStages() != nil || gw.UsageInsertErrors() != 0 {
		t.Fatalf("a custom pipeline lists no stages and has no insert errors")
	}
	if err := gw.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}
