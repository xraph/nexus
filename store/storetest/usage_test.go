package storetest_test

import (
	"testing"

	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/store"
	"github.com/xraph/nexus/store/storetest"
	"github.com/xraph/nexus/usage"
)

func TestUsageRecordRoundTripsEveryField(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		tn := storetest.InsertTenant(t, s)
		want := storetest.Record(tn.ID, "0.000024975")
		want.Outcome, want.BlockedBy, want.StatusCode = usage.OutcomeBlocked, "pii", 400
		storetest.InsertRecord(t, s, want)
		storetest.SameRecord(t, storetest.FindRecord(t, s, want.ID), want)

		refused := storetest.Record(tn.ID, "")
		refused.Outcome, refused.RefusalCode, refused.StatusCode = usage.OutcomeRefused, "budget_exceeded", 429
		storetest.InsertRecord(t, s, refused)
		storetest.SameRecord(t, storetest.FindRecord(t, s, refused.ID), refused)
	})
}

func TestUnpricedCostStaysUnknown(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		tn := storetest.InsertTenant(t, s)
		r := storetest.Record(tn.ID, "")
		storetest.InsertRecord(t, s, r)
		got := storetest.FindRecord(t, s, r.ID)
		if got.CostUSD != nil || got.PricingStatus != usage.PricingUnpricedModel {
			t.Fatalf("unpriced record came back as cost %v, status %s", got.CostUSD, got.PricingStatus)
		}
	})
}

func TestUnattributedRecordRoundTrips(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		r := storetest.Record(id.Nil, "0.01")
		r.KeyID, r.RequestID = id.Nil, id.Nil
		storetest.InsertRecord(t, s, r)
		got := storetest.FindRecord(t, s, r.ID)
		if !got.TenantID.IsNil() || !got.KeyID.IsNil() || !got.RequestID.IsNil() {
			t.Fatalf("unattributed record came back attributed: %+v", got)
		}
		storetest.SameRecord(t, got, r)
	})
}
