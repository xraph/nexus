package storetest_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/store"
	"github.com/xraph/nexus/store/storetest"
	"github.com/xraph/nexus/usage"
)

func TestSeriesBucketsExactlyAndFillsGaps(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		tn := storetest.InsertTenant(t, s)
		start := usage.BucketStart(time.Now().Add(-5*time.Hour), usage.BucketHour)
		for _, r := range []struct {
			at   time.Duration
			cost string
		}{{10 * time.Minute, "0.000000150"}, {50 * time.Minute, "0.000000150"}, {3*time.Hour + time.Minute, ""}} {
			rec := storetest.Record(tn.ID, r.cost)
			rec.CreatedAt = start.Add(r.at)
			storetest.InsertRecord(t, s, rec)
		}
		other := storetest.Record(storetest.InsertTenant(t, s).ID, "9")
		other.CreatedAt = start.Add(10 * time.Minute)
		storetest.InsertRecord(t, s, other)

		got, err := s.Usage().Series(context.Background(), &usage.SeriesOptions{
			TenantID: tn.ID.String(), Start: start, End: start.Add(4 * time.Hour), Bucket: usage.BucketHour,
		})
		if err != nil || len(got) != 4 {
			t.Fatalf("series = %v, %v", got, err)
		}
		if got[0].Requests != 2 || got[0].CostUSD.String() != "0.0000003" {
			t.Fatalf("first hour = %+v", got[0])
		}
		if got[1].Requests != 0 || got[2].Requests != 0 {
			t.Fatalf("gap hours = %+v, %+v", got[1], got[2])
		}
		if got[3].Requests != 1 || got[3].Unpriced != 1 || !got[3].CostUSD.IsZero() {
			t.Fatalf("fourth hour = %+v", got[3])
		}
	})
}

func insertAt(t *testing.T, s store.Store, tenantID id.TenantID, at time.Time, cost string) {
	t.Helper()
	rec := storetest.Record(tenantID, cost)
	rec.CreatedAt = at
	storetest.InsertRecord(t, s, rec)
}

func TestSeriesDayBucketsAndZeroDay(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		tn := storetest.InsertTenant(t, s)
		day1 := time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)
		insertAt(t, s, tn.ID, day1.Add(23*time.Hour+59*time.Minute), "0.5")
		insertAt(t, s, tn.ID, day1.Add(48*time.Hour+time.Hour), "0.25")
		got, err := s.Usage().Series(context.Background(), &usage.SeriesOptions{
			TenantID: tn.ID.String(), Start: day1, End: day1.Add(72 * time.Hour), Bucket: usage.BucketDay,
		})
		if err != nil || len(got) != 3 {
			t.Fatalf("series = %v, %v", got, err)
		}
		for i, want := range []struct {
			requests int
			cost     string
		}{{1, "0.5"}, {0, "0"}, {1, "0.25"}} {
			if !got[i].Start.Equal(day1.Add(time.Duration(i)*24*time.Hour)) || got[i].Requests != want.requests || got[i].CostUSD.String() != want.cost {
				t.Fatalf("day %d = %+v, want %+v", i, got[i], want)
			}
		}
	})
}

func TestSeriesRangeEdges(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		tn := storetest.InsertTenant(t, s)
		start := time.Date(2026, 3, 10, 8, 0, 0, 0, time.UTC)
		end := start.Add(2 * time.Hour)
		insertAt(t, s, tn.ID, start, "1")                      // exactly at the aligned start: in
		insertAt(t, s, tn.ID, end, "100")                      // exactly at End: out
		insertAt(t, s, tn.ID, start.Add(-time.Second), "1000") // before the range: out
		got, err := s.Usage().Series(context.Background(), &usage.SeriesOptions{
			TenantID: tn.ID.String(), Start: start, End: end, Bucket: usage.BucketHour,
		})
		if err != nil || len(got) != 2 {
			t.Fatalf("series = %v, %v", got, err)
		}
		if got[0].Requests != 1 || got[0].CostUSD.String() != "1" || got[1].Requests != 0 {
			t.Fatalf("buckets = %+v, %+v", got[0], got[1])
		}
	})
}

