package usage

// PricingStatus says whether a request's cost is known, and if not, why.
type PricingStatus string

const (
	// PricingPriced: the cost is the model's list price over the tokens the
	// provider reported.
	PricingPriced PricingStatus = "priced"
	// PricingUnpricedModel: the model has no price for the tokens used, so
	// the cost is unknown. It is never reported as $0.
	PricingUnpricedModel PricingStatus = "unpriced_model"
	// PricingCached: served from the response cache, so no provider was
	// called and the cost is exactly $0.
	PricingCached PricingStatus = "cached"
	// PricingNotCharged: no provider was called (the request was refused,
	// blocked before the call, or failed before reaching one), so the cost
	// is exactly $0.
	PricingNotCharged PricingStatus = "not_charged"
	// PricingUnknown: a provider was called but the request failed, so
	// whether and what it charged is unknown. The cost is nil, never $0.
	PricingUnknown PricingStatus = "unknown"
)

// CostUnknown reports whether a record with this status has no known cost.
func (s PricingStatus) CostUnknown() bool {
	return s == PricingUnpricedModel || s == PricingUnknown
}
