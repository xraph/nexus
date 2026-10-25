package middlewares

import (
	"context"
	"errors"
	"fmt"

	"github.com/xraph/nexus/pipeline"
	"github.com/xraph/nexus/provider"
)

// ErrModelUnpriced refuses a request for a model that has no price at the
// provider it was routed to. It is wrapped with the model and the provider;
// match it with errors.Is.
var ErrModelUnpriced error = &pipeline.RefusalError{Code: pipeline.CodeModelUnpriced, Status: 400, Message: "model has no price"}

// UnpricedPolicy decides whether a request for a model with no price may
// reach its provider. The provider call asks it only when the model the
// request was routed to has no price there for the kind of request: no
// input or output price for a completion or a stream, no embedding price
// for an embedding. tenantID is the request's nexus tenant, empty for an
// unattributed request. A nil error lets the call through; its cost is
// recorded as unknown. A refusal (such as ErrModelUnpriced) refuses the
// request at $0; any other error fails it, and neither is retried.
type UnpricedPolicy func(ctx context.Context, tenantID, providerName, modelID string) error

// RefuseUnpriced is the policy that refuses every request for a model with
// no price, so no spend a budget cannot see reaches a provider.
func RefuseUnpriced(_ context.Context, _, providerName, modelID string) error {
	return fmt.Errorf("%w: %s at %s", ErrModelUnpriced, modelID, providerName)
}

// WithUnpricedPolicy asks policy, before each provider call, about a model
// that prices has no price for. A nil policy or nil prices asks nothing.
func (m *ProviderCallMiddleware) WithUnpricedPolicy(prices PriceLookup, policy UnpricedPolicy) *ProviderCallMiddleware {
	m.prices, m.unpriced = prices, policy
	return m
}

// checkPriced runs the unpriced policy when modelID has no price at p for
// this kind of request. A refusal from the policy is returned as it is; any
// other error is marked permanent, so a retry does not ask again.
func (m *ProviderCallMiddleware) checkPriced(ctx context.Context, p provider.Provider, modelID string, embedding bool) error {
	if m.unpriced == nil || m.prices == nil {
		return nil
	}
	if price, ok := m.prices.Price(ctx, p.Name(), modelID); ok && pricedFor(price, embedding) {
		return nil
	}
	err := m.unpriced(ctx, pipeline.TenantID(ctx), p.Name(), modelID)
	if err == nil {
		return nil
	}
	var refused pipeline.Refusal
	if errors.As(err, &refused) {
		return err
	}
	return pipeline.Permanent(fmt.Errorf("nexus: unpriced model policy: %w", err))
}

// pricedFor reports whether price can price a request of this kind. It is
// model.Cost's rule: Free, an embedding price for an embedding, an input or
// output price for anything else.
func pricedFor(price provider.Pricing, embedding bool) bool {
	switch {
	case price.Free:
		return true
	case embedding:
		return !price.EmbeddingPerMillion.IsZero()
	default:
		return !price.InputPerMillion.IsZero() || !price.OutputPerMillion.IsZero()
	}
}
