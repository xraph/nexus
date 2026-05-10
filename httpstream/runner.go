package httpstream

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/xraph/nexus/provider"
)

// RunOptions tunes the streaming event loop. Zero value is reasonable
// production defaults: 15-second heartbeat, no per-event timeout, accept
// the request id from the caller.
type RunOptions struct {
	// HeartbeatInterval controls keepalive emission. The runner only sends
	// a heartbeat if no real event was flushed within the interval.
	// Zero defaults to 15s. Set to a negative value to disable.
	HeartbeatInterval time.Duration

	// RequestID is echoed into wire events for client-side correlation.
	RequestID string

	// OnError is called once when the stream terminates with an error
	// (after the encoder has emitted the typed error event but before
	// End is called). Useful for recording mid-stream failures upstream.
	OnError func(error)
}

// Run is the single shared streaming event loop used by both /v1/chat/
// completions (proxy) and the native nexus completions endpoint (api).
// It writes encoder.WriteHeaders, drains stream.Next in a loop, fires
// heartbeats during idle periods, sanitizes mid-stream errors, calls
// stream.Close exactly once, and writes encoder.End before returning.
//
// Cancellation: when ctx is canceled (HTTP client disconnect, server
// shutdown, abort frame on a WebSocket), the runner stops emitting and
// closes the stream. The provider stream's own ctx-AfterFunc tears the
// upstream connection.
//
// Concurrency: a single goroutine drives the loop; heartbeats are
// scheduled with a time.Ticker on the same goroutine using a
// non-blocking select. Encoder writes are not concurrent with each other.
func Run(ctx context.Context, w http.ResponseWriter, stream provider.Stream, encoder StreamEncoder, opts RunOptions) {
	if encoder == nil {
		http.Error(w, "no stream encoder", http.StatusInternalServerError)
		return
	}
	flusher, _ := w.(http.Flusher) //nolint:errcheck // optional; absence is non-fatal but degrades to one-shot writes

	defer func() { _ = stream.Close() }()

	encoder.WriteHeaders(w)
	w.WriteHeader(http.StatusOK)
	flush(w, flusher)

	heartbeat := opts.HeartbeatInterval
	if heartbeat == 0 {
		heartbeat = 15 * time.Second
	}

	var ticker *time.Ticker
	var tickerC <-chan time.Time
	if heartbeat > 0 {
		ticker = time.NewTicker(heartbeat)
		tickerC = ticker.C
		defer ticker.Stop()
	}

	// Run the read loop in a goroutine so the main goroutine can multiplex
	// chunk arrivals with heartbeat ticks. Capacity 1 — backpressure is
	// fine; we drain serially anyway.
	type readResult struct {
		chunk *provider.StreamChunk
		err   error
	}
	resultsCh := make(chan readResult, 1)
	readerCtx, cancelReader := context.WithCancel(ctx)
	defer cancelReader()

	var readerOnce sync.Once
	startNextRead := func() {
		readerOnce.Do(func() {})
		go func() {
			c, err := stream.Next(readerCtx)
			select {
			case resultsCh <- readResult{c, err}:
			case <-readerCtx.Done():
			}
		}()
	}
	startNextRead()

	for {
		select {
		case <-ctx.Done():
			cancelReader()
			emitError(w, flusher, encoder, ctx.Err(), opts.RequestID)
			endStream(w, flusher, encoder)
			return
		case <-tickerC:
			if err := encoder.Heartbeat(w); err == nil {
				flush(w, flusher)
			}
		case res := <-resultsCh:
			if errors.Is(res.err, io.EOF) {
				endStream(w, flusher, encoder)
				return
			}
			if res.err != nil {
				emitError(w, flusher, encoder, res.err, opts.RequestID)
				endStream(w, flusher, encoder)
				if opts.OnError != nil {
					opts.OnError(res.err)
				}
				return
			}
			if res.chunk != nil {
				ev := FromChunk(res.chunk, opts.RequestID)
				if err := encoder.EncodeEvent(w, ev); err != nil {
					if opts.OnError != nil {
						opts.OnError(err)
					}
					return
				}
				flush(w, flusher)
			}
			startNextRead()
		}
	}
}

func emitError(w http.ResponseWriter, flusher http.Flusher, enc StreamEncoder, err error, requestID string) {
	werr := SanitizeError(err, requestID)
	if werr == nil {
		return
	}
	_ = enc.EncodeError(w, werr) //nolint:errcheck // best-effort; connection may already be torn
	flush(w, flusher)
}

func endStream(w http.ResponseWriter, flusher http.Flusher, enc StreamEncoder) {
	_ = enc.End(w) //nolint:errcheck // best-effort; connection may already be torn
	flush(w, flusher)
}

func flush(w http.ResponseWriter, flusher http.Flusher) {
	_ = w
	if flusher != nil {
		flusher.Flush()
	}
}
