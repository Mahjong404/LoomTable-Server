CREATE TABLE IF NOT EXISTS schema_migrations (
    version TEXT PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS actors (
    id TEXT PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS auth_tokens (
    id TEXT PRIMARY KEY,
    actor_id TEXT NOT NULL REFERENCES actors(id),
    name TEXT NOT NULL,
    name_key TEXT NOT NULL,
    token_hash TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    revoked_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS server_secrets (
    name TEXT PRIMARY KEY,
    secret BYTEA NOT NULL CHECK (octet_length(secret) = 32),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS workspaces (
    id TEXT PRIMARY KEY,
    actor_id TEXT NOT NULL REFERENCES actors(id),
    name TEXT NOT NULL,
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS bases (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces(id),
    name TEXT NOT NULL,
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS tables (
    id TEXT PRIMARY KEY,
    base_id TEXT NOT NULL REFERENCES bases(id),
    name TEXT NOT NULL,
    primary_field_id TEXT NOT NULL,
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS fields (
    id TEXT PRIMARY KEY,
    table_id TEXT NOT NULL REFERENCES tables(id),
    name TEXT NOT NULL,
    type TEXT NOT NULL,
    position_index INTEGER NOT NULL CHECK (position_index >= 0),
    schema_version INTEGER NOT NULL DEFAULT 1 CHECK (schema_version > 0),
    config JSONB NOT NULL DEFAULT '{}'::JSONB,
    is_primary BOOLEAN NOT NULL DEFAULT FALSE,
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS views (
    id TEXT PRIMARY KEY,
    table_id TEXT NOT NULL REFERENCES tables(id),
    name TEXT NOT NULL,
    type TEXT NOT NULL,
    config JSONB NOT NULL DEFAULT '{}'::JSONB,
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS records (
    id TEXT PRIMARY KEY,
    table_id TEXT NOT NULL REFERENCES tables(id),
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    "values" JSONB NOT NULL DEFAULT '{}'::JSONB,
    query_values JSONB NOT NULL DEFAULT '{}'::JSONB,
    search_text TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS changes (
    change_sequence BIGSERIAL PRIMARY KEY,
    id TEXT NOT NULL UNIQUE,
    kind TEXT NOT NULL,
    table_id TEXT NOT NULL REFERENCES tables(id),
    record_id TEXT,
    object_id TEXT,
    revision BIGINT NOT NULL,
    actor_id TEXT NOT NULL REFERENCES actors(id),
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS idempotency_keys (
    actor_id TEXT NOT NULL REFERENCES actors(id),
    client_mutation_id TEXT NOT NULL,
    request_hash BYTEA NOT NULL,
    response JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (actor_id, client_mutation_id)
);

CREATE TABLE IF NOT EXISTS change_retention_watermarks (
    table_id TEXT PRIMARY KEY REFERENCES tables(id),
    expired_through BIGINT NOT NULL CHECK (expired_through >= 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

ALTER TABLE tables
    ADD CONSTRAINT tables_primary_field_fk
    FOREIGN KEY (primary_field_id) REFERENCES fields(id)
    DEFERRABLE INITIALLY DEFERRED;

CREATE UNIQUE INDEX IF NOT EXISTS auth_tokens_actor_active_name_idx ON auth_tokens (actor_id, name_key) WHERE revoked_at IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS fields_table_primary_idx ON fields (table_id) WHERE is_primary;
CREATE INDEX IF NOT EXISTS workspaces_actor_idx ON workspaces (actor_id, created_at, id);
CREATE INDEX IF NOT EXISTS bases_workspace_live_idx ON bases (workspace_id, created_at, id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS tables_base_live_idx ON tables (base_id, created_at, id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS fields_table_live_idx ON fields (table_id, position_index, id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS views_table_live_idx ON views (table_id, created_at, id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS records_table_cursor_idx ON records (table_id, created_at, id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS changes_table_sequence_idx ON changes (table_id, change_sequence);
CREATE INDEX IF NOT EXISTS changes_retention_idx ON changes (occurred_at, change_sequence);
CREATE INDEX IF NOT EXISTS idempotency_keys_retention_idx ON idempotency_keys (created_at, actor_id, client_mutation_id);
