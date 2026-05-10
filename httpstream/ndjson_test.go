package httpstream_test

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xraph/nexus/httpstream"
	"github.com/xraph/nexus/provider"
)

func TestNDJSONEncoder_RoundTrip(t *testing.T) {
	t.Parallel()
	enc := httpstream.NewNDJSONEncoder()

	rec := httptest.NewRecorder()
	enc.WriteHeaders(rec)
	if got := rec.Header().Get("Content-Type"); got != "application/x-ndjson" {
		t.Fatalf("content-type = %q", got)
	}

	var buf bytes.Buffer
	events := []*httpstream.StreamEvent{
		{Type: httpstream.EventTypeDelta, Model: "m", Delta: &provider.Delta{Content: "hi"}},
		{Type: httpstream.EventTypeReasoning, Delta: &provider.Delta{Reasoning: "thinking"}},
		{Type: httpstream.EventTypeUsage, Usage: &provider.Usage{TotalTokens: 9}},
	}
	for _, e := range events {
		if err := enc.EncodeEvent(&buf, e); err != nil {
			t.Fatalf("encode: %v", err)
		}
	}
	if err := enc.End(&buf); err != nil {
		t.Fatalf("end: %v", err)
	}

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("got %d lines, want 4: %q", len(lines), buf.String())
	}

	var first map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("first line not valid JSON: %v", err)
	}
	if first["type"] != "delta" {
		t.Fatalf("first line type = %v", first["type"])
	}
	var last map[string]any
	if err := json.Unmarshal([]byte(lines[3]), &last); err != nil {
		t.Fatalf("done line not valid JSON: %v", err)
	}
	if last["type"] != "done" {
		t.Fatalf("done line missing terminator: %+v", last)
	}
}

func TestNDJSONEncoder_HeartbeatAndError(t *testing.T) {
	t.Parallel()
	enc := httpstream.NewNDJSONEncoder()
	var buf bytes.Buffer

	if err := enc.Heartbeat(&buf); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	if err := enc.EncodeError(&buf, &httpstream.WireError{Type: "timeout", Message: "t/o"}); err != nil {
		t.Fatalf("error: %v", err)
	}
	if !strings.Contains(buf.String(), `"type":"heartbeat"`) {
		t.Fatalf("heartbeat missing: %q", buf.String())
	}
	if !strings.Contains(buf.String(), `"type":"error"`) || !strings.Contains(buf.String(), `"timeout"`) {
		t.Fatalf("error envelope missing: %q", buf.String())
	}
}
