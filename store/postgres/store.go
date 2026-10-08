// Package postgres provides a PostgreSQL-backed store implementation for Nexus
// using grove ORM with programmatic migrations.
package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/xraph/grove"
	"github.com/xraph/grove/drivers/pgdriver"
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
			return nil, tenant.ErrNotFound
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
			return nil, tenant.ErrNotFound
		}
		return nil, fmt.Errorf("nexus/postgres: find tenant by slug: %w", err)
	}
	return tenantFromModel(m)
}

func (s *tenantStore) Update(ctx context.Context, t *tenant.Tenant) error {
	m := tenantToModel(t)
	res, err := s.pgdb.NewUpdate(m).WherePK().Exec(ctx)
	if err != nil {
		return fmt.Errorf("nexus/postgres: update tenant: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return tenant.ErrNotFound
	}
	return nil
}

func (s *tenantStore) Delete(ctx context.Context, tid string) error {
	_, err := s.pgdb.NewDelete((*tenantModel)(nil)).
		Where("id = ?", tid).
		Exec(ctx)
	if err != nil {
		// Keys and usage rows reference the tenant: it is in use.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgForeignKeyViolation {
			return fmt.Errorf("nexus/postgres: delete tenant: %w", tenant.ErrInUse)
		}
		return fmt.Errorf("nexus/postgres: delete tenant: %w", err)
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
	q := s.pgdb.NewSelect(&models).OrderExpr(`id COLLATE "C" DESC`).Limit(limit + 1)
	if opts.Status != "" {
		q = q.Where("status = ?", opts.Status)
	}
	if opts.Search != "" {
		p := conv.LikePattern(opts.Search)
		q = q.Where("(name ILIKE ? OR slug ILIKE ?)", p, p)
	}
	if opts.Cursor != "" {
		q = q.Where(`id COLLATE "C" < ?`, opts.Cursor)
	}
	if err := q.Scan(ctx); err != nil {
		return nil, fmt.Errorf("nexus/postgres: list tenants: %w", err)
	}
	rows := make([]*tenant.Tenant, 0, len(models))
	for i := range models {
		t, err := tenantFromModel(&models[i])
		if err != nil {
			return nil, fmt.Errorf("nexus/postgres: convert tenant model: %w", err)
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
	pgdb *pgdriver.PgDB
}

// pgUniqueViolation and pgForeignKeyViolation are the SQLSTATEs for a
// unique_violation and a foreign_key_violation.
const (
	pgUniqueViolation     = "23505"
	pgForeignKeyViolation = "23503"
)

func (s *keyStore) Insert(ctx context.Context, k *key.APIKey) error {
	m := apiKeyToModel(k)
	_, err := s.pgdb.NewInsert(m).Exec(ctx)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
			return fmt.Errorf("nexus/postgres: insert key: %w", key.ErrDuplicate)
		}
		return fmt.Errorf("nexus/postgres: insert key: %w", err)
	}
	return nil
}

func (s *keyStore) FindByID(ctx context.Context, kid string) (*key.APIKey, error) {
	m := new(apiKeyModel)
	err := s.pgdb.NewSelect(m).Where("id = ?", kid).Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, key.ErrNotFound
		}
		return nil, fmt.Errorf("nexus/postgres: find key by id: %w", err)
	}
	return apiKeyFromModel(m)
}

func (s *keyStore) FindByPrefix(ctx context.Context, prefix string) ([]*key.APIKey, error) {
	var models []apiKeyModel
	if err := s.pgdb.NewSelect(&models).Where("prefix = ?", prefix).Scan(ctx); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("nexus/postgres: find keys by prefix: %w", err)
	}
	out := make([]*key.APIKey, 0, len(models))
	for i := range models {
		k, err := apiKeyFromModel(&models[i])
		if err != nil {
			return nil, fmt.Errorf("nexus/postgres: convert key model: %w", err)
		}
		out = append(out, k)
	}
	return out, nil
}

