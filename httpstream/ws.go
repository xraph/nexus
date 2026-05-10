package httpstream

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/coder/websocket"

	"github.com/xraph/nexus/provider"
)

// WSHandler upgrades an HTTP request to a WebSocket connection that streams
// nexus events as JSON text frames. The protocol is:
//
//	client → server (text JSON):
//	  {"type":"start","request":{...CompletionRequest...}}
//	  {"type":"abort"}
//	  {"type":"audio_chunk","format":"pcm16","b64":"..."}    (multi-modal in)
//	  {"type":"image","mime_type":"image/png","b64":"..."}   (multi-modal in)
//
//	server → client (text JSON):
//	  StreamEvent objects (same shape as NDJSON)
//	  Final {"type":"done"} then graceful close.
//	  {"type":"error", error:{...}} on failure then close StatusInternalError.
//
// The handler is constructed with a CompletionStreamer — typically the
// gateway engine — that produces the provider.Stream for a parsed request.
// Auth lives in the parent middleware chain; the handler reads from
// r.Context() and respects auth/tenant decisions made before the upgrade.
type WSHandler struct {
	streamer CompletionStreamer
	opts     WSOptions
}

// CompletionStreamer is the minimal interface WSHandler needs from the
// nexus gateway. Engine.CompleteStream satisfies this implicitly.
type CompletionStreamer interface {
	CompleteStream(ctx context.Context, req *provider.CompletionRequest) (provider.Stream, error)
}

// WSOptions configures the WebSocket handler.
type WSOptions struct {
	// AcceptOrigins is the allowed list for the Origin check. Empty means
	// the same-origin policy (nhooyr default). Pass `[]string{"*"}` to
	// allow any origin (use only for trusted networks).
	AcceptOrigins []string

	// SendBuffer is the buffered channel size feeding the writer goroutine.
	// Default: 64.
	SendBuffer int

	// HeartbeatInterval, when > 0, sends WS pings on the configured cadence
	// to keep the connection alive across idle proxies. Default: 20s.
	HeartbeatInterval time.Duration
}

// NewWSHandler returns a WebSocket handler.
func NewWSHandler(streamer CompletionStreamer, opts WSOptions) *WSHandler {
	if opts.SendBuffer <= 0 {
		opts.SendBuffer = 64
	}
	if opts.HeartbeatInterval == 0 {
		opts.HeartbeatInterval = 20 * time.Second
	}
	return &WSHandler{streamer: streamer, opts: opts}
}

// ServeHTTP implements http.Handler.
func (h *WSHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	acceptOpts := &websocket.AcceptOptions{
		OriginPatterns: h.opts.AcceptOrigins,
	}
	if len(h.opts.AcceptOrigins) == 1 && h.opts.AcceptOrigins[0] == "*" {
		acceptOpts.InsecureSkipVerify = true
	}

	conn, err := websocket.Accept(w, r, acceptOpts)
	if err != nil {
		// Accept already wrote the error response.
		return
	}
	defer func() { _ = conn.CloseNow() }() //nolint:errcheck // best-effort close

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// First message must be a "start" envelope describing the request.
	startEnv, err := h.readEnvelope(ctx, conn)
	if err != nil {
		_ = conn.Close(websocket.StatusInvalidFramePayloadData, "expected start envelope")
		return
	}
	if startEnv.Type != "start" || startEnv.Request == nil {
		_ = conn.Close(websocket.StatusInvalidFramePayloadData, "first frame must be start")
		return
	}

	stream, err := h.streamer.CompleteStream(ctx, startEnv.Request)
	if err != nil {
		errEv := &StreamEvent{Type: EventTypeError, Err: SanitizeError(err, "")}
		_ = writeJSONFrame(ctx, conn, errEv) //nolint:errcheck // best-effort: connection may already be torn
		_ = conn.Close(websocket.StatusInternalError, "stream init failed")
		return
	}
	defer func() { _ = stream.Close() }()

	// Type-assert to BiStream — when the upstream provider supports
	// bidirectional traffic (OpenAI Realtime, Gemini Live), client
	// envelopes are forwarded; otherwise they're dropped silently.
	bi, _ := stream.(provider.BiStream) //nolint:errcheck // assertion is safe; nil branch handled by readerLoop

	// Reader goroutine: watches for abort frames + multi-modal client input.
	go h.readerLoop(ctx, conn, cancel, bi)

	// Heartbeat ticker for WS-level pings.
	if h.opts.HeartbeatInterval > 0 {
		go h.heartbeatLoop(ctx, conn)
	}

	// Drive writes from the stream.
	for {
		chunk, err := stream.Next(ctx)
		if errors.Is(err, io.EOF) {
			_ = writeJSONFrame(ctx, conn, &StreamEvent{Type: EventTypeDone}) //nolint:errcheck // best-effort
			_ = conn.Close(websocket.StatusNormalClosure, "")
			return
		}
		if err != nil {
			ev := &StreamEvent{Type: EventTypeError, Err: SanitizeError(err, "")}
			_ = writeJSONFrame(ctx, conn, ev) //nolint:errcheck // best-effort
			_ = conn.Close(websocket.StatusInternalError, "stream error")
			return
		}
		if chunk == nil {
			continue
		}
		ev := FromChunk(chunk, "")
		if err := writeJSONFrame(ctx, conn, ev); err != nil {
			cancel()
			return
		}
	}
}

