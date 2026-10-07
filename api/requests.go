package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/xraph/nexus/auth"
	"github.com/xraph/nexus/pipeline"
)

// writeJSON writes a JSON response.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		return
	}
}

// writeError writes an error response in OpenAI-compatible format.
func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{
			"message": message,
			"type":    mapStatusToErrorType(status),
			"code":    mapStatusToCode(status),
		},
	}); err != nil {
		return
	}
}

func mapStatusToErrorType(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "invalid_request_error"
	case http.StatusUnauthorized:
		return "authentication_error"
	case http.StatusForbidden:
		return "permission_error"
	case http.StatusNotFound:
		return "not_found_error"
	case http.StatusTooManyRequests:
		return "rate_limit_error"
	case http.StatusNotImplemented:
		return "not_implemented"
	default:
		return "internal_error"
	}
}

func mapStatusToCode(status int) string {
	switch status {
	case http.StatusBadRequest:
		return pipeline.CodeInvalidRequest
	case http.StatusNotFound:
		return "not_found"
	case http.StatusNotImplemented:
		return "not_implemented"
	default:
		return "internal_error"
	}
}

// writePipelineError answers an error from the engine with the refusal's
// status and code, or a fixed 500, and Retry-After when the refusal says how
// long to wait. The cause of a server-side failure never reaches the client;
// it goes to the gateway log, with the request id.
func (a *API) writePipelineError(w http.ResponseWriter, r *http.Request, err error) {
	auth.WriteFailure(w, r, a.gw.Logger(), err)
}

// onAuthError is the OnError of the auth middleware.
func (a *API) onAuthError(w http.ResponseWriter, r *http.Request, err error) {
	a.writePipelineError(w, r, err)
}

// onStreamError logs the cause of a stream that failed after the response
// began, where the client was told only the sanitized envelope.
func (a *API) onStreamError(ctx context.Context, err error) {
	auth.LogServerError(ctx, a.gw.Logger(), "", err)
}
