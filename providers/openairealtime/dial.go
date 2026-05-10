package openairealtime

import (
	"context"
	"fmt"
	"net/http"

	"github.com/coder/websocket"
)

// wsConn is the minimal WebSocket interface the stream uses. coder/websocket's
// *Conn satisfies it; tests inject a fakeConn.
type wsConn interface {
	Read(ctx context.Context) (websocket.MessageType, []byte, error)
	Write(ctx context.Context, t websocket.MessageType, b []byte) error
	Close(code websocket.StatusCode, reason string) error
}

// dialFunc is the upstream-connect signature; tests substitute it via the
// withDialer Option to avoid hitting api.openai.com.
type dialFunc func(ctx context.Context, baseURL, apiKey, model string) (wsConn, error)

func dialDefault(ctx context.Context, baseURL, apiKey, model string) (wsConn, error) {
	url := baseURL
	if model != "" {
		url = fmt.Sprintf("%s?model=%s", baseURL, model)
	}

	hdr := http.Header{}
	hdr.Set("Authorization", "Bearer "+apiKey)
	hdr.Set("OpenAI-Beta", "realtime=v1")

	conn, dialResp, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: hdr})
	if dialResp != nil && dialResp.Body != nil {
		_ = dialResp.Body.Close()
	}
	if err != nil {
		return nil, fmt.Errorf("openairealtime: dial: %w", err)
	}
	return conn, nil
}
