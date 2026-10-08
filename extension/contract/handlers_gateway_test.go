package contract

import (
	"context"
	"errors"
	"testing"
	"time"

	dash "github.com/xraph/forge/extensions/dashboard/contract"

	nexus "github.com/xraph/nexus"
	"github.com/xraph/nexus/cache"
	"github.com/xraph/nexus/cache/stores"
	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/model"
	"github.com/xraph/nexus/money"
	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/store/storetest"
	"github.com/xraph/nexus/usage"
)

type catalogProvider struct {
	provider.Provider
	name        string
	models      []provider.Model
	err         error
	healthCalls int
	free        bool
}

func (p *catalogProvider) Name() string { return p.name }
func (p *catalogProvider) Capabilities() provider.Capabilities {
	return provider.Capabilities{Chat: true, StreamingTools: true}
}
func (p *catalogProvider) Models(context.Context) ([]provider.Model, error) { return p.models, p.err }
func (p *catalogProvider) Healthy(context.Context) bool                     { p.healthCalls++; return true }
func (p *catalogProvider) FreeOfCharge() bool                               { return p.free }

type customCache struct{ cache.Cache }

func TestCatalogPricingAndTrafficWithoutPings(t *testing.T) {
	p := &catalogProvider{name: "z", models: []provider.Model{{ID: "unknown"}, {ID: "priced", Pricing: provider.Pricing{InputPerMillion: money.MustParse("0.123456789")}}}}
	local := &catalogProvider{name: "a", models: []provider.Model{{ID: "local", Pricing: provider.Pricing{Free: true, InputPerMillion: money.MustParse("5")}}}}
	gw := testGateway(t, nexus.WithProvider(p), nexus.WithProvider(local))
	d := testDispatcher(t, Deps{Gateway: func() *nexus.Gateway { return gw }})
	for i := range 503 {
		r := storetest.Record(id.Nil, "0.1")
		r.KeyID = id.Nil
		r.Provider = "z"
		if i == 0 {
			r.Outcome = usage.OutcomeError
		}
		storetest.InsertRecord(t, gw.Store(), r)
	}
	old := storetest.Record(id.Nil, "9")
	old.KeyID = id.Nil
	old.Provider = "z"
	old.CreatedAt = time.Now().Add(-time.Hour)
	storetest.InsertRecord(t, gw.Store(), old)
	providers := mustDispatch(t, d, "providers.list", map[string]any{}, dash.KindQuery)["items"].([]any)
	if providers[0].(map[string]any)["name"] != "a" {
		t.Fatal("provider order is unstable")
	}
	z := providers[1].(map[string]any)
	if z["requests"] != float64(503) || z["errors"] != float64(1) || z["modelCount"] != float64(2) {
		t.Fatal("traffic was truncated or includes old records")
	}
	models := mustDispatch(t, d, "models.list", map[string]any{}, dash.KindQuery)["items"].([]any)
	a := models[0].(map[string]any)
	b := models[1].(map[string]any)
	c := models[2].(map[string]any)
	if a["free"] != true || a["inputPerMillionUsd"] != "0" || b["inputPerMillionUsd"] != "0.123456789" || c["priced"] != false || c["inputPerMillionUsd"] != nil {
		t.Fatal("free, priced and unknown models were conflated")
	}
	if p.healthCalls != 0 || local.healthCalls != 0 {
		t.Fatal("dashboard pinged providers")
	}
	local.models[0].Pricing = provider.Pricing{}
	local.free = true
	freeModels := mustDispatch(t, d, "models.list", map[string]any{}, dash.KindQuery)["items"].([]any)
	if freeModels[0].(map[string]any)["inputPerMillionUsd"] != "0" {
		t.Fatal("provider-wide free pricing was ignored")
	}
	p.err = errors.New("private provider error")
	_, err := dispatch(t, d, "models.list", `{}`, dash.KindQuery)
	if codeOf(err) != dash.CodeInternal || err.Error() == p.err.Error() {
		t.Fatal("catalog failure was hidden or leaked")
	}
}
func TestGatewayReportsAvailableInspectionOnly(t *testing.T) {
	for _, tc := range []struct {
		name         string
		opts         []nexus.Option
		kind, stream string
	}{
		{"none", nil, "none", "none"},
		{"memory", []nexus.Option{nexus.WithCache(stores.NewMemory())}, "memory", "none"},
		{"redis", []nexus.Option{nexus.WithCache(stores.NewRedis(nil))}, "redis", "none"},
		{"custom", []nexus.Option{nexus.WithCache(customCache{})}, "unknown", "none"},
		{"stream", []nexus.Option{nexus.WithStreamCache(stores.NewMemoryStream(), cache.StreamCacheOptions{})}, "none", "memory"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gw := testGateway(t, tc.opts...)
			d := testDispatcher(t, Deps{Gateway: func() *nexus.Gateway { return gw }})
			out := mustDispatch(t, d, "gateway.get", map[string]any{}, dash.KindQuery)
			c := out["cache"].(map[string]any)
			if c["kind"] != tc.kind || c["streamKind"] != tc.stream || c["size"] != nil || c["bytes"] != nil {
				t.Fatal("cache inspection fabricated a capability")
			}
			if tc.kind == "none" && c["hits"] != nil {
				t.Fatal("missing completion stats became zero")
			}
			for _, k := range []string{"guards", "aliases", "transforms"} {
				if len(out[k].([]any)) != 0 {
					t.Fatal("empty list was not an array")
				}
			}
			stages := out["stages"].([]any)
			last := stages[len(stages)-1].(map[string]any)
			if last["name"] != "provider_call" || last["terminal"] != true {
				t.Fatal("pipeline not inspected")
			}
		})
	}
}
func TestCatalogUsageOffAndAliasOrder(t *testing.T) {
	p := &catalogProvider{name: "a"}
	gw := testGateway(t, nexus.WithProvider(p), nexus.WithUsageEnabled(false), nexus.WithAlias("z", model.AliasTarget{Provider: "a", Model: "b"}), nexus.WithAlias("a", model.AliasTarget{Provider: "a", Model: "c"}))
	d := testDispatcher(t, Deps{Gateway: func() *nexus.Gateway { return gw }})
	out := mustDispatch(t, d, "providers.list", map[string]any{}, dash.KindQuery)
	pout := out["items"].([]any)[0].(map[string]any)
	if pout["requests"] != nil || pout["errors"] != nil || out["usageEnabled"] != false {
		t.Fatal("usage off traffic appeared measured")
	}
	aliases := mustDispatch(t, d, "gateway.get", map[string]any{}, dash.KindQuery)["aliases"].([]any)
	if aliases[0].(map[string]any)["name"] != "a" {
		t.Fatal("alias order is unstable")
	}
}
