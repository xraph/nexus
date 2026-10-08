// Package mongo provides a MongoDB-backed store implementation for Nexus
// using grove ORM with the mongodriver.
package mongo

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/xraph/grove"
	"github.com/xraph/grove/drivers/mongodriver"

	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/key"
	"github.com/xraph/nexus/money"
	"github.com/xraph/nexus/paging"
	"github.com/xraph/nexus/store"
	"github.com/xraph/nexus/tenant"
	"github.com/xraph/nexus/usage"
)

// Collection name constants.
const (
	colTenants = "nexus_tenants"
	colKeys    = "nexus_api_keys"
	colUsage   = "nexus_usage_records"
)

// Compile-time interface check.
var _ store.Store = (*Store)(nil)

// Store is a MongoDB-backed persistence store.
type Store struct {
	db  *grove.DB
	mdb *mongodriver.MongoDB
}

// New creates a new MongoDB store with the given grove database connection.
func New(db *grove.DB) *Store {
	return &Store{
		db:  db,
		mdb: mongodriver.Unwrap(db),
	}
}

func (s *Store) Tenants() tenant.Store { return &tenantStore{mdb: s.mdb} }
func (s *Store) Keys() key.Store       { return &keyStore{mdb: s.mdb} }
func (s *Store) Usage() usage.Store    { return &usageStore{mdb: s.mdb} }

// Migrate creates indexes for all nexus collections.
func (s *Store) Migrate() error {
	ctx := context.Background()
	indexes := migrationIndexes()

	for col, models := range indexes {
		if len(models) == 0 {
			continue
		}
		_, err := s.mdb.Collection(col).Indexes().CreateMany(ctx, models)
		if err != nil {
			return fmt.Errorf("nexus/mongo: migrate %s indexes: %w", col, err)
		}
	}

	// Documents written before exact money have no pricing_status. Nothing
	// computed a cost then, so their stored 0 means "unknown", not "free".
	// A cache hit is the exception: it called no provider, so it cost
	// exactly 0.
	zero, err := bson.ParseDecimal128("0")
	if err != nil {
		return fmt.Errorf("nexus/mongo: decimal128 zero: %w", err)
	}
	unknown := bson.M{"$eq": bson.A{"$cost_usd", 0}}
	_, err = s.mdb.Collection(colUsage).UpdateMany(ctx,
		bson.M{"pricing_status": bson.M{"$exists": false}},
		bson.A{bson.M{"$set": bson.M{
			"pricing_status": bson.M{"$cond": bson.A{"$cached", "cached",
				bson.M{"$cond": bson.A{unknown, "unpriced_model", "priced"}}}},
			"cost_usd": bson.M{"$cond": bson.A{"$cached", zero,
				bson.M{"$cond": bson.A{unknown, nil, bson.M{"$toDecimal": "$cost_usd"}}}}},
			"outcome": bson.M{"$cond": bson.A{"$cached", "cached",
				bson.M{"$cond": bson.A{bson.M{"$gte": bson.A{"$status_code", 400}}, "error", "ok"}}}},
			"blocked_by":   "",
			"refusal_code": "",
		}}},
	)
	if err != nil {
		return fmt.Errorf("nexus/mongo: normalise legacy usage: %w", err)
	}
	return nil
}

// Close closes the database connection.
func (s *Store) Close() error { return s.db.Close() }

// isNoDocuments checks if an error wraps mongo.ErrNoDocuments.
func isNoDocuments(err error) bool {
	return errors.Is(err, mongo.ErrNoDocuments)
}

// ──────────────────────────────────────────────────
// Tenant Store
// ──────────────────────────────────────────────────

type tenantStore struct {
	mdb *mongodriver.MongoDB
}

func (s *tenantStore) Insert(ctx context.Context, t *tenant.Tenant) error {
	m, err := tenantToModel(t)
	if err != nil {
		return err
	}
	if _, err := s.mdb.NewInsert(m).Exec(ctx); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return fmt.Errorf("nexus/mongo: insert tenant: %w", tenant.ErrDuplicate)
		}
		return fmt.Errorf("nexus/mongo: insert tenant: %w", err)
	}
	return nil
}

