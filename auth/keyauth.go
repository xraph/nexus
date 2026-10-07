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

// RawKey returns the key a request presents, from Authorization: Bearer or
// x-api-key, or "".
func RawKey(r *http.Request) string {
	if v := r.Header.Get("x-api-key"); v != "" {
		return strings.TrimSpace(v)
	}
	if v, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

// Authenticate validates rawKey and returns ctx carrying the key's tenant,
// id and scopes for the pipeline. A gRPC interceptor can call it too. Every
// error message is a fixed string: none carries the key.
func Authenticate(ctx context.Context, keys KeyValidator, rawKey string) (context.Context, error) {
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
	// still checked.
	Required bool
	// OnError writes a refusal. The default is WriteError.
	OnError func(http.ResponseWriter, *http.Request, error)
}

// KeyAuth authenticates every request with a gateway key. Its refusals
// happen before the pipeline and are not recorded as usage: they have no
// tenant to charge.
func KeyAuth(o KeyAuthOptions) func(http.Handler) http.Handler {
	onError := o.OnError
	if onError == nil {
		onError = func(w http.ResponseWriter, _ *http.Request, err error) { WriteError(w, err) }
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := RawKey(r)
			if raw == "" && !o.Required {
				next.ServeHTTP(w, r)
				return
			}
			ctx, err := Authenticate(r.Context(), o.Keys, raw)
			if err != nil {
				onError(w, r, err)
				return
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireScope refuses an authenticated request whose key lacks scope. A
// request that no key authenticated (an open gateway, or an in-process
// caller) carries no scopes and passes, so put RequireScope behind KeyAuth.
func RequireScope(scope string, onError func(http.ResponseWriter, *http.Request, error)) func(http.Handler) http.Handler {
	if onError == nil {
		onError = func(w http.ResponseWriter, _ *http.Request, err error) { WriteError(w, err) }
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if scopes, ok := pipeline.Scopes(r.Context()); ok && !slices.Contains(scopes, scope) {
				onError(w, r, &pipeline.RefusalError{Code: pipeline.CodeForbidden, Status: 403, Message: "the key lacks the " + scope + " scope"})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// WriteError writes err as {"error":{"message","type","code"}} with the
// refusal's status, and Retry-After in whole seconds when the refusal says
// how long to wait.
func WriteError(w http.ResponseWriter, err error) {
	status, code := pipeline.HTTPStatus(err)
	if ra := pipeline.RetryAfter(err); ra > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(ra.Seconds()))))
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	body := map[string]any{"error": map[string]string{
		"message": err.Error(), "type": errorType(status), "code": code,
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
