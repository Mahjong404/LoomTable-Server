ALTER TABLE changes ADD COLUMN IF NOT EXISTS fields JSONB;
ALTER TABLE changes ADD COLUMN IF NOT EXISTS primary_field_text TEXT;

CREATE INDEX IF NOT EXISTS changes_table_record_sequence_idx
    ON changes (table_id, record_id, change_sequence);
