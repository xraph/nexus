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
		colKeys: {
			{Keys: bson.D{{Key: "prefix", Value: 1}}},
			{Keys: bson.D{{Key: "tenant_id", Value: 1}}},
			{Keys: bson.D{{Key: "prefix", Value: 1}, {Key: "status", Value: 1}}},
		},
		colUsage: {
			{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "created_at", Value: -1}}},
			{Keys: bson.D{{Key: "provider", Value: 1}}},
			{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "provider", Value: 1}, {Key: "model", Value: 1}}},
		},
	}
}
