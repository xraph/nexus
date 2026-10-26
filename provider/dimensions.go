package provider

// DimensionsAware is implemented by a provider that can return embeddings
// of a requested width (EmbeddingRequest.Dimensions). SupportsDimensions
// reports whether it can for model. The gateway refuses a request with
// Dimensions set when the routed provider does not implement this or
// answers false, so the provider is never called with a field it would
// drop.
type DimensionsAware interface {
	SupportsDimensions(model string) bool
}
