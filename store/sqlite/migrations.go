package sqlite

import (
	"context"
	"errors"

	"github.com/xraph/grove/migrate"
)

// Migrations is the grove migration group for the Nexus SQLite store.
var Migrations = func() *migrate.Group {
	g := migrate.NewGroup("nexus")
	g.MustRegister(
		&migrate.Migration{
			Name:    "create_tenants",
			Version: "20240101000001",
			Comment: "Create tenants table",
			Up: func(ctx context.Context, exec migrate.Executor) error {
				_, err := exec.Exec(ctx, `
CREATE TABLE IF NOT EXISTS tenants (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL DEFAULT '',
    slug       TEXT NOT NULL DEFAULT '' UNIQUE,
    status     TEXT NOT NULL DEFAULT 'active',
    quota      TEXT NOT NULL DEFAULT '{}',
    config     TEXT NOT NULL DEFAULT '{}',
    metadata   TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_tenants_slug ON tenants(slug);
`)
				return err
			},
			Down: func(ctx context.Context, exec migrate.Executor) error {
				_, err := exec.Exec(ctx, `DROP TABLE IF EXISTS tenants`)
				return err
			},
		},
		&migrate.Migration{
			Name:    "create_api_keys",
			Version: "20240101000002",
			Comment: "Create api_keys table",
			Up: func(ctx context.Context, exec migrate.Executor) error {
				_, err := exec.Exec(ctx, `
CREATE TABLE IF NOT EXISTS api_keys (
    id           TEXT PRIMARY KEY,
    tenant_id    TEXT NOT NULL DEFAULT '',
    name         TEXT NOT NULL DEFAULT '',
    prefix       TEXT NOT NULL DEFAULT '',
    hash         TEXT NOT NULL DEFAULT '',
    scopes       TEXT NOT NULL DEFAULT '[]',
    status       TEXT NOT NULL DEFAULT 'active',
    expires_at   TEXT,
    last_used_at TEXT,
    metadata     TEXT NOT NULL DEFAULT '{}',
    created_at   TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_api_keys_prefix ON api_keys(prefix);
CREATE INDEX IF NOT EXISTS idx_api_keys_tenant ON api_keys(tenant_id);
`)
				return err
			},
			Down: func(ctx context.Context, exec migrate.Executor) error {
				_, err := exec.Exec(ctx, `DROP TABLE IF EXISTS api_keys`)
				return err
			},
		},
		&migrate.Migration{
			Name:    "create_usage_records",
			Version: "20240101000003",
			Comment: "Create usage_records table",
			Up: func(ctx context.Context, exec migrate.Executor) error {
				_, err := exec.Exec(ctx, `
CREATE TABLE IF NOT EXISTS usage_records (
    id                TEXT PRIMARY KEY,
    tenant_id         TEXT NOT NULL DEFAULT '',
    key_id            TEXT NOT NULL DEFAULT '',
    request_id        TEXT NOT NULL DEFAULT '',
    provider          TEXT NOT NULL DEFAULT '',
    model             TEXT NOT NULL DEFAULT '',
    prompt_tokens     INTEGER NOT NULL DEFAULT 0,
    completion_tokens INTEGER NOT NULL DEFAULT 0,
    total_tokens      INTEGER NOT NULL DEFAULT 0,
    cost_usd          REAL NOT NULL DEFAULT 0,
    latency_ns        INTEGER NOT NULL DEFAULT 0,
    cached            INTEGER NOT NULL DEFAULT 0,
    status_code       INTEGER NOT NULL DEFAULT 200,
    created_at        TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_usage_tenant ON usage_records(tenant_id);
CREATE INDEX IF NOT EXISTS idx_usage_created ON usage_records(created_at);
`)
				return err
			},
			Down: func(ctx context.Context, exec migrate.Executor) error {
				_, err := exec.Exec(ctx, `DROP TABLE IF EXISTS usage_records`)
				return err
			},
		},
		&migrate.Migration{
			Name:    "exact_money_and_outcomes",
			Version: "20261007000001",
			Comment: "Rebuild usage_records with TEXT cost, outcome fields, nullable attribution and sortable times",
			Up: func(ctx context.Context, exec migrate.Executor) error {
				// One Exec runs on one connection, so BEGIN and COMMIT bracket
				// the whole rebuild. A leftover usage_records_next from an
				// earlier failed run is dropped first. If a statement fails the
				// transaction is still open on that connection, so roll it back.
				_, err := exec.Exec(ctx, `
DROP TABLE IF EXISTS usage_records_next;

BEGIN;

CREATE TABLE usage_records_next (
    id                TEXT PRIMARY KEY,
    tenant_id         TEXT,
    key_id            TEXT,
    request_id        TEXT,
    provider          TEXT NOT NULL DEFAULT '',
    model             TEXT NOT NULL DEFAULT '',
    prompt_tokens     INTEGER NOT NULL DEFAULT 0,
    completion_tokens INTEGER NOT NULL DEFAULT 0,
    total_tokens      INTEGER NOT NULL DEFAULT 0,
    cost_usd          TEXT,
    pricing_status    TEXT NOT NULL DEFAULT 'unpriced_model',
    outcome           TEXT NOT NULL DEFAULT 'ok',
    blocked_by        TEXT NOT NULL DEFAULT '',
    refusal_code      TEXT NOT NULL DEFAULT '',
    latency_ns        INTEGER NOT NULL DEFAULT 0,
    cached            INTEGER NOT NULL DEFAULT 0,
    status_code       INTEGER NOT NULL DEFAULT 200,
    created_at        TEXT NOT NULL
);

-- Nothing computed a cost before this migration, so a stored 0 meant
-- "unknown", not "free". A cache hit is the exception: it called no provider,
-- so it cost exactly 0. created_at is copied as is; Store.Migrate rewrites
-- it to fixed-width UTC text in Go, because SQLite cannot parse the formats
-- the old store wrote. The pricing_status default is 'unpriced_model' so a
-- writer that omits the column (an old binary) never makes a priced $0 row.
INSERT INTO usage_records_next
    (id, tenant_id, key_id, request_id, provider, model, prompt_tokens,
     completion_tokens, total_tokens, cost_usd, pricing_status, outcome,
     latency_ns, cached, status_code, created_at)
SELECT id, NULLIF(tenant_id, ''), NULLIF(key_id, ''), NULLIF(request_id, ''),
       provider, model, prompt_tokens, completion_tokens, total_tokens,
       CASE WHEN cached = 1 THEN '0'
            WHEN cost_usd = 0 THEN NULL
            ELSE printf('%.18f', cost_usd) END,
       CASE WHEN cached = 1 THEN 'cached'
            WHEN cost_usd = 0 THEN 'unpriced_model'
            ELSE 'priced' END,
       CASE WHEN cached = 1 THEN 'cached' WHEN status_code >= 400 THEN 'error' ELSE 'ok' END,
       latency_ns, cached, status_code,
       created_at
  FROM usage_records;

DROP TABLE usage_records;
ALTER TABLE usage_records_next RENAME TO usage_records;

CREATE INDEX IF NOT EXISTS idx_usage_tenant ON usage_records(tenant_id);
CREATE INDEX IF NOT EXISTS idx_usage_created ON usage_records(created_at);
CREATE INDEX IF NOT EXISTS idx_usage_key ON usage_records(key_id);

COMMIT;
`)
				if err != nil {
					if _, rbErr := exec.Exec(ctx, `ROLLBACK`); rbErr != nil {
						return errors.Join(err, rbErr)
					}
				}
				return err
			},
			Down: func(ctx context.Context, exec migrate.Executor) error {
				_, err := exec.Exec(ctx, `
UPDATE usage_records SET cost_usd = '0' WHERE cost_usd IS NULL;
ALTER TABLE usage_records DROP COLUMN refusal_code;
ALTER TABLE usage_records DROP COLUMN blocked_by;
ALTER TABLE usage_records DROP COLUMN outcome;
ALTER TABLE usage_records DROP COLUMN pricing_status;
`)
				return err
			},
		},
	)
	return g
}()