func (s *keyStore) TouchLastUsed(ctx context.Context, kid string, at time.Time) error {
	m := &apiKeyModel{ID: kid, LastUsedAt: &at}
	res, err := s.pgdb.NewUpdate(m).Column("last_used_at").WherePK().Exec(ctx)
	if err != nil {
		return fmt.Errorf("nexus/postgres: touch key: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return key.ErrNotFound
	}
	return nil
}

func (s *keyStore) Update(ctx context.Context, k *key.APIKey) error {
	m := apiKeyToModel(k)
	res, err := s.pgdb.NewUpdate(m).WherePK().Exec(ctx)
	if err != nil {
		return fmt.Errorf("nexus/postgres: update key: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return key.ErrNotFound
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

// keyFilter applies opts' tenant and status filters to q, with expiry derived
// against now. The status clauses mirror key.Effective.
func keyFilter(q *pgdriver.SelectQuery, opts *key.ListOptions, now time.Time) *pgdriver.SelectQuery {
	if opts.TenantID != "" {
		q = q.Where("tenant_id = ?", opts.TenantID)
	}
	switch opts.Status {
	case "":
	case key.KeyActive:
		q = q.Where(`status = 'active' AND (expires_at IS NULL OR expires_at > ?)`, now)
	case key.KeyExpired:
		q = q.Where(`(status = 'expired' OR (status = 'active' AND expires_at <= ?))`, now)
	default:
		q = q.Where("status = ?", string(opts.Status))
	}
	return q
}

func (s *keyStore) List(ctx context.Context, opts *key.ListOptions) (*key.ListResult, error) {
	if opts == nil {
		opts = &key.ListOptions{}
	}
	if err := paging.CheckCursor(opts.Cursor, id.PrefixKey); err != nil {
		return nil, err
	}
	limit := paging.Limit(opts.Limit)
	now := opts.At()
	var models []apiKeyModel
	q := keyFilter(s.pgdb.NewSelect(&models), opts, now).OrderExpr(`id COLLATE "C" DESC`).Limit(limit + 1)
	if opts.Cursor != "" {
		q = q.Where(`id COLLATE "C" < ?`, opts.Cursor)
	}
	if err := q.Scan(ctx); err != nil {
		return nil, fmt.Errorf("nexus/postgres: list keys: %w", err)
	}
	rows := make([]*key.APIKey, 0, len(models))
	for i := range models {
		k, err := apiKeyFromModel(&models[i])
		if err != nil {
			return nil, fmt.Errorf("nexus/postgres: convert key model: %w", err)
		}
		k.Status = key.Effective(k, now)
		rows = append(rows, k)
	}
	page, next := paging.Trim(rows, limit, func(k *key.APIKey) string { return k.ID.String() })
	return &key.ListResult{Items: page, NextCursor: next}, nil
}

func (s *keyStore) Count(ctx context.Context, opts *key.ListOptions) (int, error) {
	if opts == nil {
		opts = &key.ListOptions{}
	}
	n, err := keyFilter(s.pgdb.NewSelect((*apiKeyModel)(nil)), opts, opts.At()).Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("nexus/postgres: count keys: %w", err)
	}
	return int(n), nil
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

func (s *usageStore) MonthlySpend(ctx context.Context, tenantID string) (money.USD, error) {
	since, err := usage.PeriodStart("month", time.Now())
	if err != nil {
		return money.Zero, err
	}
	q := `SELECT SUM(cost_usd) FROM nexus_usage_records WHERE created_at >= $1`
	args := []any{since}
	if tenantID != "" {
		q += ` AND tenant_id = $2`
		args = append(args, tenantID)
	}
	var total numeric
	if err = s.pgdb.QueryRow(ctx, q, args...).Scan(&total); err != nil {
		return money.Zero, fmt.Errorf("nexus/postgres: monthly spend: %w", err)
	}
	u, err := total.usd()
	if err != nil || u == nil {
		return money.Zero, err
	}
	return *u, nil
}

func (s *usageStore) DailyRequests(ctx context.Context, tenantID string) (int, error) {
	since, err := usage.PeriodStart("day", time.Now())
	if err != nil {
		return 0, err
	}
	q := `SELECT COUNT(*) FROM nexus_usage_records WHERE created_at >= $1 AND outcome <> 'refused'`
	args := []any{since}
	if tenantID != "" {
		q += ` AND tenant_id = $2`
		args = append(args, tenantID)
	}
	var count int
	if err = s.pgdb.QueryRow(ctx, q, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("nexus/postgres: daily requests: %w", err)
	}
	return count, nil
}

func (s *usageStore) Summary(ctx context.Context, tenantID, period string) (*usage.Summary, error) {
	since, err := usage.PeriodStart(period, time.Now())
	if err != nil {
		return nil, err
	}
	rows, err := s.pgdb.Query(ctx,
		`SELECT provider, model, outcome, pricing_status, cached, COUNT(*),
		        COALESCE(SUM(total_tokens), 0)::bigint, COALESCE(SUM(latency_ns), 0)::bigint,
		        SUM(cost_usd)
		   FROM nexus_usage_records
		  WHERE ($1 = '' OR tenant_id = $1) AND created_at >= $2
		  GROUP BY provider, model, outcome, pricing_status, cached`,
		tenantID, since)
	if err != nil {
		return nil, fmt.Errorf("nexus/postgres: summary: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []usage.SummaryRow
	for rows.Next() {
		var r usage.SummaryRow
		var outcome, status string
		var tokens, latency int64
		var cost numeric
		if err := rows.Scan(&r.Provider, &r.Model, &outcome, &status, &r.Cached, &r.Requests, &tokens, &latency, &cost); err != nil {
			return nil, fmt.Errorf("nexus/postgres: summary scan: %w", err)
		}
		r.Outcome, r.PricingStatus, r.Tokens, r.LatencyNs = usage.Outcome(outcome), usage.PricingStatus(status), int(tokens), latency
		c, err := cost.usd()
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
	rows, err := s.pgdb.Query(ctx,
		`SELECT date_trunc($2, created_at, 'UTC'), COUNT(*), COALESCE(SUM(total_tokens), 0)::bigint,
		        SUM(cost_usd), COUNT(*) FILTER (WHERE pricing_status IN ('unpriced_model', 'unknown'))
		   FROM nexus_usage_records
		  WHERE ($1 = '' OR tenant_id = $1) AND created_at >= $3 AND created_at < $4
		  GROUP BY 1`,
		opts.TenantID, string(opts.Bucket), usage.BucketStart(opts.Start, opts.Bucket), opts.End.UTC())
	if err != nil {
		return nil, fmt.Errorf("nexus/postgres: series: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var points []usage.SeriesPoint
	for rows.Next() {
		var p usage.SeriesPoint
		var tokens int64
		var cost numeric
		if err := rows.Scan(&p.Start, &p.Requests, &tokens, &cost, &p.Unpriced); err != nil {
			return nil, fmt.Errorf("nexus/postgres: series scan: %w", err)
		}
		p.Tokens = int(tokens)
		c, err := cost.usd()
		if err != nil {
			return nil, err
		}
		if c != nil {
			p.CostUSD = *c
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
	q := s.pgdb.NewSelect(&models).OrderExpr(`id COLLATE "C" DESC`).Limit(limit + 1)
	for col, v := range map[string]string{
		"tenant_id": opts.TenantID, "key_id": opts.KeyID, "provider": opts.Provider,
		"model": opts.Model, "outcome": string(opts.Outcome),
	} {
		if v != "" {
			q = q.Where(col+" = ?", v)
		}
	}
	if !opts.StartTime.IsZero() {
		q = q.Where("created_at >= ?", opts.StartTime.UTC())
	}
	if !opts.EndTime.IsZero() {
		q = q.Where("created_at < ?", opts.EndTime.UTC())
	}
	if opts.Cursor != "" {
		q = q.Where(`id COLLATE "C" < ?`, opts.Cursor)
	}
	if err := q.Scan(ctx); err != nil {
		return nil, fmt.Errorf("nexus/postgres: query usage: %w", err)
	}
	rows := make([]*usage.Record, 0, len(models))
	for i := range models {
		rec, err := usageFromModel(&models[i])
		if err != nil {
			return nil, fmt.Errorf("nexus/postgres: convert usage model: %w", err)
		}
		rows = append(rows, rec)
	}
	page, next := paging.Trim(rows, limit, func(r *usage.Record) string { return r.ID.String() })
	return &usage.QueryResult{Items: page, NextCursor: next}, nil
}
