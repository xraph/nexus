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
