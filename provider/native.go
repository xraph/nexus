package provider

import "encoding/json"

// NativeContent is an assistant reply in the provider's own wire format,
// kept beside the unified fields so the turn can be replayed exactly.
//
// Some models reject a history whose earlier turns were edited, signed
// thinking blocks included. Store Choices[0].Message.Native with the
// assistant message and send it back unchanged on the next request: a
// provider that recognises it sends Content as is while the message still
// matches it, and rebuilds the turn from the unified fields otherwise.
//
// It is never serialized, and the response and stream caches drop it on
// every backend, so a cached reply carries none. An output guard that
// rewrites a message drops it too.
type NativeContent struct {
	// Provider names the provider whose wire format Content is in.
	Provider string `json:"provider"`
	// Model is the model that produced the reply.
	Model string `json:"model"`
	// Content is the reply's content as that provider returned it.
	Content json.RawMessage `json:"content"`
}
