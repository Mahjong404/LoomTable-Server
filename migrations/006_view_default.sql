ALTER TABLE views ADD COLUMN IF NOT EXISTS is_default boolean NOT NULL DEFAULT false;

CREATE UNIQUE INDEX IF NOT EXISTS views_single_default_per_table
    ON views (table_id) WHERE is_default AND deleted_at IS NULL;
