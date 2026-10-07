package auth

import (
	"context"
	"errors"
	"net/http"

	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/pipeline"
)

// Logger is the part of the gateway logger the HTTP edges need.
// nexus.Logger satisfies it.
type Logger interface {
	Error(msg string, args ...any)
}

// RequestID gives every request an id of its own and returns it in the
// X-Request-Id response header. It never takes an id from the client: the
// header is the gateway's to set. The pipeline keeps an id that is already in
// the context, so the usage record, the logs and the header all name the same
// request. Put it ahead of KeyAuth so that a refusal at the edge has one too.
func RequestID() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rid := id.NewRequestID().String()
			w.Header().Set("X-Request-Id", rid)
			next.ServeHTTP(w, r.WithContext(pipeline.WithRequestID(r.Context(), rid)))
		})
	}
}

// LogServerError logs err, with the request id, when the client is told
// nothing about it: an error that is not a refusal (answered as a fixed 500)
// and a refusal with a status of 500 or above (the 503 whose cause is hidden).
// A cancel is the client leaving and is not logged. The logged text is the
// error's own: the auth messages are fixed strings and never carry a key.
func LogServerError(ctx context.Context, log Logger, path string, err error) {
	if log == nil || err == nil || errors.Is(err, context.Canceled) {
		return
	}
	if status, _ := pipeline.HTTPStatus(err); status < http.StatusInternalServerError {
		return
	}
	args := []any{"request_id", pipeline.RequestID(ctx), "error", err.Error()}
	if path != "" {
		args = append(args, "path", path)
	}
	var ref *pipeline.RefusalError
	if errors.As(err, &ref) && ref.Cause != nil {
		args = append(args, "cause", ref.Cause.Error())
	}
	log.Error("request failed", args...)
}

// WriteFailure logs err when it answers as a 5xx, then writes it with
// WriteError: the refusal's own status and text, or a fixed 500.
func WriteFailure(w http.ResponseWriter, r *http.Request, log Logger, err error) {
	LogServerError(r.Context(), log, r.URL.Path, err)
	WriteError(w, err)
}
