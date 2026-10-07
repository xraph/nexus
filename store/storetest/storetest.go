// Package storetest runs one conformance test body against every Nexus
// store backend: memory and SQLite always, PostgreSQL when
// NEXUS_TEST_POSTGRES_DSN is set and MongoDB when NEXUS_TEST_MONGO_URI is
// set. Each subtest gets a database nothing else touches.
package storetest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	mongodrv "go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/xraph/grove"
	"github.com/xraph/grove/drivers/mongodriver"
	"github.com/xraph/grove/drivers/pgdriver"
	"github.com/xraph/grove/drivers/sqlitedriver"

	"github.com/xraph/nexus/store"
	mongostore "github.com/xraph/nexus/store/mongo"
	pgstore "github.com/xraph/nexus/store/postgres"
	sqlitestore "github.com/xraph/nexus/store/sqlite"
)

const (
	envPostgres = "NEXUS_TEST_POSTGRES_DSN"
	envMongo    = "NEXUS_TEST_MONGO_URI"
)

// Each runs fn once per backend, as a subtest named after it, against a
// migrated store of that backend.
func Each(t *testing.T, fn func(t *testing.T, s store.Store)) {
	t.Helper()
	backends := []struct {
		name string
		open func(t *testing.T) store.Store
	}{
		{"memory", func(*testing.T) store.Store { return store.NewMemory() }},
		{"sqlite", func(t *testing.T) store.Store { return migrated(t, sqlitestore.New(OpenSQLiteDB(t))) }},
		{"postgres", func(t *testing.T) store.Store { return migrated(t, pgstore.New(OpenPostgresDB(t))) }},
		{"mongo", func(t *testing.T) store.Store { db, _ := OpenMongoDB(t); return migrated(t, mongostore.New(db)) }},
	}
	for _, b := range backends {
		t.Run(b.name, func(t *testing.T) { fn(t, b.open(t)) })
	}
}

func migrated(t *testing.T, s store.Store) store.Store {
	t.Helper()
	if err := s.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return s
}

// OpenSQLiteDB opens an empty SQLite database in the test's temp dir. It is
// not migrated, so a test can lay down a legacy schema first.
func OpenSQLiteDB(t *testing.T) *grove.DB {
	t.Helper()
	drv := sqlitedriver.New()
	if err := drv.Open(context.Background(), filepath.Join(t.TempDir(), "nexus.db")); err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	return groveOpen(t, drv)
}

// OpenPostgresDB opens the database NEXUS_TEST_POSTGRES_DSN names, confined
// to a schema of this test's own that is dropped when the test ends. It
// skips when the variable is unset and refuses the default port.
func OpenPostgresDB(t *testing.T) *grove.DB {
	t.Helper()
	dsn := os.Getenv(envPostgres)
	if dsn == "" {
		t.Skipf("%s is not set", envPostgres)
	}
	if err := checkPostgresDSN(dsn); err != nil {
		t.Fatalf("%s: %v", envPostgres, err)
	}
	schema := "nexus_test_" + randomHex(t)
	quoted := pgx.Identifier{schema}.Sanitize()
	pgExec(t, dsn, "CREATE SCHEMA "+quoted)
	t.Cleanup(func() { pgExec(t, dsn, "DROP SCHEMA IF EXISTS "+quoted+" CASCADE") })

	scoped, err := pinSearchPath(dsn, schema)
	if err != nil {
		t.Fatalf("pin search_path: %v", err)
	}
	drv := pgdriver.New()
	if err := drv.Open(context.Background(), scoped); err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	return groveOpen(t, drv)
}

// OpenMongoDB opens a database of this test's own on the server
// NEXUS_TEST_MONGO_URI names and drops it when the test ends. It returns the
// database name so a test can write raw documents. It skips when the
// variable is unset and refuses the default port.
func OpenMongoDB(t *testing.T) (db *grove.DB, name string) {
	t.Helper()
	uri := os.Getenv(envMongo)
	if uri == "" {
		t.Skipf("%s is not set", envMongo)
	}
	if err := checkMongoURI(uri); err != nil {
		t.Fatalf("%s: %v", envMongo, err)
	}
	u, err := url.Parse(uri)
	if err != nil {
		t.Fatalf("parse %s: %v", envMongo, err)
	}
	name = "nexus_test_" + randomHex(t)
	u.Path = "/" + name
	scoped := u.String()

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		client, err := mongodrv.Connect(options.Client().ApplyURI(scoped))
		if err != nil {
			t.Errorf("connect to drop %s: %v", name, err)
			return
		}
		defer func() {
			if err := client.Disconnect(ctx); err != nil {
				t.Errorf("disconnect after dropping %s: %v", name, err)
			}
		}()
		if err := client.Database(name).Drop(ctx); err != nil {
			t.Errorf("drop %s: %v", name, err)
		}
	})

	drv := mongodriver.New()
	if err := drv.Open(context.Background(), scoped); err != nil {
		t.Fatalf("open mongo: %v", err)
	}
	return groveOpen(t, drv), name
}

