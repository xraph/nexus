package provider_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/xraph/nexus/provider"
)

func TestParseToolChoice(t *testing.T) {
	fn := func(name string) provider.ToolChoice {
		return provider.ToolChoice{Mode: provider.ToolChoiceFunction, Function: name}
	}
	mode := func(m provider.ToolChoiceMode) provider.ToolChoice { return provider.ToolChoice{Mode: m} }
	openaiFn := map[string]any{"type": "function", "function": map[string]any{"name": "f"}}

	tests := []struct {
		name string
		in   any
		want provider.ToolChoice
		ok   bool
		err  bool
	}{
		{name: "nil", in: nil},
		{name: "auto", in: "auto", want: mode(provider.ToolChoiceAuto), ok: true},
		{name: "none", in: "none", want: mode(provider.ToolChoiceNone), ok: true},
		{name: "required", in: "required", want: mode(provider.ToolChoiceRequired), ok: true},
		{name: "any", in: "any", want: mode(provider.ToolChoiceRequired), ok: true},
		{name: "openai function map", in: openaiFn, want: fn("f"), ok: true},
		{name: "provider struct", in: fn("f"), want: fn("f"), ok: true},
		{name: "provider struct pointer", in: &provider.ToolChoice{Mode: provider.ToolChoiceNone}, want: mode(provider.ToolChoiceNone), ok: true},
		{name: "raw json object", in: json.RawMessage(`{"type":"function","function":{"name":"f"}}`), want: fn("f"), ok: true},
		{name: "raw json string", in: json.RawMessage(`"required"`), want: mode(provider.ToolChoiceRequired), ok: true},
		{name: "bytes", in: []byte(`{"type":"function","function":{"name":"f"}}`), want: fn("f"), ok: true},
		{name: "anthropic tool", in: map[string]any{"type": "tool", "name": "f"}, want: fn("f"), ok: true},
		{name: "anthropic any", in: map[string]any{"type": "any"}, want: mode(provider.ToolChoiceRequired), ok: true},
		{name: "anthropic auto", in: map[string]any{"type": "auto"}, want: mode(provider.ToolChoiceAuto), ok: true},
		{name: "typed struct round trip", in: struct {
			Type     string `json:"type"`
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		}{Type: "function", Function: struct {
			Name string `json:"name"`
		}{Name: "f"}}, want: fn("f"), ok: true},
		{name: "number", in: 42, err: true},
		{name: "unknown string", in: "sometimes", err: true},
		{name: "function without name", in: map[string]any{"type": "function"}, err: true},
		{name: "struct function without name", in: provider.ToolChoice{Mode: provider.ToolChoiceFunction}, err: true},
		{name: "bad json", in: json.RawMessage(`{`), err: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok, err := provider.ParseToolChoice(tt.in)
			if (err != nil) != tt.err {
				t.Fatalf("err = %v, want error %v", err, tt.err)
			}
			if ok != tt.ok || got != tt.want {
				t.Fatalf("got (%+v, %v), want (%+v, %v)", got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestParseToolChoiceErrorsAreNotSupported(t *testing.T) {
	for _, in := range []any{"sometimes", 42, map[string]any{"type": "function"}, json.RawMessage(`{`)} {
		if _, _, err := provider.ParseToolChoice(in); !errors.Is(err, provider.ErrNotSupported) {
			t.Errorf("ParseToolChoice(%v) = %v, want an error wrapping ErrNotSupported", in, err)
		}
	}
}

func TestToolChoiceInOpenAIShape(t *testing.T) {
	tests := []struct {
		in   provider.ToolChoice
		want any
	}{
		{provider.ToolChoice{Mode: provider.ToolChoiceAuto}, "auto"},
		{provider.ToolChoice{Mode: provider.ToolChoiceNone}, "none"},
		{provider.ToolChoice{Mode: provider.ToolChoiceRequired}, "required"},
		{provider.ToolChoice{Mode: provider.ToolChoiceFunction, Function: "f"}, map[string]any{"type": "function", "function": map[string]any{"name": "f"}}},
		{provider.ToolChoice{}, nil},
	}
	for _, tt := range tests {
		if got := tt.in.OpenAI(); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%+v.OpenAI() = %#v, want %#v", tt.in, got, tt.want)
		}
	}
}

func TestOpenAIToolChoicePassesThroughShapesOpenAIOwns(t *testing.T) {
	allowed := map[string]any{"type": "allowed_tools", "allowed_tools": map[string]any{
		"mode":  "auto",
		"tools": []any{map[string]any{"type": "function", "function": map[string]any{"name": "f"}}},
	}}
	custom := map[string]any{"type": "custom", "custom": map[string]any{"name": "grammar_tool"}}
	raw := json.RawMessage(`{"type":"custom","custom":{"name":"g"}}`)
	for name, in := range map[string]any{"allowed_tools": allowed, "custom": custom} {
		got, err := provider.OpenAIToolChoice(in)
		if err != nil || !reflect.DeepEqual(got, in) {
			t.Errorf("%s: got (%#v, %v), want it unchanged", name, got, err)
		}
	}
	if got, err := provider.OpenAIToolChoice(raw); err != nil || !reflect.DeepEqual(got, any(raw)) {
		t.Errorf("raw JSON: got (%#v, %v), want it unchanged", got, err)
	}
	// ParseToolChoice still does not model them: Anthropic refuses them.
	if _, _, err := provider.ParseToolChoice(allowed); !errors.Is(err, provider.ErrNotSupported) {
		t.Errorf("ParseToolChoice(allowed_tools) = %v, want ErrNotSupported", err)
	}
}

func TestOpenAIToolChoiceStillRefusesMalformedValues(t *testing.T) {
	for _, in := range []any{
		"sometimes", 42, map[string]any{}, map[string]any{"type": ""},
		map[string]any{"type": "function"}, map[string]any{"type": "tool"}, json.RawMessage(`{`),
	} {
		if _, err := provider.OpenAIToolChoice(in); !errors.Is(err, provider.ErrNotSupported) {
			t.Errorf("OpenAIToolChoice(%v) = %v, want an error wrapping ErrNotSupported", in, err)
		}
	}
	if got, err := provider.OpenAIToolChoice(nil); got != nil || err != nil {
		t.Errorf("OpenAIToolChoice(nil) = (%v, %v), want (nil, nil)", got, err)
	}
}
