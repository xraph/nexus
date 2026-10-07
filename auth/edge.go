package auth

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"

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
// As a backstop, query values that name a credential (key, api_key, apikey,
// access_token, token) are redacted from the logged text, because a
// provider's transport error prints the URL it called.
func LogServerError(ctx context.Context, log Logger, path string, err error) {
	if log == nil || err == nil || errors.Is(err, context.Canceled) {
		return
	}
	if status, _ := pipeline.HTTPStatus(err); status < http.StatusInternalServerError {
		return
	}
	args := []any{"request_id", pipeline.RequestID(ctx), "error", redact(err)}
	if path != "" {
		args = append(args, "path", path)
	}
	var ref *pipeline.RefusalError
	if errors.As(err, &ref) && ref.Cause != nil {
		args = append(args, "cause", redact(ref.Cause))
	}
	log.Error("request failed", args...)
}

// WriteFailure logs err when it answers as a 5xx, then writes it with
// WriteError: the refusal's own status and text, or a fixed 500.
func WriteFailure(w http.ResponseWriter, r *http.Request, log Logger, err error) {
	LogServerError(r.Context(), log, r.URL.Path, err)
	WriteError(w, err)
}

// secretParams are the query parameters redact hides.
var secretParams = map[string]bool{"key": true, "api_key": true, "apikey": true, "access_token": true, "token": true}

// secretQuery finds a credential-named query value in free text.
var secretQuery = regexp.MustCompile(`(?i)([?&](?:key|api_key|apikey|access_token|token)=)[^&\s"'#]*`)

// redact returns err's text with every credential-named query value
// replaced by REDACTED. It rewrites the URL of a *url.Error it wraps
// properly, then sweeps the whole text with a pattern for the cases where
// the URL was formatted into a plain string. It returns "" for a nil error.
func redact(err error) string {
	if err == nil {
		return ""
	}
	text := err.Error()
	var ue *url.Error
	if errors.As(err, &ue) {
		if u, perr := url.Parse(ue.URL); perr == nil {
			q, changed := u.Query(), false
			for name := range q {
				if secretParams[strings.ToLower(name)] {
					q.Set(name, "REDACTED")
					changed = true
				}
			}
			if changed {
				u.RawQuery = q.Encode()
				text = strings.ReplaceAll(text, ue.URL, u.String())
			}
		}
	}
	return secretQuery.ReplaceAllString(text, "${1}REDACTED")
}
