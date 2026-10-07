package postgres

import (
	"context"

	"github.com/xraph/grove/migrate"
)

// Migrations is the grove migration group for the Nexus postgres store.
// It contains all schema migrations in version order.
var Migrations = func() *migrate.Group {
	g := migrate.NewGroup("nexus")
	g.MustRegister(
		&migrate.Migration{
			Name:    "create_tenants",
			Version: "20240101000001",
			Comment: "Create nexus_tenants table",
			Up: func(ctx context.Context, exec migrate.Executor) error {
				_, err := exec.Exec(ctx, `
CREATE TABLE IF NOT EXISTS nexus_tenants (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    slug       TEXT UNIQUE NOT NULL,
    status     TEXT NOT NULL DEFAULT 'active',
    quota      JSONB NOT NULL DEFAULT '{}',
    config     JSONB NOT NULL DEFAULT '{}',
    metadata   JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_nexus_tenants_slug ON nexus_tenants(slug);
`)
				return err
			},
			Down: func(ctx context.Context, exec migrate.Executor) error {
				_, err := exec.Exec(ctx, `DROP TABLE IF EXISTS nexus_tenants CASCADE`)
				return err
			},
		},
		&migrate.Migration{
			Name:    "create_api_keys",
			Version: "20240101000002",
			Comment: "Create nexus_api_keys table",
			Up: func(ctx context.Context, exec migrate.Executor) error {
				_, err := exec.Exec(ctx, `
CREATE TABLE IF NOT EXISTS nexus_api_keys (
    id           TEXT PRIMARY KEY,
    tenant_id    TEXT NOT NULL REFERENCES nexus_tenants(id),
    name         TEXT NOT NULL,
    prefix       TEXT NOT NULL,
    hash         TEXT NOT NULL,
    scopes       JSONB NOT NULL DEFAULT '[]',
    status       TEXT NOT NULL DEFAULT 'active',
    expires_at   TIMESTAMPTZ,
    last_used_at TIMESTAMPTZ,
    metadata     JSONB NOT NULL DEFAULT '{}',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_nexus_api_keys_prefix ON nexus_api_keys(prefix);
CREATE INDEX IF NOT EXISTS idx_nexus_api_keys_tenant ON nexus_api_keys(tenant_id);
`)
				return err
			},
			Down: func(ctx context.Context, exec migrate.Executor) error {
				_, err := exec.Exec(ctx, `DROP TABLE IF EXISTS nexus_api_keys CASCADE`)
				return err
			},
		},
		&migrate.Migration{
			Name:    "create_usage_records",
			Version: "20240101000003",
			Comment: "Create nexus_usage_records table",
			Up: func(ctx context.Context, exec migrate.Executor) error {
				_, err := exec.Exec(ctx, `
CREATE TABLE IF NOT EXISTS nexus_usage_records (
    id                TEXT PRIMARY KEY,
    tenant_id         TEXT NOT NULL REFERENCES nexus_tenants(id),
    key_id            TEXT NOT NULL,
    request_id        TEXT NOT NULL,
    provider          TEXT NOT NULL,
    model             TEXT NOT NULL,
    prompt_tokens     INTEGER NOT NULL DEFAULT 0,
    completion_tokens INTEGER NOT NULL DEFAULT 0,
    total_tokens      INTEGER NOT NULL DEFAULT 0,
    cost_usd          DOUBLE PRECISION NOT NULL DEFAULT 0,
    latency_ns        BIGINT NOT NULL DEFAULT 0,
    cached            BOOLEAN NOT NULL DEFAULT FALSE,
    status_code       INTEGER NOT NULL DEFAULT 200,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_nexus_usage_tenant ON nexus_usage_records(tenant_id);
CREATE INDEX IF NOT EXISTS idx_nexus_usage_created ON nexus_usage_records(created_at);
`)
				return err
			},
			Down: func(ctx context.Context, exec migrate.Executor) error {
				_, err := exec.Exec(ctx, `DROP TABLE IF EXISTS nexus_usage_records CASCADE`)
				return err
			},
		},
		&migrate.Migration{
			Name:    "exact_money_and_outcomes",
			Version: "20261007000001",
			Comment: "Store usage cost as exact NUMERIC, add outcome fields, allow unattributed rows",
			Up: func(ctx context.Context, exec migrate.Executor) error {
				_, err := exec.Exec(ctx, `
ALTER TABLE nexus_usage_records
    ALTER COLUMN cost_usd DROP DEFAULT,
    ALTER COLUMN cost_usd DROP NOT NULL,
    ALTER COLUMN cost_usd TYPE NUMERIC(38,18) USING cost_usd::numeric,
    ALTER COLUMN tenant_id DROP NOT NULL,
    ALTER COLUMN key_id DROP NOT NULL,
    ALTER COLUMN request_id DROP NOT NULL,
    ADD COLUMN IF NOT EXISTS pricing_status TEXT NOT NULL DEFAULT 'priced',
    ADD COLUMN IF NOT EXISTS outcome TEXT NOT NULL DEFAULT 'ok',
    ADD COLUMN IF NOT EXISTS blocked_by TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS refusal_code TEXT NOT NULL DEFAULT '';

-- Nothing computed a cost before this migration, so a stored 0 meant
-- "unknown", not "free".
UPDATE nexus_usage_records
   SET cost_usd = NULL, pricing_status = 'unpriced_model'
 WHERE cost_usd = 0;

UPDATE nexus_usage_records
   SET outcome = CASE WHEN cached THEN 'cached'
                      WHEN status_code >= 400 THEN 'error'
                      ELSE 'ok' END;

UPDATE nexus_usage_records SET key_id = NULL WHERE key_id = '';
UPDATE nexus_usage_records SET request_id = NULL WHERE request_id = '';

CREATE INDEX IF NOT EXISTS idx_nexus_usage_key ON nexus_usage_records(key_id);
`)
				return err
			},
			Down: func(ctx context.Context, exec migrate.Executor) error {
				_, err := exec.Exec(ctx, `
DROP INDEX IF EXISTS idx_nexus_usage_key;
ALTER TABLE nexus_usage_records
    DROP COLUMN IF EXISTS refusal_code,
    DROP COLUMN IF EXISTS blocked_by,
    DROP COLUMN IF EXISTS outcome,
    DROP COLUMN IF EXISTS pricing_status,
    ALTER COLUMN cost_usd TYPE DOUBLE PRECISION USING COALESCE(cost_usd, 0)::double precision,
    ALTER COLUMN cost_usd SET DEFAULT 0,
    ALTER COLUMN cost_usd SET NOT NULL;
`)
				return err
			},
		},
	)
	return g
}()
