package middlewares_test

import (
	"context"
	"errors"
	"io"
	"maps"
	"reflect"
	"sync"
	"testing"

	"github.com/xraph/nexus/guard"
	"github.com/xraph/nexus/guard/guards"
	"github.com/xraph/nexus/internal/telemetry/telemetrytest"
	"github.com/xraph/nexus/pipeline"
	"github.com/xraph/nexus/pipeline/middlewares"
	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/testutil"
)

func streamRequest() *pipeline.Request {
	return &pipeline.Request{
		Type:       pipeline.RequestStream,
		Completion: &provider.CompletionRequest{Model: "gpt-4o", Messages: []provider.Message{{Role: "user", Content: "hi"}}},
		State:      map[string]any{},
	}
}

func serving(chunks ...*provider.StreamChunk) pipeline.NextFunc {
	return func(context.Context) (*pipeline.Response, error) {
		return &pipeline.Response{Stream: testutil.NewFakeStream(chunks, nil)}, nil
	}
}

func readAll(t *testing.T, s provider.Stream) (string, error) {
	t.Helper()
	text := ""
	for range 100 {
		c, err := s.Next(context.Background())
		if err != nil {
			return text, err
		}
		text += c.Delta.Content
	}
	t.Fatal("the stream never ended")
	return "", nil
}

func outputGuards(gs ...guard.Guard) guard.Service {
	svc := guard.NewService()
	for _, g := range gs {
		svc.Register(g)
	}
	return svc
}

func TestStreamGuardrailBuffersByDefault(t *testing.T) {
	mw := middlewares.NewStreamGuardrail(nil, "").WithOutputGuards(outputGuards(guards.NewContentFilter(guard.ActionBlock, "secret")))
	resp, err := mw.Process(context.Background(), streamRequest(), serving(
		&provider.StreamChunk{Delta: provider.Delta{Content: "a "}}, &provider.StreamChunk{Delta: provider.Delta{Content: "secret"}}))
	if err != nil {
		t.Fatal(err)
	}
	text, err := readAll(t, resp.Stream)
	var blocked *guard.BlockedError
	if text != "" || !errors.As(err, &blocked) || blocked.Phase != guard.PhaseOutput {
		t.Fatalf("delivered %q then %v, want nothing then an output block", text, err)
	}
}

