// Package sqlite provides a SQLite-backed store implementation for Nexus
// using grove ORM with programmatic migrations.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/xraph/grove"
	"github.com/xraph/grove/drivers/sqlitedriver"
	"github.com/xraph/grove/migrate"

	"github.com/xraph/nexus/key"
	"github.com/xraph/nexus/store"
	"github.com/xraph/nexus/tenant"
	"github.com/xraph/nexus/usage"
)

// Store is a SQLite-backed persistence store.
type Store struct {
	db  *grove.DB
	sdb *sqlitedriver.SqliteDB
}

// Compile-time check.
var _ store.Store = (*Store)(nil)

// New creates a new SQLite store with the given grove database connection.
func New(db *grove.DB) *Store {
	return &Store{
		db:  db,
		sdb: sqlitedriver.Unwrap(db),
	}
}

func (s *Store) Tenants() tenant.Store { return &tenantStore{sdb: s.sdb} }
func (s *Store) Keys() key.Store       { return &keyStore{sdb: s.sdb} }
func (s *Store) Usage() usage.Store    { return &usageStore{sdb: s.sdb} }

// Migrate runs programmatic migrations via the grove orchestrator.
func (s *Store) Migrate() error {
	ctx := context.Background()
	executor, err := migrate.NewExecutorFor(s.sdb)
	if err != nil {
		return fmt.Errorf("nexus/sqlite: create migration executor: %w", err)
	}
	orch := migrate.NewOrchestrator(executor, Migrations)
	if _, err := orch.Migrate(ctx); err != nil {
		return fmt.Errorf("nexus/sqlite: migration failed: %w", err)
	}
	return nil
}

// Close closes the database connection.
func (s *Store) Close() error { return s.db.Close() }

// isNoRows returns true if the error is sql.ErrNoRows.
func isNoRows(err error) bool { return errors.Is(err, sql.ErrNoRows) }

// ──────────────────────────────────────────────────
// Tenant Store
// ──────────────────────────────────────────────────

type tenantStore struct {
	sdb *sqlitedriver.SqliteDB
}

func (s *tenantStore) Insert(ctx context.Context, t *tenant.Tenant) error {
	m := tenantToModel(t)
	_, err := s.sdb.NewInsert(m).Exec(ctx)
	if err != nil {
		return fmt.Errorf("nexus/sqlite: insert tenant: %w", err)
	}
	return nil
}

func (s *tenantStore) FindByID(ctx context.Context, tid string) (*tenant.Tenant, error) {
	m := new(tenantModel)
	err := s.sdb.NewSelect(m).Where("id = ?", tid).Scan(ctx)
	if err != nil {
		if isNoRows(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("nexus/sqlite: find tenant by id: %w", err)
	}
	return tenantFromModel(m)
}

func (s *tenantStore) FindBySlug(ctx context.Context, slug string) (*tenant.Tenant, error) {
	m := new(tenantModel)
	err := s.sdb.NewSelect(m).Where("slug = ?", slug).Scan(ctx)
	if err != nil {
		if isNoRows(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("nexus/sqlite: find tenant by slug: %w", err)
	}
	return tenantFromModel(m)
}

func (s *tenantStore) Update(ctx context.Context, t *tenant.Tenant) error {
	m := tenantToModel(t)
	_, err := s.sdb.NewUpdate(m).WherePK().Exec(ctx)
	if err != nil {
		return fmt.Errorf("nexus/sqlite: update tenant: %w", err)
	}
	return nil
}

func (s *tenantStore) Delete(ctx context.Context, tid string) error {
	_, err := s.sdb.NewDelete((*tenantModel)(nil)).
		Where("id = ?", tid).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("nexus/sqlite: delete tenant: %w", err)
	}
	return nil
}

func (s *tenantStore) List(ctx context.Context, opts *tenant.ListOptions) ([]*tenant.Tenant, int, error) {
	var models []tenantModel
	q := s.sdb.NewSelect(&models).OrderExpr("created_at DESC")

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
		return nil, 0, fmt.Errorf("nexus/sqlite: list tenants: %w", err)
	}

	tenants := make([]*tenant.Tenant, 0, len(models))
	for i := range models {
		t, err := tenantFromModel(&models[i])
		if err != nil {
			return nil, 0, fmt.Errorf("nexus/sqlite: convert tenant model: %w", err)
		}
		tenants = append(tenants, t)
	}
	return tenants, len(tenants), nil
}

// ──────────────────────────────────────────────────
// Key Store
// ──────────────────────────────────────────────────

type keyStore struct {
	sdb *sqlitedriver.SqliteDB
}

func (s *keyStore) Insert(ctx context.Context, k *key.APIKey) error {
	m := apiKeyToModel(k)
	_, err := s.sdb.NewInsert(m).Exec(ctx)
	if err != nil {
		return fmt.Errorf("nexus/sqlite: insert key: %w", err)
	}
	return nil
}

