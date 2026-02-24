// Package relay_hook bridges Nexus lifecycle events to webhook notifications.
// When registered as an extension, it captures lifecycle events and forwards
// them as typed events to a configurable webhook endpoint.
package relay_hook

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/plugin"
)

// Compile-time interface checks.
var (
	_ plugin.Extension         = (*Extension)(nil)
	_ plugin.RequestReceived   = (*Extension)(nil)
	_ plugin.RequestCompleted  = (*Extension)(nil)
	_ plugin.RequestFailed     = (*Extension)(nil)
	_ plugin.RequestCached     = (*Extension)(nil)
	_ plugin.ProviderFailed    = (*Extension)(nil)
	_ plugin.CircuitOpened     = (*Extension)(nil)
	_ plugin.FallbackTriggered = (*Extension)(nil)
	_ plugin.GuardrailBlocked  = (*Extension)(nil)
	_ plugin.TenantCreated     = (*Extension)(nil)
	_ plugin.TenantDisabled    = (*Extension)(nil)
	_ plugin.KeyCreated        = (*Extension)(nil)
	_ plugin.KeyRevoked        = (*Extension)(nil)
	_ plugin.BudgetWarning     = (*Extension)(nil)
	_ plugin.BudgetExceeded    = (*Extension)(nil)
)

// Sender delivers relay events to a destination.
type Sender interface {
	Send(ctx context.Context, event *Event) error
}

// SenderFunc adapts a function to the Sender interface.
type SenderFunc func(ctx context.Context, event *Event) error

func (f SenderFunc) Send(ctx context.Context, event *Event) error {
	return f(ctx, event)
}

// Extension is the relay webhook extension.
type Extension struct {
	sender Sender
	logger *slog.Logger
}

// Option configures the relay extension.
type Option func(*Extension)

// WithLogger sets a custom logger.
func WithLogger(l *slog.Logger) Option {
	return func(e *Extension) { e.logger = l }
}

// New creates a new relay hook extension with the given sender.
func New(sender Sender, opts ...Option) *Extension {
	e := &Extension{
		sender: sender,
		logger: slog.Default(),
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// NewWebhook creates a relay extension that POSTs events to a URL.
func NewWebhook(url string, opts ...Option) *Extension {
	sender := &webhookSender{
		url: url,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
	return New(sender, opts...)
}

func (e *Extension) Name() string { return "relay_hook" }

// ──────────────────────────────────────────────────
// Lifecycle hooks
// ──────────────────────────────────────────────────

func (e *Extension) OnRequestReceived(ctx context.Context, requestID id.RequestID, model, tenantID string) error {
	return e.send(ctx, EventRequestReceived, map[string]any{
		"request_id": requestID.String(),
		"model":      model,
		"tenant_id":  tenantID,
	})
}

func (e *Extension) OnRequestCompleted(ctx context.Context, requestID id.RequestID, model, providerName string, elapsed time.Duration, inputTokens, outputTokens int) error {
	return e.send(ctx, EventRequestCompleted, map[string]any{
		"request_id":    requestID.String(),
		"model":         model,
		"provider":      providerName,
		"elapsed_ms":    elapsed.Milliseconds(),
		"input_tokens":  inputTokens,
		"output_tokens": outputTokens,
	})
}

func (e *Extension) OnRequestFailed(ctx context.Context, requestID id.RequestID, model string, err error) error {
	return e.send(ctx, EventRequestFailed, map[string]any{
		"request_id": requestID.String(),
		"model":      model,
		"error":      err.Error(),
	})
}

func (e *Extension) OnRequestCached(ctx context.Context, requestID id.RequestID, model string) error {
	return e.send(ctx, EventRequestCached, map[string]any{
		"request_id": requestID.String(),
		"model":      model,
	})
}

func (e *Extension) OnProviderFailed(ctx context.Context, providerName, model string, err error) error {
	return e.send(ctx, EventProviderFailed, map[string]any{
		"provider": providerName,
		"model":    model,
		"error":    err.Error(),
	})
}

func (e *Extension) OnCircuitOpened(ctx context.Context, providerName string) error {
	return e.send(ctx, EventCircuitOpened, map[string]any{
		"provider": providerName,
	})
}

func (e *Extension) OnFallbackTriggered(ctx context.Context, from, to string) error {
	return e.send(ctx, EventFallbackTriggered, map[string]any{
		"from": from,
		"to":   to,
	})
}

func (e *Extension) OnGuardrailBlocked(ctx context.Context, guardName string, requestID id.RequestID) error {
	return e.send(ctx, EventGuardrailBlocked, map[string]any{
		"guard":      guardName,
		"request_id": requestID.String(),
	})
}

func (e *Extension) OnTenantCreated(ctx context.Context, tenantID id.TenantID) error {
	return e.send(ctx, EventTenantCreated, map[string]any{
		"tenant_id": tenantID.String(),
	})
}

func (e *Extension) OnTenantDisabled(ctx context.Context, tenantID id.TenantID) error {
	return e.send(ctx, EventTenantDisabled, map[string]any{
		"tenant_id": tenantID.String(),
	})
}

func (e *Extension) OnKeyCreated(ctx context.Context, keyID id.KeyID, tenantID id.TenantID) error {
	return e.send(ctx, EventKeyCreated, map[string]any{
		"key_id":    keyID.String(),
		"tenant_id": tenantID.String(),
	})
}

func (e *Extension) OnKeyRevoked(ctx context.Context, keyID id.KeyID) error {
	return e.send(ctx, EventKeyRevoked, map[string]any{
		"key_id": keyID.String(),
	})
}

func (e *Extension) OnBudgetWarning(ctx context.Context, tenantID id.TenantID, usedPct float64) error {
	return e.send(ctx, EventBudgetWarning, map[string]any{
		"tenant_id": tenantID.String(),
		"used_pct":  usedPct,
	})
}

func (e *Extension) OnBudgetExceeded(ctx context.Context, tenantID id.TenantID) error {
	return e.send(ctx, EventBudgetExceeded, map[string]any{
		"tenant_id": tenantID.String(),
	})
}

// send creates an event and dispatches it via the sender.
func (e *Extension) send(ctx context.Context, eventType EventType, data map[string]any) error {
	event := &Event{
		Type:      eventType,
		Timestamp: time.Now(),
		Data:      data,
	}
	if err := e.sender.Send(ctx, event); err != nil {
		e.logger.Warn("relay_hook: failed to send event",
			slog.String("event", string(eventType)),
			slog.String("error", err.Error()),
		)
		// Non-fatal: relay errors must not block the pipeline
		return nil
	}
	return nil
}

// ──────────────────────────────────────────────────
// Webhook sender
// ──────────────────────────────────────────────────

type webhookSender struct {
	url    string
	client *http.Client
}

func (s *webhookSender) Send(ctx context.Context, event *Event) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url, bytesReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Nexus-Event", string(event.Type))

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	return nil
}

// bytesReader returns a reader from a byte slice.
func bytesReader(data []byte) *bytesReaderImpl {
	return &bytesReaderImpl{data: data}
}

type bytesReaderImpl struct {
	data []byte
	pos  int
}

func (r *bytesReaderImpl) Read(p []byte) (n int, err error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n = copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}