func TestStreamGuardrailActsOnStreamRequests(t *testing.T) {
	mw := middlewares.NewStreamGuardrail(nil, guard.StrategyBuffer).WithOutputGuards(outputGuards(guards.NewContentFilter(guard.ActionBlock, "secret")))

	// The request type decides, not the Stream flag a caller may leave unset.
	req := streamRequest()
	resp, err := mw.Process(context.Background(), req, serving(&provider.StreamChunk{Delta: provider.Delta{Content: "secret"}}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = readAll(t, resp.Stream); err == nil || errors.Is(err, io.EOF) {
		t.Fatalf("a stream request with Stream unset was not guarded: %v", err)
	}

	req = streamRequest()
	req.Type = pipeline.RequestCompletion
	req.Completion.Stream = true
	plain := testutil.NewFakeStream(nil, nil)
	resp, err = mw.Process(context.Background(), req, func(context.Context) (*pipeline.Response, error) {
		return &pipeline.Response{Stream: plain}, nil
	})
	if err != nil || resp.Stream != plain {
		t.Fatalf("a completion request was wrapped: %T, %v", resp.Stream, err)
	}
}

func TestPassthroughRedactionIsLoggedAndTraced(t *testing.T) {
	rec := telemetrytest.Spans(t)
	log := &warnLog{}
	mw := middlewares.NewStreamGuardrail(nil, guard.StrategyPassthrough).
		WithOutputGuards(outputGuards(guards.NewPII(guard.ActionRedact))).
		WithLogger(log)
	resp, err := mw.Process(context.Background(), streamRequest(), serving(&provider.StreamChunk{Delta: provider.Delta{Content: "mail a@b.co"}}))
	if err != nil {
		t.Fatal(err)
	}
	if text, err := readAll(t, resp.Stream); !errors.Is(err, io.EOF) || text != "mail a@b.co" {
		t.Fatalf("delivered %q then %v, want the text as sent then io.EOF", text, err)
	}
	want := []any{"nexus: output guard modified a passthrough stream; the change was not applied", "guard", "pii"}
	if len(log.warns) != 1 || !reflect.DeepEqual(log.warns[0], want) {
		t.Fatalf("warnings = %v, want %v", log.warns, want)
	}
	check := telemetrytest.One(t, rec, "check guardrails")
	wantAttrs(t, check, map[string]string{
		"nexus.guardrail.phase": "output", "nexus.guardrail.blocked": "false", "nexus.guardrail.modified": "true",
	})
	telemetrytest.AssertAbsent(t, rec, "a@b.co")
}

func TestBufferedRedactionIsNotWarned(t *testing.T) {
	log := &warnLog{}
	mw := middlewares.NewStreamGuardrail(nil, guard.StrategyBuffer).
		WithOutputGuards(outputGuards(guards.NewPII(guard.ActionRedact))).
		WithLogger(log)
	resp, err := mw.Process(context.Background(), streamRequest(), serving(&provider.StreamChunk{Delta: provider.Delta{Content: "mail a@b.co"}}))
	if err != nil {
		t.Fatal(err)
	}
	if text, err := readAll(t, resp.Stream); !errors.Is(err, io.EOF) || text != "mail [EMAIL_REDACTED]" {
		t.Fatalf("delivered %q then %v", text, err)
	}
	if len(log.warns) != 0 {
		t.Fatalf("a redaction that took effect was warned: %v", log.warns)
	}
}

// metadataGuard keeps a copy of the metadata of every check, then changes the
// map it was given.
type metadataGuard struct {
	mu   sync.Mutex
	seen []map[string]string
}

func (g *metadataGuard) Name() string       { return "metadata" }
func (g *metadataGuard) Phase() guard.Phase { return guard.PhaseBoth }
func (g *metadataGuard) Check(_ context.Context, in *guard.CheckInput) (*guard.CheckResult, error) {
	g.mu.Lock()
	g.seen = append(g.seen, maps.Clone(in.Metadata))
	g.mu.Unlock()
	if in.Metadata != nil {
		in.Metadata["project"] = "changed"
	}
	return &guard.CheckResult{Passed: true, Action: guard.ActionAllow}, nil
}

func TestGuardInputCarriesRequestMetadata(t *testing.T) {
	want := map[string]string{"project": "p1"}
	check := func(name string, g *metadataGuard, req *pipeline.Request, phases int) {
		t.Helper()
		if len(g.seen) != phases {
			t.Fatalf("%s: the guard ran %d times, want %d", name, len(g.seen), phases)
		}
		for i, m := range g.seen {
			if !reflect.DeepEqual(m, want) {
				t.Fatalf("%s: check %d saw metadata %v, want %v", name, i, m, want)
			}
		}
		if !reflect.DeepEqual(req.Completion.Metadata, want) {
			t.Fatalf("%s: a guard changed the request's metadata: %v", name, req.Completion.Metadata)
		}
	}

	g := &metadataGuard{}
	req := &pipeline.Request{Type: pipeline.RequestCompletion, Completion: &provider.CompletionRequest{
		Messages: []provider.Message{{Role: "user", Content: "hi"}}, Metadata: map[string]string{"project": "p1"},
	}, State: map[string]any{}}
	_, err := middlewares.NewGuardrail(outputGuards(g)).Process(context.Background(), req, func(context.Context) (*pipeline.Response, error) {
		return &pipeline.Response{Completion: &provider.CompletionResponse{Choices: []provider.Choice{{Message: provider.Message{Role: "assistant", Content: "ok"}}}}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	check("completion", g, req, 2)

	g = &metadataGuard{}
	req = streamRequest()
	req.Completion.Metadata = map[string]string{"project": "p1"}
	resp, err := middlewares.NewStreamGuardrail(nil, guard.StrategyBuffer).WithOutputGuards(outputGuards(g)).
		Process(context.Background(), req, serving(&provider.StreamChunk{Delta: provider.Delta{Content: "ok"}}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := readAll(t, resp.Stream); !errors.Is(err, io.EOF) {
		t.Fatalf("stream ended with %v", err)
	}
	check("stream", g, req, 1)
}
