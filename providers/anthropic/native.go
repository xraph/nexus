package anthropic

import (
	"encoding/json"
	"maps"
	"reflect"
	"slices"
	"strings"

	"github.com/xraph/nexus/provider"
)

const (
	// providerName is the Provider of the NativeContent this package writes
	// and the only one it replays.
	providerName = "anthropic"

	// blockBindingBeta is the beta header value the thinking block_binding
	// field needs.
	blockBindingBeta = "thinking-binding-controls-2026-08-01"
)

// anthropicBlockBinding is the thinking block_binding field.
type anthropicBlockBinding struct {
	PrefixMismatchBehavior string `json:"prefix_mismatch_behavior"`
}

// newNative wraps a reply's content array, or returns nil when there is no
// content to keep.
func newNative(model string, content json.RawMessage) *provider.NativeContent {
	if len(content) == 0 {
		return nil
	}
	return &provider.NativeContent{Provider: providerName, Model: model, Content: content}
}

// nativeReplay returns the assistant message's native content when it can be
// sent back verbatim, or nil when the turn must be rebuilt from its fields.
// It is sent verbatim only when it came from this provider and still matches
// the message: its text blocks join to the message text, and its tool_use
// blocks are the message's tool calls in order, with JSON-equal input. Any
// edit therefore drops the signed thinking blocks, which the API would
// reject next to edited content.
func nativeReplay(m *provider.Message) json.RawMessage {
	if m.Native == nil || m.Native.Provider != providerName || len(m.Native.Content) == 0 {
		return nil
	}
	var blocks []struct {
		Type  string          `json:"type"`
		Text  string          `json:"text"`
		ID    string          `json:"id"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(m.Native.Content, &blocks); err != nil {
		return nil
	}

	var text strings.Builder
	calls := 0
	for _, b := range blocks {
		switch b.Type {
		case "text":
			text.WriteString(b.Text)
		case "tool_use":
			if calls >= len(m.ToolCalls) {
				return nil
			}
			tc := m.ToolCalls[calls]
			calls++
			if b.ID != tc.ID || b.Name != tc.Function.Name || !jsonEqual(b.Input, tc.Function.Arguments) {
				return nil
			}
		}
	}
	if calls != len(m.ToolCalls) || text.String() != messageText(m.Content) {
		return nil
	}
	return m.Native.Content
}

// jsonEqual reports whether a tool_use input and a tool call's arguments hold
// the same JSON value. Empty arguments stand for an empty object, as
// toolInput sends them.
func jsonEqual(input json.RawMessage, arguments string) bool {
	if strings.TrimSpace(arguments) == "" {
		arguments = "{}"
	}
	var a, b any
	if err := json.Unmarshal(input, &a); err != nil {
		return false
	}
	if err := json.Unmarshal([]byte(arguments), &b); err != nil {
		return false
	}
	return reflect.DeepEqual(a, b)
}

// nativeBlock rebuilds one content block of a streamed reply from its
// content_block_start event and the deltas that follow it. Fields the
// deltas do not touch, and blocks of types this package does not know, keep
// what the start event sent.
type nativeBlock struct {
	fields map[string]json.RawMessage

	text, thinking, signature, partialJSON strings.Builder

	hasText, hasThinking, hasSignature, hasCitations bool

	citations []json.RawMessage
}

func newNativeBlock(start json.RawMessage) *nativeBlock {
	b := &nativeBlock{}
	if err := json.Unmarshal(start, &b.fields); err != nil || b.fields == nil {
		b.fields = map[string]json.RawMessage{}
	}
	b.hasText = startString(b.fields["text"], &b.text)
	b.hasThinking = startString(b.fields["thinking"], &b.thinking)
	b.hasSignature = startString(b.fields["signature"], &b.signature)
	if raw, ok := b.fields["citations"]; ok {
		b.hasCitations = true
		var list []json.RawMessage
		if err := json.Unmarshal(raw, &list); err == nil {
			b.citations = list
		}
	}
	return b
}

// startString seeds sb with the start event's string field and reports
// whether the field was present.
func startString(raw json.RawMessage, sb *strings.Builder) bool {
	if raw == nil {
		return false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		sb.WriteString(s)
	}
	return true
}

// nativeDelta is the delta of a content_block_delta event.
type nativeDelta struct {
	Type        string          `json:"type"`
	Text        string          `json:"text"`
	Thinking    string          `json:"thinking"`
	Signature   string          `json:"signature"`
	PartialJSON string          `json:"partial_json"`
	Citation    json.RawMessage `json:"citation"`
}

func (b *nativeBlock) apply(d *nativeDelta) {
	switch d.Type {
	case "text_delta":
		b.hasText = true
		b.text.WriteString(d.Text)
	case "thinking_delta":
		b.hasThinking = true
		b.thinking.WriteString(d.Thinking)
	case "signature_delta":
		// The signature arrives whole, just before the block stops.
		b.hasSignature = true
		b.signature.Reset()
		b.signature.WriteString(d.Signature)
	case "input_json_delta":
		b.partialJSON.WriteString(d.PartialJSON)
	case "citations_delta":
		if len(d.Citation) > 0 {
			b.hasCitations = true
			b.citations = append(b.citations, d.Citation)
		}
	}
}

func (b *nativeBlock) build() json.RawMessage {
	out := maps.Clone(b.fields)
	if b.hasText {
		out["text"] = jsonString(b.text.String())
	}
	if b.hasThinking {
		out["thinking"] = jsonString(b.thinking.String())
	}
	if b.hasSignature {
		out["signature"] = jsonString(b.signature.String())
	}
	if b.hasCitations {
		if raw, err := json.Marshal(b.citations); err == nil {
			out["citations"] = raw
		}
	}
	// A cut-off tool input is not valid JSON; the start event's input stays.
	if in := strings.TrimSpace(b.partialJSON.String()); in != "" && json.Valid([]byte(in)) {
		out["input"] = json.RawMessage(in)
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return nil
	}
	return raw
}

func jsonString(s string) json.RawMessage {
	raw, _ := json.Marshal(s) //nolint:errcheck // a string always marshals
	return raw
}

// nativeContent assembles the streamed blocks in index order, or returns nil
// when the reply had none.
func nativeContent(blocks map[int]*nativeBlock) json.RawMessage {
	if len(blocks) == 0 {
		return nil
	}
	out := make([]json.RawMessage, 0, len(blocks))
	for _, idx := range slices.Sorted(maps.Keys(blocks)) {
		if raw := blocks[idx].build(); raw != nil {
			out = append(out, raw)
		}
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return nil
	}
	return raw
}
