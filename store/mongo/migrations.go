package mongo

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/xraph/grove/drivers/mongodriver/mongomigrate"
	"github.com/xraph/grove/migrate"
)

// Migrations is the grove migration group for the Nexus mongo store.
var Migrations = migrate.NewGroup("nexus")

func init() {
	Migrations.MustRegister(
		&migrate.Migration{
			Name:    "create_nexus_tenants",
			Version: "20240101000001",
			Up: func(ctx context.Context, exec migrate.Executor) error {
				mexec, ok := exec.(*mongomigrate.Executor)
				if !ok {
					return fmt.Errorf("expected mongomigrate executor, got %T", exec)
				}

				if err := mexec.CreateCollection(ctx, (*tenantModel)(nil)); err != nil {
					return err
				}

				return mexec.CreateIndexes(ctx, colTenants, []mongo.IndexModel{
					{
						Keys:    bson.D{{Key: "slug", Value: 1}},
						Options: options.Index().SetUnique(true),
					},
					{Keys: bson.D{{Key: "status", Value: 1}}},
				})
			},
			Down: func(ctx context.Context, exec migrate.Executor) error {
				mexec, ok := exec.(*mongomigrate.Executor)
				if !ok {
					return fmt.Errorf("expected mongomigrate executor, got %T", exec)
				}
				return mexec.DropCollection(ctx, (*tenantModel)(nil))
			},
		},
		&migrate.Migration{
			Name:    "create_nexus_api_keys",
			Version: "20240101000002",
			Up: func(ctx context.Context, exec migrate.Executor) error {
				mexec, ok := exec.(*mongomigrate.Executor)
				if !ok {
					return fmt.Errorf("expected mongomigrate executor, got %T", exec)
				}

				if err := mexec.CreateCollection(ctx, (*apiKeyModel)(nil)); err != nil {
					return err
				}

				return mexec.CreateIndexes(ctx, colKeys, []mongo.IndexModel{
					{Keys: bson.D{{Key: "prefix", Value: 1}}},
					{Keys: bson.D{{Key: "tenant_id", Value: 1}}},
					{Keys: bson.D{{Key: "prefix", Value: 1}, {Key: "status", Value: 1}}},
				})
			},
			Down: func(ctx context.Context, exec migrate.Executor) error {
				mexec, ok := exec.(*mongomigrate.Executor)
				if !ok {
					return fmt.Errorf("expected mongomigrate executor, got %T", exec)
				}
				return mexec.DropCollection(ctx, (*apiKeyModel)(nil))
			},
		},
		&migrate.Migration{
			Name:    "create_nexus_usage_records",
			Version: "20240101000003",
			Up: func(ctx context.Context, exec migrate.Executor) error {
				mexec, ok := exec.(*mongomigrate.Executor)
				if !ok {
					return fmt.Errorf("expected mongomigrate executor, got %T", exec)
				}

				if err := mexec.CreateCollection(ctx, (*usageModel)(nil)); err != nil {
					return err
				}

				return mexec.CreateIndexes(ctx, colUsage, []mongo.IndexModel{
					{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "created_at", Value: -1}}},
					{Keys: bson.D{{Key: "provider", Value: 1}}},
					{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "provider", Value: 1}, {Key: "model", Value: 1}}},
				})
			},
			Down: func(ctx context.Context, exec migrate.Executor) error {
				mexec, ok := exec.(*mongomigrate.Executor)
				if !ok {
					return fmt.Errorf("expected mongomigrate executor, got %T", exec)
				}
				return mexec.DropCollection(ctx, (*usageModel)(nil))
			},
		},
		&migrate.Migration{
			Name:    "index_keys_by_status_and_usage_by_time_and_key",
			Version: "20261008000001",
			Comment: "Index keys by tenant and status and by status and expiry; index usage by time and by key",
			Up: func(ctx context.Context, exec migrate.Executor) error {
				mexec, ok := exec.(*mongomigrate.Executor)
				if !ok {
					return fmt.Errorf("expected mongomigrate executor, got %T", exec)
				}
				// The key list and count filter on tenant_id and status, and
				// derive expiry from expires_at.
				if err := mexec.CreateIndexes(ctx, colKeys, keyStatusIndexes()); err != nil {
					return err
				}
				// Operator-wide usage queries (no tenant) sort by time and
				// filter by key.
				return mexec.CreateIndexes(ctx, colUsage, usageOperatorIndexes())
			},
			Down: func(ctx context.Context, exec migrate.Executor) error {
				mexec, ok := exec.(*mongomigrate.Executor)
				if !ok {
					return fmt.Errorf("expected mongomigrate executor, got %T", exec)
				}
				for col, names := range map[string][]string{
					colKeys:  {"tenant_id_1_status_1", "status_1_expires_at_1"},
					colUsage: {"created_at_-1", "key_id_1"},
				} {
					for _, name := range names {
						if err := mexec.DB().Collection(col).Indexes().DropOne(ctx, name); err != nil {
							return fmt.Errorf("drop index %s on %s: %w", name, col, err)
						}
					}
				}
				return nil
			},
		},
	)
}

// migrationIndexes returns the index definitions for all nexus collections.
func migrationIndexes() map[string][]mongo.IndexModel {
	return map[string][]mongo.IndexModel{
		colTenants: {
			{
				Keys:    bson.D{{Key: "slug", Value: 1}},
				Options: options.Index().SetUnique(true),
			},
			{Keys: bson.D{{Key: "status", Value: 1}}},
		},
		colKeys: append([]mongo.IndexModel{
			{Keys: bson.D{{Key: "prefix", Value: 1}}},
			{Keys: bson.D{{Key: "tenant_id", Value: 1}}},
			{Keys: bson.D{{Key: "prefix", Value: 1}, {Key: "status", Value: 1}}},
		}, keyStatusIndexes()...),
		colUsage: append([]mongo.IndexModel{
			{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "created_at", Value: -1}}},
			{Keys: bson.D{{Key: "provider", Value: 1}}},
			{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "provider", Value: 1}, {Key: "model", Value: 1}}},
		}, usageOperatorIndexes()...),
	}
}

// keyStatusIndexes back the key list and count: they filter on tenant_id and
// status, and derive expiry from expires_at. Added by the
// 20261008000001 migration, and kept in migrationIndexes() for Store.Migrate.
func keyStatusIndexes() []mongo.IndexModel {
	return []mongo.IndexModel{
		{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "status", Value: 1}}},
		{Keys: bson.D{{Key: "status", Value: 1}, {Key: "expires_at", Value: 1}}},
	}
}

// usageOperatorIndexes back operator-wide usage queries (no tenant), which
// sort by time and filter by key. Added by the 20261008000001 migration, and
// kept in migrationIndexes() for Store.Migrate.
func usageOperatorIndexes() []mongo.IndexModel {
	return []mongo.IndexModel{
		{Keys: bson.D{{Key: "created_at", Value: -1}}},
		{Keys: bson.D{{Key: "key_id", Value: 1}}},
	}
}
