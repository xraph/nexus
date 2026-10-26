package extension

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/xraph/forge"
	"github.com/xraph/go-utils/log"
	"github.com/xraph/vessel"

	nexus "github.com/xraph/nexus"
	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/store"
)

// okProvider answers every completion.
type okProvider struct{}

func (okProvider) Name() string { return "openai" }
func (okProvider) Capabilities() provider.Capabilities {
	return provider.Capabilities{Chat: true}
}
func (okProvider) Models(context.Context) ([]provider.Model, error) {
	return []provider.Model{{ID: "gpt-4o"}}, nil
}
func (okProvider) Complete(_ context.Context, req *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	return &provider.CompletionResponse{
		Provider: "openai", Model: req.Model,
		Choices: []provider.Choice{{Message: provider.Message{Role: "assistant", Content: "hi"}, FinishReason: "stop"}},
	}, nil
}
func (okProvider) CompleteStream(context.Context, *provider.CompletionRequest) (provider.Stream, error) {
	return nil, provider.ErrNotSupported
}
func (okProvider) Embed(context.Context, *provider.EmbeddingRequest) (*provider.EmbeddingResponse, error) {
	return nil, provider.ErrNotSupported
}
func (okProvider) Healthy(context.Context) bool { return true }

func hello() *provider.CompletionRequest {
	return &provider.CompletionRequest{Model: "gpt-4o", Messages: []provider.Message{{Role: "user", Content: "hi"}}}
}

func TestGatewayFromDIBeforeStartIsTheStartedOne(t *testing.T) {
	app := forge.New(forge.WithAppName("lifecycle"), forge.WithAppLogger(forge.NewNoopLogger()))
	ext := New(WithDatabase(store.NewMemory()), WithProvider(okProvider{}))
	if err := ext.Register(app); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// What another extension sees in its own Register.
	gw, err := vessel.Inject[*nexus.Gateway](app.Container())
	if err != nil || gw == nil {
		t.Fatalf("inject gateway after Register: %v, %v", gw, err)
	}
	if gw != ext.Gateway() {
		t.Fatal("the DI gateway and Extension.Gateway must be the same pointer")
	}
	if _, cerr := gw.Engine().Complete(context.Background(), hello()); !errors.Is(cerr, nexus.ErrNotInitialized) {
		t.Fatalf("before Start: %v, want ErrNotInitialized", cerr)
	}

	if err = ext.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = ext.Stop(context.Background()) })
	if ext.Gateway() != gw {
		t.Fatal("Start must not replace the gateway")
	}
	resp, err := gw.Engine().Complete(context.Background(), hello())
	if err != nil {
		t.Fatalf("after Start: %v", err)
	}
	if resp.Provider != "openai" {
		t.Fatalf("provider = %q, want the fake provider", resp.Provider)
	}
}

func TestDeprecatedRouteSettingsWarn(t *testing.T) {
	warnings := func(t *testing.T, opts ...Option) []log.LogEntry {
		t.Helper()
		logger := log.NewTestLogger().(*log.TestLogger)
		app := forge.New(forge.WithAppName("deprecated"), forge.WithAppLogger(logger))
		e := New(append([]Option{WithDatabase(store.NewMemory())}, opts...)...)
		if err := e.Register(app); err != nil {
			t.Fatalf("Register: %v", err)
		}
		var out []log.LogEntry
		for _, l := range logger.GetLogs() {
			if strings.Contains(l.Message, "deprecated") {
				out = append(out, l)
			}
		}
		return out
	}

	got := warnings(t, WithDisableRoutes())
	if len(got) != 1 || !strings.Contains(got[0].Message, "disable_routes") {
		t.Fatalf("WithDisableRoutes: warnings = %+v, want exactly one naming disable_routes", got)
	}
	got = warnings(t, WithBasePath("/ai"))
	if len(got) != 1 || !strings.Contains(got[0].Message, "base_path") {
		t.Fatalf("WithBasePath: warnings = %+v, want exactly one naming base_path", got)
	}
	if got = warnings(t); len(got) != 0 {
		t.Fatalf("defaults must not warn, got %+v", got)
	}
}

func TestHealthNotReadyUntilStart(t *testing.T) {
	app := forge.New(forge.WithAppName("health"), forge.WithAppLogger(forge.NewNoopLogger()))
	ext := New(WithDatabase(store.NewMemory()), WithProvider(okProvider{}))
	ctx := context.Background()
	if err := ext.Health(ctx); err == nil {
		t.Fatal("Health before Register must report not ready")
	}
	if err := ext.Register(app); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := ext.Health(ctx); err == nil {
		t.Fatal("Health after Register but before Start must report not ready")
	}
	if err := ext.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := ext.Health(ctx); err != nil {
		t.Fatalf("Health after Start: %v", err)
	}
	if err := ext.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := ext.Health(ctx); err == nil {
		t.Fatal("Health after Stop must report not ready")
	}
	if err := ext.Start(ctx); err == nil {
		t.Fatal("Start after Stop must fail: a stopped gateway is not restarted")
	}
	if err := ext.Health(ctx); err == nil {
		t.Fatal("Health after a failed restart must report not ready")
	}
}

func TestStopWithoutStartDoesNotPanic(t *testing.T) {
	app := forge.New(forge.WithAppName("stop"), forge.WithAppLogger(forge.NewNoopLogger()))
	ext := New(WithDatabase(store.NewMemory()), WithProvider(okProvider{}))
	if err := ext.Register(app); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := ext.Stop(context.Background()); err != nil {
		t.Fatalf("Stop without Start: %v", err)
	}
}
