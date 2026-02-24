// Package postgres provides a PostgreSQL-backed store implementation for Nexus.
package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/key"
	"github.com/xraph/nexus/store"
	"github.com/xraph/nexus/tenant"
	"github.com/xraph/nexus/usage"
)

// Store is a PostgreSQL-backed persistence store.
type Store struct {
	db *sql.DB
}

// Compile-time check.
var _ store.Store = (*Store)(nil)

// New creates a new PostgreSQL store with the given database connection.
func New(db *sql.DB) *Store {
	return &Store{db: db}
}

// Open opens a PostgreSQL database with the given DSN and returns a Store.
func Open(dsn string) (*Store, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("nexus/postgres: failed to open database: %w", err)
	}
	if err := db.PingContext(context.Background()); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("nexus/postgres: failed to connect: %w", err)
	}
	return New(db), nil
}

func (s *Store) Tenants() tenant.Store { return &tenantStore{db: s.db} }
func (s *Store) Keys() key.Store       { return &keyStore{db: s.db} }
func (s *Store) Usage() usage.Store    { return &usageStore{db: s.db} }

func (s *Store) Migrate() error {
	for _, stmt := range migrations {
		if _, err := s.db.ExecContext(context.Background(), stmt); err != nil {
			return fmt.Errorf("nexus/postgres: migration failed: %w", err)
		}
	}
	return nil
}

func (s *Store) Close() error { return s.db.Close() }

// ──────────────────────────────────────────────────
// Migrations
// ──────────────────────────────────────────────────

var migrations = []string{
	`CREATE TABLE IF NOT EXISTS tenants (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		slug TEXT UNIQUE NOT NULL,
		status TEXT NOT NULL DEFAULT 'active',
		quota JSONB NOT NULL DEFAULT '{}',
		config JSONB NOT NULL DEFAULT '{}',
		metadata JSONB NOT NULL DEFAULT '{}',
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`,
	`CREATE INDEX IF NOT EXISTS idx_tenants_slug ON tenants(slug)`,
	`CREATE TABLE IF NOT EXISTS api_keys (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL REFERENCES tenants(id),
		name TEXT NOT NULL,
		prefix TEXT NOT NULL,
		hash TEXT NOT NULL,
		scopes JSONB NOT NULL DEFAULT '[]',
		status TEXT NOT NULL DEFAULT 'active',
		expires_at TIMESTAMPTZ,
		last_used_at TIMESTAMPTZ,
		metadata JSONB NOT NULL DEFAULT '{}',
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`,
	`CREATE INDEX IF NOT EXISTS idx_api_keys_prefix ON api_keys(prefix)`,
	`CREATE INDEX IF NOT EXISTS idx_api_keys_tenant ON api_keys(tenant_id)`,
	`CREATE TABLE IF NOT EXISTS usage_records (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL REFERENCES tenants(id),
		key_id TEXT NOT NULL,
		request_id TEXT NOT NULL,
		provider TEXT NOT NULL,
		model TEXT NOT NULL,
		prompt_tokens INTEGER NOT NULL DEFAULT 0,
		completion_tokens INTEGER NOT NULL DEFAULT 0,
		total_tokens INTEGER NOT NULL DEFAULT 0,
		cost_usd DOUBLE PRECISION NOT NULL DEFAULT 0,
		latency_ns BIGINT NOT NULL DEFAULT 0,
		cached BOOLEAN NOT NULL DEFAULT FALSE,
		status_code INTEGER NOT NULL DEFAULT 200,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`,
	`CREATE INDEX IF NOT EXISTS idx_usage_tenant ON usage_records(tenant_id)`,
	`CREATE INDEX IF NOT EXISTS idx_usage_created ON usage_records(created_at)`,
}

// ──────────────────────────────────────────────────
// Tenant Store
// ──────────────────────────────────────────────────

type tenantStore struct {
	db *sql.DB
}

