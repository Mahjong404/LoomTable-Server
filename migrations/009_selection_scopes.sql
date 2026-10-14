-- Ephemeral, immutable membership snapshots. No business data backfill.
CREATE TABLE selection_scopes (
    id TEXT PRIMARY KEY,
    actor_id TEXT NOT NULL REFERENCES actors(id),
    table_id TEXT NOT NULL REFERENCES tables(id),
    request_id TEXT NOT NULL,
    request_hash BYTEA NOT NULL,
    query JSONB NOT NULL,
    membership_fingerprint TEXT NOT NULL,
    schema_hash TEXT NOT NULL,
    semantic_data_version TEXT NOT NULL,
    member_count BIGINT NOT NULL CHECK (member_count BETWEEN 0 AND 100000),
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    invalidated BOOLEAN NOT NULL DEFAULT FALSE,
    UNIQUE (actor_id, request_id),
    CHECK (expires_at > created_at)
);
CREATE INDEX selection_scopes_expiry ON selection_scopes(expires_at);
CREATE INDEX selection_scopes_table ON selection_scopes(table_id, expires_at) WHERE NOT invalidated;
CREATE TABLE selection_scope_members (
    scope_id TEXT NOT NULL REFERENCES selection_scopes(id) ON DELETE CASCADE,
    record_id TEXT NOT NULL,
    PRIMARY KEY (scope_id, record_id)
);

-- Ancestor revocation is permanent for existing scopes, including after restore.
CREATE FUNCTION revoke_ancestor_selection_scopes() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.deleted_at IS NULL AND NEW.deleted_at IS NOT NULL THEN
        UPDATE selection_scopes s SET invalidated = TRUE
        FROM tables t JOIN bases b ON b.id=t.base_id
        WHERE s.table_id=t.id AND NOT s.invalidated AND
          ((TG_TABLE_NAME='tables' AND t.id=NEW.id) OR
           (TG_TABLE_NAME='bases' AND b.id=NEW.id) OR
           (TG_TABLE_NAME='workspaces' AND b.workspace_id=NEW.id));
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER tables_revoke_selection_scopes AFTER UPDATE OF deleted_at ON tables
FOR EACH ROW EXECUTE FUNCTION revoke_ancestor_selection_scopes();
CREATE TRIGGER bases_revoke_selection_scopes AFTER UPDATE OF deleted_at ON bases
FOR EACH ROW EXECUTE FUNCTION revoke_ancestor_selection_scopes();
CREATE TRIGGER workspaces_revoke_selection_scopes AFTER UPDATE OF deleted_at ON workspaces
FOR EACH ROW EXECUTE FUNCTION revoke_ancestor_selection_scopes();
