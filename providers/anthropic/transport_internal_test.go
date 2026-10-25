package anthropic

import (
	"errors"
	"net/http"
	"testing"
	"time"
)

// otherRoundTripper stands in for an http.DefaultTransport someone replaced.
type otherRoundTripper struct{}

func (otherRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("unused")
}

func TestReplacedDefaultTransportKeepsTheStockSettings(t *testing.T) {
	tr := newTransport(otherRoundTripper{}, time.Minute)
	if tr.DialContext == nil || tr.TLSHandshakeTimeout == 0 || tr.IdleConnTimeout == 0 || !tr.ForceAttemptHTTP2 || tr.Proxy == nil {
		t.Fatalf("fallback transport lost the stock settings: %+v", tr)
	}
	if tr.ResponseHeaderTimeout != time.Minute {
		t.Fatalf("ResponseHeaderTimeout = %v, want 1m", tr.ResponseHeaderTimeout)
	}
}

func TestShortKeysAreNotCutFromErrors(t *testing.T) {
	err := apiError(401, []byte(`{"error":{"message":"mock error"}}`), "k")
	if err.Error() != `anthropic: API error (status 401): {"error":{"message":"mock error"}}` {
		t.Fatalf("err = %v, want the body intact", err)
	}
	err = apiError(401, []byte(`bad key sk-ant-long-secret`), "sk-ant-long-secret")
	if err.Error() != `anthropic: API error (status 401): bad key [redacted]` {
		t.Fatalf("err = %v, want the long key cut out", err)
	}
}