func (s *tenantStore) Insert(ctx context.Context, t *tenant.Tenant) error {
	quotaJSON, err := json.Marshal(t.Quota)
	if err != nil {
		return fmt.Errorf("nexus/postgres: marshal quota: %w", err)
	}
	configJSON, err := json.Marshal(t.Config)
	if err != nil {
		return fmt.Errorf("nexus/postgres: marshal config: %w", err)
	}
	metaJSON, err := json.Marshal(t.Metadata)
	if err != nil {
		return fmt.Errorf("nexus/postgres: marshal metadata: %w", err)
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO tenants (id, name, slug, status, quota, config, metadata, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		t.ID.String(), t.Name, t.Slug, string(t.Status),
		string(quotaJSON), string(configJSON), string(metaJSON),
		t.CreatedAt, t.UpdatedAt,
	)
	return err
}

func (s *tenantStore) FindByID(ctx context.Context, tid string) (*tenant.Tenant, error) {
	return s.scanTenant(s.db.QueryRowContext(ctx,
		`SELECT id, name, slug, status, quota, config, metadata, created_at, updated_at
		 FROM tenants WHERE id = $1`, tid))
}

func (s *tenantStore) FindBySlug(ctx context.Context, slug string) (*tenant.Tenant, error) {
	return s.scanTenant(s.db.QueryRowContext(ctx,
		`SELECT id, name, slug, status, quota, config, metadata, created_at, updated_at
		 FROM tenants WHERE slug = $1`, slug))
}

func (s *tenantStore) Update(ctx context.Context, t *tenant.Tenant) error {
	quotaJSON, err := json.Marshal(t.Quota)
	if err != nil {
		return fmt.Errorf("nexus/postgres: marshal quota: %w", err)
	}
	configJSON, err := json.Marshal(t.Config)
	if err != nil {
		return fmt.Errorf("nexus/postgres: marshal config: %w", err)
	}
	metaJSON, err := json.Marshal(t.Metadata)
	if err != nil {
		return fmt.Errorf("nexus/postgres: marshal metadata: %w", err)
	}

	_, err = s.db.ExecContext(ctx,
		`UPDATE tenants SET name=$1, slug=$2, status=$3, quota=$4, config=$5, metadata=$6, updated_at=$7
		 WHERE id=$8`,
		t.Name, t.Slug, string(t.Status),
		string(quotaJSON), string(configJSON), string(metaJSON),
		t.UpdatedAt, t.ID.String(),
	)
	return err
}

func (s *tenantStore) Delete(ctx context.Context, tid string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM tenants WHERE id = $1`, tid)
	return err
}

func (s *tenantStore) List(ctx context.Context, opts *tenant.ListOptions) ([]*tenant.Tenant, int, error) {
	query := `SELECT id, name, slug, status, quota, config, metadata, created_at, updated_at FROM tenants`
	var args []any
	argIdx := 1

	if opts != nil && opts.Status != "" {
		query += fmt.Sprintf(` WHERE status = $%d`, argIdx)
		args = append(args, opts.Status)
		argIdx++
	}
	query += ` ORDER BY created_at DESC`

	if opts != nil && opts.Limit > 0 {
		query += fmt.Sprintf(` LIMIT $%d`, argIdx)
		args = append(args, opts.Limit)
		argIdx++
		if opts.Offset > 0 {
			//nolint:gosec // G202 -- parameterized placeholder index, not user input
			query += fmt.Sprintf(` OFFSET $%d`, argIdx)
			args = append(args, opts.Offset)
		}
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()

	var tenants []*tenant.Tenant
	for rows.Next() {
		t, err := scanTenantRow(rows)
		if err != nil {
			return nil, 0, err
		}
		tenants = append(tenants, t)
	}
	return tenants, len(tenants), rows.Err()
}

func (s *tenantStore) scanTenant(row *sql.Row) (*tenant.Tenant, error) {
	var t tenant.Tenant
	var idStr, status string
	var quotaJSON, configJSON, metaJSON string

	err := row.Scan(&idStr, &t.Name, &t.Slug, &status,
		&quotaJSON, &configJSON, &metaJSON, &t.CreatedAt, &t.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	t.ID, err = id.ParseTenantID(idStr)
	if err != nil {
		return nil, fmt.Errorf("nexus/postgres: invalid tenant ID: %w", err)
	}
	t.Status = tenant.Status(status)
	if err := json.Unmarshal([]byte(quotaJSON), &t.Quota); err != nil {
		return nil, fmt.Errorf("nexus/postgres: unmarshal quota: %w", err)
	}
	if err := json.Unmarshal([]byte(configJSON), &t.Config); err != nil {
		return nil, fmt.Errorf("nexus/postgres: unmarshal config: %w", err)
	}
	if err := json.Unmarshal([]byte(metaJSON), &t.Metadata); err != nil {
		return nil, fmt.Errorf("nexus/postgres: unmarshal metadata: %w", err)
	}
	return &t, nil
}

type scannable interface {
	Scan(dest ...any) error
}

func scanTenantRow(row scannable) (*tenant.Tenant, error) {
	var t tenant.Tenant
	var idStr, status string
	var quotaJSON, configJSON, metaJSON string

	err := row.Scan(&idStr, &t.Name, &t.Slug, &status,
		&quotaJSON, &configJSON, &metaJSON, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, err
	}

	t.ID, err = id.ParseTenantID(idStr)
	if err != nil {
		return nil, fmt.Errorf("nexus/postgres: invalid tenant ID: %w", err)
	}
	t.Status = tenant.Status(status)
	if err := json.Unmarshal([]byte(quotaJSON), &t.Quota); err != nil {
		return nil, fmt.Errorf("nexus/postgres: unmarshal quota: %w", err)
	}
	if err := json.Unmarshal([]byte(configJSON), &t.Config); err != nil {
		return nil, fmt.Errorf("nexus/postgres: unmarshal config: %w", err)
	}
	if err := json.Unmarshal([]byte(metaJSON), &t.Metadata); err != nil {
		return nil, fmt.Errorf("nexus/postgres: unmarshal metadata: %w", err)
	}
	return &t, nil
}

// ──────────────────────────────────────────────────
// Key Store
// ──────────────────────────────────────────────────

type keyStore struct {
	db *sql.DB
}

func (s *keyStore) Insert(ctx context.Context, k *key.APIKey) error {
	scopesJSON, err := json.Marshal(k.Scopes)
	if err != nil {
		return fmt.Errorf("nexus/postgres: marshal scopes: %w", err)
	}
	metaJSON, err := json.Marshal(k.Metadata)
	if err != nil {
		return fmt.Errorf("nexus/postgres: marshal metadata: %w", err)
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO api_keys (id, tenant_id, name, prefix, hash, scopes, status, expires_at, last_used_at, metadata, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		k.ID.String(), k.TenantID.String(), k.Name, k.Prefix, k.Hash,
		string(scopesJSON), string(k.Status), k.ExpiresAt, k.LastUsedAt,
		string(metaJSON), k.CreatedAt,
	)
	return err
}

func (s *keyStore) FindByID(ctx context.Context, kid string) (*key.APIKey, error) {
	return s.scanKey(s.db.QueryRowContext(ctx,
		`SELECT id, tenant_id, name, prefix, hash, scopes, status, expires_at, last_used_at, metadata, created_at
		 FROM api_keys WHERE id = $1`, kid))
}

func (s *keyStore) FindByPrefix(ctx context.Context, prefix string) (*key.APIKey, error) {
	return s.scanKey(s.db.QueryRowContext(ctx,
		`SELECT id, tenant_id, name, prefix, hash, scopes, status, expires_at, last_used_at, metadata, created_at
		 FROM api_keys WHERE prefix = $1 AND status = 'active'`, prefix))
}

func (s *keyStore) Update(ctx context.Context, k *key.APIKey) error {
	scopesJSON, err := json.Marshal(k.Scopes)
	if err != nil {
		return fmt.Errorf("nexus/postgres: marshal scopes: %w", err)
	}
	metaJSON, err := json.Marshal(k.Metadata)
	if err != nil {
		return fmt.Errorf("nexus/postgres: marshal metadata: %w", err)
	}

	_, err = s.db.ExecContext(ctx,
		`UPDATE api_keys SET name=$1, status=$2, scopes=$3, expires_at=$4, last_used_at=$5, metadata=$6
		 WHERE id=$7`,
		k.Name, string(k.Status), string(scopesJSON),
		k.ExpiresAt, k.LastUsedAt, string(metaJSON), k.ID.String(),
	)
	return err
}

func (s *keyStore) Delete(ctx context.Context, kid string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM api_keys WHERE id = $1`, kid)
	return err
}

