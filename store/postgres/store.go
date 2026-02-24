// Package postgres provides a PostgreSQL-backed store implementation for Nexus
// using grove ORM with programmatic migrations.
package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/xraph/grove"
	"github.com/xraph/grove/drivers/pgdriver"
	"github.com/xraph/grove/migrate"

	"github.com/xraph/nexus/key"
	"github.com/xraph/nexus/store"
	"github.com/xraph/nexus/tenant"
	"github.com/xraph/nexus/usage"
)

// Store is a PostgreSQL-backed persistence store.
type Store struct {
	db   *grove.DB
	pgdb *pgdriver.PgDB
}

// Compile-time check.
var _ store.Store = (*Store)(nil)

// New creates a new PostgreSQL store with the given grove database connection.
func New(db *grove.DB) *Store {
	return &Store{
		db:   db,
		pgdb: pgdriver.Unwrap(db),
	}
}

func (s *Store) Tenants() tenant.Store { return &tenantStore{pgdb: s.pgdb} }
func (s *Store) Keys() key.Store       { return &keyStore{pgdb: s.pgdb} }
func (s *Store) Usage() usage.Store    { return &usageStore{pgdb: s.pgdb} }

// Migrate runs programmatic migrations via the grove orchestrator.
func (s *Store) Migrate() error {
	ctx := context.Background()
	executor := &pgMigrateExecutor{pgdb: s.pgdb}

	orch := migrate.NewOrchestrator(executor, Migrations)
	if _, err := orch.Migrate(ctx); err != nil {
		return fmt.Errorf("nexus/postgres: migration failed: %w", err)
	}
	return nil
}

// Close closes the database connection.
func (s *Store) Close() error { return s.db.Close() }

// ──────────────────────────────────────────────────
// Tenant Store
// ──────────────────────────────────────────────────

type tenantStore struct {
	pgdb *pgdriver.PgDB
}

func (s *tenantStore) Insert(ctx context.Context, t *tenant.Tenant) error {
	m := tenantToModel(t)
	_, err := s.pgdb.NewInsert(m).Exec(ctx)
	if err != nil {
		return fmt.Errorf("nexus/postgres: insert tenant: %w", err)
	}
	return nil
}

func (s *tenantStore) FindByID(ctx context.Context, tid string) (*tenant.Tenant, error) {
	m := new(tenantModel)
	err := s.pgdb.NewSelect(m).Where("id = ?", tid).Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("nexus/postgres: find tenant by id: %w", err)
	}
	return tenantFromModel(m)
}

func (s *tenantStore) FindBySlug(ctx context.Context, slug string) (*tenant.Tenant, error) {
	m := new(tenantModel)
	err := s.pgdb.NewSelect(m).Where("slug = ?", slug).Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("nexus/postgres: find tenant by slug: %w", err)
	}
	return tenantFromModel(m)
}

func (s *tenantStore) Update(ctx context.Context, t *tenant.Tenant) error {
	m := tenantToModel(t)
	_, err := s.pgdb.NewUpdate(m).WherePK().Exec(ctx)
	if err != nil {
		return fmt.Errorf("nexus/postgres: update tenant: %w", err)
	}
	return nil
}

func (s *tenantStore) Delete(ctx context.Context, tid string) error {
	_, err := s.pgdb.NewDelete((*tenantModel)(nil)).
		Where("id = ?", tid).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("nexus/postgres: delete tenant: %w", err)
	}
	return nil
}

func (s *tenantStore) List(ctx context.Context, opts *tenant.ListOptions) ([]*tenant.Tenant, int, error) {
	var models []tenantModel
	q := s.pgdb.NewSelect(&models).OrderExpr("created_at DESC")

	if opts != nil {
		if opts.Status != "" {
			q = q.Where("status = ?", opts.Status)
		}
		if opts.Limit > 0 {
			q = q.Limit(opts.Limit)
		}
		if opts.Offset > 0 {
			q = q.Offset(opts.Offset)
		}
	}

	if err := q.Scan(ctx); err != nil {
		return nil, 0, fmt.Errorf("nexus/postgres: list tenants: %w", err)
	}

	tenants := make([]*tenant.Tenant, 0, len(models))
	for i := range models {
		t, err := tenantFromModel(&models[i])
		if err != nil {
			return nil, 0, fmt.Errorf("nexus/postgres: convert tenant model: %w", err)
		}
		tenants = append(tenants, t)
	}
	return tenants, len(tenants), nil
}

// ──────────────────────────────────────────────────
// Key Store
// ──────────────────────────────────────────────────

type keyStore struct {
	pgdb *pgdriver.PgDB
}

func (s *keyStore) Insert(ctx context.Context, k *key.APIKey) error {
	m := apiKeyToModel(k)
	_, err := s.pgdb.NewInsert(m).Exec(ctx)
	if err != nil {
		return fmt.Errorf("nexus/postgres: insert key: %w", err)
	}
	return nil
}

