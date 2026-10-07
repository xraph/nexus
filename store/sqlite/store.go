// Package sqlite provides a SQLite-backed store implementation for Nexus
// using grove ORM with programmatic migrations.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/xraph/grove"
	"github.com/xraph/grove/drivers/sqlitedriver"

	// Registers the "sqlite" migration executor that Migrate looks up.
	_ "github.com/xraph/grove/drivers/sqlitedriver/sqlitemigrate"
	"github.com/xraph/grove/migrate"

	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/key"
	"github.com/xraph/nexus/money"
	"github.com/xraph/nexus/paging"
	"github.com/xraph/nexus/store"
	"github.com/xraph/nexus/store/internal/conv"
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
	return s.normaliseUsageTimes(ctx)
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
			return nil, tenant.ErrNotFound
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
			return nil, tenant.ErrNotFound
		}
		return nil, fmt.Errorf("nexus/sqlite: find tenant by slug: %w", err)
	}
	return tenantFromModel(m)
}

func (s *tenantStore) Update(ctx context.Context, t *tenant.Tenant) error {
	m := tenantToModel(t)
	res, err := s.sdb.NewUpdate(m).WherePK().Exec(ctx)
	if err != nil {
		return fmt.Errorf("nexus/sqlite: update tenant: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return tenant.ErrNotFound
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

func (s *tenantStore) List(ctx context.Context, opts *tenant.ListOptions) (*tenant.ListResult, error) {
	if opts == nil {
		opts = &tenant.ListOptions{}
	}
	if err := paging.CheckCursor(opts.Cursor, id.PrefixTenant); err != nil {
		return nil, err
	}
	limit := paging.Limit(opts.Limit)
	var models []tenantModel
	q := s.sdb.NewSelect(&models).OrderExpr("id DESC").Limit(limit + 1)
	if opts.Status != "" {
		q = q.Where("status = ?", opts.Status)
	}
	if opts.Search != "" {
		p := conv.LikePattern(opts.Search)
		q = q.Where(`(name LIKE ? ESCAPE '\' OR slug LIKE ? ESCAPE '\')`, p, p)
	}
	if opts.Cursor != "" {
		q = q.Where("id < ?", opts.Cursor)
	}
	if err := q.Scan(ctx); err != nil {
		return nil, fmt.Errorf("nexus/sqlite: list tenants: %w", err)
	}
	rows := make([]*tenant.Tenant, 0, len(models))
	for i := range models {
		t, err := tenantFromModel(&models[i])
		if err != nil {
			return nil, fmt.Errorf("nexus/sqlite: convert tenant model: %w", err)
		}
		rows = append(rows, t)
	}
	page, next := paging.Trim(rows, limit, func(t *tenant.Tenant) string { return t.ID.String() })
	return &tenant.ListResult{Items: page, NextCursor: next}, nil
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
			return nil, key.ErrNotFound
		}
		return nil, fmt.Errorf("nexus/sqlite: find key by id: %w", err)
	}
	return apiKeyFromModel(m)
}

func (s *keyStore) FindByPrefix(ctx context.Context, prefix string) ([]*key.APIKey, error) {
	var models []apiKeyModel
	if err := s.sdb.NewSelect(&models).Where("prefix = ?", prefix).Scan(ctx); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("nexus/sqlite: find keys by prefix: %w", err)
	}
	out := make([]*key.APIKey, 0, len(models))
	for i := range models {
		k, err := apiKeyFromModel(&models[i])
		if err != nil {
			return nil, fmt.Errorf("nexus/sqlite: convert key model: %w", err)
		}
		out = append(out, k)
	}
	return out, nil
}

