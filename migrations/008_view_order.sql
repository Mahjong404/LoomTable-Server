ALTER TABLE tables
    ADD COLUMN view_order_revision bigint NOT NULL DEFAULT 1 CHECK (view_order_revision >= 1),
    ADD COLUMN view_order jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(view_order) = 'array' AND jsonb_array_length(view_order) <= 100);

UPDATE tables t SET view_order = COALESCE((
    SELECT jsonb_agg(v.id ORDER BY v.created_at, v.id)
    FROM views v WHERE v.table_id = t.id AND v.deleted_at IS NULL
), '[]'::jsonb);

-- Keep membership valid even after a binary downgrade. Retain this trigger,
-- both Order columns and identity guards; use one writer version at a time.
CREATE FUNCTION maintain_view_order() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    parent_id text;
    view_id text;
    active_member boolean;
    current_order jsonb;
    next_order jsonb;
BEGIN
    IF TG_OP = 'DELETE' THEN
        parent_id := OLD.table_id;
        view_id := OLD.id;
        active_member := false;
    ELSE
        parent_id := NEW.table_id;
        view_id := NEW.id;
        active_member := NEW.deleted_at IS NULL;
        IF TG_OP = 'UPDATE' AND (OLD.deleted_at IS NULL) = active_member THEN
            RETURN NEW;
        END IF;
    END IF;
    SELECT view_order INTO current_order FROM tables WHERE id = parent_id FOR UPDATE;
    IF NOT FOUND THEN
        -- A physical parent cascade has no surviving Order to maintain.
        RETURN NULL;
    END IF;
    next_order := current_order;
    IF active_member AND NOT current_order ? view_id THEN
        next_order := current_order || jsonb_build_array(view_id);
    ELSIF NOT active_member AND current_order ? view_id THEN
        SELECT COALESCE(jsonb_agg(value ORDER BY ordinal), '[]'::jsonb) INTO next_order
        FROM jsonb_array_elements(current_order) WITH ORDINALITY AS members(value, ordinal)
        WHERE value <> to_jsonb(view_id);
    END IF;
    IF next_order <> current_order THEN
        UPDATE tables SET view_order = next_order, view_order_revision = view_order_revision + 1
        WHERE id = parent_id;
    END IF;
    RETURN NULL;
END;
$$;

CREATE TRIGGER views_maintain_order
AFTER INSERT OR UPDATE OF deleted_at OR DELETE ON views
FOR EACH ROW EXECUTE FUNCTION maintain_view_order();
