// Package telemetrytest records the spans and metrics Nexus emits, so a test
// can assert on them without a Forge app.
package telemetrytest

import (
	"context"
	"maps"
	"sort"
	"strings"
	"sync"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	gometrics "github.com/xraph/go-utils/metrics"
)

// Spans installs an in-memory tracer provider as the OTel global for the rest
// of the test and returns its recorder. The previous global comes back in
// t.Cleanup. Tests that call it must not run in parallel with each other.
func Spans(t testing.TB) *tracetest.SpanRecorder {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		otel.SetTracerProvider(prev)
		_ = tp.Shutdown(context.Background()) //nolint:errcheck // nothing to flush to
	})
	return rec
}

// Named returns the ended spans called name, oldest first.
func Named(rec *tracetest.SpanRecorder, name string) []sdktrace.ReadOnlySpan {
	var out []sdktrace.ReadOnlySpan
	for _, s := range rec.Ended() {
		if s.Name() == name {
			out = append(out, s)
		}
	}
	return out
}

// One returns the one ended span called name and fails the test unless there
// is exactly one.
func One(t testing.TB, rec *tracetest.SpanRecorder, name string) sdktrace.ReadOnlySpan {
	t.Helper()
	got := Named(rec, name)
	if len(got) != 1 {
		t.Fatalf("want one %q span, got %d (ended: %v)", name, len(got), Names(rec))
	}
	return got[0]
}

// Names lists the names of the ended spans, oldest first.
func Names(rec *tracetest.SpanRecorder) []string {
	ended := rec.Ended()
	out := make([]string, 0, len(ended))
	for _, s := range ended {
		out = append(out, s.Name())
	}
	return out
}

// Attrs returns every attribute on span as strings.
func Attrs(span sdktrace.ReadOnlySpan) map[string]string {
	m := make(map[string]string, len(span.Attributes()))
	for _, kv := range span.Attributes() {
		m[string(kv.Key)] = kv.Value.String()
	}
	return m
}

// AssertAbsent fails the test if value appears anywhere on any ended span:
// its name, an attribute, an event's name or attributes, or its status.
func AssertAbsent(t testing.TB, rec *tracetest.SpanRecorder, value string) {
	t.Helper()
	for _, s := range rec.Ended() {
		where := ""
		switch {
		case strings.Contains(s.Name(), value):
			where = "name"
		case strings.Contains(s.Status().Description, value):
			where = "status"
		}
		for _, kv := range s.Attributes() {
			if strings.Contains(kv.Value.String(), value) {
				where = "attribute " + string(kv.Key)
			}
		}
		for _, ev := range s.Events() {
			if strings.Contains(ev.Name, value) {
				where = "event " + ev.Name
			}
			for _, kv := range ev.Attributes {
				if strings.Contains(kv.Value.String(), value) {
					where = "event " + ev.Name + " attribute " + string(kv.Key)
				}
			}
		}
		if where != "" {
			t.Errorf("%q leaked onto span %q (%s)", value, s.Name(), where)
		}
	}
}

// Series is one label set of one metric, as the sink recorded it.
type Series struct {
	Name   string
	Unit   string
	Labels map[string]string
	// Value is a counter's total. Count and Sum are a histogram's.
	Value float64
	Count uint64
	Sum   float64
}

// Sink is a go-utils metrics sink that keeps every counter and histogram it
// hands out, so a test can read back names, units, labels and values. Only
// Counter and Histogram are implemented; anything else panics on the nil
// embedded interface.
type Sink struct {
	gometrics.Metrics

	mu       sync.Mutex
	counters map[string]*counterEntry
	hists    map[string]*histEntry
}

type counterEntry struct {
	name, unit string
	labels     map[string]string
	c          gometrics.Counter
}

type histEntry struct {
	name, unit string
	labels     map[string]string
	h          gometrics.Histogram
}

// NewSink returns an empty sink.
func NewSink() *Sink {
	return &Sink{counters: map[string]*counterEntry{}, hists: map[string]*histEntry{}}
}

func options(opts []gometrics.MetricOption) gometrics.MetricOptions {
	var o gometrics.MetricOptions
	for _, f := range opts {
		f(&o)
	}
	return o
}

func seriesKey(name string, labels map[string]string) string {
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(name)
	for _, k := range keys {
		b.WriteString("|" + k + "=" + labels[k])
	}
	return b.String()
}

// Counter implements gometrics.MetricFactory.
func (s *Sink) Counter(name string, opts ...gometrics.MetricOption) gometrics.Counter {
	o := options(opts)
	key := seriesKey(name, o.Labels)
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.counters[key]; ok {
		return e.c
	}
	e := &counterEntry{name: name, unit: o.Unit, labels: maps.Clone(o.Labels), c: gometrics.NewCounter(name, opts...)}
	s.counters[key] = e
	return e.c
}

// Histogram implements gometrics.MetricFactory.
func (s *Sink) Histogram(name string, opts ...gometrics.MetricOption) gometrics.Histogram {
	o := options(opts)
	key := seriesKey(name, o.Labels)
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.hists[key]; ok {
		return e.h
	}
	e := &histEntry{name: name, unit: o.Unit, labels: maps.Clone(o.Labels), h: gometrics.NewHistogram(name, opts...)}
	s.hists[key] = e
	return e.h
}

// Series returns every label set recorded under name, in a stable order.
func (s *Sink) Series(name string) []Series {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Series
	for _, e := range s.counters {
		if e.name == name {
			out = append(out, Series{Name: name, Unit: e.unit, Labels: e.labels, Value: e.c.Value()})
		}
	}
	for _, e := range s.hists {
		if e.name == name {
			out = append(out, Series{Name: name, Unit: e.unit, Labels: e.labels, Count: e.h.Count(), Sum: e.h.Sum()})
		}
	}
	sort.Slice(out, func(i, j int) bool { return seriesKey(name, out[i].Labels) < seriesKey(name, out[j].Labels) })
	return out
}

// Find returns the series of name whose labels include every pair in want,
// and whether there was one.
func (s *Sink) Find(name string, want map[string]string) (Series, bool) {
	for _, se := range s.Series(name) {
		match := true
		for k, v := range want {
			if got, ok := se.Labels[k]; !ok || got != v {
				match = false
				break
			}
		}
		if match {
			return se, true
		}
	}
	return Series{}, false
}

// Names lists every metric name the sink handed out, sorted.
func (s *Sink) Names() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := map[string]bool{}
	for _, e := range s.counters {
		seen[e.name] = true
	}
	for _, e := range s.hists {
		seen[e.name] = true
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
