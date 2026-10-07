package mongo

import (
	"fmt"
	"strconv"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/xraph/grove"

	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/key"
	"github.com/xraph/nexus/money"
	"github.com/xraph/nexus/store/internal/conv"
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
	Quota           quotaDoc          `grove:"quota"      bson:"quota"`
	Config          tenant.Config     `grove:"config"     bson:"config"`
	Metadata        map[string]string `grove:"metadata"   bson:"metadata,omitempty"`
	CreatedAt       time.Time         `grove:"created_at" bson:"created_at"`
	UpdatedAt       time.Time         `grove:"updated_at" bson:"updated_at"`
}

// quotaDoc is tenant.Quota as stored. Its keys are the lowercased names the
// driver's default codec wrote before this type existed, so old documents
// still read.
type quotaDoc struct {
	RPM               int           `bson:"rpm"`
	TPM               int           `bson:"tpm"`
	DailyRequests     int           `bson:"dailyrequests"`
	MonthlyBudgetUSD  any           `bson:"monthlybudgetusd"`
	MaxTokensPerReq   int           `bson:"maxtokensperreq"`
	MaxStreamDuration time.Duration `bson:"maxstreamduration"`
	MaxStreamTokens   int           `bson:"maxstreamtokens"`
}

func quotaToDoc(q tenant.Quota) (quotaDoc, error) {
	var budget any
	if !q.MonthlyBudgetUSD.IsZero() {
		b, err := decimalOf(&q.MonthlyBudgetUSD)
		if err != nil {
			return quotaDoc{}, err
		}
		budget = b
	}
	return quotaDoc{
		RPM: q.RPM, TPM: q.TPM, DailyRequests: q.DailyRequests, MonthlyBudgetUSD: budget,
		MaxTokensPerReq: q.MaxTokensPerReq, MaxStreamDuration: q.MaxStreamDuration, MaxStreamTokens: q.MaxStreamTokens,
	}, nil
}

func quotaFromDoc(d quotaDoc) (tenant.Quota, error) {
	q := tenant.Quota{
		RPM: d.RPM, TPM: d.TPM, DailyRequests: d.DailyRequests, MaxTokensPerReq: d.MaxTokensPerReq,
		MaxStreamDuration: d.MaxStreamDuration, MaxStreamTokens: d.MaxStreamTokens,
	}
	budget, err := usdFromBSON(d.MonthlyBudgetUSD)
	if err != nil {
		return tenant.Quota{}, err
	}
	if budget != nil {
		q.MonthlyBudgetUSD = *budget
	}
	return q, nil
}

func tenantToModel(t *tenant.Tenant) (*tenantModel, error) {
	quota, err := quotaToDoc(t.Quota)
	if err != nil {
		return nil, err
	}
	return &tenantModel{
		ID:        t.ID.String(),
		Name:      t.Name,
		Slug:      t.Slug,
		Status:    string(t.Status),
		Quota:     quota,
		Config:    t.Config,
		Metadata:  t.Metadata,
		CreatedAt: t.CreatedAt,
		UpdatedAt: t.UpdatedAt,
	}, nil
}

