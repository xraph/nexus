package provider

import "context"

// BiStream extends Stream with a client → server channel for true
// bidirectional streaming providers (OpenAI Realtime, Gemini Live).
//
// Unidirectional providers do not implement BiStream; the WebSocket
// handler in package httpstream type-asserts the inner stream and only
// forwards client multi-modal envelopes when the assertion succeeds.
type BiStream interface {
	Stream

	// Send pushes a client event upstream. May return ErrNotSupported on
	// streams that don't accept the requested event type.
	Send(ctx context.Context, evt ClientEvent) error
}

// ClientEvent is a unified envelope for inbound (client → server) events on
// a bidirectional stream. The discriminator is Type; consumers populate the
// matching pointer field.
type ClientEvent struct {
	// Type identifies the variant. Common values:
	//   "audio_chunk" — Audio populated; appends a partial audio buffer
	//   "image"       — Image populated; uploads a still frame
	//   "commit"      — finalize the current input turn (Realtime API)
	//   "cancel"      — abort the current response generation
	//   "control"     — provider-specific Data payload
	Type string

	Audio *AudioChunk
	Image *ImageChunk

	// Data is a free-form payload for provider-specific control messages.
	// JSON-marshalable; the upstream provider deserializes it.
	Data any
}
