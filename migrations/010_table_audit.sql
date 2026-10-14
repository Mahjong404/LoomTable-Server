-- Retained audit facts are additive; existing Change kinds/DTOs are untouched.
CREATE TABLE IF NOT EXISTS audit_coverage (
 singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton), captured_since timestamptz NOT NULL DEFAULT clock_timestamp()
);
INSERT INTO audit_coverage(singleton) VALUES(true) ON CONFLICT DO NOTHING;
CREATE TABLE IF NOT EXISTS audit_instrumentation_gaps(table_id text PRIMARY KEY REFERENCES tables(id), first_observed_at timestamptz NOT NULL DEFAULT clock_timestamp());
CREATE TABLE IF NOT EXISTS audit_events (
 sequence bigserial PRIMARY KEY, id text NOT NULL UNIQUE, operation_id text, change_ids text[] NOT NULL DEFAULT '{}',
 table_id text NOT NULL REFERENCES tables(id), object_id text NOT NULL, record_id text, field_id text, view_id text,
 actor_id text REFERENCES actors(id), client_mutation_id text, occurred_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 category text NOT NULL CHECK(category IN ('record','metadata','order')), kind text NOT NULL,
 payload_version integer NOT NULL CHECK(payload_version IN (0,1)), payload jsonb NOT NULL,
 legacy_change_id text UNIQUE
);
CREATE INDEX IF NOT EXISTS audit_table_sequence ON audit_events(table_id,sequence DESC);
CREATE INDEX IF NOT EXISTS audit_table_category_sequence ON audit_events(table_id,category,sequence DESC);
CREATE INDEX IF NOT EXISTS audit_table_record_sequence ON audit_events(table_id,record_id,sequence DESC);
CREATE INDEX IF NOT EXISTS audit_association ON audit_events(operation_id,object_id,table_id);
CREATE INDEX IF NOT EXISTS audit_change_ids ON audit_events USING gin(change_ids);
CREATE INDEX IF NOT EXISTS audit_operation_sequence ON audit_events(table_id,operation_id,sequence);
CREATE INDEX IF NOT EXISTS audit_retention ON audit_events(occurred_at,sequence);
CREATE INDEX IF NOT EXISTS audit_actor_mutation ON audit_events(actor_id,client_mutation_id,table_id);
CREATE TABLE IF NOT EXISTS audit_retention_watermarks(table_id text PRIMARY KEY REFERENCES tables(id),expired_through bigint NOT NULL, generation bigint NOT NULL DEFAULT 1, expired_through_time timestamptz NOT NULL);
ALTER TABLE idempotency_keys ADD COLUMN IF NOT EXISTS operation_id text;
-- Audit omits attachment storage coordinates; legacy Record/Attachment DTOs stay unchanged.
CREATE OR REPLACE FUNCTION loomtable_audit_safe_value(input_json jsonb) RETURNS jsonb LANGUAGE plpgsql IMMUTABLE AS $$
DECLARE result jsonb;
BEGIN
 IF NOT COALESCE(jsonb_path_exists(input_json,'$.**.storageKey') OR jsonb_path_exists(input_json,'$.**.vaultPath'),false) THEN RETURN input_json; END IF;
 CASE jsonb_typeof(input_json)
 WHEN 'object' THEN
  SELECT COALESCE(jsonb_object_agg(member.key,loomtable_audit_safe_value(member.val)),'{}'::jsonb) INTO result FROM jsonb_each(input_json) AS member(key,val) WHERE member.key NOT IN ('storageKey','vaultPath');
 WHEN 'array' THEN
  SELECT COALESCE(jsonb_agg(loomtable_audit_safe_value(entry.item) ORDER BY entry.ord),'[]'::jsonb) INTO result FROM jsonb_array_elements(input_json) WITH ORDINALITY AS entry(item,ord);
 ELSE result:=input_json;
 END CASE;
 RETURN result;
