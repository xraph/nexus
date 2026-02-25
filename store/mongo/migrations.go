package mongo

import (
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

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
