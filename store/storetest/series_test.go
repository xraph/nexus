package storetest_test

import (
	"context"
	"testing"
	"time"

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