func (s *keyStore) FindByID(ctx context.Context, kid string) (*key.APIKey, error) {
	m := new(apiKeyModel)
	err := s.sdb.NewSelect(m).Where("id = ?", kid).Scan(ctx)
	if err != nil {
		if isNoRows(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("nexus/sqlite: find key by id: %w", err)
	}
	return apiKeyFromModel(m)
}

func (s *keyStore) FindByPrefix(ctx context.Context, prefix string) (*key.APIKey, error) {
	m := new(apiKeyModel)
	err := s.sdb.NewSelect(m).
		Where("prefix = ?", prefix).
		Where("status = ?", "active").
		Scan(ctx)
	if err != nil {
		if isNoRows(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("nexus/sqlite: find key by prefix: %w", err)
	}
	return apiKeyFromModel(m)
}

func (s *keyStore) Update(ctx context.Context, k *key.APIKey) error {
	m := apiKeyToModel(k)
	_, err := s.sdb.NewUpdate(m).WherePK().Exec(ctx)
	if err != nil {
		return fmt.Errorf("nexus/sqlite: update key: %w", err)
	}
	return nil
}

func (s *keyStore) Delete(ctx context.Context, kid string) error {
	_, err := s.sdb.NewDelete((*apiKeyModel)(nil)).
		Where("id = ?", kid).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("nexus/sqlite: delete key: %w", err)
	}
	return nil
}

func (s *keyStore) ListByTenant(ctx context.Context, tenantID string) ([]*key.APIKey, error) {
	var models []apiKeyModel
	err := s.sdb.NewSelect(&models).
		Where("tenant_id = ?", tenantID).
		OrderExpr("created_at DESC").
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("nexus/sqlite: list keys by tenant: %w", err)
	}

	keys := make([]*key.APIKey, 0, len(models))
	for i := range models {
		k, err := apiKeyFromModel(&models[i])
		if err != nil {
			return nil, fmt.Errorf("nexus/sqlite: convert key model: %w", err)
		}
		keys = append(keys, k)
	}
	return keys, nil
}

// ──────────────────────────────────────────────────
// Usage Store
// ──────────────────────────────────────────────────

type usageStore struct {
	sdb *sqlitedriver.SqliteDB
}

func (s *usageStore) Insert(ctx context.Context, rec *usage.Record) error {
	m := usageToModel(rec)
	_, err := s.sdb.NewInsert(m).Exec(ctx)
	if err != nil {
		return fmt.Errorf("nexus/sqlite: insert usage: %w", err)
	}
	return nil
}

func (s *usageStore) MonthlySpend(ctx context.Context, tenantID string) (float64, error) {
	var total float64
	row := s.sdb.QueryRow(ctx,
		"SELECT COALESCE(SUM(cost_usd), 0) FROM usage_records"+
			" WHERE tenant_id = ? AND created_at >= strftime('%Y-%m-01', 'now')",
		tenantID)
	err := row.Scan(&total)
	return total, err
}

func (s *usageStore) DailyRequests(ctx context.Context, tenantID string) (int, error) {
	var count int
	row := s.sdb.QueryRow(ctx,
		"SELECT COUNT(*) FROM usage_records"+
			" WHERE tenant_id = ? AND created_at >= date('now')",
		tenantID)
	err := row.Scan(&count)
	return count, err
}

func (s *usageStore) Summary(ctx context.Context, tenantID, period string) (*usage.Summary, error) {
	var interval string
	switch period {
	case "day":
		interval = "date('now')"
	case "week":
		interval = "datetime('now', '-7 days')"
	default:
		interval = "strftime('%Y-%m-01', 'now')"
	}

	summary := &usage.Summary{
		TenantID:   tenantID,
		Period:     period,
		ByProvider: make(map[string]*usage.ProviderUsage),
		ByModel:    make(map[string]*usage.ModelUsage),
	}

	rows, err := s.sdb.Query(ctx,
		fmt.Sprintf("SELECT provider, model, COUNT(*), SUM(total_tokens), SUM(cost_usd), SUM(CASE WHEN cached = 1 THEN 1 ELSE 0 END)"+
			" FROM usage_records WHERE tenant_id = ? AND created_at >= %s"+
			" GROUP BY provider, model", interval), tenantID)
	if err != nil {
		return nil, fmt.Errorf("nexus/sqlite: summary query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var prov, mdl string
		var requests, tokens int
		var cost float64
		var cached int
		if err := rows.Scan(&prov, &mdl, &requests, &tokens, &cost, &cached); err != nil {
			return nil, fmt.Errorf("nexus/sqlite: summary scan: %w", err)
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
	q := s.sdb.NewSelect(&models).OrderExpr("created_at DESC")

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
		return nil, 0, fmt.Errorf("nexus/sqlite: query usage: %w", err)
	}

	records := make([]*usage.Record, 0, len(models))
	for i := range models {
		rec, err := usageFromModel(&models[i])
		if err != nil {
			return nil, 0, fmt.Errorf("nexus/sqlite: convert usage model: %w", err)
		}
		records = append(records, rec)
	}
	return records, len(records), nil
}