END $$;
CREATE OR REPLACE FUNCTION loomtable_audit_safe_payload(input_json jsonb) RETURNS jsonb LANGUAGE plpgsql IMMUTABLE AS $$
DECLARE omitted jsonb;
BEGIN
 SELECT COALESCE(jsonb_agg(property ORDER BY property),'[]'::jsonb) INTO omitted FROM (
  SELECT DISTINCT property FROM (
   SELECT jsonb_array_elements_text(CASE WHEN jsonb_typeof(input_json->'redactedProperties')='array' THEN input_json->'redactedProperties' ELSE '[]'::jsonb END) property
   UNION ALL SELECT 'storageKey' WHERE jsonb_path_exists(input_json,'$.**.storageKey')
   UNION ALL SELECT 'vaultPath' WHERE jsonb_path_exists(input_json,'$.**.vaultPath')
  ) properties WHERE property IN ('storageKey','vaultPath')
 ) names;
 RETURN loomtable_audit_safe_value(input_json)||jsonb_build_object('redactedProperties',omitted);
END $$;
-- Import only actual retained facts; never reconstruct event-time metadata or grouping.
INSERT INTO audit_events(id,change_ids,table_id,object_id,record_id,actor_id,occurred_at,category,kind,payload_version,payload,legacy_change_id)
 SELECT 'aud_'||replace(gen_random_uuid()::text,'-',''),ARRAY[id],table_id,COALESCE(record_id,object_id,table_id),record_id,actor_id,occurred_at,
 CASE WHEN kind IN ('recordCreated','recordUpdated','recordDeleted','recordRestored') THEN 'record' WHEN kind='recordMoved' THEN 'order' ELSE 'metadata' END,
 kind,0,loomtable_audit_safe_payload(jsonb_build_object('before',null,'after',null,'fields','[]'::jsonb,'metadataRecorded',false,'legacyFields',fields,'legacyPrimaryFieldText',primary_field_text)),id FROM changes c WHERE NOT EXISTS(SELECT 1 FROM audit_events e WHERE e.change_ids @> ARRAY[c.id]) ORDER BY change_sequence
 ON CONFLICT(legacy_change_id) DO NOTHING;
CREATE OR REPLACE FUNCTION loomtable_audit_operation() RETURNS text LANGUAGE plpgsql AS $$
DECLARE result text;
BEGIN
 result:=nullif(current_setting('loomtable.audit_operation',true),'');
 IF result IS NULL THEN
  result:='op_'||replace(gen_random_uuid()::text,'-','');
  PERFORM set_config('loomtable.audit_operation',result,true);
 END IF;
 RETURN result;
