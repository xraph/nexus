package pipeline_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/xraph/nexus/pipeline"
	"github.com/xraph/nexus/provider"
)

func TestARefusalCarriesItsCodeStatusAndWait(t *testing.T) {
	r := &pipeline.RefusalError{Code: pipeline.CodeRateLimited, Status: 429, Message: "60 requests a minute", Limit: "60", RetryAfter: 12 * time.Second}
	wrapped := fmt.Errorf("quota stage: %w", r)
	status, code := pipeline.HTTPStatus(wrapped)
	if status != 429 || code != "rate_limited" {
		t.Fatalf("HTTPStatus = %d %q", status, code)
	}
	if got := pipeline.RetryAfter(wrapped); got != 12*time.Second {
		t.Fatalf("RetryAfter = %v", got)
	}
	if r.Error() != "nexus: 60 requests a minute" {
		t.Fatalf("Error() = %q", r.Error())
	}
	var asRefusal pipeline.Refusal
	if !errors.As(wrapped, &asRefusal) {
		t.Fatal("a RefusalError must satisfy pipeline.Refusal")
	}
}

func TestAnyOtherErrorIsAnInternalError(t *testing.T) {
	status, code := pipeline.HTTPStatus(errors.New("upstream 502"))
	if status != 500 || code != "internal_error" {
		t.Fatalf("HTTPStatus = %d %q", status, code)
	}
	if pipeline.RetryAfter(errors.New("x")) != 0 {
		t.Fatal("RetryAfter of a plain error must be 0")
	}
}

func TestPermanentSurvivesWrapping(t *testing.T) {
	base := errors.New("nexus: no providers registered")
	err := fmt.Errorf("call: %w", pipeline.Permanent(base))
	if !pipeline.IsPermanent(err) || !errors.Is(err, base) {
		t.Fatal("a wrapped permanent error must stay permanent and keep its cause")
	}
	if pipeline.IsPermanent(base) || pipeline.Permanent(nil) != nil {
		t.Fatal("only marked errors are permanent, and Permanent(nil) is nil")
	}
}

func TestExecuteRefusesAStreamRequest(t *testing.T) {
	var log []string
	svc, err := pipeline.NewBuilder().Use(terminal{stage{"call", 350, &log}}).Build()
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Execute(t.Context(), &provider.CompletionRequest{Model: "m", Stream: true})
	status, code := pipeline.HTTPStatus(err)
	if status != 400 || code != pipeline.CodeInvalidRequest {
		t.Fatalf("Execute with Stream = %v (%d %s); want invalid_request", err, status, code)
	}
	if len(log) != 0 {
		t.Fatalf("a refused stream request must not reach any stage, ran %v", log)
	}
}
