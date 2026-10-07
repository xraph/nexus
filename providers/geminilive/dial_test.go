package geminilive

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coder/websocket"
)

// The Live handshake carries the API key in x-goog-api-key and never in the
// URL. A failed dial's error prints the URL, and that text reaches logs.
func TestTheLiveKeyTravelsInTheHandshakeHeader(t *testing.T) {
	const secret = "AIza-test-secret"
	var query string
	var header bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query, header = r.URL.RawQuery, r.Header.Get("x-goog-api-key") == secret
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		_ = c.Close(websocket.StatusNormalClosure, "")
	}))
	defer srv.Close()

	conn, err := dialDefault(context.Background(), "ws"+strings.TrimPrefix(srv.URL, "http"), secret)
	if err != nil {
		t.Fatalf("dial failed: %v", strings.ReplaceAll(err.Error(), secret, "<key>"))
	}
	_ = conn.Close(websocket.StatusNormalClosure, "")
	if strings.Contains(query, "key=") || strings.Contains(query, secret) {
		t.Error("the handshake URL carries the key")
	}
	if !header {
		t.Error("the handshake has no x-goog-api-key header with the key")
	}
}

func TestAFailedDialDoesNotPrintTheKey(t *testing.T) {
	const secret = "AIza-test-secret"
	_, err := dialDefault(context.Background(), "ws://127.0.0.1:1/ws", secret)
	if err == nil {
		t.Fatal("dial to a closed port succeeded")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatal("the dial error carries the key")
	}
}
