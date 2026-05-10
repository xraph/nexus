package httpstream_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/xraph/nexus/httpstream"
)

func TestNegotiate_DefaultsToOpenAISSE(t *testing.T) {
	t.Parallel()
	r := httpstream.DefaultRegistry()
	req := httptest.NewRequest("POST", "/", nil)
	enc := httpstream.Negotiate(req, r)
	if enc == nil || enc.ContentType() != "text/event-stream" {
		t.Fatalf("expected text/event-stream default, got %v", enc)
	}
}

func TestNegotiate_AcceptHeader(t *testing.T) {
	t.Parallel()
	r := httpstream.DefaultRegistry()
	cases := []struct {
		accept string
		want   string
	}{
		{"application/x-ndjson", "application/x-ndjson"},
		{"application/vnd.nexus.events+sse", "application/vnd.nexus.events+sse"},
		{"text/event-stream", "text/event-stream"},
		{"*/*", "text/event-stream"},
	}
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		req.Header.Set("Accept", c.accept)
		enc := httpstream.Negotiate(req, r)
		if enc == nil || enc.ContentType() != c.want {
			t.Errorf("Accept=%q: got %v, want %s", c.accept, enc, c.want)
		}
	}
}

func TestNegotiate_QueryParamWins(t *testing.T) {
	t.Parallel()
	r := httpstream.DefaultRegistry()
	req := httptest.NewRequest(http.MethodPost, "/?stream_format=ndjson", nil)
	req.Header.Set("Accept", "text/event-stream")
	enc := httpstream.Negotiate(req, r)
	if enc.ContentType() != "application/x-ndjson" {
		t.Fatalf("query override ignored: got %s", enc.ContentType())
	}
}

func TestNegotiate_NexusHeaderOverridesAccept(t *testing.T) {
	t.Parallel()
	r := httpstream.DefaultRegistry()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("X-Nexus-Stream-Format", "nexus-sse")
	enc := httpstream.Negotiate(req, r)
	if enc.ContentType() != "application/vnd.nexus.events+sse" {
		t.Fatalf("nexus header override ignored: got %s", enc.ContentType())
	}
}