func (s *tenantStore) FindByID(ctx context.Context, tid string) (*tenant.Tenant, error) {
	var m tenantModel
	err := s.mdb.NewFind(&m).Filter(bson.M{"_id": tid}).Scan(ctx)
	if err != nil {
		if isNoDocuments(err) {
			return nil, tenant.ErrNotFound
		}
		return nil, fmt.Errorf("nexus/mongo: find tenant by id: %w", err)
	}
	return tenantFromModel(&m)
}

func (s *tenantStore) FindBySlug(ctx context.Context, slug string) (*tenant.Tenant, error) {
	var m tenantModel
	err := s.mdb.NewFind(&m).Filter(bson.M{"slug": slug}).Scan(ctx)
	if err != nil {
		if isNoDocuments(err) {
			return nil, tenant.ErrNotFound
		}
		return nil, fmt.Errorf("nexus/mongo: find tenant by slug: %w", err)
	}
	return tenantFromModel(&m)
}

func (s *tenantStore) Update(ctx context.Context, t *tenant.Tenant) error {
	m, err := tenantToModel(t)
	if err != nil {
		return err
	}
	res, err := s.mdb.NewUpdate(m).Filter(bson.M{"_id": m.ID}).Exec(ctx)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return fmt.Errorf("nexus/mongo: update tenant: %w", tenant.ErrDuplicate)
		}
		return fmt.Errorf("nexus/mongo: update tenant: %w", err)
	}
	if res.MatchedCount() == 0 {
		return tenant.ErrNotFound
	}
	return nil
}

func (s *tenantStore) Delete(ctx context.Context, tid string) error {
	_, err := s.mdb.NewDelete((*tenantModel)(nil)).Filter(bson.M{"_id": tid}).Exec(ctx)
	if err != nil {
		return fmt.Errorf("nexus/mongo: delete tenant: %w", err)
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
	filter := bson.M{}
	if opts.Status != "" {
		filter["status"] = opts.Status
	}
	if opts.Search != "" {
		re := bson.M{"$regex": regexp.QuoteMeta(opts.Search), "$options": "i"}
		filter["$or"] = bson.A{bson.M{"name": re}, bson.M{"slug": re}}
	}
	if opts.Cursor != "" {
		filter["_id"] = bson.M{"$lt": opts.Cursor}
	}
	var models []tenantModel
	err := s.mdb.NewFind(&models).Filter(filter).Sort(bson.D{{Key: "_id", Value: -1}}).Limit(int64(limit + 1)).Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("nexus/mongo: list tenants: %w", err)
	}
	rows := make([]*tenant.Tenant, 0, len(models))
	for i := range models {
		t, err := tenantFromModel(&models[i])
		if err != nil {
			return nil, fmt.Errorf("nexus/mongo: convert tenant model: %w", err)
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
	mdb *mongodriver.MongoDB
}

func (s *keyStore) Insert(ctx context.Context, k *key.APIKey) error {
	m := apiKeyToModel(k)
	_, err := s.mdb.NewInsert(m).Exec(ctx)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return fmt.Errorf("nexus/mongo: insert key: %w", key.ErrDuplicate)
		}
		return fmt.Errorf("nexus/mongo: insert key: %w", err)
	}
	return nil
}

func (s *keyStore) FindByID(ctx context.Context, kid string) (*key.APIKey, error) {
	var m apiKeyModel
	err := s.mdb.NewFind(&m).Filter(bson.M{"_id": kid}).Scan(ctx)
	if err != nil {
		if isNoDocuments(err) {
			return nil, key.ErrNotFound
		}
		return nil, fmt.Errorf("nexus/mongo: find key by id: %w", err)
	}
	return apiKeyFromModel(&m)
}

