package batch_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/xraph/nexus/batch"
	"github.com/xraph/nexus/internal/telemetry"
	"github.com/xraph/nexus/provider"
)

// executor records the trace each item ran in and fails one of them.
type executor struct {
	mu     sync.Mutex
	traces []trace.SpanContext
}

func (e *executor) Execute(ctx context.Context, req *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	e.mu.Lock()
	e.traces = append(e.traces, trace.SpanContextFromContext(ctx))
	e.mu.Unlock()
	if req.Model == "bad" {
		return nil, errors.New("alice@example.com")
	}
	return &provider.CompletionResponse{}, nil
}

// endSignal closes done when the job's span ends.
type endSignal struct {
	sdktrace.SpanProcessor
	once sync.Once
	done chan struct{}
}

func (s *endSignal) OnEnd(span sdktrace.ReadOnlySpan) {
	if span.Name() == "run batch job" {
		s.once.Do(func() { close(s.done) })
	}
}

func TestABatchJobIsOneRootTraceLinkedToItsCreator(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	sig := &endSignal{SpanProcessor: tracetest.NewSpanRecorder(), done: make(chan struct{})}
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec), sdktrace.WithSpanProcessor(sig))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { otel.SetTracerProvider(prev) })

	ex := &executor{}
	// One at a time: the items' writes to the job's counters are not
	// synchronized with each other, and this test is about the trace.
	svc := batch.NewService(ex, 1)
	ctx, creator := telemetry.Tracer().Start(context.Background(), "POST /v1/batches")
	if _, err := svc.Create(ctx, &batch.CreateInput{Inputs: []batch.Input{
		{CustomID: "a", Request: &provider.CompletionRequest{Model: "gpt-4o"}},
		{CustomID: "b", Request: &provider.CompletionRequest{Model: "bad"}},
	}}); err != nil {
		t.Fatalf("create: %v", err)
	}
	creator.End()

	select {
	case <-sig.done:
	case <-time.After(5 * time.Second):
		t.Fatal("the batch job span never ended")
	}

	var job sdktrace.ReadOnlySpan
	for _, s := range rec.Ended() {
		if s.Name() == "run batch job" {
			job = s
		}
	}
	if job == nil {
		t.Fatal("no batch job span")
	}
	if job.Parent().IsValid() || job.SpanContext().TraceID() == creator.SpanContext().TraceID() {
		t.Fatal("the job span must be a root of its own trace")
	}
	if links := job.Links(); len(links) != 1 || links[0].SpanContext.SpanID() != creator.SpanContext().SpanID() {
		t.Fatalf("links = %+v, want one to the creating span", links)
	}
	attrs := map[string]string{}
	for _, kv := range job.Attributes() {
		attrs[string(kv.Key)] = kv.Value.String()
	}
	if attrs["nexus.batch.items"] != "2" || attrs["nexus.batch.failed"] != "1" || attrs["nexus.batch.status"] != "completed" || attrs["nexus.batch.job.id"] == "" {
		t.Fatalf("job attributes = %v", attrs)
	}
	ex.mu.Lock()
	defer ex.mu.Unlock()
	for _, sc := range ex.traces {
		if sc.SpanID() != job.SpanContext().SpanID() {
			t.Fatal("an item did not run under the job span")
		}
	}
}
