package mongo_test

import (
	"context"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/xraph/nexus/id"
	mongostore "github.com/xraph/nexus/store/mongo"
	"github.com/xraph/nexus/store/storetest"
	"github.com/xraph/nexus/usage"
)

func TestLegacyUsageDocumentMigratesToUnpriced(t *testing.T) {
	ctx := context.Background()
	db, name := storetest.OpenMongoDB(t)
	usageID := id.NewUsageID().String()
	_, err := storetest.MongoClient(t).Database(name).Collection("nexus_usage_records").InsertOne(ctx, bson.M{
		"_id": usageID, "tenant_id": "", "key_id": "", "request_id": "",
		"provider": "openai", "model": "gpt-4o", "total_tokens": 10,
		"cost_usd": 0.0, "latency_ns": int64(0), "cached": false, "status_code": 502,
		"created_at": time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("insert legacy document: %v", err)
	}

	s := mongostore.New(db)
	if err := s.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	got := storetest.FindRecord(t, s, id.MustParseUsageID(usageID))
	if got.CostUSD != nil || got.PricingStatus != usage.PricingUnpricedModel || got.Outcome != usage.OutcomeError {
		t.Fatalf("legacy document = cost %v, status %s, outcome %s", got.CostUSD, got.PricingStatus, got.Outcome)
	}
	if !got.TenantID.IsNil() {
		t.Fatalf("legacy empty tenant should read as nil, got %s", got.TenantID)
	}
}
