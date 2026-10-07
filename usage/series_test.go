package usage_test

import (
	"errors"
	"testing"
	"time"

	"github.com/xraph/nexus/money"
	"github.com/xraph/nexus/usage"
)

func TestFillSeriesMergesAndZeroFills(t *testing.T) {
	start := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	opts := &usage.SeriesOptions{Start: start, End: start.Add(3 * time.Hour), Bucket: usage.BucketHour}
	got, err := usage.FillSeries(opts, []usage.SeriesPoint{
		{Start: start.Add(2 * time.Hour), Requests: 1, Tokens: 10, CostUSD: money.MustParse("0.1")},
		{Start: start, Requests: 1, Tokens: 5, CostUSD: money.MustParse("0.2"), Unpriced: 0},
		{Start: start, Requests: 1, Tokens: 5, Unpriced: 1},
	})
	if err != nil || len(got) != 3 {
		t.Fatalf("FillSeries = %v, %v", got, err)
	}
	if got[0].Requests != 2 || got[0].Tokens != 10 || got[0].CostUSD.String() != "0.2" || got[0].Unpriced != 1 {
		t.Fatalf("first bucket = %+v", got[0])
	}
	if !got[1].Start.Equal(start.Add(time.Hour)) || got[1].Requests != 0 || !got[1].CostUSD.IsZero() {
		t.Fatalf("empty bucket = %+v", got[1])
	}
	if got[2].CostUSD.String() != "0.1" {
		t.Fatalf("last bucket = %+v", got[2])
	}
}

func TestSeriesOptionsAreChecked(t *testing.T) {
	start := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	for _, o := range []*usage.SeriesOptions{
		{Start: start, End: start, Bucket: usage.BucketHour},
		{Start: start, End: start.Add(time.Hour), Bucket: "minute"},
		{Start: start, End: start.Add(1001 * time.Hour), Bucket: usage.BucketHour},
	} {
		if _, err := usage.FillSeries(o, nil); !errors.Is(err, usage.ErrInvalidSeries) {
			t.Errorf("%+v = %v, want ErrInvalidSeries", o, err)
		}
	}
	if got := usage.BucketStart(start.Add(90*time.Minute+5*time.Second), usage.BucketHour); !got.Equal(start.Add(time.Hour)) {
		t.Fatalf("BucketStart hour = %s", got)
	}
}