END $$;
CREATE OR REPLACE FUNCTION loomtable_audit_cell(vals jsonb, fid text, tid text, after_cell boolean) RETURNS jsonb LANGUAGE plpgsql AS $$
DECLARE definition jsonb; override_field jsonb; options jsonb; cell_value jsonb; present text; missing_options jsonb;
BEGIN
 SELECT to_jsonb(f) INTO definition FROM fields f WHERE f.id=fid AND f.table_id=tid;
 IF after_cell THEN
  override_field:=nullif(current_setting('loomtable.audit_conversion',true),'')::jsonb;
  IF override_field->>'id'=fid THEN definition:=override_field; END IF;
 END IF;
 cell_value:=vals->fid;
 present:=CASE WHEN NOT COALESCE(vals ? fid,false) THEN 'missing' WHEN cell_value='null'::jsonb THEN 'null' ELSE 'value' END;
 IF COALESCE(definition->>'type','') IN ('select','multiSelect') THEN
 SELECT COALESCE(jsonb_agg(opt ORDER BY opt->>'id'),'[]'::jsonb) INTO options
 FROM jsonb_array_elements(CASE WHEN jsonb_typeof(definition->'config'->'options')='array' THEN definition->'config'->'options' ELSE '[]'::jsonb END || CASE WHEN jsonb_typeof(definition->'config'->'deletedOptions')='array' THEN definition->'config'->'deletedOptions' ELSE '[]'::jsonb END) opt
 WHERE cell_value=to_jsonb(opt->>'id') OR (jsonb_typeof(cell_value)='array' AND cell_value ? (opt->>'id'));
 SELECT COALESCE(jsonb_agg(selected),'[]'::jsonb) INTO missing_options FROM (SELECT cell_value #>> '{}' selected WHERE jsonb_typeof(cell_value)='string' UNION ALL SELECT jsonb_array_elements_text(CASE WHEN jsonb_typeof(cell_value)='array' THEN cell_value ELSE '[]'::jsonb END)) chosen WHERE definition->>'type' IN ('select','multiSelect') AND NOT EXISTS(SELECT 1 FROM jsonb_array_elements(options) opt WHERE opt->>'id'=chosen.selected);
 ELSE options:='[]'::jsonb; missing_options:='[]'::jsonb; END IF;
 RETURN jsonb_build_object('presence',present,'value',cell_value,'missingOptionIds',missing_options,'metadataRecorded',definition IS NOT NULL AND jsonb_array_length(missing_options)=0,
  'field',CASE WHEN definition IS NULL THEN null ELSE jsonb_build_object('id',fid,'name',definition->'name','type',definition->'type','deletedAt',definition->'deleted_at') END,'options',options);
END $$;
CREATE OR REPLACE FUNCTION loomtable_capture_audit() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE b jsonb; a jsonb; entity jsonb; tid text; actor text; op text; kind text; category text:='metadata'; cells jsonb:='[]'::jsonb; fid text; rid text; fieldid text; vid text;
BEGIN
 IF TG_OP='UPDATE' THEN
  b:=to_jsonb(OLD); a:=to_jsonb(NEW);
  IF (b-'revision'-'updated_at'-'query_values'-'search_text'-'view_order_revision')=(a-'revision'-'updated_at'-'query_values'-'search_text'-'view_order_revision') THEN RETURN NEW; END IF;
 ELSIF TG_OP='INSERT' THEN a:=to_jsonb(NEW); ELSE b:=to_jsonb(OLD); END IF;
 entity:=COALESCE(a,b); tid:=CASE WHEN TG_TABLE_NAME='tables' THEN entity->>'id' ELSE entity->>'table_id' END;
 actor:=nullif(current_setting('loomtable.audit_actor',true),''); op:=loomtable_audit_operation();
 IF actor IS NULL THEN INSERT INTO audit_instrumentation_gaps(table_id) VALUES(tid) ON CONFLICT DO NOTHING; END IF;
 kind:=CASE TG_TABLE_NAME WHEN 'records' THEN 'record' WHEN 'fields' THEN 'field' WHEN 'views' THEN 'view' ELSE 'table' END||
 CASE WHEN TG_OP='INSERT' THEN 'Created' WHEN TG_OP='DELETE' OR (b->>'deleted_at' IS NULL AND a->>'deleted_at' IS NOT NULL) THEN 'Deleted' WHEN b->>'deleted_at' IS NOT NULL AND a->>'deleted_at' IS NULL THEN 'Restored' ELSE 'Updated' END;
 IF TG_TABLE_NAME='records' THEN
  rid:=entity->>'id'; category:='record';
  IF TG_OP='UPDATE' AND b->'values'=a->'values' AND b->'deleted_at'=a->'deleted_at' THEN kind:='recordMoved'; category:='order'; END IF;
  FOR fid IN SELECT key FROM (SELECT jsonb_object_keys(COALESCE(b->'values','{}'::jsonb)) key UNION SELECT jsonb_object_keys(COALESCE(a->'values','{}'::jsonb)) key) keys ORDER BY key LOOP
   IF TG_OP<>'UPDATE' OR b->'deleted_at' IS DISTINCT FROM a->'deleted_at' OR (b->'values'->fid IS DISTINCT FROM a->'values'->fid) THEN
    cells:=cells||jsonb_build_array(jsonb_build_object('fieldId',fid,'before',loomtable_audit_cell(b->'values',fid,tid,false),'after',loomtable_audit_cell(a->'values',fid,tid,true)));
   END IF;
  END LOOP;
  IF actor IS NULL THEN
   SELECT COALESCE(jsonb_agg(cell || jsonb_build_object('before',cell->'before'||jsonb_build_object('metadataRecorded',false,'field',null,'options','[]'::jsonb),'after',cell->'after'||jsonb_build_object('metadataRecorded',false,'field',null,'options','[]'::jsonb))),'[]'::jsonb) INTO cells FROM jsonb_array_elements(cells) cell;
  END IF;
  b:=b-'query_values'-'search_text'; a:=a-'query_values'-'search_text';
 ELSIF TG_TABLE_NAME='fields' THEN fieldid:=entity->>'id'; IF TG_OP='UPDATE' AND b->'type' IS DISTINCT FROM a->'type' THEN kind:='fieldConverted'; END IF;
 ELSIF TG_TABLE_NAME='views' THEN vid:=entity->>'id';
 ELSIF TG_OP='UPDATE' AND (b-'view_order'-'view_order_revision')=(a-'view_order'-'view_order_revision') THEN kind:='viewOrderChanged'; category:='order'; END IF;
 INSERT INTO audit_events(id,operation_id,table_id,object_id,record_id,field_id,view_id,actor_id,category,kind,payload_version,payload)
 VALUES('aud_'||replace(gen_random_uuid()::text,'-',''),op,tid,entity->>'id',rid,fieldid,vid,actor,category,kind,1,
 loomtable_audit_safe_payload(jsonb_build_object('before',b,'after',a,'fields',cells,'metadataRecorded',true)));
 RETURN COALESCE(NEW,OLD);
END $$;
CREATE OR REPLACE FUNCTION loomtable_associate_audit_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 UPDATE audit_events SET change_ids=array_append(change_ids,NEW.id)
 WHERE operation_id=nullif(current_setting('loomtable.audit_operation',true),'') AND table_id=NEW.table_id AND object_id=COALESCE(NEW.record_id,NEW.object_id,NEW.table_id) AND NOT (NEW.id=ANY(change_ids));
 RETURN NEW;
END $$;
CREATE OR REPLACE FUNCTION loomtable_capture_ancestor_audit() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE tid text; actor text;
BEGIN
 IF OLD.name=NEW.name AND OLD.deleted_at IS NOT DISTINCT FROM NEW.deleted_at THEN RETURN NEW; END IF;
 actor:=nullif(current_setting('loomtable.audit_actor',true),'');

 FOR tid IN SELECT t.id FROM tables t JOIN bases b ON b.id=t.base_id WHERE (TG_TABLE_NAME='bases' AND b.id=NEW.id) OR (TG_TABLE_NAME='workspaces' AND b.workspace_id=NEW.id) ORDER BY t.id FOR UPDATE OF t LOOP
  IF actor IS NULL THEN INSERT INTO audit_instrumentation_gaps(table_id) VALUES(tid) ON CONFLICT DO NOTHING; END IF;
  INSERT INTO audit_events(id,operation_id,table_id,object_id,actor_id,category,kind,payload_version,payload)
  VALUES('aud_'||replace(gen_random_uuid()::text,'-',''),loomtable_audit_operation(),tid,NEW.id,actor,'metadata',CASE TG_TABLE_NAME WHEN 'bases' THEN 'baseUpdated' ELSE 'workspaceUpdated' END,1,loomtable_audit_safe_payload(jsonb_build_object('before',to_jsonb(OLD),'after',to_jsonb(NEW),'fields','[]'::jsonb,'metadataRecorded',true)));
 END LOOP;
 RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS capture_audit ON records;
CREATE TRIGGER capture_audit AFTER INSERT OR UPDATE OR DELETE ON records FOR EACH ROW EXECUTE FUNCTION loomtable_capture_audit();
DROP TRIGGER IF EXISTS capture_audit ON fields;
CREATE TRIGGER capture_audit AFTER INSERT OR UPDATE OR DELETE ON fields FOR EACH ROW EXECUTE FUNCTION loomtable_capture_audit();
DROP TRIGGER IF EXISTS capture_audit ON views;
CREATE TRIGGER capture_audit AFTER INSERT OR UPDATE OR DELETE ON views FOR EACH ROW EXECUTE FUNCTION loomtable_capture_audit();
DROP TRIGGER IF EXISTS capture_audit ON tables;
CREATE TRIGGER capture_audit AFTER INSERT OR UPDATE OR DELETE ON tables FOR EACH ROW EXECUTE FUNCTION loomtable_capture_audit();
DROP TRIGGER IF EXISTS associate_audit_change ON changes;
CREATE TRIGGER associate_audit_change AFTER INSERT ON changes FOR EACH ROW EXECUTE FUNCTION loomtable_associate_audit_change();
DROP TRIGGER IF EXISTS capture_ancestor_audit ON bases;
CREATE TRIGGER capture_ancestor_audit AFTER UPDATE ON bases FOR EACH ROW EXECUTE FUNCTION loomtable_capture_ancestor_audit();
DROP TRIGGER IF EXISTS capture_ancestor_audit ON workspaces;
CREATE TRIGGER capture_ancestor_audit AFTER UPDATE ON workspaces FOR EACH ROW EXECUTE FUNCTION loomtable_capture_ancestor_audit();

-- Raw SQL retry also scrubs any pre-fix retained Audit rows without new identities/events.
UPDATE audit_events SET payload=loomtable_audit_safe_payload(payload) WHERE NOT payload ? 'redactedProperties' OR jsonb_path_exists(payload,'$.**.storageKey') OR jsonb_path_exists(payload,'$.**.vaultPath');
