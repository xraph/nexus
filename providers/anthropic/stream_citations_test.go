package anthropic_test

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/providers/anthropic"
	"github.com/xraph/nexus/testutil"
)

// TestStream_CitationsDelta_CitedText covers the canonical Anthropic field name.
func TestStream_CitationsDelta_CitedText(t *testing.T) {
	t.Parallel()
	mock := testutil.NewMockServer(t)

	lines := []string{
		testutil.AnthropicEventJSON("message_start", map[string]any{
			"type":    "message_start",
			"message": map[string]any{"id": "msg_cite", "model": "claude"},
		}),
		"",
		testutil.AnthropicEventJSON("content_block_delta", map[string]any{
			"type":  "content_block_delta",
			"index": 0,
			"delta": map[string]any{
				"type": "citations_delta",
				"citation": map[string]any{
					"url":              "https://example.com/source",
					"title":            "Example Source",
					"cited_text":       "the actual quote",
					"start_char_index": 10,
					"end_char_index":   25,
				},
			},
		}),
		"",
		testutil.AnthropicEventJSON("message_stop", map[string]any{"type": "message_stop"}),
		"",
	}
	mock.Ctrl.SetStreamHandler(testutil.AnthropicStreamHandler(lines))

	p := anthropic.New("k", anthropic.WithBaseURL(mock.Server.URL))
	stream, err := p.CompleteStream(context.Background(), &provider.CompletionRequest{
		Model:    "claude",
		Messages: []provider.Message{{Role: "user", Content: "?"}},
		Stream:   true,
	})
	if err != nil {
		t.Fatalf("CompleteStream: %v", err)
	}
	defer func() { _ = stream.Close() }()

	var citation *provider.Citation
	for {
		c, e := stream.Next(context.Background())
		if errors.Is(e, io.EOF) {
			break
		}
		if e != nil {
			t.Fatalf("Next: %v", e)
		}
		if c.Kind == provider.EventCitation && len(c.Delta.Citations) > 0 {
			cit := c.Delta.Citations[0]
			citation = &cit
		}
	}

	if citation == nil {
		t.Fatal("no citation chunk emitted")
	}
	if citation.URL != "https://example.com/source" || citation.Title != "Example Source" {
		t.Fatalf("citation metadata: %+v", citation)
	}
	if citation.Quoted != "the actual quote" {
		t.Fatalf("Quoted = %q, want %q", citation.Quoted, "the actual quote")
	}
	if citation.StartIdx != 10 || citation.EndIdx != 25 {
		t.Fatalf("char range = %d..%d", citation.StartIdx, citation.EndIdx)
	}
}

// TestStream_CitationsDelta_TextFallback covers older shapes where the field
// was named `text` instead of `cited_text`.
func TestStream_CitationsDelta_TextFallback(t *testing.T) {
	t.Parallel()
	mock := testutil.NewMockServer(t)

	lines := []string{
		testutil.AnthropicEventJSON("message_start", map[string]any{
			"type":    "message_start",
			"message": map[string]any{"id": "msg_cite_alt", "model": "claude"},
		}),
		"",
		testutil.AnthropicEventJSON("content_block_delta", map[string]any{
			"type":  "content_block_delta",
			"index": 0,
			"delta": map[string]any{
				"type": "citations_delta",
				"citation": map[string]any{
					"url":   "https://x.com",
					"title": "X",
					"text":  "older shape",
				},
			},
		}),
		"",
		testutil.AnthropicEventJSON("message_stop", map[string]any{"type": "message_stop"}),
		"",
	}
	mock.Ctrl.SetStreamHandler(testutil.AnthropicStreamHandler(lines))

	p := anthropic.New("k", anthropic.WithBaseURL(mock.Server.URL))
	stream, err := p.CompleteStream(context.Background(), &provider.CompletionRequest{
		Model:    "claude",
		Messages: []provider.Message{{Role: "user", Content: "?"}},
		Stream:   true,
	})
	if err != nil {
		t.Fatalf("CompleteStream: %v", err)
	}
	defer func() { _ = stream.Close() }()

	var quoted string
	for {
		c, e := stream.Next(context.Background())
		if errors.Is(e, io.EOF) {
			break
		}
		if e != nil {
			t.Fatalf("Next: %v", e)
		}
		if c.Kind == provider.EventCitation && len(c.Delta.Citations) > 0 {
			quoted = c.Delta.Citations[0].Quoted
		}
	}
	if quoted != "older shape" {
		t.Fatalf("Quoted via text fallback = %q", quoted)
	}
}
