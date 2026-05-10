package httpstream_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/xraph/nexus/httpstream"
	"github.com/xraph/nexus/provider"
)

func TestSSENativeEncoder_NamedEvents(t *testing.T) {
	t.Parallel()
	enc := httpstream.NewSSENativeEncoder()

	if enc.ContentType() != "application/vnd.nexus.events+sse" {
		t.Fatalf("content-type = %q", enc.ContentType())
	}

	var buf bytes.Buffer
	cases := []*httpstream.StreamEvent{
		{Type: httpstream.EventTypeDelta, Delta: &provider.Delta{Content: "Hi"}},
		{Type: httpstream.EventTypeReasoning, Delta: &provider.Delta{Reasoning: "thinking"}},
		{Type: httpstream.EventTypeUsage, Usage: &provider.Usage{TotalTokens: 9}},
	}
	for _, ev := range cases {
		if err := enc.EncodeEvent(&buf, ev); err != nil {
			t.Fatalf("encode %s: %v", ev.Type, err)
		}
	}
	if err := enc.End(&buf); err != nil {
		t.Fatalf("end: %v", err)
	}

	out := buf.String()
	for _, want := range []string{
		"event: delta",
		"event: reasoning",
		"event: usage",
		"event: done",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output:\n%s", want, out)
		}
	}
	// Each named event must be followed by a data: line carrying JSON.
	if !strings.Contains(out, `"type":"delta"`) ||
		!strings.Contains(out, `"reasoning":"thinking"`) ||
		!strings.Contains(out, `"total_tokens":9`) {
		t.Fatalf("payload missing fields: %s", out)
	}
}

func TestSSENativeEncoder_HeartbeatIsComment(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := httpstream.NewSSENativeEncoder().Heartbeat(&buf); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	if buf.String() != ": ping\n\n" {
		t.Fatalf("heartbeat = %q, want SSE comment", buf.String())
	}
}

func TestSSENativeEncoder_ErrorEvent(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	enc := httpstream.NewSSENativeEncoder()
	werr := &httpstream.WireError{Type: "upstream", Message: "boom"}
	if err := enc.EncodeError(&buf, werr); err != nil {
		t.Fatalf("error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "event: error") {
		t.Fatalf("missing event: error: %q", out)
	}
	if !strings.Contains(out, `"type":"error"`) || !strings.Contains(out, `"boom"`) {
		t.Fatalf("payload missing: %q", out)
	}
}
