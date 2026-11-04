package nexus

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/xraph/nexus/store"
)

type closeSpy struct {
	store.Store
	closed atomic.Int32
}

func (s *closeSpy) Close() error {
	s.closed.Add(1)
	return s.Store.Close()
}

// A store the gateway built for itself is the gateway's to close.
func TestShutdownClosesTheStoreItOpened(t *testing.T) {
	ctx := context.Background()
	gw := New()
	if err := gw.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	spy := &closeSpy{Store: gw.store}
	gw.store = spy // the default store, observed
	if err := gw.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if n := spy.closed.Load(); n != 1 {
		t.Fatalf("Shutdown closed the gateway's own store %d time(s), want 1", n)
	}
}

// A store handed in with WithDatabase belongs to the caller.
func TestShutdownLeavesAHandedStoreOpen(t *testing.T) {
	ctx := context.Background()
	spy := &closeSpy{Store: store.NewMemory()}
	gw := New(WithDatabase(spy))
	if err := gw.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if err := gw.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if n := spy.closed.Load(); n != 0 {
		t.Fatalf("Shutdown closed the handed store %d time(s), want 0", n)
	}
}
