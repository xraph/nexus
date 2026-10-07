package mongo_test

import (
	"context"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/money"
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

// A cache hit called no provider, so a legacy cached document is exactly $0,
// not unknown.
func TestLegacyCacheHitMigratesToCachedZero(t *testing.T) {
	ctx := context.Background()
	db, name := storetest.OpenMongoDB(t)
	usageID := id.NewUsageID().String()
	_, err := storetest.MongoClient(t).Database(name).Collection("nexus_usage_records").InsertOne(ctx, bson.M{
		"_id": usageID, "tenant_id": "", "key_id": "", "request_id": "",
		"provider": "openai", "model": "gpt-4o", "total_tokens": 10,
		"cost_usd": 0.0, "latency_ns": int64(0), "cached": true, "status_code": 200,
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
	if got.CostUSD == nil || !got.CostUSD.Equal(money.Zero) || got.PricingStatus != usage.PricingCached || got.Outcome != usage.OutcomeCached {
		t.Fatalf("legacy cache hit = cost %v, status %s, outcome %s; want $0, cached, cached", got.CostUSD, got.PricingStatus, got.Outcome)
	}
}

// A binary from before exact money may still be running when the new one
// migrates. It writes no pricing_status and a cost of 0. That document must
// read as unknown, not as a priced $0. With a real cost it was priced.
func TestDocumentFromAnOldBinaryReadsAsUnpriced(t *testing.T) {
	ctx := context.Background()
	db, name := storetest.OpenMongoDB(t)
	s := mongostore.New(db)
	if err := s.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	coll := storetest.MongoClient(t).Database(name).Collection("nexus_usage_records")
	free, paid := id.NewUsageID().String(), id.NewUsageID().String()
	for rid, cost := range map[string]float64{free: 0, paid: 0.25} {
		_, err := coll.InsertOne(ctx, bson.M{
			"_id": rid, "tenant_id": "", "key_id": "", "request_id": "",
			"provider": "openai", "model": "gpt-4o", "total_tokens": 10,
			"cost_usd": cost, "latency_ns": int64(0), "cached": false, "status_code": 200,
			"created_at": time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC),
		})
		if err != nil {
			t.Fatalf("insert as the old binary: %v", err)
		}
	}
	got := storetest.FindRecord(t, s, id.MustParseUsageID(free))
	if got.CostUSD != nil || got.PricingStatus != usage.PricingUnpricedModel {
		t.Fatalf("old binary $0 document = cost %v, status %s; want unknown and unpriced_model", got.CostUSD, got.PricingStatus)
	}
	got = storetest.FindRecord(t, s, id.MustParseUsageID(paid))
	if got.CostUSD == nil || got.CostUSD.String() != "0.25" || got.PricingStatus != usage.PricingPriced {
		t.Fatalf("old binary $0.25 document = cost %v, status %s; want 0.25 and priced", got.CostUSD, got.PricingStatus)
	}
}
