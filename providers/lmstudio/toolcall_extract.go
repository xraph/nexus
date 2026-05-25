package lmstudio

import (
	"context"
	"encoding/json"
	"io"
	"regexp"
	"strings"

	"github.com/xraph/nexus/provider"
)

// ExtractTextFormToolCalls scans `text` for any of the common text-form
// tool-call invocations a local LLM might emit when its provider
// doesn't support structured tool calls in streaming deltas. Recognises:
//
//   - `<tool_call>...</tool_call>` (and unclosed `<tool_call>...EOS`)
//   - `<function_call>...</function_call>` (and unclosed variant)
//   - `<|tool_call|>...<|/tool_call|>` (and unclosed variant)
//
// The payload inside each marker may be either a single JSON object
// (`{...}`) or a JSON array (`[{...},{...}]`). For each call, `name`
// (or `tool`, `function`) names the tool; an optional `arguments`
// (or `args`, `parameters`) object/string is serialised back to a
// JSON string for `ToolCall.Function.Arguments`.
//
// Unclosed markers are tolerated because real-world local models
// (Mistral / Qwen / TinyLlama variants on LM Studio) frequently emit
// the opening marker, the payload, and then stop. The fallback path
// only fires when no closed-form marker was found in the input, so
// it can't accidentally swallow text past a legitimate close tag.
//
// Returns the parsed calls in source order plus the input with the
// matched markers stripped (so prose around the tool calls survives).
// When nothing matches it returns (nil, text).
//
// This is a pure function — safe to call repeatedly, no state. The
// LM Studio provider uses it under the [WithToolCallExtraction] option;
// other consumers can call it directly on the accumulator's final
// content if they prefer post-stream extraction.
func ExtractTextFormToolCalls(text string) ([]provider.ToolCall, string) {
	if text == "" {
		return nil, text
	}

	var calls []provider.ToolCall
	cleaned := text

	// Pass 1: closed-form markers, draining all occurrences per regex.
	for _, re := range closedPatterns {
		for {
			match := re.FindStringSubmatchIndex(cleaned)
			if match == nil {
				break
			}
			body := cleaned[match[2]:match[3]]
			parsed, ok := parseInvocationPayload(body)
			if !ok {
				// Avoid infinite loop: sentinel the opening `<`.
				cleaned = cleaned[:match[0]] + "\x00" + cleaned[match[0]+1:]
				continue
			}
			calls = append(calls, parsed...)
			left := strings.TrimRight(cleaned[:match[0]], " \t\n")
			right := strings.TrimLeft(cleaned[match[1]:], " \t\n")
			if left != "" && right != "" {
				cleaned = left + "\n" + right
			} else {
				cleaned = left + right
			}
		}
	}
	cleaned = strings.ReplaceAll(cleaned, "\x00", "<")

	// Pass 2: markdown fenced code blocks. Real-world local models
	// frequently emit `` ```json\n{...}\n``` `` instead of any wrapper
	// marker. Bare fences without a `json` tag are accepted too, but
	// the parseInvocationPayload guard rejects non-tool-call bodies
	// (Python, SQL, DTL code) automatically.
	for {
		match := fencedCodeBlockPattern.FindStringSubmatchIndex(cleaned)
		if match == nil {
			break
		}
		body := cleaned[match[2]:match[3]]
		parsed, ok := parseInvocationPayload(body)
		if !ok {
			cleaned = cleaned[:match[0]] + "\x00\x00\x00" + cleaned[match[0]+3:]
			continue
		}
		calls = append(calls, parsed...)
		left := strings.TrimRight(cleaned[:match[0]], " \t\n")
		right := strings.TrimLeft(cleaned[match[1]:], " \t\n")
		if left != "" && right != "" {
			cleaned = left + "\n\n" + right
		} else {
			cleaned = left + right
		}
	}
	cleaned = strings.ReplaceAll(cleaned, "\x00\x00\x00", "```")

	// Pass 3: unclosed wrapper fallback — only if no calls extracted
	// by either of the first two explicit-framing passes.
	if len(calls) == 0 {
		for _, re := range unclosedPatterns {
			match := re.FindStringSubmatchIndex(cleaned)
			if match == nil {
				continue
			}
			body := cleaned[match[2]:match[3]]
			parsed, ok := parseInvocationPayload(body)
			if !ok {
				continue
			}
			calls = append(calls, parsed...)
			cleaned = strings.TrimRight(cleaned[:match[0]], " \t\n")
			break
		}
	}

	if len(calls) == 0 {
		return nil, text
	}
	cleaned = strings.TrimSpace(cleaned)
	return calls, cleaned
}

var closedPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?s)<tool_call>\s*(.*?)\s*</tool_call>`),
	regexp.MustCompile(`(?s)<function_call>\s*(.*?)\s*</function_call>`),
	regexp.MustCompile(`(?s)<\|tool_call\|>\s*(.*?)\s*<\|/tool_call\|>`),
}

var unclosedPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?s)<tool_call>\s*(.+)$`),
	regexp.MustCompile(`(?s)<function_call>\s*(.+)$`),
	regexp.MustCompile(`(?s)<\|tool_call\|>\s*(.+)$`),
}

// fencedCodeBlockPattern matches ` ```json\n...\n``` ` and bare
// ` ```\n...\n``` ` fences. The lang tag (case-insensitive) is
// optional — bare fences are common for non-tool code (SQL, DTL,
// Python), so we also require the body to be a valid tool-call
// payload before extracting.
var fencedCodeBlockPattern = regexp.MustCompile("(?si)```(?:json)?\\s*\\n(.*?)\\n\\s*```")

func parseInvocationPayload(raw string) ([]provider.ToolCall, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, false
	}
	switch raw[0] {
	case '{':
		call, ok := parseInvocationObject(raw)
		if !ok {
			return nil, false
		}
		return []provider.ToolCall{call}, true
	case '[':
		var rawList []json.RawMessage
		if err := json.Unmarshal([]byte(raw), &rawList); err != nil {
			return nil, false
		}
		out := make([]provider.ToolCall, 0, len(rawList))
		for _, item := range rawList {
			call, ok := parseInvocationObject(string(item))
			if !ok {
				continue
			}
			out = append(out, call)
		}
		if len(out) == 0 {
			return nil, false
		}
		return out, true
	default:
		return nil, false
	}
}

func parseInvocationObject(raw string) (provider.ToolCall, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw[0] != '{' {
		return provider.ToolCall{}, false
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		return provider.ToolCall{}, false
	}
	name := firstString(obj, "name", "tool", "function")
	if name == "" {
		return provider.ToolCall{}, false
	}
	var argsRaw any
	// Every alias real-world local models have been observed using.
	for _, key := range []string{"arguments", "args", "parameters", "params", "input", "inputs"} {
		if v, ok := obj[key]; ok {
			argsRaw = v
			break
		}
	}
	argsStr := "{}"
	switch v := argsRaw.(type) {
	case string:
		if strings.TrimSpace(v) != "" {
			argsStr = v
		}
	case map[string]any:
		if b, err := json.Marshal(v); err == nil {
			argsStr = string(b)
		}
	case nil:
		// no args
	default:
		if b, err := json.Marshal(v); err == nil {
			argsStr = string(b)
		}
	}
	return provider.ToolCall{
		Type: "function",
		Function: provider.ToolCallFunc{
			Name:      name,
			Arguments: argsStr,
		},
	}, true
}

func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

// extractingStream wraps a provider.Stream and, on EOF, synthesises
// EventToolCallDelta chunks from any text-form markers it observed in
// the content deltas. See ExtractTextFormToolCalls for the marker
// grammar (closed + unclosed wrapper variants, single + array
// payloads).
type extractingStream struct {
	inner       provider.Stream
	contentBuf  strings.Builder
	pending     []*provider.StreamChunk
	pendingNext bool
}

func (s *extractingStream) Next(ctx context.Context) (*provider.StreamChunk, error) {
	if len(s.pending) > 0 {
		chunk := s.pending[0]
		s.pending = s.pending[1:]
		return chunk, nil
	}
	if s.pendingNext {
		return nil, io.EOF
	}
	chunk, err := s.inner.Next(ctx)
	if err == io.EOF {
		calls, _ := ExtractTextFormToolCalls(s.contentBuf.String())
		s.pendingNext = true
		for i := range calls {
			s.pending = append(s.pending, &provider.StreamChunk{
				Provider: "lmstudio",
				Kind:     provider.EventToolCallDelta,
				Delta: provider.Delta{
					ToolCalls: []provider.ToolCall{calls[i]},
				},
			})
		}
		if len(s.pending) > 0 {
			next := s.pending[0]
			s.pending = s.pending[1:]
			return next, nil
		}
		return nil, io.EOF
	}
	if err != nil {
		return nil, err
	}
	if chunk != nil && chunk.Delta.Content != "" {
		s.contentBuf.WriteString(chunk.Delta.Content)
	}
	return chunk, nil
}

func (s *extractingStream) Close() error {
	return s.inner.Close()
}

func (s *extractingStream) Usage() *provider.Usage {
	return s.inner.Usage()
}

var _ provider.Stream = (*extractingStream)(nil)
