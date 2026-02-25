// Package mongo provides a MongoDB-backed store implementation for Nexus
// using grove ORM with the mongodriver.
package mongo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/xraph/grove"
	"github.com/xraph/grove/drivers/mongodriver"

	"github.com/xraph/nexus/key"
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
	m := tenantToModel(t)
	_, err := s.mdb.NewInsert(m).Exec(ctx)
	if err != nil {
		return fmt.Errorf("nexus/mongo: insert tenant: %w", err)
	}
	return nil
}

func (s *tenantStore) FindByID(ctx context.Context, tid string) (*tenant.Tenant, error) {
	var m tenantModel
	err := s.mdb.NewFind(&m).Filter(bson.M{"_id": tid}).Scan(ctx)
	if err != nil {
		if isNoDocuments(err) {
			return nil, nil
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
			return nil, nil
		}
		return nil, fmt.Errorf("nexus/mongo: find tenant by slug: %w", err)
	}
	return tenantFromModel(&m)
}

func (s *tenantStore) Update(ctx context.Context, t *tenant.Tenant) error {
	m := tenantToModel(t)
	res, err := s.mdb.NewUpdate(m).Filter(bson.M{"_id": m.ID}).Exec(ctx)
	if err != nil {
		return fmt.Errorf("nexus/mongo: update tenant: %w", err)
	}
	if res.MatchedCount() == 0 {
		return fmt.Errorf("nexus/mongo: tenant not found")
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

func (s *tenantStore) List(ctx context.Context, opts *tenant.ListOptions) ([]*tenant.Tenant, int, error) {
	var models []tenantModel
	filter := bson.M{}
	if opts != nil && opts.Status != "" {
		filter["status"] = opts.Status
	}

	q := s.mdb.NewFind(&models).
		Filter(filter).
		Sort(bson.D{{Key: "created_at", Value: -1}})

	if opts != nil {
		if opts.Limit > 0 {
			q = q.Limit(int64(opts.Limit))
		}
		if opts.Offset > 0 {
			q = q.Skip(int64(opts.Offset))
		}
	}

	if err := q.Scan(ctx); err != nil {
		return nil, 0, fmt.Errorf("nexus/mongo: list tenants: %w", err)
	}

	tenants := make([]*tenant.Tenant, 0, len(models))
	for i := range models {
		t, err := tenantFromModel(&models[i])
		if err != nil {
			return nil, 0, fmt.Errorf("nexus/mongo: convert tenant model: %w", err)
		}
		tenants = append(tenants, t)
	}
	return tenants, len(tenants), nil
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
		return fmt.Errorf("nexus/mongo: insert key: %w", err)
	}
	return nil
}

func (s *keyStore) FindByID(ctx context.Context, kid string) (*key.APIKey, error) {
	var m apiKeyModel
	err := s.mdb.NewFind(&m).Filter(bson.M{"_id": kid}).Scan(ctx)
	if err != nil {
		if isNoDocuments(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("nexus/mongo: find key by id: %w", err)
	}
	return apiKeyFromModel(&m)
}

func (s *keyStore) FindByPrefix(ctx context.Context, prefix string) (*key.APIKey, error) {
	var m apiKeyModel
	err := s.mdb.NewFind(&m).
		Filter(bson.M{"prefix": prefix, "status": "active"}).
		Scan(ctx)
	if err != nil {
		if isNoDocuments(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("nexus/mongo: find key by prefix: %w", err)
	}
	return apiKeyFromModel(&m)
}

func (s *keyStore) Update(ctx context.Context, k *key.APIKey) error {
	m := apiKeyToModel(k)
	res, err := s.mdb.NewUpdate(m).Filter(bson.M{"_id": m.ID}).Exec(ctx)
	if err != nil {
		return fmt.Errorf("nexus/mongo: update key: %w", err)
	}
	if res.MatchedCount() == 0 {
		return fmt.Errorf("nexus/mongo: key not found")
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

// ──────────────────────────────────────────────────
// Usage Store
// ──────────────────────────────────────────────────

type usageStore struct {
	mdb *mongodriver.MongoDB
}

func (s *usageStore) Insert(ctx context.Context, rec *usage.Record) error {
	m := usageToModel(rec)
	_, err := s.mdb.NewInsert(m).Exec(ctx)
	if err != nil {
		return fmt.Errorf("nexus/mongo: insert usage: %w", err)
	}
	return nil
}

func (s *usageStore) MonthlySpend(ctx context.Context, tenantID string) (float64, error) {
	now := time.Now().UTC()
	startOfMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	pipeline := bson.A{
		bson.M{"$match": bson.M{
			"tenant_id":  tenantID,
			"created_at": bson.M{"$gte": startOfMonth},
		}},
		bson.M{"$group": bson.M{
			"_id":   nil,
			"total": bson.M{"$sum": "$cost_usd"},
		}},
	}

	cursor, err := s.mdb.Collection(colUsage).Aggregate(ctx, pipeline)
	if err != nil {
		return 0, fmt.Errorf("nexus/mongo: monthly spend: %w", err)
	}
	defer func() { _ = cursor.Close(ctx) }()

	var result struct {
		Total float64 `bson:"total"`
	}
	if cursor.Next(ctx) {
		if err := cursor.Decode(&result); err != nil {
			return 0, fmt.Errorf("nexus/mongo: monthly spend decode: %w", err)
		}
	}
	return result.Total, nil
}

func (s *usageStore) DailyRequests(ctx context.Context, tenantID string) (int, error) {
	now := time.Now().UTC()
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	pipeline := bson.A{
		bson.M{"$match": bson.M{
			"tenant_id":  tenantID,
			"created_at": bson.M{"$gte": startOfDay},
		}},
		bson.M{"$count": "count"},
	}

	cursor, err := s.mdb.Collection(colUsage).Aggregate(ctx, pipeline)
	if err != nil {
		return 0, fmt.Errorf("nexus/mongo: daily requests: %w", err)
	}
	defer func() { _ = cursor.Close(ctx) }()

	var result struct {
		Count int `bson:"count"`
	}
	if cursor.Next(ctx) {
		if err := cursor.Decode(&result); err != nil {
			return 0, fmt.Errorf("nexus/mongo: daily requests decode: %w", err)
		}
	}
	return result.Count, nil
}

func (s *usageStore) Summary(ctx context.Context, tenantID, period string) (*usage.Summary, error) {
	now := time.Now().UTC()
	var startTime time.Time
	switch period {
	case "day":
		startTime = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	case "week":
		startTime = now.AddDate(0, 0, -7)
	default:
		startTime = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	}

	pipeline := bson.A{
		bson.M{"$match": bson.M{
			"tenant_id":  tenantID,
			"created_at": bson.M{"$gte": startTime},
		}},
		bson.M{"$group": bson.M{
			"_id":      bson.M{"provider": "$provider", "model": "$model"},
			"requests": bson.M{"$sum": 1},
			"tokens":   bson.M{"$sum": "$total_tokens"},
			"cost":     bson.M{"$sum": "$cost_usd"},
		}},
	}

	cursor, err := s.mdb.Collection(colUsage).Aggregate(ctx, pipeline)
	if err != nil {
		return nil, fmt.Errorf("nexus/mongo: summary query: %w", err)
	}
	defer func() { _ = cursor.Close(ctx) }()

	summary := &usage.Summary{
		TenantID:   tenantID,
		Period:     period,
		ByProvider: make(map[string]*usage.ProviderUsage),
		ByModel:    make(map[string]*usage.ModelUsage),
	}

	for cursor.Next(ctx) {
		var row struct {
			ID struct {
				Provider string `bson:"provider"`
				Model    string `bson:"model"`
			} `bson:"_id"`
			Requests int     `bson:"requests"`
			Tokens   int     `bson:"tokens"`
			Cost     float64 `bson:"cost"`
		}
		if err := cursor.Decode(&row); err != nil {
			return nil, fmt.Errorf("nexus/mongo: summary decode: %w", err)
		}

		prov := row.ID.Provider
		mdl := row.ID.Model

		summary.TotalRequests += row.Requests
		summary.TotalTokens += row.Tokens
		summary.TotalCostUSD += row.Cost

		if _, ok := summary.ByProvider[prov]; !ok {
			summary.ByProvider[prov] = &usage.ProviderUsage{}
		}
		summary.ByProvider[prov].Requests += row.Requests
		summary.ByProvider[prov].Tokens += row.Tokens
		summary.ByProvider[prov].CostUSD += row.Cost

		if _, ok := summary.ByModel[mdl]; !ok {
			summary.ByModel[mdl] = &usage.ModelUsage{}
		}
		summary.ByModel[mdl].Requests += row.Requests
		summary.ByModel[mdl].Tokens += row.Tokens
		summary.ByModel[mdl].CostUSD += row.Cost
	}

	return summary, cursor.Err()
}

func (s *usageStore) Query(ctx context.Context, opts *usage.QueryOptions) ([]*usage.Record, int, error) {
	var models []usageModel
	filter := bson.M{}

	if opts != nil {
		if opts.TenantID != "" {
			filter["tenant_id"] = opts.TenantID
		}
		if opts.Provider != "" {
			filter["provider"] = opts.Provider
		}
		if opts.Model != "" {
			filter["model"] = opts.Model
		}
		if !opts.StartTime.IsZero() || !opts.EndTime.IsZero() {
			ts := bson.M{}
			if !opts.StartTime.IsZero() {
				ts["$gte"] = opts.StartTime
			}
			if !opts.EndTime.IsZero() {
				ts["$lte"] = opts.EndTime
			}
			filter["created_at"] = ts
		}
	}

	q := s.mdb.NewFind(&models).
		Filter(filter).
		Sort(bson.D{{Key: "created_at", Value: -1}})

	if opts != nil {
		if opts.Limit > 0 {
			q = q.Limit(int64(opts.Limit))
		}
		if opts.Offset > 0 {
			q = q.Skip(int64(opts.Offset))
		}
	}

	if err := q.Scan(ctx); err != nil {
		return nil, 0, fmt.Errorf("nexus/mongo: query usage: %w", err)
	}

	records := make([]*usage.Record, 0, len(models))
	for i := range models {
		rec, err := usageFromModel(&models[i])
		if err != nil {
			return nil, 0, fmt.Errorf("nexus/mongo: convert usage model: %w", err)
		}
		records = append(records, rec)
	}
	return records, len(records), nil
}