func (s *keyStore) FindByPrefix(ctx context.Context, prefix string) ([]*key.APIKey, error) {
	var models []apiKeyModel
	err := s.mdb.NewFind(&models).Filter(bson.M{"prefix": prefix}).Scan(ctx)
	if err != nil && !isNoDocuments(err) {
		return nil, fmt.Errorf("nexus/mongo: find keys by prefix: %w", err)
	}
	out := make([]*key.APIKey, 0, len(models))
	for i := range models {
		k, err := apiKeyFromModel(&models[i])
		if err != nil {
			return nil, fmt.Errorf("nexus/mongo: convert key model: %w", err)
		}
		out = append(out, k)
	}
	return out, nil
}

func (s *keyStore) TouchLastUsed(ctx context.Context, kid string, at time.Time) error {
	res, err := s.mdb.Collection(colKeys).UpdateOne(ctx, bson.M{"_id": kid}, bson.M{"$set": bson.M{"last_used_at": at}})
	if err != nil {
		return fmt.Errorf("nexus/mongo: touch key: %w", err)
	}
	if res.MatchedCount == 0 {
		return key.ErrNotFound
	}
	return nil
}

func (s *keyStore) Update(ctx context.Context, k *key.APIKey) error {
	m := apiKeyToModel(k)
	res, err := s.mdb.NewUpdate(m).Filter(bson.M{"_id": m.ID}).Exec(ctx)
	if err != nil {
		return fmt.Errorf("nexus/mongo: update key: %w", err)
	}
	if res.MatchedCount() == 0 {
		return key.ErrNotFound
	}
	return nil
}

func (s *keyStore) Delete(ctx context.Context, kid string) error {
	_, err := s.mdb.NewDelete((*apiKeyModel)(nil)).Filter(bson.M{"_id": kid}).Exec(ctx)
	if err != nil {
		return fmt.Errorf("nexus/mongo: delete key: %w", err)
	}
	return nil
}

func (s *keyStore) ListByTenant(ctx context.Context, tenantID string) ([]*key.APIKey, error) {
	var models []apiKeyModel
	err := s.mdb.NewFind(&models).
		Filter(bson.M{"tenant_id": tenantID}).
		Sort(bson.D{{Key: "created_at", Value: -1}}).
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("nexus/mongo: list keys by tenant: %w", err)
	}

	keys := make([]*key.APIKey, 0, len(models))
	for i := range models {
		k, err := apiKeyFromModel(&models[i])
		if err != nil {
			return nil, fmt.Errorf("nexus/mongo: convert key model: %w", err)
		}
		keys = append(keys, k)
	}
	return keys, nil
}

