package mongo_test

import (
	"context"
	"slices"
	"testing"

	"github.com/xraph/grove/drivers/mongodriver"
	"github.com/xraph/grove/migrate"

	mongostore "github.com/xraph/nexus/store/mongo"
	"github.com/xraph/nexus/store/storetest"
)

const (
	colKeys  = "nexus_api_keys"
	colUsage = "nexus_usage_records"
)

// The indexes the key list and the operator-wide usage queries need.
var wantIndexes = map[string][]string{
	colKeys:  {"tenant_id_1_status_1", "status_1_expires_at_1"},
	colUsage: {"created_at_-1", "key_id_1"},
}

func indexNames(t *testing.T, dbName, col string) []string {
	t.Helper()
	ctx := context.Background()
	cur, err := storetest.MongoClient(t).Database(dbName).Collection(col).Indexes().List(ctx)
	if err != nil {
		t.Fatalf("list indexes on %s: %v", col, err)
	}
	var specs []struct {
		Name string `bson:"name"`
	}
	if err := cur.All(ctx, &specs); err != nil {
		t.Fatalf("read indexes on %s: %v", col, err)
	}
	names := make([]string, 0, len(specs))
	for _, s := range specs {
		names = append(names, s.Name)
	}
	return names
}

func assertIndexes(t *testing.T, dbName string, present bool) {
	t.Helper()
	for col, names := range wantIndexes {
		got := indexNames(t, dbName, col)
		for _, name := range names {
			if slices.Contains(got, name) != present {
				t.Fatalf("%s indexes = %v; %s present should be %v", col, got, name, present)
			}
		}
	}
}

// Store.Migrate keeps every index, so a database that already ran the
// released migrations gets the new ones.
func TestStoreMigrateCreatesTheKeyAndUsageIndexes(t *testing.T) {
	db, name := storetest.OpenMongoDB(t)
	if err := mongostore.New(db).Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	assertIndexes(t, name, true)
}

// The released create migrations are not re-run on an existing database, so
// the new indexes come from their own migration. Run the group through grove's
// orchestrator, as a user does, then take that migration's Down.
func TestIndexMigrationCreatesAndDropsTheIndexes(t *testing.T) {
	ctx := context.Background()
	db, name := storetest.OpenMongoDB(t)
	exec, err := migrate.NewExecutorFor(mongodriver.Unwrap(db))
	if err != nil {
		t.Fatalf("executor: %v", err)
	}
	if _, err := migrate.NewOrchestrator(exec, mongostore.Migrations).Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	assertIndexes(t, name, true)

	var m *migrate.Migration
	for _, mig := range mongostore.Migrations.Migrations() {
		if mig.Version == "20261008000001" {
			m = mig
		}
	}
	if m == nil {
		t.Fatal("no 20261008000001 migration")
	}
	if err := m.Down(ctx, exec); err != nil {
		t.Fatalf("down: %v", err)
	}
	assertIndexes(t, name, false)
}
