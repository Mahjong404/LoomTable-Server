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
-- Protect all original 010 capture paths without duplicating their business capture logic.
CREATE OR REPLACE FUNCTION loomtable_sanitize_audit_payload() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 NEW.payload:=loomtable_audit_safe_payload(NEW.payload);
 RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS sanitize_audit_payload ON audit_events;
CREATE TRIGGER sanitize_audit_payload BEFORE INSERT OR UPDATE OF payload ON audit_events FOR EACH ROW EXECUTE FUNCTION loomtable_sanitize_audit_payload();
-- Deliberate irreversible redaction only in Audit copies; legacy Changes and business rows stay intact.
UPDATE audit_events SET payload=payload WHERE NOT payload ? 'redactedProperties' OR jsonb_path_exists(payload,'$.**.storageKey') OR jsonb_path_exists(payload,'$.**.vaultPath');