// keyFilter is the filter for a key list or count. The status is derived
// against now, mirroring key.Effective. A missing expires_at never expires.
func keyFilter(opts *key.ListOptions, now time.Time) bson.M {
	filter := bson.M{}
	if opts.TenantID != "" {
		filter["tenant_id"] = opts.TenantID
	}
	switch opts.Status {
	case "":
	case key.KeyActive:
		filter["status"] = string(key.KeyActive)
		filter["$or"] = bson.A{
			bson.M{"expires_at": nil}, // null or missing
			bson.M{"expires_at": bson.M{"$gt": now}},
		}
	case key.KeyExpired:
		filter["$or"] = bson.A{
			bson.M{"status": string(key.KeyExpired)},
			bson.M{"status": string(key.KeyActive), "expires_at": bson.M{"$lte": now}},
		}
	default:
		filter["status"] = string(opts.Status)
	}
	return filter
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
	filter := keyFilter(opts, now)
	if opts.Cursor != "" {
		filter["_id"] = bson.M{"$lt": opts.Cursor}
	}
	var models []apiKeyModel
	err := s.mdb.NewFind(&models).Filter(filter).Sort(bson.D{{Key: "_id", Value: -1}}).Limit(int64(limit + 1)).Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("nexus/mongo: list keys: %w", err)
	}
	rows := make([]*key.APIKey, 0, len(models))
	for i := range models {
		k, err := apiKeyFromModel(&models[i])
		if err != nil {
			return nil, fmt.Errorf("nexus/mongo: convert key model: %w", err)
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
	n, err := s.mdb.Collection(colKeys).CountDocuments(ctx, keyFilter(opts, opts.At()))
	if err != nil {
		return 0, fmt.Errorf("nexus/mongo: count keys: %w", err)
	}
	return int(n), nil
}

// ──────────────────────────────────────────────────
// Usage Store
// ──────────────────────────────────────────────────

type usageStore struct {
	mdb *mongodriver.MongoDB
}

func (s *usageStore) Insert(ctx context.Context, rec *usage.Record) error {
	m, err := usageToModel(rec)
	if err != nil {
		return err
	}
	if _, err := s.mdb.NewInsert(m).Exec(ctx); err != nil {
		return fmt.Errorf("nexus/mongo: insert usage: %w", err)
	}
	return nil
}

func tenantMatch(tenantID string, since time.Time) bson.M {
	m := bson.M{"created_at": bson.M{"$gte": since}}
	if tenantID != "" {
		m["tenant_id"] = tenantID
	}
	return m
}

func (s *usageStore) MonthlySpend(ctx context.Context, tenantID string) (money.USD, error) {
	since, err := usage.PeriodStart("month", time.Now())
	if err != nil {
		return money.Zero, err
	}
	cursor, err := s.mdb.Collection(colUsage).Aggregate(ctx, bson.A{
		bson.M{"$match": tenantMatch(tenantID, since)},
		bson.M{"$group": bson.M{"_id": nil, "total": bson.M{"$sum": "$cost_usd"}}},
	})
	if err != nil {
		return money.Zero, fmt.Errorf("nexus/mongo: monthly spend: %w", err)
	}
	defer func() { _ = cursor.Close(ctx) }()
	var result struct {
		Total any `bson:"total"`
	}
	if cursor.Next(ctx) {
		if err = cursor.Decode(&result); err != nil {
			return money.Zero, fmt.Errorf("nexus/mongo: monthly spend decode: %w", err)
		}
	}
	u, err := usdFromBSON(result.Total)
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
	m := tenantMatch(tenantID, since)
	m["outcome"] = bson.M{"$ne": string(usage.OutcomeRefused)}
	n, err := s.mdb.Collection(colUsage).CountDocuments(ctx, m)
	if err != nil {
		return 0, fmt.Errorf("nexus/mongo: daily requests: %w", err)
	}
	return int(n), nil
}

func (s *usageStore) Summary(ctx context.Context, tenantID, period string) (*usage.Summary, error) {
	since, err := usage.PeriodStart(period, time.Now())
	if err != nil {
		return nil, err
	}
	cursor, err := s.mdb.Collection(colUsage).Aggregate(ctx, bson.A{
		bson.M{"$match": tenantMatch(tenantID, since)},
		bson.M{"$group": bson.M{
			"_id": bson.M{
				"provider": "$provider", "model": "$model", "outcome": "$outcome",
				"pricing_status": "$pricing_status", "cached": "$cached",
			},
			"requests": bson.M{"$sum": 1},
			"tokens":   bson.M{"$sum": "$total_tokens"},
			"latency":  bson.M{"$sum": "$latency_ns"},
			"cost":     bson.M{"$sum": "$cost_usd"},
		}},
	})
	if err != nil {
		return nil, fmt.Errorf("nexus/mongo: summary: %w", err)
	}
	defer func() { _ = cursor.Close(ctx) }()
	var out []usage.SummaryRow
	for cursor.Next(ctx) {
		var g struct {
			ID struct {
				Provider      string `bson:"provider"`
				Model         string `bson:"model"`
				Outcome       string `bson:"outcome"`
				PricingStatus string `bson:"pricing_status"`
				Cached        bool   `bson:"cached"`
			} `bson:"_id"`
			Requests int   `bson:"requests"`
			Tokens   int   `bson:"tokens"`
			Latency  int64 `bson:"latency"`
			Cost     any   `bson:"cost"`
		}
		if err := cursor.Decode(&g); err != nil {
			return nil, fmt.Errorf("nexus/mongo: summary decode: %w", err)
		}
		r := usage.SummaryRow{
			Provider: g.ID.Provider, Model: g.ID.Model, Outcome: usage.Outcome(g.ID.Outcome),
			PricingStatus: usage.PricingStatus(g.ID.PricingStatus), Cached: g.ID.Cached,
			Requests: g.Requests, Tokens: g.Tokens, LatencyNs: g.Latency,
		}
		c, err := usdFromBSON(g.Cost)
		if err != nil {
			return nil, err
		}
		if c != nil {
			r.Cost = *c
		}
		out = append(out, r)
	}
	if err := cursor.Err(); err != nil {
		return nil, err
	}
	return usage.BuildSummary(tenantID, period, out), nil
}

func (s *usageStore) Series(ctx context.Context, opts *usage.SeriesOptions) ([]usage.SeriesPoint, error) {
	if _, err := usage.FillSeries(opts, nil); err != nil {
		return nil, err
	}
	match := bson.M{"created_at": bson.M{"$gte": usage.BucketStart(opts.Start, opts.Bucket), "$lt": opts.End.UTC()}}
	if opts.TenantID != "" {
		match["tenant_id"] = opts.TenantID
	}
	cursor, err := s.mdb.Collection(colUsage).Aggregate(ctx, bson.A{
		bson.M{"$match": match},
		bson.M{"$group": bson.M{
			"_id":      bson.M{"$dateTrunc": bson.M{"date": "$created_at", "unit": string(opts.Bucket), "timezone": "UTC"}},
			"requests": bson.M{"$sum": 1},
			"tokens":   bson.M{"$sum": "$total_tokens"},
			"cost":     bson.M{"$sum": "$cost_usd"},
			"unpriced": bson.M{"$sum": bson.M{"$cond": bson.A{bson.M{"$in": bson.A{"$pricing_status", bson.A{"unpriced_model", "unknown"}}}, 1, 0}}},
		}},
	})
	if err != nil {
		return nil, fmt.Errorf("nexus/mongo: series: %w", err)
	}
	defer func() { _ = cursor.Close(ctx) }()
	var points []usage.SeriesPoint
	for cursor.Next(ctx) {
		var g struct {
			Start    time.Time `bson:"_id"`
			Requests int       `bson:"requests"`
			Tokens   int       `bson:"tokens"`
			Cost     any       `bson:"cost"`
			Unpriced int       `bson:"unpriced"`
		}
		if err := cursor.Decode(&g); err != nil {
			return nil, fmt.Errorf("nexus/mongo: series decode: %w", err)
		}
		p := usage.SeriesPoint{Start: g.Start, Requests: g.Requests, Tokens: g.Tokens, Unpriced: g.Unpriced}
		c, err := usdFromBSON(g.Cost)
		if err != nil {
			return nil, err
		}
		if c != nil {
			p.CostUSD = *c
		}
		points = append(points, p)
	}
	if err := cursor.Err(); err != nil {
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
	filter := bson.M{}
	for field, v := range map[string]string{
		"tenant_id": opts.TenantID, "key_id": opts.KeyID, "provider": opts.Provider,
		"model": opts.Model, "outcome": string(opts.Outcome),
	} {
		if v != "" {
			filter[field] = v
		}
	}
	if !opts.StartTime.IsZero() || !opts.EndTime.IsZero() {
		window := bson.M{}
		if !opts.StartTime.IsZero() {
			window["$gte"] = opts.StartTime.UTC()
		}
		if !opts.EndTime.IsZero() {
			window["$lt"] = opts.EndTime.UTC()
		}
		filter["created_at"] = window
	}
	if opts.Cursor != "" {
		filter["_id"] = bson.M{"$lt": opts.Cursor}
	}
	var models []usageModel
	err := s.mdb.NewFind(&models).Filter(filter).Sort(bson.D{{Key: "_id", Value: -1}}).Limit(int64(limit + 1)).Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("nexus/mongo: query usage: %w", err)
	}
	rows := make([]*usage.Record, 0, len(models))
	for i := range models {
		rec, err := usageFromModel(&models[i])
		if err != nil {
			return nil, fmt.Errorf("nexus/mongo: convert usage model: %w", err)
		}
		rows = append(rows, rec)
	}
	page, next := paging.Trim(rows, limit, func(r *usage.Record) string { return r.ID.String() })
	return &usage.QueryResult{Items: page, NextCursor: next}, nil
}