func (s *keyStore) FindByID(ctx context.Context, kid string) (*key.APIKey, error) {
	m := new(apiKeyModel)
	err := s.pgdb.NewSelect(m).Where("id = ?", kid).Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("nexus/postgres: find key by id: %w", err)
	}
	return apiKeyFromModel(m)
}

func (s *keyStore) FindByPrefix(ctx context.Context, prefix string) (*key.APIKey, error) {
	m := new(apiKeyModel)
	err := s.pgdb.NewSelect(m).
		Where("prefix = ?", prefix).
		Where("status = ?", "active").
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("nexus/postgres: find key by prefix: %w", err)
	}
	return apiKeyFromModel(m)
}

func (s *keyStore) Update(ctx context.Context, k *key.APIKey) error {
	m := apiKeyToModel(k)
	_, err := s.pgdb.NewUpdate(m).WherePK().Exec(ctx)
	if err != nil {
		return fmt.Errorf("nexus/postgres: update key: %w", err)
	}
	return nil
}

func (s *keyStore) Delete(ctx context.Context, kid string) error {
	_, err := s.pgdb.NewDelete((*apiKeyModel)(nil)).
		Where("id = ?", kid).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("nexus/postgres: delete key: %w", err)
	}
	return nil
}

func (s *keyStore) ListByTenant(ctx context.Context, tenantID string) ([]*key.APIKey, error) {
	var models []apiKeyModel
	err := s.pgdb.NewSelect(&models).
		Where("tenant_id = ?", tenantID).
		OrderExpr("created_at DESC").
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("nexus/postgres: list keys by tenant: %w", err)
	}

	keys := make([]*key.APIKey, 0, len(models))
	for i := range models {
		k, err := apiKeyFromModel(&models[i])
		if err != nil {
			return nil, fmt.Errorf("nexus/postgres: convert key model: %w", err)
		}
		keys = append(keys, k)
	}
	return keys, nil
}

// ──────────────────────────────────────────────────
// Usage Store
// ──────────────────────────────────────────────────

type usageStore struct {
	pgdb *pgdriver.PgDB
}

func (s *usageStore) Insert(ctx context.Context, rec *usage.Record) error {
	m := usageToModel(rec)
	_, err := s.pgdb.NewInsert(m).Exec(ctx)
	if err != nil {
		return fmt.Errorf("nexus/postgres: insert usage: %w", err)
	}
	return nil
}

func (s *usageStore) MonthlySpend(ctx context.Context, tenantID string) (float64, error) {
	var total float64
	row := s.pgdb.QueryRow(ctx,
		`SELECT COALESCE(SUM(cost_usd), 0) FROM usage_records
		 WHERE tenant_id = $1 AND created_at >= date_trunc('month', NOW())`,
		tenantID)
	err := row.Scan(&total)
	return total, err
}

func (s *usageStore) DailyRequests(ctx context.Context, tenantID string) (int, error) {
	var count int
	row := s.pgdb.QueryRow(ctx,
		`SELECT COUNT(*) FROM usage_records
		 WHERE tenant_id = $1 AND created_at >= date_trunc('day', NOW())`,
		tenantID)
	err := row.Scan(&count)
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

	rows, err := s.pgdb.Query(ctx,
		fmt.Sprintf(`SELECT provider, model, COUNT(*), SUM(total_tokens), SUM(cost_usd), SUM(CASE WHEN cached THEN 1 ELSE 0 END)
		 FROM usage_records WHERE tenant_id = $1 AND created_at >= %s
		 GROUP BY provider, model`, interval), tenantID)
	if err != nil {
		return nil, fmt.Errorf("nexus/postgres: summary query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var prov, mdl string
		var requests, tokens int
		var cost float64
		var cached int
		if err := rows.Scan(&prov, &mdl, &requests, &tokens, &cost, &cached); err != nil {
			return nil, fmt.Errorf("nexus/postgres: summary scan: %w", err)
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
	var models []usageModel
	q := s.pgdb.NewSelect(&models).OrderExpr("created_at DESC")

	if opts != nil {
		if opts.TenantID != "" {
			q = q.Where("tenant_id = ?", opts.TenantID)
		}
		if opts.Provider != "" {
			q = q.Where("provider = ?", opts.Provider)
		}
		if opts.Model != "" {
			q = q.Where("model = ?", opts.Model)
		}
		if !opts.StartTime.IsZero() {
			q = q.Where("created_at >= ?", opts.StartTime)
		}
		if !opts.EndTime.IsZero() {
			q = q.Where("created_at <= ?", opts.EndTime)
		}
		if opts.Limit > 0 {
			q = q.Limit(opts.Limit)
		}
		if opts.Offset > 0 {
			q = q.Offset(opts.Offset)
		}
	}

	if err := q.Scan(ctx); err != nil {
		return nil, 0, fmt.Errorf("nexus/postgres: query usage: %w", err)
	}

	records := make([]*usage.Record, 0, len(models))
	for i := range models {
		rec, err := usageFromModel(&models[i])
		if err != nil {
			return nil, 0, fmt.Errorf("nexus/postgres: convert usage model: %w", err)
		}
		records = append(records, rec)
	}
	return records, len(records), nil
}
