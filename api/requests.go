package api

import (
	"encoding/json"
	"errors"
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
// it goes to the gateway log.
func (a *API) writePipelineError(w http.ResponseWriter, r *http.Request, err error) {
	a.logServerError(r, err)
	auth.WriteError(w, err)
}

// onAuthError is the OnError of the auth middleware: writePipelineError for
// a request that has not reached the pipeline.
func (a *API) onAuthError(w http.ResponseWriter, r *http.Request, err error) {
	a.writePipelineError(w, r, err)
}

// logServerError logs err when it answers as a 5xx, which is when the client
// is told nothing about it. No error message here carries a gateway key:
// every auth message is a fixed string.
func (a *API) logServerError(r *http.Request, err error) {
	if status, _ := pipeline.HTTPStatus(err); status < http.StatusInternalServerError {
		return
	}
	args := []any{"request_id", pipeline.RequestID(r.Context()), "path", r.URL.Path, "error", err.Error()}
	var ref *pipeline.RefusalError
	if errors.As(err, &ref) && ref.Cause != nil {
		args = append(args, "cause", ref.Cause.Error())
	}
	a.gw.Logger().Error("request failed", args...)
}
