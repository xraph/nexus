package provider

import (
	"encoding/json"
	"fmt"
)

// ToolChoiceMode is how a request constrains the model's use of tools.
type ToolChoiceMode string

const (
	// ToolChoiceAuto lets the model decide whether to call a tool.
	ToolChoiceAuto ToolChoiceMode = "auto"
	// ToolChoiceNone forbids tool calls.
	ToolChoiceNone ToolChoiceMode = "none"
	// ToolChoiceRequired forces the model to call some tool.
	ToolChoiceRequired ToolChoiceMode = "required"
	// ToolChoiceFunction forces the model to call one named tool.
	ToolChoiceFunction ToolChoiceMode = "function"
)

// ToolChoice is the parsed form of CompletionRequest.ToolChoice.
type ToolChoice struct {
	Mode     ToolChoiceMode
	Function string // set when Mode is ToolChoiceFunction
}

// ParseToolChoice parses a CompletionRequest.ToolChoice value. It reads
// OpenAI's shape ("auto", "none", "required", or
// {"type":"function","function":{"name":...}}) and, for callers that already
// speak Anthropic, {"type":"auto"|"any"|"none"} and {"type":"tool","name":...}.
// The bare string "any" is read as required. ok is false for nil. A value
// it cannot read fails with an error that wraps ErrNotSupported, so the
// gateway refuses the request as the caller's mistake and does not retry it.
func ParseToolChoice(v any) (tc ToolChoice, ok bool, err error) {
	tc, ok, err = parseToolChoice(v)
	if err != nil {
		return ToolChoice{}, false, fmt.Errorf("%w: %w", err, ErrNotSupported)
	}
	return tc, ok, nil
}

// OpenAI returns tc in OpenAI's tool_choice shape: the string "auto",
// "none" or "required", or {"type":"function","function":{"name":...}}.
// It returns nil for a mode it does not know, such as the zero ToolChoice.
func (tc ToolChoice) OpenAI() any {
	switch tc.Mode {
	case ToolChoiceAuto, ToolChoiceNone, ToolChoiceRequired:
		return string(tc.Mode)
	case ToolChoiceFunction:
		return map[string]any{"type": "function", "function": map[string]any{"name": tc.Function}}
	default:
		return nil
	}
}

func parseToolChoice(v any) (ToolChoice, bool, error) {
	switch x := v.(type) {
	case nil:
		return ToolChoice{}, false, nil
	case ToolChoice:
		return validateToolChoice(x)
	case *ToolChoice:
		if x == nil {
			return ToolChoice{}, false, nil
		}
		return validateToolChoice(*x)
	case string:
		return parseToolChoiceString(x)
	case map[string]any:
		return parseToolChoiceMap(x)
	case json.RawMessage:
		return parseToolChoiceJSON(x)
	case []byte:
		return parseToolChoiceJSON(x)
	default:
		raw, merr := json.Marshal(v)
		if merr != nil {
			return ToolChoice{}, false, fmt.Errorf("nexus: tool_choice %T cannot be read: %w", v, merr)
		}
		return parseToolChoiceJSON(raw)
	}
}

func validateToolChoice(tc ToolChoice) (ToolChoice, bool, error) {
	switch tc.Mode {
	case ToolChoiceAuto, ToolChoiceNone, ToolChoiceRequired:
		return ToolChoice{Mode: tc.Mode}, true, nil
	case ToolChoiceFunction:
		if tc.Function == "" {
			return ToolChoice{}, false, fmt.Errorf("nexus: tool_choice function has no name")
		}
		return tc, true, nil
	default:
		return ToolChoice{}, false, fmt.Errorf("nexus: unknown tool_choice mode %q", tc.Mode)
	}
}

func parseToolChoiceString(s string) (ToolChoice, bool, error) {
	switch s {
	case "auto":
		return ToolChoice{Mode: ToolChoiceAuto}, true, nil
	case "none":
		return ToolChoice{Mode: ToolChoiceNone}, true, nil
	case "required", "any":
		return ToolChoice{Mode: ToolChoiceRequired}, true, nil
	default:
		return ToolChoice{}, false, fmt.Errorf("nexus: unknown tool_choice %q", s)
	}
}

func parseToolChoiceJSON(raw []byte) (ToolChoice, bool, error) {
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return ToolChoice{}, false, fmt.Errorf("nexus: tool_choice is not valid JSON: %w", err)
	}
	switch d := decoded.(type) {
	case nil:
		return ToolChoice{}, false, nil
	case string:
		return parseToolChoiceString(d)
	case map[string]any:
		return parseToolChoiceMap(d)
	default:
		return ToolChoice{}, false, fmt.Errorf("nexus: tool_choice must be a string or an object, got %T", decoded)
	}
}

func parseToolChoiceMap(m map[string]any) (ToolChoice, bool, error) {
	typ := stringField(m, "type")
	switch typ {
	case "auto", "none", "required", "any":
		return parseToolChoiceString(typ)
	case "function":
		name := ""
		if fn, isMap := m["function"].(map[string]any); isMap {
			name = stringField(fn, "name")
		}
		if name == "" {
			return ToolChoice{}, false, fmt.Errorf("nexus: tool_choice function has no name")
		}
		return ToolChoice{Mode: ToolChoiceFunction, Function: name}, true, nil
	case "tool":
		name := stringField(m, "name")
		if name == "" {
			return ToolChoice{}, false, fmt.Errorf("nexus: tool_choice tool has no name")
		}
		return ToolChoice{Mode: ToolChoiceFunction, Function: name}, true, nil
	default:
		return ToolChoice{}, false, fmt.Errorf("nexus: unknown tool_choice type %q", typ)
	}
}

// stringField returns m[key] when it is a string and "" otherwise.
func stringField(m map[string]any, key string) string {
	if s, isString := m[key].(string); isString {
		return s
	}
	return ""
}
