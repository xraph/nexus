package sqlite

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/xraph/grove"

	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/key"
	"github.com/xraph/nexus/store/internal/conv"
	"github.com/xraph/nexus/tenant"
	"github.com/xraph/nexus/usage"
)

// ──────────────────────────────────────────────────
// Tenant model
// ──────────────────────────────────────────────────

type tenantModel struct {
	grove.BaseModel `grove:"table:tenants"`
	ID              string `grove:"id,pk"`
	Name            string `grove:"name,notnull"`
	Slug            string `grove:"slug,notnull"`
	Status          string `grove:"status,notnull"`
	Quota           string `grove:"quota"`
	Config          string `grove:"config"`
	Metadata        string `grove:"metadata"`
	CreatedAt       string `grove:"created_at,notnull,default:current_timestamp"`
	UpdatedAt       string `grove:"updated_at,notnull,default:current_timestamp"`
}

func tenantToModel(t *tenant.Tenant) *tenantModel {
	return &tenantModel{
		ID:        t.ID.String(),
		Name:      t.Name,
		Slug:      t.Slug,
		Status:    string(t.Status),
		Quota:     mustJSON(t.Quota),
		Config:    mustJSON(t.Config),
		Metadata:  mustJSON(t.Metadata),
		CreatedAt: conv.TimeText(t.CreatedAt),
		UpdatedAt: conv.TimeText(t.UpdatedAt),
	}
}

func tenantFromModel(m *tenantModel) (*tenant.Tenant, error) {
	tid, err := id.ParseTenantID(m.ID)
	if err != nil {
		return nil, err
	}
	created, err := parseLegacyTime(m.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("nexus: tenant %s created_at: %w", m.ID, err)
	}
	updated, err := parseLegacyTime(m.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("nexus: tenant %s updated_at: %w", m.ID, err)
	}
	t := &tenant.Tenant{
		ID:        tid,
		Name:      m.Name,
		Slug:      m.Slug,
		Status:    tenant.Status(m.Status),
		CreatedAt: created,
		UpdatedAt: updated,
	}

	if err = json.Unmarshal([]byte(m.Quota), &t.Quota); err != nil {
		return nil, fmt.Errorf("nexus: unmarshal quota: %w", err)
	}
	if err = json.Unmarshal([]byte(m.Config), &t.Config); err != nil {
		return nil, fmt.Errorf("nexus: unmarshal config: %w", err)
	}
	if err = json.Unmarshal([]byte(m.Metadata), &t.Metadata); err != nil {
		return nil, fmt.Errorf("nexus: unmarshal metadata: %w", err)
	}
	return t, nil
}

// ──────────────────────────────────────────────────
// API Key model
// ──────────────────────────────────────────────────

type apiKeyModel struct {
	grove.BaseModel `grove:"table:api_keys"`
	ID              string  `grove:"id,pk"`
	TenantID        string  `grove:"tenant_id,notnull"`
	Name            string  `grove:"name,notnull"`
	Prefix          string  `grove:"prefix,notnull"`
	Hash            string  `grove:"hash,notnull"`
	Scopes          string  `grove:"scopes"`
	Status          string  `grove:"status,notnull"`
	ExpiresAt       *string `grove:"expires_at"` // conv.TimeText, so a status filter compares it as a time
	LastUsedAt      *string `grove:"last_used_at"`
	Metadata        string  `grove:"metadata"`
	CreatedAt       string  `grove:"created_at,notnull,default:current_timestamp"`
}

func apiKeyToModel(k *key.APIKey) *apiKeyModel {
	var expires *string
	if k.ExpiresAt != nil {
		text := conv.TimeText(*k.ExpiresAt)
		expires = &text
	}
	var lastUsed *string
	if k.LastUsedAt != nil {
		text := conv.TimeText(*k.LastUsedAt)
		lastUsed = &text
	}
	return &apiKeyModel{
		ID:         k.ID.String(),
		TenantID:   k.TenantID.String(),
		Name:       k.Name,
		Prefix:     k.Prefix,
		Hash:       k.Hash,
		Scopes:     mustJSON(k.Scopes),
		Status:     string(k.Status),
		ExpiresAt:  expires,
		LastUsedAt: lastUsed,
		Metadata:   mustJSON(k.Metadata),
		CreatedAt:  conv.TimeText(k.CreatedAt),
	}
}

