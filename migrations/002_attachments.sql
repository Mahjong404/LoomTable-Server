CREATE TABLE IF NOT EXISTS attachments (
    id TEXT PRIMARY KEY,
    actor_id TEXT NOT NULL REFERENCES actors(id),
    source TEXT NOT NULL CHECK (source IN ('managed', 'vault')),
    status TEXT NOT NULL CHECK (status IN ('pending', 'ready')),
    filename TEXT NOT NULL,
    mime_type TEXT NOT NULL DEFAULT '',
    size_bytes BIGINT CHECK (size_bytes IS NULL OR size_bytes >= 0),
    storage_key TEXT,
    vault_path TEXT,
    sha256 TEXT NOT NULL DEFAULT '',
    width INTEGER CHECK (width IS NULL OR width > 0),
    height INTEGER CHECK (height IS NULL OR height > 0),
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMPTZ,
    CHECK (
        (source = 'managed' AND storage_key IS NOT NULL AND vault_path IS NULL)
        OR (source = 'vault' AND storage_key IS NULL AND vault_path IS NOT NULL)
    ),
    CHECK (source = 'vault' OR status IN ('pending', 'ready')),
    CHECK (source = 'vault' OR sha256 = '' OR sha256 ~ '^[0-9a-f]{64}$')
);

CREATE INDEX IF NOT EXISTS attachments_actor_live_idx
    ON attachments (actor_id, created_at, id)
    WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS attachments_cleanup_idx
    ON attachments (deleted_at, updated_at, id)
    WHERE deleted_at IS NOT NULL;

