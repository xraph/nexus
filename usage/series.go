package usage

import (
	"errors"
	"fmt"
	"time"

	"github.com/xraph/nexus/money"
)

// Bucket is the width of one point in a series.
type Bucket string

const (
	BucketHour Bucket = "hour"
	BucketDay  Bucket = "day"
)

// maxBuckets bounds a series so a careless range cannot ask a store for
// months of hourly points.
const maxBuckets = 1000

// ErrInvalidSeries reports a range or bucket a series cannot be built for.
var ErrInvalidSeries = errors.New("nexus: invalid series")

// SeriesOptions asks for usage between Start and End (exclusive), one point
// per bucket. An empty TenantID means every tenant.
//
// Start is rounded down to the start of its bucket, so every point covers its
// whole bucket and its label is true. End is used as given: when it is not on
// a bucket boundary the last bucket covers usage up to End only.
type SeriesOptions struct {
	TenantID string    `json:"tenant_id,omitempty"`
	Start    time.Time `json:"start"`
	End      time.Time `json:"end"`
	Bucket   Bucket    `json:"bucket"`
}

// SeriesPoint is usage within one bucket. CostUSD sums priced requests;
// Unpriced counts the requests it leaves out.
type SeriesPoint struct {
	Start    time.Time `json:"start"`
	Requests int       `json:"requests"`
	Tokens   int       `json:"tokens"`
	CostUSD  money.USD `json:"cost_usd"`
	Unpriced int       `json:"unpriced"`
}

// BucketStart truncates t, in UTC, to the start of its bucket.
func BucketStart(t time.Time, b Bucket) time.Time {
	t = t.UTC()
	if b == BucketDay {
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	}
	return t.Truncate(time.Hour)
}

func step(b Bucket) time.Duration {
	if b == BucketDay {
		return 24 * time.Hour
	}
	return time.Hour
}

// FillSeries merges the points a store found (possibly one per record) into
// one point per bucket from Start to End, oldest first, with empty buckets
// present as zeros.
func FillSeries(opts *SeriesOptions, points []SeriesPoint) ([]SeriesPoint, error) {
	if opts == nil {
		return nil, fmt.Errorf("%w: no options", ErrInvalidSeries)
	}
	if opts.Bucket != BucketHour && opts.Bucket != BucketDay {
		return nil, fmt.Errorf("%w: bucket %q", ErrInvalidSeries, opts.Bucket)
	}
	if !opts.End.After(opts.Start) {
		return nil, fmt.Errorf("%w: end must be after start", ErrInvalidSeries)
	}
	first, end := BucketStart(opts.Start, opts.Bucket), opts.End.UTC()
	// Sub saturates on a very wide range, so compare against the cap before
	// doing any arithmetic that could overflow a Duration.
	width := end.Sub(first)
	if width > time.Duration(maxBuckets)*step(opts.Bucket) {
		return nil, fmt.Errorf("%w: more than %d buckets", ErrInvalidSeries, maxBuckets)
	}
	n := int((width + step(opts.Bucket) - 1) / step(opts.Bucket))
	out := make([]SeriesPoint, n)
	for i := range out {
		out[i].Start = first.Add(time.Duration(i) * step(opts.Bucket))
	}
	for _, p := range points {
		i := int(BucketStart(p.Start, opts.Bucket).Sub(first) / step(opts.Bucket))
		if i < 0 || i >= n {
			continue
		}
		out[i].Requests += p.Requests
		out[i].Tokens += p.Tokens
		out[i].CostUSD = out[i].CostUSD.Add(p.CostUSD)
		out[i].Unpriced += p.Unpriced
	}
	return out, nil
}
