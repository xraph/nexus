package sqlite_test

import (
	"context"
	"testing"
	"time"

	"github.com/xraph/grove/drivers/sqlitedriver"

	"github.com/xraph/nexus/key"
	sqlitestore "github.com/xraph/nexus/store/sqlite"
	"github.com/xraph/nexus/store/storetest"
)

func TestKeyServiceClocksAndLegacyTimes(t *testing.T) {
	ctx := context.Background()
	db := storetest.OpenSQLiteDB(t)
	s := sqlitestore.New(db)
	if err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	tn := storetest.InsertTenant(t, s)
	svc := key.NewService(s.Keys(), key.WithTenants(s.Tenants()))
	k, raw, err := svc.Create(ctx, &key.CreateInput{TenantID: tn.ID.String(), Name: "Clock"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.Get(ctx, k.ID.String())
	if err != nil {
		t.Fatal(err)
	}
	if !got.CreatedAt.Equal(k.CreatedAt) {
		t.Fatal("created time did not round trip")
	}
	used, err := svc.Validate(ctx, raw)
	if err != nil {
		t.Fatal("key validation failed")
	}
	got, err = svc.Get(ctx, k.ID.String())
	if err != nil {
		t.Fatal(err)
	}
	if got.LastUsedAt == nil || !got.LastUsedAt.Equal(*used.LastUsedAt) {
		t.Fatal("last-used time did not round trip")
	}
	legacy := "2026-10-08 12:14:01.104899 -0500 CDT m=+0.019153876"
	if _, execErr := sqlitedriver.Unwrap(db).Exec(ctx, `UPDATE api_keys SET created_at = ?, last_used_at = ? WHERE id = ?`, legacy, legacy, k.ID.String()); execErr != nil {
		t.Fatal(execErr)
	}
	got, err = svc.Get(ctx, k.ID.String())
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 10, 8, 17, 14, 1, 104899000, time.UTC)
	if !got.CreatedAt.Equal(want) || got.LastUsedAt == nil || !got.LastUsedAt.Equal(want) {
		t.Fatal("legacy key time lost its offset or precision")
	}
}
