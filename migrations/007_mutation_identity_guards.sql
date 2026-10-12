-- G2 retains only the operation identity/hash after its full receipt expires.
-- Keep this table on binary downgrade; deleting guards permits re-execution.
CREATE TABLE mutation_identity_guards (
    actor_id TEXT NOT NULL REFERENCES actors(id),
    client_mutation_id TEXT NOT NULL,
    request_hash BYTEA NOT NULL,
    PRIMARY KEY (actor_id, client_mutation_id)
);