func (s *keyStore) ListByTenant(ctx context.Context, tenantID string) ([]*key.APIKey, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, tenant_id, name, prefix, hash, scopes, status, expires_at, last_used_at, metadata, created_at
		 FROM api_keys WHERE tenant_id = $1 ORDER BY created_at DESC`, tenantID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var keys []*key.APIKey
	for rows.Next() {
		k, err := scanKeyRow(rows)
		if err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

func (s *keyStore) scanKey(row *sql.Row) (*key.APIKey, error) {
	var k key.APIKey
	var idStr, tenantIDStr, status string
	var scopesJSON, metaJSON string

	err := row.Scan(&idStr, &tenantIDStr, &k.Name, &k.Prefix, &k.Hash,
		&scopesJSON, &status, &k.ExpiresAt, &k.LastUsedAt, &metaJSON, &k.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	k.ID, err = id.ParseKeyID(idStr)
	if err != nil {
		return nil, fmt.Errorf("nexus/postgres: invalid key ID: %w", err)
	}
	k.TenantID, err = id.ParseTenantID(tenantIDStr)
	if err != nil {
		return nil, fmt.Errorf("nexus/postgres: invalid tenant ID in key: %w", err)
	}
	k.Status = key.Status(status)
	if err := json.Unmarshal([]byte(scopesJSON), &k.Scopes); err != nil {
		return nil, fmt.Errorf("nexus/postgres: unmarshal scopes: %w", err)
	}
	if err := json.Unmarshal([]byte(metaJSON), &k.Metadata); err != nil {
		return nil, fmt.Errorf("nexus/postgres: unmarshal metadata: %w", err)
	}
	return &k, nil
}

func scanKeyRow(row scannable) (*key.APIKey, error) {
	var k key.APIKey
	var idStr, tenantIDStr, status string
	var scopesJSON, metaJSON string

	err := row.Scan(&idStr, &tenantIDStr, &k.Name, &k.Prefix, &k.Hash,
		&scopesJSON, &status, &k.ExpiresAt, &k.LastUsedAt, &metaJSON, &k.CreatedAt)
	if err != nil {
		return nil, err
	}

	k.ID, err = id.ParseKeyID(idStr)
	if err != nil {
		return nil, fmt.Errorf("nexus/postgres: invalid key ID: %w", err)
	}
	k.TenantID, err = id.ParseTenantID(tenantIDStr)
	if err != nil {
		return nil, fmt.Errorf("nexus/postgres: invalid tenant ID in key: %w", err)
	}
	k.Status = key.Status(status)
	if err := json.Unmarshal([]byte(scopesJSON), &k.Scopes); err != nil {
		return nil, fmt.Errorf("nexus/postgres: unmarshal scopes: %w", err)
	}
	if err := json.Unmarshal([]byte(metaJSON), &k.Metadata); err != nil {
		return nil, fmt.Errorf("nexus/postgres: unmarshal metadata: %w", err)
	}
	return &k, nil
}

// ──────────────────────────────────────────────────
// Usage Store
// ──────────────────────────────────────────────────

type usageStore struct {
	db *sql.DB
}

func (s *usageStore) Insert(ctx context.Context, rec *usage.Record) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO usage_records (id, tenant_id, key_id, request_id, provider, model,
			prompt_tokens, completion_tokens, total_tokens, cost_usd, latency_ns,
			cached, status_code, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
		rec.ID.String(), rec.TenantID.String(), rec.KeyID.String(), rec.RequestID.String(),
		rec.Provider, rec.Model, rec.PromptTokens, rec.CompletionTokens, rec.TotalTokens,
		rec.CostUSD, rec.Latency.Nanoseconds(), rec.Cached, rec.StatusCode, rec.CreatedAt,
	)
	return err
}

func (s *usageStore) MonthlySpend(ctx context.Context, tenantID string) (float64, error) {
	var total float64
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(cost_usd), 0) FROM usage_records
		 WHERE tenant_id = $1 AND created_at >= date_trunc('month', NOW())`,
		tenantID).Scan(&total)
	return total, err
}

