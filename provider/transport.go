package provider

import "net/http"

// TransportWrapper is implemented by a provider that sends its requests
// through an HTTP client it owns and lets the gateway wrap that client's
// transport, to trace every outbound call for example. Every HTTP provider
// in this repo implements it; the WebSocket ones (geminilive,
// openairealtime) do not.
//
// The gateway calls WrapTransport once, from Initialize, with the function
// given to nexus.WithProviderTransport. wrap receives the client's current
// transport (nil means http.DefaultTransport) and returns the one to use. A
// nil result leaves the transport as it was. Call it before the first
// request, never while one is in flight.
type TransportWrapper interface {
	WrapTransport(wrap func(http.RoundTripper) http.RoundTripper)
}
