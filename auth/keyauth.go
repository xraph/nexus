package auth

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/xraph/nexus/key"
	"github.com/xraph/nexus/pipeline"
)

// KeyValidator checks a raw gateway key. key.Service satisfies it.
type KeyValidator interface {
	Validate(ctx context.Context, rawKey string) (*key.APIKey, error)
}

// RawKey returns the key a request presents, from x-api-key or
// Authorization: Bearer, or "".
func RawKey(r *http.Request) string {
	raw, _ := presented(r)
	return raw
}

// presented returns the key a request carries and whether it carries
// anything that claims to be one. A non-empty Authorization header that is
// not a usable Bearer key (Basic, an empty Bearer) still counts as
// presented, so it is checked and refused rather than read as no key.
func presented(r *http.Request) (raw string, present bool) {
	if v := strings.TrimSpace(r.Header.Get("x-api-key")); v != "" {
		return v, true
	}
	h := strings.TrimSpace(r.Header.Get("Authorization"))
	if h == "" {
		return "", false
	}
	if len(h) >= len("bearer ") && strings.EqualFold(h[:len("bearer ")], "bearer ") {
		return strings.TrimSpace(h[len("bearer "):]), true
	}
	return "", true
}

// Authenticate validates rawKey and returns ctx carrying the key's tenant,
// id and scopes for the pipeline. A gRPC interceptor can call it too. Every
// error message is a fixed string: none carries the key.
func Authenticate(ctx context.Context, keys KeyValidator, rawKey string) (context.Context, error) {
	rawKey = strings.TrimSpace(rawKey)
	if rawKey == "" {
		return ctx, unauthenticated("an API key is required")
	}
	k, err := keys.Validate(ctx, rawKey)
	switch {
	case errors.Is(err, key.ErrNotFound):
		return ctx, unauthenticated("invalid API key")
	case errors.Is(err, key.ErrRevoked):
		return ctx, unauthenticated("API key revoked")
	case errors.Is(err, key.ErrExpired):
		return ctx, unauthenticated("API key expired")
	case err != nil:
		return ctx, &pipeline.RefusalError{Code: pipeline.CodeUnavailable, Status: 503, Message: "key check is unavailable", Cause: err}
	}
	ctx = pipeline.WithTenantID(ctx, k.TenantID.String())
	ctx = pipeline.WithKeyID(ctx, k.ID.String())
	return pipeline.WithScopes(ctx, slices.Clone(k.Scopes)), nil
}

func unauthenticated(msg string) *pipeline.RefusalError {
	return &pipeline.RefusalError{Code: pipeline.CodeUnauthenticated, Status: 401, Message: msg}
}

// KeyAuthOptions configures KeyAuth.
type KeyAuthOptions struct {
	Keys KeyValidator
	// Required refuses a request without a key. When false, a request
	// without one passes unauthenticated, and a key that is presented is
	// still checked. Any non-empty Authorization header counts as presented,
	// so an unrelated Basic or Bearer credential sent to an open gateway
	// gets 401.
	Required bool
	// OnError writes a refusal. The default is WriteError.
	OnError func(http.ResponseWriter, *http.Request, error)
}

type checkedKey struct{}

func defaultOnError(w http.ResponseWriter, _ *http.Request, err error) { WriteError(w, err) }

// KeyAuth authenticates every request with a gateway key. Its refusals
// happen before the pipeline and are not recorded as usage: they have no
// tenant to charge. It marks every request it passes, anonymous ones too, so
// that RequireScope can tell "no key" from "KeyAuth never ran". It panics
// when o.Keys is nil.
func KeyAuth(o KeyAuthOptions) func(http.Handler) http.Handler {
	if o.Keys == nil {
		panic("auth: KeyAuth needs KeyAuthOptions.Keys")
	}
	onError := o.OnError
	if onError == nil {
		onError = defaultOnError
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw, present := presented(r)
			if !present && !o.Required {
				next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), checkedKey{}, struct{}{})))
				return
			}
			ctx, err := Authenticate(r.Context(), o.Keys, raw)
			if err != nil {
				onError(w, r, err)
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(ctx, checkedKey{}, struct{}{})))
		})
	}
}

// RequireScope refuses a request unless a key authenticated by KeyAuth holds
// scope: 401 when no key authenticated it (KeyAuth never ran, or let an
// anonymous request through), 403 when the key lacks the scope. It fails
// closed, so a route that needs a scope always needs a key, whatever
// KeyAuthOptions.Required says. Put it behind KeyAuth.
func RequireScope(scope string, onError func(http.ResponseWriter, *http.Request, error)) func(http.Handler) http.Handler {
	if onError == nil {
		onError = defaultOnError
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Context().Value(checkedKey{}) == nil {
				onError(w, r, unauthenticated("an API key is required"))
				return
			}
			scopes, ok := pipeline.Scopes(r.Context())
			if !ok {
				onError(w, r, unauthenticated("an API key is required"))
				return
			}
			if !slices.Contains(scopes, scope) {
				onError(w, r, &pipeline.RefusalError{Code: pipeline.CodeForbidden, Status: 403, Message: "the key lacks the " + scope + " scope"})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// WriteError writes err as {"error":{"message","type","code"}} with the
// refusal's status, and Retry-After in whole seconds when the refusal says
// how long to wait. Only a pipeline.Refusal's own text reaches the client,
// never a wrapper's: anything else (a provider or store error, which may
// carry URLs, bodies or DSNs) is answered as a 500 "internal error". A 401
// carries WWW-Authenticate: Bearer.
func WriteError(w http.ResponseWriter, err error) {
	status, code, msg := 500, "internal_error", "internal error"
	var ref pipeline.Refusal
	if errors.As(err, &ref) {
		status, code, msg = ref.StatusCode(), ref.RefusalCode(), ref.Error()
		if ra := pipeline.RetryAfter(err); ra > 0 {
			w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(ra.Seconds()))))
		}
	}
	if status < 400 || status > 599 {
		status = http.StatusInternalServerError // WriteHeader panics outside 100-999
	}
	if status == http.StatusUnauthorized {
		w.Header().Set("WWW-Authenticate", "Bearer")
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	body := map[string]any{"error": map[string]string{
		"message": msg, "type": errorType(status), "code": code,
	}}
	if encErr := json.NewEncoder(w).Encode(body); encErr != nil {
		return // the status is already sent and the client has gone
	}
}

func errorType(status int) string {
	switch status {
	case 400:
		return "invalid_request_error"
	case 401:
		return "authentication_error"
	case 403:
		return "permission_error"
	case 429:
		return "rate_limit_error"
	case 503:
		return "service_unavailable"
	}
	return "internal_error"
}
