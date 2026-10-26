package guard_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/xraph/nexus/guard"
	"github.com/xraph/nexus/guard/guards"
	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/testutil"
)

// streamUsage is what every fake stream here reports in its usage chunk.
var streamUsage = provider.Usage{PromptTokens: 12, CompletionTokens: 7, TotalTokens: 19}

// textStream is a stream of text deltas, then a message stop carrying native
// content (as the Anthropic stream ends), then a usage chunk.
func textStream(parts ...string) *testutil.FakeStream {
	chunks := make([]*provider.StreamChunk, 0, len(parts)+2)
	for _, p := range parts {
		chunks = append(chunks, &provider.StreamChunk{ID: "m1", Provider: "anthropic", Model: "claude", Delta: provider.Delta{Content: p}})
	}
	native := &provider.NativeContent{Provider: "anthropic", Model: "claude", Content: json.RawMessage(`[{"type":"text","text":"` + strings.Join(parts, "") + `"}]`)}
	u := streamUsage
	chunks = append(chunks,
		&provider.StreamChunk{ID: "m1", Provider: "anthropic", Model: "claude", Kind: provider.EventMessageStop, Delta: provider.Delta{Native: native}, FinishReason: "stop"},
		&provider.StreamChunk{Kind: provider.EventUsage, Usage: &u},
	)
	return testutil.NewFakeStream(chunks, nil)
}

func outputService(gs ...guard.Guard) guard.Service {
	svc := guard.NewService()
	for _, g := range gs {
		svc.Register(g)
	}
	return svc
}

// drain reads s until it fails or ends and returns what it delivered and the
// error it ended with.
func drain(t *testing.T, s provider.Stream) ([]*provider.StreamChunk, error) {
	t.Helper()
	var got []*provider.StreamChunk
	for range 100 {
		c, err := s.Next(context.Background())
		if err != nil {
			return got, err
		}
		got = append(got, c)
	}
	t.Fatal("the stream never ended")
	return nil, nil
}

func content(chunks []*provider.StreamChunk) string {
	var b strings.Builder
	for _, c := range chunks {
		b.WriteString(c.Delta.Content)
	}
	return b.String()
}

func carriesNative(chunks []*provider.StreamChunk) bool {
	for _, c := range chunks {
		if c.Delta.Native != nil {
			return true
		}
	}
	return false
}

func wantOutputBlock(t *testing.T, err error, name string) *guard.BlockedError {
	t.Helper()
	var blocked *guard.BlockedError
	if !errors.As(err, &blocked) {
		t.Fatalf("err = %v, want a *guard.BlockedError", err)
	}
	if blocked.Phase != guard.PhaseOutput || blocked.Guard != name {
		t.Fatalf("blocked = %+v, want an output block by %s", blocked, name)
	}
	return blocked
}

