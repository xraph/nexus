// Package geminilive implements the Nexus provider interface against the
// Gemini Live WebSocket API. The upstream protocol is bidirectional and
// based on the Multimodal Live API event vocabulary; this package
// translates the Live envelope into nexus StreamChunk frames.
package geminilive

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/coder/websocket"

	"github.com/xraph/nexus/provider"
)

// SetupConfig is the optional Live API session-init payload. Non-nil fields
// extend the minimal default Setup{model} frame; zero values are omitted.
type SetupConfig struct {
	GenerationConfig  any            `json:"generation_config,omitempty"`
	SystemInstruction map[string]any `json:"system_instruction,omitempty"`
	Tools             []any          `json:"tools,omitempty"`
	RealtimeInputCfg  any            `json:"realtime_input_config,omitempty"`
}

// Provider implements provider.Provider against Gemini Live.
type Provider struct {
	apiKey  string
	baseURL string
	model   string
	setup   SetupConfig
	dial    dialFunc
}

// Option configures the Live provider.
type Option func(*Provider)

// WithBaseURL overrides the upstream WebSocket URL.
func WithBaseURL(u string) Option { return func(p *Provider) { p.baseURL = u } }

// WithModel sets the default model.
func WithModel(m string) Option { return func(p *Provider) { p.model = m } }

// WithSetup attaches an optional rich session-setup payload. Use this to
// pass generation_config, system_instruction, tools, or realtime_input_config
// to the Live API on session start.
func WithSetup(cfg SetupConfig) Option { return func(p *Provider) { p.setup = cfg } }

func withDialer(d dialFunc) Option { return func(p *Provider) { p.dial = d } }

// New returns a Live provider.
func New(apiKey string, opts ...Option) *Provider {
	p := &Provider{
		apiKey:  apiKey,
		baseURL: "wss://generativelanguage.googleapis.com/ws/google.ai.generativelanguage.v1beta.GenerativeService.BidiGenerateContent",
		model:   "models/gemini-2.0-flash-exp",
		dial:    dialDefault,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

func (p *Provider) Name() string { return "gemini-live" }

func (p *Provider) Capabilities() provider.Capabilities {
	return provider.Capabilities{
		Chat:           true,
		Streaming:      true,
		Tools:          true,
		Audio:          true,
		Vision:         true,
		StreamingTools: true,
		StreamingAudio: true,
		RealtimeAudio:  true,
		LiveBidi:       true,
	}
}

func (p *Provider) Models(_ context.Context) ([]provider.Model, error) {
	return []provider.Model{
		{Provider: p.Name(), Name: "models/gemini-2.0-flash-exp"},
	}, nil
}

// Complete is unsupported — Live API is streaming-only.
func (p *Provider) Complete(_ context.Context, _ *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	return nil, errors.New("geminilive: non-streaming Complete is not supported")
}

func (p *Provider) CompleteStream(ctx context.Context, req *provider.CompletionRequest) (provider.Stream, error) {
	model := req.Model
	if model == "" {
		model = p.model
	}
	conn, err := p.dial(ctx, p.baseURL, p.apiKey)
	if err != nil {
		return nil, err
	}
	stream := newLiveStream(ctx, conn, model)
	if err := stream.sendSetup(ctx, model, p.setup); err != nil {
		_ = conn.Close(websocket.StatusInternalError, "setup failed")
		return nil, err
	}
	return stream, nil
}

func (p *Provider) Embed(_ context.Context, _ *provider.EmbeddingRequest) (*provider.EmbeddingResponse, error) {
	return nil, errors.New("geminilive: embeddings not supported")
}

func (p *Provider) Healthy(_ context.Context) bool { return true }

// wsConn is the minimal interface used by the stream.
type wsConn interface {
	Read(ctx context.Context) (websocket.MessageType, []byte, error)
	Write(ctx context.Context, t websocket.MessageType, b []byte) error
	Close(code websocket.StatusCode, reason string) error
}

type dialFunc func(ctx context.Context, baseURL, apiKey string) (wsConn, error)

func dialDefault(ctx context.Context, baseURL, apiKey string) (wsConn, error) {
	url := fmt.Sprintf("%s?key=%s", baseURL, apiKey)
	conn, dialResp, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: http.Header{}})
	if dialResp != nil && dialResp.Body != nil {
		_ = dialResp.Body.Close()
	}
	if err != nil {
		return nil, fmt.Errorf("geminilive: dial: %w", err)
	}
	return conn, nil
}

// Compile-time check.
var _ provider.Provider = (*Provider)(nil)
