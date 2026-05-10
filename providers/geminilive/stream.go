package geminilive

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/coder/websocket"

	"github.com/xraph/nexus/provider"
)

type liveStream struct {
	conn   wsConn
	ctx    context.Context
	model  string
	usage  *provider.Usage
	done   bool
	closed bool
	mu     sync.Mutex
}

func newLiveStream(ctx context.Context, conn wsConn, model string) *liveStream {
	return &liveStream{conn: conn, ctx: ctx, model: model}
}

// sendSetup is the first frame Live API expects after Connect — establishes
// the session model and any caller-provided generation config / system
// instructions / tools / realtime input config.
func (s *liveStream) sendSetup(ctx context.Context, model string, cfg SetupConfig) error {
	setup := map[string]any{"model": model}
	if cfg.GenerationConfig != nil {
		setup["generation_config"] = cfg.GenerationConfig
	}
	if cfg.SystemInstruction != nil {
		setup["system_instruction"] = cfg.SystemInstruction
	}
	if cfg.Tools != nil {
		setup["tools"] = cfg.Tools
	}
	if cfg.RealtimeInputCfg != nil {
		setup["realtime_input_config"] = cfg.RealtimeInputCfg
	}
	body, err := json.Marshal(map[string]any{"setup": setup})
	if err != nil {
		return err
	}
	return s.conn.Write(ctx, websocket.MessageText, body)
}

func (s *liveStream) Next(ctx context.Context) (*provider.StreamChunk, error) {
	if s.done {
		return nil, io.EOF
	}
	for {
		_, data, err := s.conn.Read(ctx)
		if err != nil {
			if isCloseErr(err) {
				s.done = true
				return nil, io.EOF
			}
			return nil, fmt.Errorf("geminilive: read: %w", err)
		}

		var env serverEnvelope
		if err := json.Unmarshal(data, &env); err != nil {
			continue
		}
		chunk, terminate := s.translate(&env)
		if chunk != nil {
			return chunk, nil
		}
		if terminate {
			s.done = true
			return nil, io.EOF
		}
	}
}

func (s *liveStream) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	s.done = true
	return s.conn.Close(websocket.StatusNormalClosure, "")
}

func (s *liveStream) Usage() *provider.Usage { return s.usage }

// Send forwards a unified ClientEvent. Live API expects realtime audio under
// `realtimeInput.audio.data`; image/text under `clientContent.turns`.
func (s *liveStream) Send(ctx context.Context, evt provider.ClientEvent) error {
	var msg map[string]any
	switch evt.Type {
	case "audio_chunk":
		if evt.Audio == nil {
			return errors.New("geminilive: audio_chunk requires Audio payload")
		}
		mime := "audio/pcm"
		if evt.Audio.SampleRate > 0 {
			mime = fmt.Sprintf("audio/pcm;rate=%d", evt.Audio.SampleRate)
		}
		msg = map[string]any{
			"realtimeInput": map[string]any{
				"mediaChunks": []map[string]any{{
					"mimeType": mime,
					"data":     base64.StdEncoding.EncodeToString(evt.Audio.Data),
				}},
			},
		}
	case "image":
		if evt.Image == nil {
			return errors.New("geminilive: image requires Image payload")
		}
		msg = map[string]any{
			"realtimeInput": map[string]any{
				"mediaChunks": []map[string]any{{
					"mimeType": evt.Image.MimeType,
					"data":     base64.StdEncoding.EncodeToString(evt.Image.Data),
				}},
			},
		}
	case "commit":
		// Live API ends a turn implicitly; explicit commit is a no-op for
		// realtimeInput. Send an empty turn marker for compatibility.
		msg = map[string]any{"clientContent": map[string]any{"turnComplete": true}}
	case "cancel":
		msg = map[string]any{"clientContent": map[string]any{"turnComplete": true, "interrupted": true}}
	case "control":
		v, ok := evt.Data.(map[string]any)
		if !ok {
			return errors.New("geminilive: control event Data must be map[string]any")
		}
		msg = v
	default:
		return fmt.Errorf("geminilive: unsupported client event %q", evt.Type)
	}

	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return s.conn.Write(ctx, websocket.MessageText, body)
}

