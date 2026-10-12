package pipeline

// Request.State keys more than one stage reads.
const (
	// StateProviderName is the name of the provider the terminal stage
	// called, set before the call so a failed call is still attributed.
	StateProviderName = "provider_name"
	// StateCacheHit is true when the response came from the response cache
	// or the stream cache.
	StateCacheHit = "cache_hit"
	// StateOriginalModel is the model name the caller asked for, set by the
	// alias stage when it rewrote the request to the alias's target.
	StateOriginalModel = "original_model"
)