func tenantFromModel(m *tenantModel) (*tenant.Tenant, error) {
	tid, err := id.ParseTenantID(m.ID)
	if err != nil {
		return nil, err
	}
	quota, err := quotaFromDoc(m.Quota)
	if err != nil {
		return nil, err
	}
	return &tenant.Tenant{
		ID:        tid,
		Name:      m.Name,
		Slug:      m.Slug,
		Status:    tenant.Status(m.Status),
		Quota:     quota,
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
	ID               string  `grove:"id,pk"             bson:"_id"`
	TenantID         *string `grove:"tenant_id"         bson:"tenant_id"`
	KeyID            *string `grove:"key_id"            bson:"key_id"`
	RequestID        *string `grove:"request_id"        bson:"request_id"`
	Provider         string  `grove:"provider"          bson:"provider"`
	Model            string  `grove:"model"             bson:"model"`
	PromptTokens     int     `grove:"prompt_tokens"     bson:"prompt_tokens"`
	CompletionTokens int     `grove:"completion_tokens" bson:"completion_tokens"`
	TotalTokens      int     `grove:"total_tokens"      bson:"total_tokens"`
	// CostUSD is a Decimal128 or null when written; a legacy document may
	// still hold a double, which usdFromBSON reads.
	CostUSD       any       `grove:"cost_usd"          bson:"cost_usd"`
	PricingStatus string    `grove:"pricing_status"    bson:"pricing_status"`
	Outcome       string    `grove:"outcome"           bson:"outcome"`
	BlockedBy     string    `grove:"blocked_by"        bson:"blocked_by"`
	RefusalCode   string    `grove:"refusal_code"      bson:"refusal_code"`
	LatencyNs     int64     `grove:"latency_ns"        bson:"latency_ns"`
	Cached        bool      `grove:"cached"            bson:"cached"`
	StatusCode    int       `grove:"status_code"       bson:"status_code"`
	CreatedAt     time.Time `grove:"created_at"        bson:"created_at"`
}

// decimalOf writes an exact amount as Decimal128, or null when unknown.
func decimalOf(c *money.USD) (any, error) {
	if c == nil {
		return nil, nil
	}
	d, err := bson.ParseDecimal128(c.String())
	if err != nil {
		return nil, fmt.Errorf("nexus/mongo: decimal128 of %s: %w", c, err)
	}
	return d, nil
}

// usdFromBSON reads what a cost or budget field may hold: Decimal128 (what
// this package writes), null, a double or an integer (legacy documents and
// $sum over nothing), or a string.
func usdFromBSON(v any) (*money.USD, error) {
	var s string
	switch x := v.(type) {
	case nil:
		return nil, nil
	case bson.Decimal128:
		s = x.String()
	case float64:
		s = strconv.FormatFloat(x, 'f', -1, 64)
	case int32:
		s = strconv.FormatInt(int64(x), 10)
	case int64:
		s = strconv.FormatInt(x, 10)
	case string:
		s = x
	default:
		return nil, fmt.Errorf("nexus/mongo: amount of type %T", v)
	}
	u, err := money.ParseLenient(s)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func usageToModel(rec *usage.Record) (*usageModel, error) {
	cost, err := decimalOf(rec.CostUSD)
	if err != nil {
		return nil, err
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
		CostUSD:          cost,
		PricingStatus:    string(rec.PricingStatus),
		Outcome:          string(rec.Outcome),
		BlockedBy:        rec.BlockedBy,
		RefusalCode:      rec.RefusalCode,
		LatencyNs:        rec.Latency.Nanoseconds(),
		Cached:           rec.Cached,
		StatusCode:       rec.StatusCode,
		CreatedAt:        rec.CreatedAt.UTC(),
	}, nil
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
	cost, err := usdFromBSON(m.CostUSD)
	if err != nil {
		return nil, err
	}
	// An old binary writes no pricing_status. With no cost, or a cost of 0,
	// that means unknown, not free; with a cost, it was priced.
	status := usage.PricingStatus(m.PricingStatus)
	switch {
	case status == "" && (cost == nil || cost.IsZero()):
		status, cost = usage.PricingUnpricedModel, nil
	case status == "":
		status = usage.PricingPriced
	case status == usage.PricingUnpricedModel:
		cost = nil
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
		PricingStatus:    status,
		Outcome:          usage.Outcome(m.Outcome),
		BlockedBy:        m.BlockedBy,
		RefusalCode:      m.RefusalCode,
		Latency:          time.Duration(m.LatencyNs),
		Cached:           m.Cached,
		StatusCode:       m.StatusCode,
		CreatedAt:        m.CreatedAt,
	}, nil
}