func (s *liveStream) translate(env *serverEnvelope) (*provider.StreamChunk, bool) {
	switch {
	case env.SetupComplete != nil:
		// Setup ack — no surface event.
		return nil, false

	case env.ServerContent != nil:
		sc := env.ServerContent
		// Model turn — may carry audio, image, or text parts.
		if sc.ModelTurn != nil {
			for _, part := range sc.ModelTurn.Parts {
				if part.Text != "" {
					return &provider.StreamChunk{
						Provider: "gemini-live",
						Model:    s.model,
						Delta:    provider.Delta{Content: part.Text},
					}, false
				}
				if part.InlineData != nil {
					data, err := base64.StdEncoding.DecodeString(part.InlineData.Data)
					if err != nil {
						continue
					}
					if part.InlineData.MimeType != "" && part.InlineData.MimeType[:5] == "audio" {
						return &provider.StreamChunk{
							Provider: "gemini-live",
							Model:    s.model,
							Kind:     provider.EventAudio,
							Delta: provider.Delta{Audio: &provider.AudioChunk{
								Format: part.InlineData.MimeType,
								Data:   data,
							}},
						}, false
					}
					return &provider.StreamChunk{
						Provider: "gemini-live",
						Model:    s.model,
						Kind:     provider.EventImage,
						Delta: provider.Delta{Image: &provider.ImageChunk{
							MimeType: part.InlineData.MimeType,
							Data:     data,
						}},
					}, false
				}
			}
		}
		if sc.OutputTranscription != nil && sc.OutputTranscription.Text != "" {
			return &provider.StreamChunk{
				Provider: "gemini-live",
				Model:    s.model,
				Kind:     provider.EventAudio,
				Delta:    provider.Delta{Audio: &provider.AudioChunk{Transcript: sc.OutputTranscription.Text}},
			}, false
		}
		if sc.TurnComplete {
			if env.UsageMetadata != nil {
				s.usage = &provider.Usage{
					PromptTokens:     env.UsageMetadata.PromptTokenCount,
					CompletionTokens: env.UsageMetadata.CandidatesTokenCount,
					TotalTokens:      env.UsageMetadata.TotalTokenCount,
				}
			}
			return &provider.StreamChunk{
				Provider:     "gemini-live",
				Model:        s.model,
				FinishReason: "stop",
				Usage:        s.usage,
			}, true
		}
	case env.ToolCall != nil:
		if len(env.ToolCall.FunctionCalls) > 0 {
			fc := env.ToolCall.FunctionCalls[0]
			args, _ := json.Marshal(fc.Args) //nolint:errcheck // args may be nil
			return &provider.StreamChunk{
				Provider: "gemini-live",
				Model:    s.model,
				Kind:     provider.EventToolCallDelta,
				Delta: provider.Delta{
					ToolCalls: []provider.ToolCall{{
						ID:   fc.ID,
						Type: "function",
						Function: provider.ToolCallFunc{
							Name:      fc.Name,
							Arguments: string(args),
						},
					}},
				},
			}, false
		}
	}
	return nil, false
}

// serverEnvelope is the union of Live API server messages we care about.
type serverEnvelope struct {
	SetupComplete *struct{}      `json:"setupComplete,omitempty"`
	ServerContent *serverContent `json:"serverContent,omitempty"`
	ToolCall      *toolCall      `json:"toolCall,omitempty"`
	UsageMetadata *usageMetadata `json:"usageMetadata,omitempty"`
}

type serverContent struct {
	ModelTurn           *modelTurn           `json:"modelTurn,omitempty"`
	TurnComplete        bool                 `json:"turnComplete,omitempty"`
	OutputTranscription *outputTranscription `json:"outputTranscription,omitempty"`
}

type modelTurn struct {
	Parts []part `json:"parts"`
}

type part struct {
	Text       string      `json:"text,omitempty"`
	InlineData *inlineData `json:"inlineData,omitempty"`
}

type inlineData struct {
	MimeType string `json:"mimeType,omitempty"`
	Data     string `json:"data,omitempty"`
}

type outputTranscription struct {
	Text string `json:"text"`
}

type toolCall struct {
	FunctionCalls []functionCall `json:"functionCalls"`
}

type functionCall struct {
	ID   string         `json:"id"`
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
}

type usageMetadata struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
}

func isCloseErr(err error) bool {
	if err == nil {
		return false
	}
	var ce websocket.CloseError
	return errors.As(err, &ce)
}

// Compile-time checks.
var (
	_ provider.Stream   = (*liveStream)(nil)
	_ provider.BiStream = (*liveStream)(nil)
)
