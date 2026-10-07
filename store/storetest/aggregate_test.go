package storetest_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/money"
	"github.com/xraph/nexus/store"
	"github.com/xraph/nexus/store/storetest"
	"github.com/xraph/nexus/usage"
)

func TestSummaryIsExactAndCountsWhatItLeavesOut(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		ctx := context.Background()
		tn := storetest.InsertTenant(t, s)
		for range 3 {
			storetest.InsertRecord(t, s, storetest.Record(tn.ID, "0.000000150"))
		}
		unpriced := storetest.Record(tn.ID, "")
		unpriced.Provider, unpriced.Model = "local", "llama"
		storetest.InsertRecord(t, s, unpriced)
		hit := storetest.Record(tn.ID, "0")
		hit.Cached, hit.Outcome, hit.PricingStatus = true, usage.OutcomeCached, usage.PricingCached
		storetest.InsertRecord(t, s, hit)

		sum, err := s.Usage().Summary(ctx, tn.ID.String(), "month")
		if err != nil {
			t.Fatalf("summary: %v", err)
		}
		if sum.TotalRequests != 5 || sum.UnpricedRequests != 1 || sum.TotalCostUSD.String() != "0.00000045" {
			t.Fatalf("summary = requests %d, unpriced %d, cost %s", sum.TotalRequests, sum.UnpricedRequests, sum.TotalCostUSD)
		}
		if sum.CacheHitRate != 0.2 || sum.ByOutcome[usage.OutcomeCached] != 1 || sum.ByOutcome[usage.OutcomeOK] != 4 {
			t.Fatalf("cache %v, outcomes %v", sum.CacheHitRate, sum.ByOutcome)
		}
		if sum.AvgLatency != 1500*time.Millisecond {
			t.Fatalf("avg latency = %s", sum.AvgLatency)
		}
		if m := sum.ByModel["llama"]; m == nil || m.Unpriced != 1 {
			t.Fatalf("llama = %+v", m)
		}
	})
}

func TestEmptyTenantMeansEveryTenantAndNoOneElse(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		ctx := context.Background()
		a, b := storetest.InsertTenant(t, s), storetest.InsertTenant(t, s)
		storetest.InsertRecord(t, s, storetest.Record(a.ID, "1.10"))
		storetest.InsertRecord(t, s, storetest.Record(b.ID, "2.20"))
		storetest.InsertRecord(t, s, storetest.Record(id.Nil, "3.30"))

		all, err := s.Usage().MonthlySpend(ctx, "")
		if err != nil || all.String() != "6.6" {
			t.Fatalf("every tenant spend = %s, %v; want 6.6", all, err)
		}
		onlyA, err := s.Usage().MonthlySpend(ctx, a.ID.String())
		if err != nil || onlyA.String() != "1.1" {
			t.Fatalf("tenant A spend = %s, %v; want 1.1", onlyA, err)
		}
		sumAll, err := s.Usage().Summary(ctx, "", "day")
		if err != nil || sumAll.TotalRequests != 3 {
			t.Fatalf("every tenant summary = %v, %v", sumAll, err)
		}
		reqA, err := s.Usage().DailyRequests(ctx, a.ID.String())
		if err != nil || reqA != 1 {
			t.Fatalf("tenant A daily requests = %d, %v", reqA, err)
		}
	})
}

func TestPeriodsStartWhereTheyShould(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		ctx := context.Background()
		tn := storetest.InsertTenant(t, s)
		now := time.Now()
		dayStart, _ := usage.PeriodStart("day", now)
		monthStart, _ := usage.PeriodStart("month", now)

		before := storetest.Record(tn.ID, "5")
		before.CreatedAt = dayStart.Add(-time.Second)
		at := storetest.Record(tn.ID, "7")
		at.CreatedAt = dayStart
		old := storetest.Record(tn.ID, "11")
		old.CreatedAt = monthStart.Add(-time.Second)
		for _, r := range []*usage.Record{before, at, old} {
			storetest.InsertRecord(t, s, r)
		}

		reqs, err := s.Usage().DailyRequests(ctx, tn.ID.String())
		if err != nil || reqs != 1 {
			t.Fatalf("daily requests = %d, %v; want only the record at midnight", reqs, err)
		}
		spend, err := s.Usage().MonthlySpend(ctx, tn.ID.String())
		want := money.MustParse("7")
		if !before.CreatedAt.Before(monthStart) {
			want = want.Add(money.MustParse("5")) // yesterday is still this month
		}
		if err != nil || !spend.Equal(want) {
			t.Fatalf("month spend = %s, %v; want %s", spend, err, want)
		}
	})
}

func TestManySubCentCostsSumExactly(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		tn := storetest.InsertTenant(t, s)
		for range 1000 {
			storetest.InsertRecord(t, s, storetest.Record(tn.ID, "0.000000150"))
		}
		spend, err := s.Usage().MonthlySpend(context.Background(), tn.ID.String())
		if err != nil || spend.String() != "0.00015" {
			t.Fatalf("1000 × 0.000000150 = %s, %v; want exactly 0.00015", spend, err)
		}
	})
}

func TestAnUnknownPeriodIsRefused(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		if _, err := s.Usage().Summary(context.Background(), "", "year"); !errors.Is(err, usage.ErrInvalidPeriod) {
			t.Fatalf("summary over a year = %v, want ErrInvalidPeriod", err)
		}
	})
}
