package telemetry

import (
	"strings"
	"sync"
	"sync/atomic"

	gometrics "github.com/xraph/go-utils/metrics"
)

// Metric names. The gen_ai ones are the OpenTelemetry GenAI client metrics.
// Forge exports names outside its semconv namespaces with its own prefix and
// no unit suffix, so these scrape as forge_gen_ai_client_operation_duration
// and forge_gen_ai_client_token_usage. The nexus ones carry their unit in the
// name and scrape as forge_<name>.
const (
	MetricOperationDuration = "gen_ai.client.operation.duration"
	MetricTokenUsage        = "gen_ai.client.token.usage" // #nosec G101 -- a metric name, not a credential
	MetricFirstChunk        = "nexus_stream_time_to_first_chunk_seconds"
	MetricRequests          = "nexus_requests_total"
	MetricGuardrailBlocks   = "nexus_guardrail_blocks_total"
	MetricCacheLookups      = "nexus_cache_lookups_total"
	MetricUsageInsertErrors = "nexus_usage_insert_errors_total"
	MetricLimiterErrors     = "nexus_limiter_errors_total"
	MetricUsageUnpriced     = "nexus_usage_unpriced_total"
)

// Metric label keys. Forge turns the dots into underscores on the scrape.
const (
	LabelOperation = "gen_ai.operation.name"
	LabelProvider  = "gen_ai.provider.name"
	LabelModel     = "gen_ai.request.model"
	LabelErrorType = "error.type"
	LabelTokenType = "gen_ai.token.type" // #nosec G101 -- a label key, not a credential
	LabelType      = "type"
	LabelOutcome   = "outcome"
	LabelCode      = "code"
	LabelPhase     = "phase"
	LabelGuard     = "guard"
	LabelKind      = "kind"
	LabelResult    = "result"
	// LabelProviderName is the provider's registered name on nexus metrics;
	// the gen_ai ones use LabelProvider.
	LabelProviderName = "provider"
)

// Histogram bounds, in the metric's unit.
var (
	// OperationDurationBuckets are the bounds the GenAI conventions advise
	// for gen_ai.client.operation.duration, in seconds.
	OperationDurationBuckets = []float64{0.01, 0.02, 0.04, 0.08, 0.16, 0.32, 0.64, 1.28, 2.56, 5.12, 10.24, 20.48, 40.96, 81.92}
	// TokenUsageBuckets are the bounds the GenAI conventions advise for
	// gen_ai.client.token.usage, in tokens.
	TokenUsageBuckets = []float64{1, 4, 16, 64, 256, 1024, 4096, 16384, 65536, 262144, 1048576, 4194304, 16777216, 67108864}
	// FirstChunkBuckets span a fast model's first token to a reasoning
	// model's long think, in seconds.
	FirstChunkBuckets = []float64{0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60}
)

// maxCachedSeries bounds the per-family cache of series handles. Past it a
// new label set still records; it just goes through the sink each time. The
// sink's own series cap is what bounds what gets exported.
const maxCachedSeries = 4096

// family declares a metric once with its label keys and caches one handle
// per label-value tuple, so the hot path takes no registry lock.
type family[T any] struct {
	keys  []string
	build func(labels map[string]string) T
	cache sync.Map // joined values -> T
	n     atomic.Int32
}

func (f *family[T]) with(values []string) (T, bool) {
	var zero T
	if f == nil || len(values) != len(f.keys) {
		return zero, false
	}
	key := strings.Join(values, "\x1f")
	if v, ok := f.cache.Load(key); ok {
		if h, ok := v.(T); ok {
			return h, true
		}
	}
	labels := make(map[string]string, len(f.keys))
	for i, k := range f.keys {
		labels[k] = values[i]
	}
	h := f.build(labels)
	if claim(&f.n, maxCachedSeries) {
		if v, loaded := f.cache.LoadOrStore(key, h); loaded {
			if prev, ok := v.(T); ok {
				return prev, true
			}
		}
	}
	return h, true
}

// claim takes one of limit slots counted on n and reports whether it got one.
func claim(n *atomic.Int32, limit int32) bool {
	for {
		cur := n.Load()
		if cur >= limit {
			return false
		}
		if n.CompareAndSwap(cur, cur+1) {
			return true
		}
	}
}

// Counter is a counter declared with label keys. A nil *Counter records
// nothing.
type Counter struct{ f *family[gometrics.Counter] }

// NewCounter declares a counter on m. A nil m returns nil.
func NewCounter(m gometrics.Metrics, name, help string, keys ...string) *Counter {
	if m == nil {
		return nil
	}
	return &Counter{f: &family[gometrics.Counter]{keys: keys, build: func(l map[string]string) gometrics.Counter {
		return m.Counter(name, gometrics.WithDescription(help), gometrics.WithLabels(l))
	}}}
}

// Inc adds one to the series for values, given in the declared key order.
func (c *Counter) Inc(values ...string) {
	if c == nil {
		return
	}
	if h, ok := c.f.with(values); ok {
		h.Inc()
	}
}

// Histogram is a histogram declared with label keys. A nil *Histogram
// records nothing.
type Histogram struct{ f *family[gometrics.Histogram] }

// NewHistogram declares a histogram on m. A nil m returns nil.
func NewHistogram(m gometrics.Metrics, name, help, unit string, buckets []float64, keys ...string) *Histogram {
	if m == nil {
		return nil
	}
	opts := []gometrics.MetricOption{gometrics.WithDescription(help), gometrics.WithUnit(unit), gometrics.WithBuckets(buckets...)}
	return &Histogram{f: &family[gometrics.Histogram]{keys: keys, build: func(l map[string]string) gometrics.Histogram {
		return m.Histogram(name, append(opts[:len(opts):len(opts)], gometrics.WithLabels(l))...)
	}}}
}

// Observe records v on the series for values, given in the declared key order.
func (h *Histogram) Observe(v float64, values ...string) {
	if h == nil {
		return
	}
	if s, ok := h.f.with(values); ok {
		s.Observe(v)
	}
}
