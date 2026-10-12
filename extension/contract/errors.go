package contract

import (
	"context"
	"errors"

	dash "github.com/xraph/forge/extensions/dashboard/contract"

	"github.com/xraph/nexus/key"
	"github.com/xraph/nexus/paging"
	"github.com/xraph/nexus/tenant"
	"github.com/xraph/nexus/usage"
)

func badRequest(message string) error {
	return &dash.Error{Code: dash.CodeBadRequest, Message: message}
}

func mapError(err error) error {
	if err == nil {
		return nil
	}
	var ce *dash.Error
	switch {
	case errors.As(err, &ce):
		return ce
	case errors.Is(err, tenant.ErrNotFound), errors.Is(err, key.ErrNotFound):
		return &dash.Error{Code: dash.CodeNotFound, Message: "resource not found"}
	case errors.Is(err, tenant.ErrInUse), errors.Is(err, tenant.ErrDuplicate), errors.Is(err, key.ErrDuplicate):
		return &dash.Error{Code: dash.CodeConflict, Message: "the operation conflicts with an existing resource"}
	case errors.Is(err, tenant.ErrInvalid), errors.Is(err, key.ErrInvalid), errors.Is(err, paging.ErrInvalidCursor), errors.Is(err, usage.ErrInvalidPeriod), errors.Is(err, usage.ErrInvalidSeries):
		return badRequest("invalid request values")
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return &dash.Error{Code: dash.CodeUnavailable, Message: "the request was cancelled or timed out", Retryable: true}
	default:
		return &dash.Error{Code: dash.CodeInternal, Message: "an internal error occurred"}
	}
}

func (d Deps) mapError(intent string, err error) error {
	mapped := mapError(err)
	var ce *dash.Error
	if d.Logger != nil && errors.As(mapped, &ce) && ce.Code == dash.CodeInternal {
		d.Logger.Error("nexus/contract: intent failed", "intent", intent, "error", err)
	}
	return mapped
}
