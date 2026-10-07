package mongo_test

import (
	"context"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/xraph/nexus/id"
	mongostore "github.com/xraph/nexus/store/mongo"
	"github.com/xraph/nexus/store/storetest"
)

// The driver wrote tenant.Quota with its default codec: lowercased field
// names and the budget as a double. Those documents must still load.
func TestLegacyTenantDocumentReadsExactly(t *testing.T) {
	ctx := context.Background()
	db, name := storetest.OpenMongoDB(t)
	s := mongostore.New(db)
	if err := s.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	tid := id.NewTenantID().String()
	now := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	_, err := storetest.MongoClient(t).Database(name).Collection("nexus_tenants").InsertOne(ctx, bson.M{
		"_id": tid, "name": "Legacy", "slug": "legacy", "status": "active",
		"quota":  bson.M{"rpm": 60, "tpm": 0, "dailyrequests": 0, "monthlybudgetusd": 12.5, "maxtokensperreq": 0},
		"config": bson.M{}, "created_at": now, "updated_at": now,
	})
	if err != nil {
		t.Fatalf("insert legacy tenant: %v", err)
	}
	got, err := s.Tenants().FindByID(ctx, tid)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if got.Quota.MonthlyBudgetUSD.String() != "12.5" || got.Quota.RPM != 60 {
		t.Fatalf("legacy quota = %+v", got.Quota)
	}
}
