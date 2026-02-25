package mongo

import (
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
	grove.BaseModel `grove:"table:nexus_tenants"`
	ID              string            `grove:"id,pk"      bson:"_id"`
	Name            string            `grove:"name"       bson:"name"`
	Slug            string            `grove:"slug"       bson:"slug"`
	Status          string            `grove:"status"     bson:"status"`
	Quota           tenant.Quota      `grove:"quota"      bson:"quota"`
	Config          tenant.Config     `grove:"config"     bson:"config"`
	Metadata        map[string]string `grove:"metadata"   bson:"metadata,omitempty"`
	CreatedAt       time.Time         `grove:"created_at" bson:"created_at"`
	UpdatedAt       time.Time         `grove:"updated_at" bson:"updated_at"`
}

func tenantToModel(t *tenant.Tenant) *tenantModel {
	return &tenantModel{
		ID:        t.ID.String(),
		Name:      t.Name,
		Slug:      t.Slug,
		Status:    string(t.Status),
		Quota:     t.Quota,
		Config:    t.Config,
		Metadata:  t.Metadata,
		CreatedAt: t.CreatedAt,
		UpdatedAt: t.UpdatedAt,
	}
}

func tenantFromModel(m *tenantModel) (*tenant.Tenant, error) {
	tid, err := id.ParseTenantID(m.ID)
	if err != nil {
		return nil, err
	}
	return &tenant.Tenant{
		ID:        tid,
		Name:      m.Name,
		Slug:      m.Slug,
		Status:    tenant.Status(m.Status),
		Quota:     m.Quota,
		Config:    m.Config,
		Metadata:  m.Metadata,
		CreatedAt: m.CreatedAt,
		UpdatedAt: m.UpdatedAt,
	}, nil
}

// ──────────────────────────────────────────────────
// API Key model
// ──────────────────────────────────────────────────

type apiKeyModel struct {
	grove.BaseModel `grove:"table:nexus_api_keys"`
	ID              string            `grove:"id,pk"        bson:"_id"`
	TenantID        string            `grove:"tenant_id"    bson:"tenant_id"`
	Name            string            `grove:"name"         bson:"name"`
	Prefix          string            `grove:"prefix"       bson:"prefix"`
	Hash            string            `grove:"hash"         bson:"hash"`
	Scopes          []string          `grove:"scopes"       bson:"scopes"`
	Status          string            `grove:"status"       bson:"status"`
	ExpiresAt       *time.Time        `grove:"expires_at"   bson:"expires_at,omitempty"`
	LastUsedAt      *time.Time        `grove:"last_used_at" bson:"last_used_at,omitempty"`
	Metadata        map[string]string `grove:"metadata"     bson:"metadata,omitempty"`
	CreatedAt       time.Time         `grove:"created_at"   bson:"created_at"`
}

func apiKeyToModel(k *key.APIKey) *apiKeyModel {
	return &apiKeyModel{
		ID:         k.ID.String(),
		TenantID:   k.TenantID.String(),
		Name:       k.Name,
		Prefix:     k.Prefix,
		Hash:       k.Hash,
		Scopes:     k.Scopes,
		Status:     string(k.Status),
		ExpiresAt:  k.ExpiresAt,
		LastUsedAt: k.LastUsedAt,
		Metadata:   k.Metadata,
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
	return &key.APIKey{
		ID:         kid,
		TenantID:   tid,
		Name:       m.Name,
		Prefix:     m.Prefix,
		Hash:       m.Hash,
		Scopes:     m.Scopes,
		Status:     key.Status(m.Status),
		ExpiresAt:  m.ExpiresAt,
		LastUsedAt: m.LastUsedAt,
		Metadata:   m.Metadata,
		CreatedAt:  m.CreatedAt,
	}, nil
}

// ──────────────────────────────────────────────────
// Usage Record model
// ──────────────────────────────────────────────────

type usageModel struct {
	grove.BaseModel  `grove:"table:nexus_usage_records"`
	ID               string    `grove:"id,pk"             bson:"_id"`
	TenantID         string    `grove:"tenant_id"         bson:"tenant_id"`
	KeyID            string    `grove:"key_id"            bson:"key_id"`
	RequestID        string    `grove:"request_id"        bson:"request_id"`
	Provider         string    `grove:"provider"          bson:"provider"`
	Model            string    `grove:"model"             bson:"model"`
	PromptTokens     int       `grove:"prompt_tokens"     bson:"prompt_tokens"`
	CompletionTokens int       `grove:"completion_tokens" bson:"completion_tokens"`
	TotalTokens      int       `grove:"total_tokens"      bson:"total_tokens"`
	CostUSD          float64   `grove:"cost_usd"          bson:"cost_usd"`
	LatencyNs        int64     `grove:"latency_ns"        bson:"latency_ns"`
	Cached           bool      `grove:"cached"            bson:"cached"`
	StatusCode       int       `grove:"status_code"       bson:"status_code"`
	CreatedAt        time.Time `grove:"created_at"        bson:"created_at"`
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
