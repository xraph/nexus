package telemetry_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"testing"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/xraph/nexus/internal/telemetry"
	"github.com/xraph/nexus/internal/telemetry/telemetrytest"
)

type sentinel struct{}

func (sentinel) Error() string { return "alice@example.com sk-live-secret" }

func TestErrorTypeNamesTheErrorNotItsText(t *testing.T) {
	dial := &url.Error{Op: "Post", URL: "https://api.openai.com", Err: &net.OpError{Op: "dial", Err: sentinel{}}}
	for _, tc := range []struct {
		err  error
		want string
	}{
		{nil, ""},
		{context.DeadlineExceeded, "context.DeadlineExceeded"},
		{fmt.Errorf("openai: request failed: %w", context.Canceled), "context.Canceled"},
		{errors.New("openai: API error (status 500): alice@example.com"), "*errors.errorString"},
		{fmt.Errorf("nexus: provider openai: %w", dial), "telemetry_test.sentinel"},
	} {
		if got := telemetry.ErrorType(tc.err); got != tc.want {
			t.Errorf("ErrorType(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
}

func TestFailRecordsTheTypeOnly(t *testing.T) {
	rec := telemetrytest.Spans(t)
	_, span := telemetry.Tracer().Start(context.Background(), "work")
	got := telemetry.Fail(span, fmt.Errorf("wrapped: %w", sentinel{}))
	span.End()

	s := telemetrytest.One(t, rec, "work")
	if got != "telemetry_test.sentinel" || telemetrytest.Attrs(s)["error.type"] != got {
		t.Fatalf("error.type = %q / %q", got, telemetrytest.Attrs(s)["error.type"])
	}
	if s.Status().Code != codes.Error || s.Status().Description != got {
		t.Fatalf("status = %+v", s.Status())
	}
	if len(s.Events()) != 1 || s.Events()[0].Name != "exception" || len(s.Events()[0].Attributes) != 1 ||
		string(s.Events()[0].Attributes[0].Key) != "exception.type" {
		t.Fatalf("events = %+v, want one exception event carrying only exception.type", s.Events())
	}
	if s.InstrumentationScope().Name != telemetry.InstrumentationName {
		t.Fatalf("scope = %q", s.InstrumentationScope().Name)
	}
	telemetrytest.AssertAbsent(t, rec, "alice@example.com")
	telemetrytest.AssertAbsent(t, rec, "sk-live-secret")
}

func TestDetachKeepsTheTraceAndDropsEverythingElse(t *testing.T) {
	telemetrytest.Spans(t)
	type key struct{}
	parent, span := telemetry.Tracer().Start(context.WithValue(context.Background(), key{}, "tenant"), "request")
	defer span.End()
	parent, cancel := context.WithCancel(parent)
	cancel()

	d := telemetry.Detach(parent)
	if d.Err() != nil || d.Value(key{}) != nil {
		t.Fatalf("detached context kept the request's cancellation or values: err %v, value %v", d.Err(), d.Value(key{}))
	}
	if trace.SpanContextFromContext(d).SpanID() != span.SpanContext().SpanID() {
		t.Fatal("detached context lost the request's span")
	}
	if telemetry.Detach(context.Background()) != context.Background() {
		t.Fatal("no trace to keep must give a plain background context")
	}
}

func TestGenAIProviderUsesTheConventionsNames(t *testing.T) {
	for in, want := range map[string]string{
		"openai": "openai", "anthropic": "anthropic", "bedrock": "aws.bedrock", "azureopenai": "azure.ai.openai",
		"gemini": "gcp.gemini", "vertex": "gcp.vertex_ai", "mistral": "mistral_ai", "xai": "x_ai", "groq": "groq",
		"together": "together",
	} {
		if got := telemetry.GenAIProvider(in); got != want {
			t.Errorf("GenAIProvider(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFamiliesKeepOneSeriesPerLabelSet(t *testing.T) {
	sink := telemetrytest.NewSink()
	c := telemetry.NewCounter(sink, "nexus_things_total", "Things.", "kind")
	h := telemetry.NewHistogram(sink, "nexus_wait_duration_seconds", "Waits.", "s", []float64{1, 2}, "kind")
	c.Inc("a")
	c.Inc("a")
	c.Inc("b")
	c.Inc("a", "extra") // wrong arity: dropped
	h.Observe(1.5, "a")
	h.Observe(0.5, "a")

	if got := sink.Series("nexus_things_total"); len(got) != 2 || got[0].Labels["kind"] != "a" || got[0].Value != 2 || got[1].Value != 1 {
		t.Fatalf("counter series = %+v", got)
	}
	got := sink.Series("nexus_wait_duration_seconds")
	if len(got) != 1 || got[0].Count != 2 || got[0].Sum != 2 || got[0].Unit != "s" {
		t.Fatalf("histogram series = %+v", got)
	}

	// A nil sink declares nothing, and nil families record nothing.
	none := telemetry.NewCounter(nil, "x", "")
	none.Inc("a")
	telemetry.NewHistogram(nil, "y", "", "s", nil).Observe(1)
}
