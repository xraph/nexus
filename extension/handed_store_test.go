package extension

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/xraph/forge"
	"github.com/xraph/grove"
	"github.com/xraph/vessel"

	"github.com/xraph/nexus/store"
	"github.com/xraph/nexus/store/storetest"
)

// closeSpy counts Close calls on the store it wraps.
type closeSpy struct {
	store.Store
	closed atomic.Int32
}

func (s *closeSpy) Close() error {
	s.closed.Add(1)
	return s.Store.Close()
}

func TestStopLeavesAHandedStoreOpen(t *testing.T) {
	spy := &closeSpy{Store: store.NewMemory()}
	app := forge.New(forge.WithAppName("handed"), forge.WithAppLogger(forge.NewNoopLogger()))
	ext := New(WithDatabase(spy))
	ctx := context.Background()
	if err := ext.Register(app); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := ext.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := ext.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if n := spy.closed.Load(); n != 0 {
		t.Fatalf("Stop closed the handed store %d time(s), want 0", n)
	}
}

func TestStopLeavesAGroveDatabaseFromTheContainerOpen(t *testing.T) {
	db := storetest.OpenSQLiteDB(t)
	app := forge.New(forge.WithAppName("handed-grove"), forge.WithAppLogger(forge.NewNoopLogger()))
	if err := vessel.Provide(app.Container(), func() (*grove.DB, error) { return db, nil }); err != nil {
		t.Fatalf("provide grove.DB: %v", err)
	}
	ext := New()
	ctx := context.Background()
	if err := ext.Register(app); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := ext.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := ext.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := db.Ping(ctx); err != nil {
		t.Fatalf("the host's database is unusable after Stop: %v", err)
	}
}
