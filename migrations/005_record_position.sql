ALTER TABLE records ADD COLUMN IF NOT EXISTS position DOUBLE PRECISION;

UPDATE records r
SET position = seq.position
FROM (
    SELECT id, (row_number() OVER (PARTITION BY table_id ORDER BY created_at, id)) * 1024.0 AS position
    FROM records
) seq
WHERE r.id = seq.id AND r.position IS NULL;

ALTER TABLE records ALTER COLUMN position SET NOT NULL;

CREATE INDEX IF NOT EXISTS records_table_position_idx
    ON records (table_id, position, id) WHERE deleted_at IS NULL;
