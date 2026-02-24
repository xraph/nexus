package postgres

import (
	"encoding/json"
	"time"

	"github.com/xraph/grove"

	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/key"
	"github.com/xraph/nexus/tenant"
	"github.com/xraph/nexus/usage"
)

// ──────────────────────────────────────────────────
// Tenant model
// ──────────────────────────────────────────────────

type tenantModel struct {
	grove.BaseModel `grove:"table:tenants"`
	ID              string    `grove:"id,pk"`
	Name            string    `grove:"name,notnull"`
	Slug            string    `grove:"slug,notnull"`
	Status          string    `grove:"status,notnull"`
	Quota           string    `grove:"quota,type:jsonb"`
	Config          string    `grove:"config,type:jsonb"`
	Metadata        string    `grove:"metadata,type:jsonb"`
	CreatedAt       time.Time `grove:"created_at,notnull,default:current_timestamp"`
	UpdatedAt       time.Time `grove:"updated_at,notnull,default:current_timestamp"`
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
		CreatedAt: t.CreatedAt,
		UpdatedAt: t.UpdatedAt,
	}
}

func tenantFromModel(m *tenantModel) (*tenant.Tenant, error) {
	tid, err := id.ParseTenantID(m.ID)
	if err != nil {
		return nil, err
	}
	t := &tenant.Tenant{
		ID:        tid,
		Name:      m.Name,
		Slug:      m.Slug,
		Status:    tenant.Status(m.Status),
		CreatedAt: m.CreatedAt,
		UpdatedAt: m.UpdatedAt,
	}
	_ = json.Unmarshal([]byte(m.Quota), &t.Quota)
	_ = json.Unmarshal([]byte(m.Config), &t.Config)
	_ = json.Unmarshal([]byte(m.Metadata), &t.Metadata)
	return t, nil
}

// ──────────────────────────────────────────────────
// API Key model
// ──────────────────────────────────────────────────

type apiKeyModel struct {
	grove.BaseModel `grove:"table:api_keys"`
	ID              string     `grove:"id,pk"`
	TenantID        string     `grove:"tenant_id,notnull"`
	Name            string     `grove:"name,notnull"`
	Prefix          string     `grove:"prefix,notnull"`
	Hash            string     `grove:"hash,notnull"`
	Scopes          string     `grove:"scopes,type:jsonb"`
	Status          string     `grove:"status,notnull"`
	ExpiresAt       *time.Time `grove:"expires_at"`
	LastUsedAt      *time.Time `grove:"last_used_at"`
	Metadata        string     `grove:"metadata,type:jsonb"`
	CreatedAt       time.Time  `grove:"created_at,notnull,default:current_timestamp"`
}

func apiKeyToModel(k *key.APIKey) *apiKeyModel {
	return &apiKeyModel{
		ID:         k.ID.String(),
		TenantID:   k.TenantID.String(),
		Name:       k.Name,
		Prefix:     k.Prefix,
		Hash:       k.Hash,
		Scopes:     mustJSON(k.Scopes),
		Status:     string(k.Status),
		ExpiresAt:  k.ExpiresAt,
		LastUsedAt: k.LastUsedAt,
		Metadata:   mustJSON(k.Metadata),
		CreatedAt:  k.CreatedAt,
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
	k := &key.APIKey{
		ID:         kid,
		TenantID:   tid,
		Name:       m.Name,
		Prefix:     m.Prefix,
		Hash:       m.Hash,
		Status:     key.Status(m.Status),
		ExpiresAt:  m.ExpiresAt,
		LastUsedAt: m.LastUsedAt,
		CreatedAt:  m.CreatedAt,
	}
	_ = json.Unmarshal([]byte(m.Scopes), &k.Scopes)
	_ = json.Unmarshal([]byte(m.Metadata), &k.Metadata)
	return k, nil
}

// ──────────────────────────────────────────────────
// Usage Record model
// ──────────────────────────────────────────────────

type usageModel struct {
	grove.BaseModel  `grove:"table:usage_records"`
	ID               string    `grove:"id,pk"`
	TenantID         string    `grove:"tenant_id,notnull"`
	KeyID            string    `grove:"key_id,notnull"`
	RequestID        string    `grove:"request_id,notnull"`
	Provider         string    `grove:"provider,notnull"`
	Model            string    `grove:"model,notnull"`
	PromptTokens     int       `grove:"prompt_tokens"`
	CompletionTokens int       `grove:"completion_tokens"`
	TotalTokens      int       `grove:"total_tokens"`
	CostUSD          float64   `grove:"cost_usd"`
	LatencyNs        int64     `grove:"latency_ns"`
	Cached           bool      `grove:"cached"`
	StatusCode       int       `grove:"status_code"`
	CreatedAt        time.Time `grove:"created_at,notnull,default:current_timestamp"`
}

func usageToModel(rec *usage.Record) *usageModel {
	return &usageModel{
		ID:               rec.ID.String(),
		TenantID:         rec.TenantID.String(),
		KeyID:            rec.KeyID.String(),
		RequestID:        rec.RequestID.String(),
		Provider:         rec.Provider,
		Model:            rec.Model,
		PromptTokens:     rec.PromptTokens,
		CompletionTokens: rec.CompletionTokens,
		TotalTokens:      rec.TotalTokens,
		CostUSD:          rec.CostUSD,
		LatencyNs:        rec.Latency.Nanoseconds(),
		Cached:           rec.Cached,
		StatusCode:       rec.StatusCode,
		CreatedAt:        rec.CreatedAt,
	}
}

func usageFromModel(m *usageModel) (*usage.Record, error) {
	uid, err := id.ParseUsageID(m.ID)
	if err != nil {
		return nil, err
	}
	tid, err := id.ParseTenantID(m.TenantID)
	if err != nil {
		return nil, err
	}
	kid, err := id.ParseKeyID(m.KeyID)
	if err != nil {
		return nil, err
	}
	rid, err := id.ParseRequestID(m.RequestID)
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
		CostUSD:          m.CostUSD,
		Latency:          time.Duration(m.LatencyNs),
		Cached:           m.Cached,
		StatusCode:       m.StatusCode,
		CreatedAt:        m.CreatedAt,
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
