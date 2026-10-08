// Package contract serves the operator-wide Nexus dashboard contract.
package contract

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"

	dash "github.com/xraph/forge/extensions/dashboard/contract"
	"github.com/xraph/forge/extensions/dashboard/contract/dispatcher"
	"github.com/xraph/forge/extensions/dashboard/contract/loader"

	nexus "github.com/xraph/nexus"
)

// ContributorName matches the extension and React plugin identity.
const ContributorName = "nexus"

//go:embed manifest.yaml
var manifestYAML []byte

// Deps resolves the gateway after extension startup. Logger is optional.
type Deps struct {
	Gateway func() *nexus.Gateway
	Logger  nexus.Logger
}

type binding struct {
	name string
	kind dash.IntentKind
	bind func(*dispatcher.Dispatcher) error
}

type handler[I, O any] func(context.Context, *nexus.Gateway, I) (O, error)

func withGateway[I, O any](deps Deps, intent string, fn handler[I, O]) func(context.Context, I, dash.Principal) (O, error) {
	return func(ctx context.Context, in I, _ dash.Principal) (O, error) {
		var zero O
		gw, err := gateway(deps)
		if err != nil {
			return zero, err
		}
		out, err := fn(ctx, gw, in)
		if err != nil {
			return zero, deps.mapError(intent, err)
		}
		return out, nil
	}
}

func query[I, O any](deps Deps, name string, fn handler[I, O]) binding {
	return binding{name: name, kind: dash.IntentKindQuery, bind: func(d *dispatcher.Dispatcher) error {
		return dispatcher.RegisterQuery(d, ContributorName, name, 1, withGateway(deps, name, fn))
	}}
}

func bindings(deps Deps) []binding {
	return []binding{query(deps, "settings.get", settingsGet)}
}

// Register validates the manifest and binds every declared intent.
func Register(d *dispatcher.Dispatcher, reg dash.Registry, wreg dash.WardenRegistry, deps Deps) error {
	if deps.Gateway == nil {
		return fmt.Errorf("nexus/contract: Gateway resolver is required")
	}
	m, err := loader.Load(bytes.NewReader(manifestYAML), "nexus/contract/manifest.yaml")
	if err != nil {
		return fmt.Errorf("nexus/contract: load manifest: %w", err)
	}
	if err := loader.Validate(m, wreg); err != nil {
		return fmt.Errorf("nexus/contract: validate manifest: %w", err)
	}
	declared := make(map[string]dash.IntentKind, len(m.Intents))
	for _, in := range m.Intents {
		declared[in.Name] = in.Kind
	}
	bs := bindings(deps)
	if len(bs) != len(declared) {
		return fmt.Errorf("nexus/contract: manifest and bindings differ")
	}
	seen := make(map[string]bool, len(bs))
	for _, b := range bs {
		if seen[b.name] || declared[b.name] != b.kind {
			return fmt.Errorf("nexus/contract: invalid binding %s", b.name)
		}
		seen[b.name] = true
	}
	if err := reg.Register(m); err != nil {
		return fmt.Errorf("nexus/contract: register manifest: %w", err)
	}
	for _, b := range bs {
		if err := b.bind(d); err != nil {
			return fmt.Errorf("nexus/contract: bind %s: %w", b.name, err)
		}
	}
	return nil
}

func gateway(deps Deps) (*nexus.Gateway, error) {
	if deps.Gateway != nil {
		if gw := deps.Gateway(); gw != nil && gw.Engine() != nil {
			return gw, nil
		}
	}
	return nil, &dash.Error{Code: dash.CodeUnavailable, Message: "Nexus is starting", Retryable: true}
}