func (h *WSHandler) readerLoop(ctx context.Context, conn *websocket.Conn, cancel context.CancelFunc, bi provider.BiStream) {
	for {
		env, err := h.readEnvelope(ctx, conn)
		if err != nil {
			cancel()
			return
		}
		switch env.Type {
		case "abort":
			cancel()
			return
		case "audio_chunk":
			if bi == nil {
				continue
			}
			ce := provider.ClientEvent{Type: "audio_chunk"}
			if env.Audio != nil {
				ce.Audio = &provider.AudioChunk{
					Format:     env.Audio.Format,
					SampleRate: env.Audio.SampleRate,
					Data:       decodeB64(env.Audio.B64),
				}
			}
			_ = bi.Send(ctx, ce) //nolint:errcheck // best-effort
		case "image":
			if bi == nil {
				continue
			}
			ce := provider.ClientEvent{Type: "image"}
			if env.Image != nil {
				ce.Image = &provider.ImageChunk{
					MimeType: env.Image.MimeType,
					Data:     decodeB64(env.Image.B64),
				}
			}
			_ = bi.Send(ctx, ce) //nolint:errcheck // best-effort
		case "commit", "cancel":
			if bi == nil {
				continue
			}
			_ = bi.Send(ctx, provider.ClientEvent{Type: env.Type}) //nolint:errcheck // best-effort forward to upstream
		default:
			// Unknown envelope — ignore rather than tear the connection.
			continue
		}
	}
}

func decodeB64(s string) []byte {
	if s == "" {
		return nil
	}
	data, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil
	}
	return data
}

func (h *WSHandler) heartbeatLoop(ctx context.Context, conn *websocket.Conn) {
	t := time.NewTicker(h.opts.HeartbeatInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := conn.Ping(pingCtx)
			cancel()
			if err != nil {
				return
			}
		}
	}
}

func (h *WSHandler) readEnvelope(ctx context.Context, conn *websocket.Conn) (*wsClientEnvelope, error) {
	_, data, err := conn.Read(ctx)
	if err != nil {
		return nil, err
	}
	var env wsClientEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("ws: invalid envelope: %w", err)
	}
	return &env, nil
}

// wsClientEnvelope is the union of inbound message shapes.
type wsClientEnvelope struct {
	Type    string                      `json:"type"`
	Request *provider.CompletionRequest `json:"request,omitempty"`
	Audio   *wsAudioChunk               `json:"audio,omitempty"`
	Image   *wsImageChunk               `json:"image,omitempty"`
}

type wsAudioChunk struct {
	Format     string `json:"format,omitempty"`
	SampleRate int    `json:"sample_rate,omitempty"`
	B64        string `json:"b64,omitempty"`
}

type wsImageChunk struct {
	MimeType string `json:"mime_type,omitempty"`
	B64      string `json:"b64,omitempty"`
}

func writeJSONFrame(ctx context.Context, conn *websocket.Conn, ev *StreamEvent) error {
	if ev == nil {
		return nil
	}
	data, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	return conn.Write(ctx, websocket.MessageText, data)
}