func (s *usageStore) DailyRequests(ctx context.Context, tenantID string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM usage_records
		 WHERE tenant_id = $1 AND created_at >= date_trunc('day', NOW())`,
		tenantID).Scan(&count)
	return count, err
}

func (s *usageStore) Summary(ctx context.Context, tenantID, period string) (*usage.Summary, error) {
	var interval string
	switch period {
	case "day":
		interval = "date_trunc('day', NOW())"
	case "week":
		interval = "NOW() - INTERVAL '7 days'"
	default:
		interval = "date_trunc('month', NOW())"
	}

	summary := &usage.Summary{
		TenantID:   tenantID,
		Period:     period,
		ByProvider: make(map[string]*usage.ProviderUsage),
		ByModel:    make(map[string]*usage.ModelUsage),
	}

	rows, err := s.db.QueryContext(ctx,
		fmt.Sprintf(`SELECT provider, model, COUNT(*), SUM(total_tokens), SUM(cost_usd), SUM(CASE WHEN cached THEN 1 ELSE 0 END)
		 FROM usage_records WHERE tenant_id = $1 AND created_at >= %s
		 GROUP BY provider, model`, interval), tenantID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var prov, mdl string
		var requests, tokens int
		var cost float64
		var cached int
		if err := rows.Scan(&prov, &mdl, &requests, &tokens, &cost, &cached); err != nil {
			return nil, err
		}

		summary.TotalRequests += requests
		summary.TotalTokens += tokens
		summary.TotalCostUSD += cost

		if _, ok := summary.ByProvider[prov]; !ok {
			summary.ByProvider[prov] = &usage.ProviderUsage{}
		}
		summary.ByProvider[prov].Requests += requests
		summary.ByProvider[prov].Tokens += tokens
		summary.ByProvider[prov].CostUSD += cost

		if _, ok := summary.ByModel[mdl]; !ok {
			summary.ByModel[mdl] = &usage.ModelUsage{}
		}
		summary.ByModel[mdl].Requests += requests
		summary.ByModel[mdl].Tokens += tokens
		summary.ByModel[mdl].CostUSD += cost
	}

	return summary, rows.Err()
}

func (s *usageStore) Query(ctx context.Context, opts *usage.QueryOptions) ([]*usage.Record, int, error) {
	query := `SELECT id, tenant_id, key_id, request_id, provider, model,
		prompt_tokens, completion_tokens, total_tokens, cost_usd, latency_ns,
		cached, status_code, created_at FROM usage_records WHERE 1=1`
	var args []any
	argIdx := 1

	if opts != nil {
		if opts.TenantID != "" {
			query += fmt.Sprintf(` AND tenant_id = $%d`, argIdx)
			args = append(args, opts.TenantID)
			argIdx++
		}
		if opts.Provider != "" {
			query += fmt.Sprintf(` AND provider = $%d`, argIdx)
			args = append(args, opts.Provider)
			argIdx++
		}
		if opts.Model != "" {
			query += fmt.Sprintf(` AND model = $%d`, argIdx)
			args = append(args, opts.Model)
			argIdx++
		}
		if !opts.StartTime.IsZero() {
			query += fmt.Sprintf(` AND created_at >= $%d`, argIdx)
			args = append(args, opts.StartTime)
			argIdx++
		}
		if !opts.EndTime.IsZero() {
			query += fmt.Sprintf(` AND created_at <= $%d`, argIdx)
			args = append(args, opts.EndTime)
			argIdx++
		}
	}

	query += ` ORDER BY created_at DESC`
	if opts != nil && opts.Limit > 0 {
		query += fmt.Sprintf(` LIMIT $%d`, argIdx)
		args = append(args, opts.Limit)
		argIdx++
		if opts.Offset > 0 {
			//nolint:gosec // G202 -- parameterized placeholder index, not user input
			query += fmt.Sprintf(` OFFSET $%d`, argIdx)
			args = append(args, opts.Offset)
		}
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()

	var records []*usage.Record
	for rows.Next() {
		rec, err := scanUsageRow(rows)
		if err != nil {
			return nil, 0, err
		}
		records = append(records, rec)
	}
	return records, len(records), rows.Err()
}

func scanUsageRow(row scannable) (*usage.Record, error) {
	var rec usage.Record
	var idStr, tenantIDStr, keyIDStr, requestIDStr string
	var latencyNs int64

	err := row.Scan(&idStr, &tenantIDStr, &keyIDStr, &requestIDStr,
		&rec.Provider, &rec.Model, &rec.PromptTokens, &rec.CompletionTokens,
		&rec.TotalTokens, &rec.CostUSD, &latencyNs, &rec.Cached,
		&rec.StatusCode, &rec.CreatedAt)
	if err != nil {
		return nil, err
	}

	var parseErr error
	rec.ID, parseErr = id.ParseUsageID(idStr)
	if parseErr != nil {
		return nil, fmt.Errorf("nexus/postgres: invalid usage ID: %w", parseErr)
	}
	rec.TenantID, parseErr = id.ParseTenantID(tenantIDStr)
	if parseErr != nil {
		return nil, fmt.Errorf("nexus/postgres: invalid tenant ID: %w", parseErr)
	}
	rec.KeyID, parseErr = id.ParseKeyID(keyIDStr)
	if parseErr != nil {
		return nil, fmt.Errorf("nexus/postgres: invalid key ID: %w", parseErr)
	}
	rec.RequestID, parseErr = id.ParseRequestID(requestIDStr)
	if parseErr != nil {
		return nil, fmt.Errorf("nexus/postgres: invalid request ID: %w", parseErr)
	}
	rec.Latency = time.Duration(latencyNs)
	return &rec, nil
}