func apiKeyFromModel(m *apiKeyModel) (*key.APIKey, error) {
	kid, err := id.ParseKeyID(m.ID)
	if err != nil {
		return nil, err
	}
	tid, err := id.ParseTenantID(m.TenantID)
	if err != nil {
		return nil, err
	}
	created, err := parseLegacyTime(m.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("nexus: key %s created_at: %w", m.ID, err)
	}
	var lastUsed *time.Time
	if m.LastUsedAt != nil {
		at, parseErr := parseLegacyTime(*m.LastUsedAt)
		if parseErr != nil {
			return nil, fmt.Errorf("nexus: key %s last_used_at: %w", m.ID, parseErr)
		}
		lastUsed = &at
	}
	k := &key.APIKey{
		ID:         kid,
		TenantID:   tid,
		Name:       m.Name,
		Prefix:     m.Prefix,
		Hash:       m.Hash,
		Status:     key.Status(m.Status),
		LastUsedAt: lastUsed,
		CreatedAt:  created,
	}
	if m.ExpiresAt != nil {
		t, parseErr := parseKeyExpiry(*m.ExpiresAt)
		if parseErr != nil {
			return nil, fmt.Errorf("nexus: key %s expires_at: %w", m.ID, parseErr)
		}
		k.ExpiresAt = &t
	}
	if err = json.Unmarshal([]byte(m.Scopes), &k.Scopes); err != nil {
		return nil, fmt.Errorf("nexus: unmarshal scopes: %w", err)
	}
	if err = json.Unmarshal([]byte(m.Metadata), &k.Metadata); err != nil {
		return nil, fmt.Errorf("nexus: unmarshal metadata: %w", err)
	}
	return k, nil
}

// ──────────────────────────────────────────────────
// Usage Record model
// ──────────────────────────────────────────────────

type usageModel struct {
	grove.BaseModel  `grove:"table:usage_records"`
	ID               string  `grove:"id,pk"`
	TenantID         *string `grove:"tenant_id"`
	KeyID            *string `grove:"key_id"`
	RequestID        *string `grove:"request_id"`
	Provider         string  `grove:"provider,notnull"`
	Model            string  `grove:"model,notnull"`
	PromptTokens     int     `grove:"prompt_tokens"`
	CompletionTokens int     `grove:"completion_tokens"`
	TotalTokens      int     `grove:"total_tokens"`
	CostUSD          *string `grove:"cost_usd"`
	PricingStatus    string  `grove:"pricing_status,notnull"`
	Outcome          string  `grove:"outcome,notnull"`
	BlockedBy        string  `grove:"blocked_by,notnull"`
	RefusalCode      string  `grove:"refusal_code,notnull"`
	LatencyNs        int64   `grove:"latency_ns"`
	Cached           int     `grove:"cached"`
	StatusCode       int     `grove:"status_code"`
	// CreatedAt is conv.TimeText: fixed-width UTC with nanoseconds, so a
	// window compares as text exactly as it would as time.
	CreatedAt string `grove:"created_at,notnull"`
}

func usageToModel(rec *usage.Record) *usageModel {
	cached := 0
	if rec.Cached {
		cached = 1
	}
	return &usageModel{
		ID:               rec.ID.String(),
		TenantID:         conv.OptionalID(rec.TenantID),
		KeyID:            conv.OptionalID(rec.KeyID),
		RequestID:        conv.OptionalID(rec.RequestID),
		Provider:         rec.Provider,
		Model:            rec.Model,
		PromptTokens:     rec.PromptTokens,
		CompletionTokens: rec.CompletionTokens,
		TotalTokens:      rec.TotalTokens,
		CostUSD:          conv.CostText(rec.CostUSD),
		PricingStatus:    string(rec.PricingStatus),
		Outcome:          string(rec.Outcome),
		BlockedBy:        rec.BlockedBy,
		RefusalCode:      rec.RefusalCode,
		LatencyNs:        rec.Latency.Nanoseconds(),
		Cached:           cached,
		StatusCode:       rec.StatusCode,
		CreatedAt:        conv.TimeText(rec.CreatedAt),
	}
}

func usageFromModel(m *usageModel) (*usage.Record, error) {
	uid, err := id.ParseUsageID(m.ID)
	if err != nil {
		return nil, err
	}
	tid, err := conv.ParseOptional(m.TenantID, id.ParseTenantID)
	if err != nil {
		return nil, err
	}
	kid, err := conv.ParseOptional(m.KeyID, id.ParseKeyID)
	if err != nil {
		return nil, err
	}
	rid, err := conv.ParseOptional(m.RequestID, id.ParseRequestID)
	if err != nil {
		return nil, err
	}
	cost, err := conv.ParseCost(m.CostUSD)
	if err != nil {
		return nil, err
	}
	// An old binary writes cost_usd = 0 and leaves pricing_status to its
	// default, unpriced_model. That 0 never meant free.
	if usage.PricingStatus(m.PricingStatus).CostUnknown() {
		cost = nil
	}
	created, err := conv.ParseTimeText(m.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &usage.Record{
		ID:               uid,
		TenantID:         tid,
		KeyID:            kid,
		RequestID:        rid,
		Provider:         m.Provider,
		Model:            m.Model,
		PromptTokens:     m.PromptTokens,
		CompletionTokens: m.CompletionTokens,
		TotalTokens:      m.TotalTokens,
		CostUSD:          cost,
		PricingStatus:    usage.PricingStatus(m.PricingStatus),
		Outcome:          usage.Outcome(m.Outcome),
		BlockedBy:        m.BlockedBy,
		RefusalCode:      m.RefusalCode,
		Latency:          time.Duration(m.LatencyNs),
		Cached:           m.Cached == 1,
		StatusCode:       m.StatusCode,
		CreatedAt:        created,
	}, nil
}

// ──────────────────────────────────────────────────
// JSON helper
// ──────────────────────────────────────────────────

func mustJSON(v any) string {
	if v == nil {
		return "null"
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "null"
	}
	return string(b)
}