func (s *keyStore) TouchLastUsed(ctx context.Context, kid string, at time.Time) error {
	m := &apiKeyModel{ID: kid, LastUsedAt: &at}
	res, err := s.sdb.NewUpdate(m).Column("last_used_at").WherePK().Exec(ctx)
	if err != nil {
		return fmt.Errorf("nexus/sqlite: touch key: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return key.ErrNotFound
	}
	return nil
}

func (s *keyStore) Update(ctx context.Context, k *key.APIKey) error {
	m := apiKeyToModel(k)
	res, err := s.sdb.NewUpdate(m).WherePK().Exec(ctx)
	if err != nil {
		return fmt.Errorf("nexus/sqlite: update key: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return key.ErrNotFound
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

func (s *keyStore) List(ctx context.Context, opts *key.ListOptions) (*key.ListResult, error) {
	if opts == nil {
		opts = &key.ListOptions{}
	}
	if err := paging.CheckCursor(opts.Cursor, id.PrefixKey); err != nil {
		return nil, err
	}
	limit := paging.Limit(opts.Limit)
	var models []apiKeyModel
	q := s.sdb.NewSelect(&models).OrderExpr("id DESC").Limit(limit + 1)
	if opts.TenantID != "" {
		q = q.Where("tenant_id = ?", opts.TenantID)
	}
	if opts.Status != "" {
		q = q.Where("status = ?", string(opts.Status))
	}
	if opts.Cursor != "" {
		q = q.Where("id < ?", opts.Cursor)
	}
	if err := q.Scan(ctx); err != nil {
		return nil, fmt.Errorf("nexus/sqlite: list keys: %w", err)
	}
	rows := make([]*key.APIKey, 0, len(models))
	for i := range models {
		k, err := apiKeyFromModel(&models[i])
		if err != nil {
			return nil, fmt.Errorf("nexus/sqlite: convert key model: %w", err)
		}
		rows = append(rows, k)
	}
	page, next := paging.Trim(rows, limit, func(k *key.APIKey) string { return k.ID.String() })
	return &key.ListResult{Items: page, NextCursor: next}, nil
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

func (s *usageStore) MonthlySpend(ctx context.Context, tenantID string) (money.USD, error) {
	since, err := usage.PeriodStart("month", time.Now())
	if err != nil {
		return money.Zero, err
	}
	q := `SELECT cost_usd FROM usage_records WHERE created_at >= ? AND cost_usd IS NOT NULL`
	args := []any{conv.TimeText(since)}
	if tenantID != "" {
		q += ` AND tenant_id = ?`
		args = append(args, tenantID)
	}
	rows, err := s.sdb.Query(ctx, q, args...)
	if err != nil {
		return money.Zero, fmt.Errorf("nexus/sqlite: monthly spend: %w", err)
	}
	defer func() { _ = rows.Close() }()
	total := money.Zero
	for rows.Next() {
		var text string
		if err := rows.Scan(&text); err != nil {
			return money.Zero, fmt.Errorf("nexus/sqlite: monthly spend scan: %w", err)
		}
		c, err := conv.ParseCost(&text)
		if err != nil {
			return money.Zero, err
		}
		total = total.Add(*c)
	}
	return total, rows.Err()
}

func (s *usageStore) DailyRequests(ctx context.Context, tenantID string) (int, error) {
	since, err := usage.PeriodStart("day", time.Now())
	if err != nil {
		return 0, err
	}
	q := `SELECT COUNT(*) FROM usage_records WHERE created_at >= ? AND outcome <> 'refused'`
	args := []any{conv.TimeText(since)}
	if tenantID != "" {
		q += ` AND tenant_id = ?`
		args = append(args, tenantID)
	}
	var count int
	if err = s.sdb.QueryRow(ctx, q, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("nexus/sqlite: daily requests: %w", err)
	}
	return count, nil
}

func (s *usageStore) Summary(ctx context.Context, tenantID, period string) (*usage.Summary, error) {
	since, err := usage.PeriodStart(period, time.Now())
	if err != nil {
		return nil, err
	}
	rows, err := s.sdb.Query(ctx,
		`SELECT provider, model, outcome, pricing_status, cached, total_tokens, latency_ns, cost_usd
		   FROM usage_records WHERE (? = '' OR tenant_id = ?) AND created_at >= ?`,
		tenantID, tenantID, conv.TimeText(since))
	if err != nil {
		return nil, fmt.Errorf("nexus/sqlite: summary: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []usage.SummaryRow
	for rows.Next() {
		r := usage.SummaryRow{Requests: 1}
		var outcome, status string
		var cost *string
		if err := rows.Scan(&r.Provider, &r.Model, &outcome, &status, &r.Cached, &r.Tokens, &r.LatencyNs, &cost); err != nil {
			return nil, fmt.Errorf("nexus/sqlite: summary scan: %w", err)
		}
		r.Outcome, r.PricingStatus = usage.Outcome(outcome), usage.PricingStatus(status)
		c, err := conv.ParseCost(cost)
		if err != nil {
			return nil, err
		}
		if c != nil {
			r.Cost = *c
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return usage.BuildSummary(tenantID, period, out), nil
}

func (s *usageStore) Series(ctx context.Context, opts *usage.SeriesOptions) ([]usage.SeriesPoint, error) {
	if _, err := usage.FillSeries(opts, nil); err != nil {
		return nil, err
	}
	rows, err := s.sdb.Query(ctx,
		`SELECT created_at, total_tokens, cost_usd, pricing_status FROM usage_records
		  WHERE (? = '' OR tenant_id = ?) AND created_at >= ? AND created_at < ?`,
		opts.TenantID, opts.TenantID, conv.TimeText(usage.BucketStart(opts.Start, opts.Bucket)), conv.TimeText(opts.End))
	if err != nil {
		return nil, fmt.Errorf("nexus/sqlite: series: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var points []usage.SeriesPoint
	for rows.Next() {
		var created, status string
		var cost *string
		p := usage.SeriesPoint{Requests: 1}
		if err = rows.Scan(&created, &p.Tokens, &cost, &status); err != nil {
			return nil, fmt.Errorf("nexus/sqlite: series scan: %w", err)
		}
		if p.Start, err = conv.ParseTimeText(created); err != nil {
			return nil, err
		}
		c, err := conv.ParseCost(cost)
		if err != nil {
			return nil, err
		}
		if c != nil {
			p.CostUSD = *c
		}
		if usage.PricingStatus(status).CostUnknown() {
			p.Unpriced = 1
		}
		points = append(points, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return usage.FillSeries(opts, points)
}

func (s *usageStore) Query(ctx context.Context, opts *usage.QueryOptions) (*usage.QueryResult, error) {
	if opts == nil {
		opts = &usage.QueryOptions{}
	}
	if err := paging.CheckCursor(opts.Cursor, id.PrefixUsage); err != nil {
		return nil, err
	}
	limit := paging.Limit(opts.Limit)
	var models []usageModel
	q := s.sdb.NewSelect(&models).OrderExpr("id DESC").Limit(limit + 1)
	for col, v := range map[string]string{
		"tenant_id": opts.TenantID, "key_id": opts.KeyID, "provider": opts.Provider,
		"model": opts.Model, "outcome": string(opts.Outcome),
	} {
		if v != "" {
			q = q.Where(col+" = ?", v)
		}
	}
	if !opts.StartTime.IsZero() {
		q = q.Where("created_at >= ?", conv.TimeText(opts.StartTime))
	}
	if !opts.EndTime.IsZero() {
		q = q.Where("created_at < ?", conv.TimeText(opts.EndTime))
	}
	if opts.Cursor != "" {
		q = q.Where("id < ?", opts.Cursor)
	}
	if err := q.Scan(ctx); err != nil {
		return nil, fmt.Errorf("nexus/sqlite: query usage: %w", err)
	}
	rows := make([]*usage.Record, 0, len(models))
	for i := range models {
		rec, err := usageFromModel(&models[i])
		if err != nil {
			return nil, fmt.Errorf("nexus/sqlite: convert usage model: %w", err)
		}
		rows = append(rows, rec)
	}
	page, next := paging.Trim(rows, limit, func(r *usage.Record) string { return r.ID.String() })
	return &usage.QueryResult{Items: page, NextCursor: next}, nil
}