func TestBufferedStreamBlockedByAnOutputGuard(t *testing.T) {
	inner := textStream("a", "secret")
	s := guard.NewOutputGuardedStream(inner, outputService(guards.NewContentFilter(guard.ActionBlock, "secret")), &guard.CheckInput{}, guard.StrategyBuffer)

	_, err := s.Next(context.Background())
	blocked := wantOutputBlock(t, err, "content_filter")
	if blocked.Usage == nil || *blocked.Usage != streamUsage || blocked.Model != "claude" || blocked.Provider != "anthropic" {
		t.Fatalf("the block must carry what the stream consumed: %+v (usage %+v)", blocked, blocked.Usage)
	}
	if !inner.Closed() {
		t.Fatal("the upstream stream was left open")
	}
	if _, err := s.Next(context.Background()); !errors.As(err, &blocked) {
		t.Fatalf("a second Next = %v, want the block again", err)
	}
	if u := s.Usage(); u == nil || *u != streamUsage {
		t.Fatalf("Usage() = %+v, want the stream's usage", u)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestBufferedStreamRedactsBeforeAnyChunk(t *testing.T) {
	s := guard.NewOutputGuardedStream(textStream("mail me ", "at a@b.co"), outputService(guards.NewPII(guard.ActionRedact)), &guard.CheckInput{}, guard.StrategyBuffer)

	got, err := drain(t, s)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("the stream ended with %v", err)
	}
	for _, c := range got {
		if strings.Contains(c.Delta.Content, "a@b.co") || strings.Contains(c.Delta.Content, "b.co") {
			t.Fatalf("a chunk carried the address: %+v", c)
		}
	}
	if text := content(got); text != "mail me at [EMAIL_REDACTED]" {
		t.Fatalf("content = %q", text)
	}
	if carriesNative(got) {
		t.Fatal("a redacted stream still carries the native content")
	}
	resp, err := provider.Accumulate(context.Background(), testutil.NewFakeStream(got, nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Choices[0].FinishReason != "stop" || resp.Usage != streamUsage || resp.Model != "claude" {
		t.Fatalf("replay = finish %q usage %+v model %q", resp.Choices[0].FinishReason, resp.Usage, resp.Model)
	}
}

// recordingGuard passes everything and keeps what it was asked to check.
type recordingGuard struct {
	phase guard.Phase
	seen  []*guard.CheckInput
}

func (g *recordingGuard) Name() string       { return "recorder" }
func (g *recordingGuard) Phase() guard.Phase { return g.phase }
func (g *recordingGuard) Check(_ context.Context, in *guard.CheckInput) (*guard.CheckResult, error) {
	cp := *in
	g.seen = append(g.seen, &cp)
	return &guard.CheckResult{Passed: true, Action: guard.ActionAllow}, nil
}

func TestOutputGuardSeesStreamedToolCalls(t *testing.T) {
	rec := &recordingGuard{phase: guard.PhaseOutput}
	for _, strategy := range []guard.StreamStrategy{guard.StrategyBuffer, guard.StrategyPassthrough, guard.StrategyChunkwise} {
		rec.seen = nil
		inner := testutil.NewFakeStream([]*provider.StreamChunk{
			{Kind: provider.EventToolCallDelta, Delta: provider.Delta{ToolCalls: []provider.ToolCall{{ID: "c1", Type: "function", Function: provider.ToolCallFunc{Name: "sh", Arguments: `{"cmd":`}}}}},
			{Kind: provider.EventToolCallDelta, Delta: provider.Delta{ToolCalls: []provider.ToolCall{{Function: provider.ToolCallFunc{Arguments: `"rm"}`}}}}},
			{FinishReason: "tool_calls"},
		}, nil)
		s := guard.NewOutputGuardedStream(inner, outputService(rec), &guard.CheckInput{TenantID: "t1"}, strategy)
		if _, err := drain(t, s); !errors.Is(err, io.EOF) {
			t.Fatalf("%s: the stream ended with %v", strategy, err)
		}
		if len(rec.seen) != 1 {
			t.Fatalf("%s: the guard ran %d times, want once", strategy, len(rec.seen))
		}
		in := rec.seen[0]
		want := []provider.ToolCall{{ID: "c1", Type: "function", Function: provider.ToolCallFunc{Name: "sh", Arguments: `{"cmd":"rm"}`}}}
		if in.TenantID != "t1" || len(in.Messages) != 1 || !reflect.DeepEqual(in.Messages[0].ToolCalls, want) {
			t.Fatalf("%s: guard input = %+v", strategy, in)
		}
	}
}

func TestPassthroughStreamsThenBlocksBeforeEOF(t *testing.T) {
	inner := testutil.NewFakeStream([]*provider.StreamChunk{
		{Delta: provider.Delta{Content: "a "}}, {Delta: provider.Delta{Content: "secret"}},
	}, &streamUsage)
	s := guard.NewOutputGuardedStream(inner, outputService(guards.NewContentFilter(guard.ActionBlock, "secret")), &guard.CheckInput{}, guard.StrategyPassthrough)

	got, err := drain(t, s)
	if content(got) != "a secret" || len(got) != 2 {
		t.Fatalf("delivered %d chunks %q, want both chunks as they came", len(got), content(got))
	}
	blocked := wantOutputBlock(t, err, "content_filter")
	if blocked.Usage == nil || *blocked.Usage != streamUsage {
		t.Fatalf("usage = %+v, want the stream's", blocked.Usage)
	}
}

func TestPassthroughKeepsARedactedStreamButEndsIt(t *testing.T) {
	s := guard.NewOutputGuardedStream(textStream("mail a@b.co"), outputService(guards.NewPII(guard.ActionRedact)), &guard.CheckInput{}, guard.StrategyPassthrough)
	got, err := drain(t, s)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("the stream ended with %v, want io.EOF: a passthrough stream cannot take the redaction back", err)
	}
	if content(got) != "mail a@b.co" {
		t.Fatalf("content = %q", content(got))
	}
	if carriesNative(got) {
		t.Fatal("the native content went out although a guard rewrote the reply")
	}
	if last := got[len(got)-1]; last.Kind != provider.EventUsage {
		t.Fatalf("the chunks held for the verdict came out of order: last is %+v", last)
	}
}

// chunkBlocker is a stream guard that blocks the chunk that carries word.
type chunkBlocker struct{ word string }

func (g chunkBlocker) Name() string       { return "chunk-blocker" }
func (g chunkBlocker) Phase() guard.Phase { return guard.PhaseOutput }
func (g chunkBlocker) Check(context.Context, *guard.CheckInput) (*guard.CheckResult, error) {
	return &guard.CheckResult{Passed: true, Action: guard.ActionAllow}, nil
}

func (g chunkBlocker) CheckChunk(_ context.Context, c *provider.StreamChunk) (*guard.CheckResult, error) {
	if strings.Contains(c.Delta.Content, g.word) {
		return &guard.CheckResult{Blocked: true, Action: guard.ActionBlock, Reason: "word"}, nil
	}
	return &guard.CheckResult{Passed: true, Action: guard.ActionAllow}, nil
}

func TestChunkwiseRunsStreamGuardsPerChunk(t *testing.T) {
	inner := testutil.NewFakeStream([]*provider.StreamChunk{
		{Delta: provider.Delta{Content: "fine "}}, {Delta: provider.Delta{Content: "bad"}}, {Delta: provider.Delta{Content: " more"}},
	}, nil)
	s := guard.NewOutputGuardedStream(inner, nil, &guard.CheckInput{}, guard.StrategyChunkwise).WithStreamGuards(chunkBlocker{word: "bad"})

	got, err := drain(t, s)
	if len(got) != 1 || content(got) != "fine " {
		t.Fatalf("delivered %q, want only the chunk before the blocked one", content(got))
	}
	wantOutputBlock(t, err, "chunk-blocker")
	if !inner.Closed() || inner.ChunksRead() != 2 {
		t.Fatalf("closed %v after %d chunks, want closed after the blocked chunk", inner.Closed(), inner.ChunksRead())
	}
}

func TestBufferRunsStreamGuardsBeforeAnyChunk(t *testing.T) {
	inner := testutil.NewFakeStream([]*provider.StreamChunk{
		{Delta: provider.Delta{Content: "fine "}}, {Delta: provider.Delta{Content: "bad"}},
	}, nil)
	s := guard.NewOutputGuardedStream(inner, nil, &guard.CheckInput{}, guard.StrategyBuffer).WithStreamGuards(chunkBlocker{word: "bad"})
	got, err := drain(t, s)
	if len(got) != 0 {
		t.Fatalf("delivered %q before the verdict", content(got))
	}
	wantOutputBlock(t, err, "chunk-blocker")
}

func TestChunkwiseRunsOutputGuardsAtTheEnd(t *testing.T) {
	inner := testutil.NewFakeStream([]*provider.StreamChunk{
		{Delta: provider.Delta{Content: "a "}}, {Delta: provider.Delta{Content: "secret"}},
	}, nil)
	s := guard.NewOutputGuardedStream(inner, outputService(guards.NewContentFilter(guard.ActionBlock, "secret")), &guard.CheckInput{}, guard.StrategyChunkwise).
		WithStreamGuards(chunkBlocker{word: "never"})
	got, err := drain(t, s)
	if len(got) != 2 {
		t.Fatalf("delivered %d chunks, want 2", len(got))
	}
	wantOutputBlock(t, err, "content_filter")
}

// A guard that rewrites or blocks a reply must keep its native content, which
// still holds the original text and thinking, away from the consumer under
// every strategy. A reply that passes keeps it.
func TestAGuardedStreamDropsNativeContentWhenAGuardActs(t *testing.T) {
	cases := []struct {
		name       string
		guard      guard.Guard
		wantNative bool
		wantBlock  bool
	}{
		{"pass", guards.NewContentFilter(guard.ActionBlock, "nothing-matches"), true, false},
		{"redact", guards.NewPII(guard.ActionRedact), false, false},
		{"block", guards.NewPII(guard.ActionBlock), false, true},
	}
	for _, strategy := range []guard.StreamStrategy{guard.StrategyBuffer, guard.StrategyPassthrough, guard.StrategyChunkwise} {
		for _, tc := range cases {
			s := guard.NewOutputGuardedStream(textStream("write to ", "a@b.co"), outputService(tc.guard), &guard.CheckInput{}, strategy)
			got, err := drain(t, s)
			if tc.wantBlock {
				wantOutputBlock(t, err, "pii")
			} else if !errors.Is(err, io.EOF) {
				t.Fatalf("%s/%s: ended with %v", strategy, tc.name, err)
			}
			if carriesNative(got) != tc.wantNative {
				t.Fatalf("%s/%s: native delivered = %v, want %v", strategy, tc.name, carriesNative(got), tc.wantNative)
			}
		}
	}
}

func TestAnUnknownStrategyBuffers(t *testing.T) {
	s := guard.NewOutputGuardedStream(textStream("a", "secret"), outputService(guards.NewContentFilter(guard.ActionBlock, "secret")), &guard.CheckInput{}, "")
	if got, err := drain(t, s); len(got) != 0 || err == nil {
		t.Fatalf("delivered %d chunks before the verdict (err %v)", len(got), err)
	}
}

// argRedactor rewrites "secret" to "[x]" in the text and the tool call
// arguments of the message it checks.
type argRedactor struct{}

func (argRedactor) Name() string       { return "arg-redactor" }
func (argRedactor) Phase() guard.Phase { return guard.PhaseOutput }
func (argRedactor) Check(_ context.Context, in *guard.CheckInput) (*guard.CheckResult, error) {
	msg := in.Messages[0]
	if s, ok := msg.Content.(string); ok {
		msg.Content = strings.ReplaceAll(s, "secret", "[x]")
	}
	msg.ToolCalls = append([]provider.ToolCall(nil), msg.ToolCalls...)
	for i := range msg.ToolCalls {
		msg.ToolCalls[i].Function.Arguments = strings.ReplaceAll(msg.ToolCalls[i].Function.Arguments, "secret", "[x]")
	}
	return &guard.CheckResult{Passed: true, Action: guard.ActionRedact, Modified: true, Messages: []provider.Message{msg}}, nil
}

func toolDeltas(chunks []*provider.StreamChunk) []*provider.StreamChunk {
	var out []*provider.StreamChunk
	for _, c := range chunks {
		if c.Kind == provider.EventToolCallDelta {
			out = append(out, c)
		}
	}
	return out
}

func TestBufferedRewriteReplaysToolCallsWithIDsOnePerDelta(t *testing.T) {
	u := streamUsage
	native := &provider.NativeContent{Provider: "anthropic", Model: "claude", Content: json.RawMessage(`[]`)}
	// Anthropic-style: every tool delta is a one-element slice, and the
	// parallel calls are told apart by id.
	inner := testutil.NewFakeStream([]*provider.StreamChunk{
		{ID: "m1", Provider: "anthropic", Model: "claude", Delta: provider.Delta{Role: "assistant", Content: "reading the secret"}},
		{Kind: provider.EventToolCallDelta, Delta: provider.Delta{ToolCalls: []provider.ToolCall{{ID: "c1", Type: "function", Function: provider.ToolCallFunc{Name: "sh", Arguments: `{"cmd":`}}}}},
		{Kind: provider.EventToolCallDelta, Delta: provider.Delta{ToolCalls: []provider.ToolCall{{ID: "c2", Type: "function", Function: provider.ToolCallFunc{Name: "ls", Arguments: `{}`}}}}},
		{Kind: provider.EventToolCallDelta, Delta: provider.Delta{ToolCalls: []provider.ToolCall{{ID: "c1", Function: provider.ToolCallFunc{Arguments: `"cat secret"}`}}}}},
		{Kind: provider.EventMessageStop, Delta: provider.Delta{Native: native}, FinishReason: "tool_calls"},
		{Kind: provider.EventUsage, Usage: &u},
	}, nil)
	s := guard.NewOutputGuardedStream(inner, outputService(argRedactor{}), &guard.CheckInput{}, guard.StrategyBuffer)

	got, err := drain(t, s)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("the stream ended with %v", err)
	}
	if carriesNative(got) {
		t.Fatal("a rewritten replay carried native content")
	}
	deltas := toolDeltas(got)
	if len(deltas) != 2 {
		t.Fatalf("replayed %d tool call deltas, want one per call (2)", len(deltas))
	}
	want := []provider.ToolCall{
		{ID: "c1", Type: "function", Function: provider.ToolCallFunc{Name: "sh", Arguments: `{"cmd":"cat [x]"}`}},
		{ID: "c2", Type: "function", Function: provider.ToolCallFunc{Name: "ls", Arguments: `{}`}},
	}
	for i, d := range deltas {
		if len(d.Delta.ToolCalls) != 1 || !reflect.DeepEqual(d.Delta.ToolCalls[0], want[i]) {
			t.Fatalf("tool delta %d = %+v, want the whole call %+v", i, d.Delta.ToolCalls, want[i])
		}
	}

	resp, err := provider.Accumulate(context.Background(), testutil.NewFakeStream(got, nil))
	if err != nil {
		t.Fatalf("accumulate the replay: %v", err)
	}
	msg := resp.Choices[0].Message
	if msg.Content != "reading the [x]" || !reflect.DeepEqual(msg.ToolCalls, want) {
		t.Fatalf("read back = %q %+v, want the rewritten text and calls", msg.Content, msg.ToolCalls)
	}
	if resp.Choices[0].FinishReason != "tool_calls" || resp.Usage != streamUsage || resp.ID != "m1" {
		t.Fatalf("read back finish %q usage %+v id %q, want tool_calls, the stream's usage and m1",
			resp.Choices[0].FinishReason, resp.Usage, resp.ID)
	}
}

func TestBufferedRewriteReplaysToolCallsWithoutIDsInOneDelta(t *testing.T) {
	u := streamUsage
	// A server that sends no call ids: calls are told apart by their slot,
	// so they must travel back in one delta to keep their slots.
	inner := testutil.NewFakeStream([]*provider.StreamChunk{
		{ID: "m2", Delta: provider.Delta{ToolCalls: []provider.ToolCall{
			{Type: "function", Function: provider.ToolCallFunc{Name: "sh", Arguments: `{"cmd":`}},
			{Type: "function", Function: provider.ToolCallFunc{Name: "ls", Arguments: `{}`}},
		}}},
		{Delta: provider.Delta{ToolCalls: []provider.ToolCall{{Function: provider.ToolCallFunc{Arguments: `"cat secret"}`}}}}},
		{FinishReason: "tool_calls"},
		{Kind: provider.EventUsage, Usage: &u},
	}, nil)
	s := guard.NewOutputGuardedStream(inner, outputService(argRedactor{}), &guard.CheckInput{}, guard.StrategyBuffer)

	got, err := drain(t, s)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("the stream ended with %v", err)
	}
	want := []provider.ToolCall{
		{Type: "function", Function: provider.ToolCallFunc{Name: "sh", Arguments: `{"cmd":"cat [x]"}`}},
		{Type: "function", Function: provider.ToolCallFunc{Name: "ls", Arguments: `{}`}},
	}
	deltas := toolDeltas(got)
	if len(deltas) != 1 || !reflect.DeepEqual(deltas[0].Delta.ToolCalls, want) {
		t.Fatalf("tool deltas = %+v, want one delta carrying both calls %+v", deltas, want)
	}

	resp, err := provider.Accumulate(context.Background(), testutil.NewFakeStream(got, nil))
	if err != nil {
		t.Fatalf("accumulate the replay: %v", err)
	}
	if calls := resp.Choices[0].Message.ToolCalls; !reflect.DeepEqual(calls, want) {
		t.Fatalf("read back calls = %+v, want %+v", calls, want)
	}
	if resp.Choices[0].FinishReason != "tool_calls" || resp.Usage != streamUsage {
		t.Fatalf("read back finish %q usage %+v", resp.Choices[0].FinishReason, resp.Usage)
	}
}