// MongoClient connects to the server NEXUS_TEST_MONGO_URI names, for tests
// that write raw documents. It is closed when the test ends.
func MongoClient(t *testing.T) *mongodrv.Client {
	t.Helper()
	uri := os.Getenv(envMongo)
	if uri == "" {
		t.Skipf("%s is not set", envMongo)
	}
	if err := checkMongoURI(uri); err != nil {
		t.Fatalf("%s: %v", envMongo, err)
	}
	client, err := mongodrv.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("connect mongo: %v", err)
	}
	t.Cleanup(func() {
		if err := client.Disconnect(context.Background()); err != nil {
			t.Errorf("disconnect mongo: %v", err)
		}
	})
	return client
}

const (
	defaultPostgresPort = 5432
	defaultMongoPort    = "27017"
)

// checkPostgresDSN refuses a DSN whose effective port, after pgx applies its
// own defaults, is PostgreSQL's default port (any host listed, for a
// multi-host DSN). That port is where a live database may be listening.
func checkPostgresDSN(dsn string) error {
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return fmt.Errorf("parse postgres DSN: %w", err)
	}
	ports := []uint16{cfg.Port}
	for _, fb := range cfg.Fallbacks {
		ports = append(ports, fb.Port)
	}
	for _, p := range ports {
		if p == defaultPostgresPort {
			return errors.New("points at the default port 5432 (spelled or implied); refusing to write to what may be a live database")
		}
	}
	return nil
}

// checkMongoURI refuses a URI that reaches MongoDB's default port, whether
// spelled or implied by leaving the port out, and any mongodb+srv URI (SRV
// records hide the port; the tests only run against the throwaway container).
func checkMongoURI(uri string) error {
	scheme, rest, ok := strings.Cut(uri, "://")
	if !ok {
		return fmt.Errorf("not a mongodb URI: %q", uri)
	}
	switch scheme {
	case "mongodb":
	case "mongodb+srv":
		return errors.New("mongodb+srv URIs are refused; point at the throwaway container with mongodb://host:port")
	default:
		return fmt.Errorf("unsupported scheme %q", scheme)
	}
	authority, _, _ := strings.Cut(rest, "/")
	authority, _, _ = strings.Cut(authority, "?")
	if i := strings.LastIndex(authority, "@"); i >= 0 {
		authority = authority[i+1:]
	}
	if authority == "" {
		return errors.New("no host in URI")
	}
	for _, host := range strings.Split(authority, ",") {
		port := defaultMongoPort
		if _, p, err := net.SplitHostPort(host); err == nil {
			port = p
		}
		if port == defaultMongoPort {
			return errors.New("points at the default port 27017 (spelled or implied); refusing to write to what may be a live database")
		}
	}
	return nil
}

func groveOpen(t *testing.T, drv grove.GroveDriver) *grove.DB {
	t.Helper()
	db, err := grove.Open(drv)
	if err != nil {
		t.Fatalf("grove open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func randomHex(t *testing.T) string {
	t.Helper()
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatalf("random suffix: %v", err)
	}
	return hex.EncodeToString(b[:])
}

func pgExec(t *testing.T, dsn, stmt string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect postgres: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()
	if _, err := conn.Exec(ctx, stmt); err != nil {
		t.Fatalf("%s: %v", stmt, err)
	}
}

// pinSearchPath sets search_path in either DSN spelling. The migrations
// create unqualified tables, so they land in the test's schema.
func pinSearchPath(dsn, schema string) (string, error) {
	if !strings.HasPrefix(dsn, "postgres://") && !strings.HasPrefix(dsn, "postgresql://") {
		return dsn + " search_path=" + schema, nil
	}
	u, err := url.Parse(dsn)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	return u.String(), nil
}