func TestSeriesUnalignedStartKeepsTheWholeFirstBucket(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		tn := storetest.InsertTenant(t, s)
		start := time.Date(2026, 3, 10, 8, 0, 0, 0, time.UTC)
		insertAt(t, s, tn.ID, start.Add(10*time.Minute), "2")
		got, err := s.Usage().Series(context.Background(), &usage.SeriesOptions{
			TenantID: tn.ID.String(), Start: start.Add(30 * time.Minute), End: start.Add(2 * time.Hour), Bucket: usage.BucketHour,
		})
		if err != nil || len(got) != 2 {
			t.Fatalf("series = %v, %v", got, err)
		}
		if !got[0].Start.Equal(start) || got[0].Requests != 1 || got[0].CostUSD.String() != "2" {
			t.Fatalf("first bucket = %+v", got[0])
		}
	})
}

func TestSeriesEmptyTenantMeansEveryTenant(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		a, b := storetest.InsertTenant(t, s), storetest.InsertTenant(t, s)
		start := time.Date(2026, 3, 10, 8, 0, 0, 0, time.UTC)
		for _, tid := range []id.TenantID{a.ID, b.ID, id.Nil} {
			insertAt(t, s, tid, start.Add(5*time.Minute), "0.1")
		}
		got, err := s.Usage().Series(context.Background(), &usage.SeriesOptions{
			Start: start, End: start.Add(time.Hour), Bucket: usage.BucketHour,
		})
		if err != nil || len(got) != 1 {
			t.Fatalf("series = %v, %v", got, err)
		}
		if got[0].Requests != 3 || got[0].CostUSD.String() != "0.3" {
			t.Fatalf("all tenants = %+v", got[0])
		}
	})
}

func TestSeriesMixesPricedAndUnpriced(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		tn := storetest.InsertTenant(t, s)
		start := time.Date(2026, 3, 10, 8, 0, 0, 0, time.UTC)
		for i, cost := range []string{"0.1", "", "0.2", ""} {
			insertAt(t, s, tn.ID, start.Add(time.Duration(i+1)*time.Minute), cost)
		}
		got, err := s.Usage().Series(context.Background(), &usage.SeriesOptions{
			TenantID: tn.ID.String(), Start: start, End: start.Add(time.Hour), Bucket: usage.BucketHour,
		})
		if err != nil || len(got) != 1 {
			t.Fatalf("series = %v, %v", got, err)
		}
		if got[0].Requests != 4 || got[0].Tokens != 600 || got[0].Unpriced != 2 || got[0].CostUSD.String() != "0.3" {
			t.Fatalf("mixed bucket = %+v", got[0])
		}
	})
}

func TestSeriesChecksOptionsOnEveryBackend(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		start := time.Date(2026, 3, 10, 8, 30, 0, 0, time.UTC)
		for _, o := range []*usage.SeriesOptions{
			nil,
			{Start: start, End: start, Bucket: usage.BucketHour},
			{Start: start, End: start.Add(-20 * time.Minute), Bucket: usage.BucketHour},
			{Start: start, End: start.Add(time.Hour), Bucket: "minute"},
			{Start: time.Time{}, End: time.Now(), Bucket: usage.BucketHour},
		} {
			if _, err := s.Usage().Series(context.Background(), o); !errors.Is(err, usage.ErrInvalidSeries) {
				t.Errorf("%+v = %v, want ErrInvalidSeries", o, err)
			}
		}
	})
}

func TestSeriesCountsUnknownCostsAsUnpriced(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		tn := storetest.InsertTenant(t, s)
		start := time.Date(2026, 3, 10, 8, 0, 0, 0, time.UTC)
		for i, cost := range []string{"", "", "0.4"} {
			rec := storetest.Record(tn.ID, cost)
			rec.CreatedAt = start.Add(time.Duration(i+1) * time.Minute)
			if cost == "" {
				rec.PricingStatus, rec.Outcome = usage.PricingUnknown, usage.OutcomeError
			}
			storetest.InsertRecord(t, s, rec)
		}
		got, err := s.Usage().Series(context.Background(), &usage.SeriesOptions{
			TenantID: tn.ID.String(), Start: start, End: start.Add(time.Hour), Bucket: usage.BucketHour,
		})
		if err != nil || len(got) != 1 {
			t.Fatalf("series = %v, %v", got, err)
		}
		if got[0].Requests != 3 || got[0].Unpriced != 2 || got[0].CostUSD.String() != "0.4" {
			t.Fatalf("bucket = %+v", got[0])
		}
	})
}
