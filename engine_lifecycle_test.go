package nexus_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	nexus "github.com/xraph/nexus"
	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/store"
)

func TestEngineBeforeInitializeReturnsErrNotInitialized(t *testing.T) {
	gw := nexus.New(nexus.WithDatabase(store.NewMemory()), nexus.WithProvider(&fakeProvider{name: "openai", price: listPrice}))
	eng := gw.Engine()
	if eng == nil {
		t.Fatal("Engine must be non-nil from New")
	}
	ctx := context.Background()
	req := &provider.CompletionRequest{Model: "gpt-4o", Messages: []provider.Message{{Role: "user", Content: "hi"}}}
	if _, err := eng.Complete(ctx, req); !errors.Is(err, nexus.ErrNotInitialized) {
		t.Fatalf("Complete: %v, want ErrNotInitialized", err)
	}
	if _, err := eng.CompleteStream(ctx, req); !errors.Is(err, nexus.ErrNotInitialized) {
		t.Fatalf("CompleteStream: %v, want ErrNotInitialized", err)
	}
	if _, err := eng.Embed(ctx, &provider.EmbeddingRequest{Model: "embed-small"}); !errors.Is(err, nexus.ErrNotInitialized) {
		t.Fatalf("Embed: %v, want ErrNotInitialized", err)
	}

	if err := gw.Initialize(ctx); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	t.Cleanup(func() { _ = gw.Shutdown(context.Background()) })
	if gw.Engine() != eng {
		t.Fatal("Initialize must keep the engine New returned")
	}
	if _, err := eng.Complete(ctx, req); err != nil {
		t.Fatalf("Complete after Initialize: %v", err)
	}
}

func TestEngineIsSafeDuringInitialize(t *testing.T) {
	gw := nexus.New(nexus.WithDatabase(store.NewMemory()), nexus.WithProvider(&fakeProvider{name: "openai", price: listPrice}))
	req := &provider.CompletionRequest{Model: "gpt-4o", Messages: []provider.Message{{Role: "user", Content: "hi"}}}

	stop := make(chan struct{})
	var wg sync.WaitGroup
	bad := make(chan error, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			if _, err := gw.Engine().Complete(context.Background(), req); err != nil && !errors.Is(err, nexus.ErrNotInitialized) {
				select {
				case bad <- err:
				default:
				}
				return
			}
		}
	}()

	if err := gw.Initialize(context.Background()); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	t.Cleanup(func() { _ = gw.Shutdown(context.Background()) })
	close(stop)
	wg.Wait()
	select {
	case err := <-bad:
		t.Fatalf("Complete during Initialize: %v, want ErrNotInitialized or success", err)
	default:
	}
}

func TestGatewayInitializedTracksLifecycle(t *testing.T) {
	gw := nexus.New(nexus.WithDatabase(store.NewMemory()), nexus.WithProvider(&fakeProvider{name: "openai", price: listPrice}))
	if gw.Initialized() {
		t.Fatal("Initialized must be false before Initialize")
	}
	ctx := context.Background()
	if err := gw.Initialize(ctx); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if !gw.Initialized() {
		t.Fatal("Initialized must be true after Initialize")
	}
	if err := gw.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if gw.Initialized() {
		t.Fatal("Initialized must be false after Shutdown")
	}
	req := &provider.CompletionRequest{Model: "gpt-4o", Messages: []provider.Message{{Role: "user", Content: "hi"}}}
	if _, err := gw.Engine().Complete(ctx, req); !errors.Is(err, nexus.ErrNotInitialized) {
		t.Fatalf("Complete after Shutdown: %v, want ErrNotInitialized", err)
	}
}

func TestShutdownWithoutInitializeDoesNotPanic(t *testing.T) {
	gw := nexus.New(nexus.WithDatabase(store.NewMemory()))
	if err := gw.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown before Initialize: %v", err)
	}
	if gw.Initialized() {
		t.Fatal("Initialized must stay false")
	}
}

func TestInitializeAfterShutdownFails(t *testing.T) {
	gw := nexus.New(nexus.WithDatabase(store.NewMemory()), nexus.WithProvider(&fakeProvider{name: "openai", price: listPrice}))
	ctx := context.Background()
	if err := gw.Initialize(ctx); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if err := gw.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	err := gw.Initialize(ctx)
	if !errors.Is(err, nexus.ErrNotInitialized) {
		t.Fatalf("Initialize after Shutdown = %v, want an error wrapping ErrNotInitialized", err)
	}
	if gw.Initialized() {
		t.Fatal("a failed Initialize must leave the gateway not ready")
	}

	never := nexus.New(nexus.WithDatabase(store.NewMemory()))
	if err := never.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown before Initialize: %v", err)
	}
	if err := never.Initialize(ctx); !errors.Is(err, nexus.ErrNotInitialized) {
		t.Fatalf("Initialize after a Shutdown that closed the store = %v, want ErrNotInitialized", err)
	}
}

func TestGatewayHealthFollowsTheLifecycle(t *testing.T) {
	gw := nexus.New(nexus.WithDatabase(store.NewMemory()), nexus.WithProvider(&fakeProvider{name: "openai", price: listPrice}))
	ctx := context.Background()
	if err := gw.Health(ctx); !errors.Is(err, nexus.ErrNotInitialized) {
		t.Fatalf("Health before Initialize = %v, want ErrNotInitialized", err)
	}
	if err := gw.Initialize(ctx); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if err := gw.Health(ctx); err != nil {
		t.Fatalf("Health after Initialize = %v, want nil", err)
	}
	if err := gw.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if err := gw.Health(ctx); !errors.Is(err, nexus.ErrNotInitialized) {
		t.Fatalf("Health after Shutdown = %v, want ErrNotInitialized", err)
	}
}
