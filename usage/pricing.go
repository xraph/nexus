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
)
