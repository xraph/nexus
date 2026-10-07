package storetest_test

import (
	"context"
	"testing"

	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/money"
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

// A group of requests none of which could be priced has no cost to sum.
func TestSummaryOfOnlyUnpricedRecords(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		tn := storetest.InsertTenant(t, s)
		storetest.InsertRecord(t, s, storetest.Record(tn.ID, ""))
		sum, err := s.Usage().Summary(context.Background(), tn.ID.String(), "month")
		if err != nil {
			t.Fatalf("summary: %v", err)
		}
		if sum == nil {
			t.Fatalf("summary is nil")
		}
		if !sum.TotalCostUSD.IsZero() {
			t.Fatalf("summary cost = %s, want 0", sum.TotalCostUSD)
		}
	})
}

// MonthlySpend is what budgets and the dashboard read. A tenant with no
// rows, or only unpriced ones, has spent nothing; it must not be an error.
func TestMonthlySpendWithNothingPriced(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		ctx := context.Background()
		empty := storetest.InsertTenant(t, s)
		unpriced := storetest.InsertTenant(t, s)
		storetest.InsertRecord(t, s, storetest.Record(unpriced.ID, ""))
		for name, tid := range map[string]id.TenantID{"no rows": empty.ID, "only unpriced": unpriced.ID} {
			got, err := s.Usage().MonthlySpend(ctx, tid.String())
			if err != nil {
				t.Fatalf("%s: monthly spend: %v", name, err)
			}
			if !got.IsZero() {
				t.Fatalf("%s: monthly spend = %s, want 0", name, got)
			}
		}
	})
}

// Amounts carry at most 18 decimal places, which is what a Postgres
// NUMERIC(38,18) keeps. The smallest one must come back exactly.
func TestSmallestAmountRoundTripsExactly(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		tn := storetest.InsertTenant(t, s)
		want := storetest.Record(tn.ID, "0.000000000000000001")
		storetest.InsertRecord(t, s, want)
		got := storetest.FindRecord(t, s, want.ID)
		if got.CostUSD == nil || got.CostUSD.String() != "0.000000000000000001" {
			t.Fatalf("cost = %v, want 0.000000000000000001", got.CostUSD)
		}
		storetest.SameRecord(t, got, want)
	})
}

// A cost the library computes must be readable on every backend, so a
// computed amount past 18 places cannot become a row that breaks Query.
func TestComputedCostIsReadableEverywhere(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		tn := storetest.InsertTenant(t, s)
		r := storetest.Record(tn.ID, "")
		c := money.MustParse("0.000000000000000009").PerMillion(500_000)
		r.CostUSD, r.PricingStatus = &c, usage.PricingPriced
		storetest.InsertRecord(t, s, r)
		got := storetest.FindRecord(t, s, r.ID)
		if got.CostUSD == nil || got.CostUSD.String() != "0.000000000000000005" {
			t.Fatalf("computed cost came back as %v", got.CostUSD)
		}
	})
}

func TestUnknownAndNotChargedRoundTrip(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		tn := storetest.InsertTenant(t, s)
		unknown := storetest.Record(tn.ID, "")
		unknown.PricingStatus, unknown.Outcome, unknown.StatusCode = usage.PricingUnknown, usage.OutcomeError, 500
		notCharged := storetest.Record(tn.ID, "0")
		notCharged.PricingStatus, notCharged.Outcome, notCharged.BlockedBy = usage.PricingNotCharged, usage.OutcomeBlocked, "pii"
		storetest.InsertRecord(t, s, unknown)
		storetest.InsertRecord(t, s, notCharged)
		storetest.SameRecord(t, storetest.FindRecord(t, s, unknown.ID), unknown)
		storetest.SameRecord(t, storetest.FindRecord(t, s, notCharged.ID), notCharged)
		sum, err := s.Usage().Summary(context.Background(), tn.ID.String(), "day")
		if err != nil || sum.UnpricedRequests != 1 || !sum.TotalCostUSD.IsZero() {
			t.Fatalf("summary = %+v, %v", sum, err)
		}
	})
}
