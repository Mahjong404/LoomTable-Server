package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/Mahjong404/LoomTable-Server/internal/catalog"
	"github.com/Mahjong404/LoomTable-Server/internal/domain"
	loomrecord "github.com/Mahjong404/LoomTable-Server/internal/record"
)

func (r *Repository) ScanFieldValues(ctx context.Context, actorID string, field domain.Field, scan func(json.RawMessage) error) (bool, error) {
	if r == nil || r.db == nil {
		return false, domain.ErrDependencyMissing
	}
	var primary bool
	err := r.db.QueryRowContext(ctx, `
		SELECT f.is_primary
		FROM fields f
		JOIN tables t ON t.id = f.table_id
		JOIN bases b ON b.id = t.base_id
		JOIN workspaces w ON w.id = b.workspace_id
		WHERE f.id = $1 AND t.deleted_at IS NULL AND b.deleted_at IS NULL
		  AND w.actor_id = $2 AND w.deleted_at IS NULL
	`, field.ID, actorID).Scan(&primary)
	if err != nil {
		return false, domain.ErrNotFound
	}
	if primary {
		return true, nil
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT "values" -> $2
		FROM records
		WHERE table_id = $1
	`, field.TableID, field.ID)
	if err != nil {
		return false, fmt.Errorf("scan field values: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return false, fmt.Errorf("scan field value: %w", err)
		}
		if err := scan(json.RawMessage(raw)); err != nil {
			return false, err
		}
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("scan field values: %w", err)
	}
	return false, nil
}

func (r *Repository) ConvertField(ctx context.Context, actorID, fieldID string, plan catalog.FieldConversionPlan) (domain.Field, error) {
	if r == nil || r.db == nil {
		return domain.Field{}, domain.ErrDependencyMissing
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Field{}, fmt.Errorf("begin convert field: %w", err)
	}
	defer tx.Rollback()
	current, primary, err := lockAccessibleField(ctx, tx, actorID, fieldID)
	if err != nil {
		return domain.Field{}, err
	}
	if err := checkFieldRevision(current, plan.ExpectedRevision); err != nil {
		return domain.Field{}, err
	}
	if current.DeletedAt != nil {
		return domain.Field{}, &domain.InvalidStateTransitionError{Resource: "field", ID: fieldID, Action: "convert", Current: "deleted"}
	}
	if primary {
		return domain.Field{}, &domain.InvalidStateTransitionError{Resource: "field", ID: fieldID, Action: "convert", Current: "primary"}
	}
	fields, err := loadFieldDefinitions(ctx, tx, current.TableID)
	if err != nil {
		return domain.Field{}, err
	}
	type pendingUpdate struct {
		id        string
		values    map[string]any
		converted json.RawMessage
		present   bool
	}
	pending := make([]pendingUpdate, 0)
	rows, err := tx.QueryContext(ctx, `
		SELECT id, "values"
		FROM records
		WHERE table_id = $1
		FOR UPDATE
	`, current.TableID)
	if err != nil {
		return domain.Field{}, fmt.Errorf("scan records: %w", err)
	}
	for rows.Next() {
		var recordID string
		var rawValues []byte
		if err := rows.Scan(&recordID, &rawValues); err != nil {
			rows.Close()
			return domain.Field{}, fmt.Errorf("scan record: %w", err)
		}
		values := make(map[string]any)
		if err := json.Unmarshal(rawValues, &values); err != nil {
			rows.Close()
			return domain.Field{}, fmt.Errorf("decode record values: %w", err)
		}
		old := values[fieldID]
		var oldRaw json.RawMessage
		if old != nil {
			oldRaw, _ = json.Marshal(old)
		}
		converted, present, err := plan.Scan(oldRaw)
		if err != nil {
			rows.Close()
			return domain.Field{}, err
		}
		if old == nil && !present {
			continue
		}
		pending = append(pending, pendingUpdate{id: recordID, values: values, converted: converted, present: present})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return domain.Field{}, fmt.Errorf("scan records: %w", err)
	}
	rows.Close()
	if err := plan.Verify(); err != nil {
		return domain.Field{}, err
	}
	newConfig, err := plan.BuildConfig()
	if err != nil {
		return domain.Field{}, err
	}
	definition := fields[fieldID]
	definition.Type = plan.TargetType
	definition.Config = json.RawMessage(newConfig)
	fields[fieldID] = definition
	for _, item := range pending {
		if item.present && plan.ResolveValue != nil {
			item.converted, item.present = plan.ResolveValue(item.converted)
		}
		old := item.values[fieldID]
		var oldRaw []byte
		if old != nil {
			oldRaw, _ = json.Marshal(old)
		}
		if !item.present && old == nil {
			continue
		}
		if item.present && bytes.Equal(item.converted, oldRaw) {
			continue
		}
		if item.present {
			var value any
			if err := json.Unmarshal(item.converted, &value); err != nil {
				return domain.Field{}, fmt.Errorf("decode converted value: %w", err)
			}
			item.values[fieldID] = value
		} else {
			delete(item.values, fieldID)
		}
		queryValues, searchText := loomrecord.BuildQueryProjection(item.values, fields)
		encodedValues, err := json.Marshal(item.values)
		if err != nil {
			return domain.Field{}, fmt.Errorf("encode record values: %w", err)
		}
		encodedQuery, err := json.Marshal(queryValues)
		if err != nil {
			return domain.Field{}, fmt.Errorf("encode query values: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE records
			SET "values" = $1::jsonb, query_values = $2::jsonb, search_text = $3,
			    updated_at = clock_timestamp()
			WHERE id = $4
		`, string(encodedValues), string(encodedQuery), searchText, item.id); err != nil {
			return domain.Field{}, fmt.Errorf("rewrite record values: %w", err)
		}
	}
	updated, err := scanField(tx.QueryRowContext(ctx, `
		UPDATE fields
		SET type = $1, config = $2::jsonb, revision = revision + 1,
		    updated_at = clock_timestamp()
		WHERE id = $3
		RETURNING id, table_id, name, position_index, schema_version,
		          revision, type, config, deleted_at, description
	`, plan.TargetType, string(newConfig), fieldID))
	if err != nil {
		return domain.Field{}, fmt.Errorf("convert field: %w", err)
	}
	if err := insertMetadataChange(ctx, tx, actorID, "schemaChanged", updated.TableID, updated.ID, updated.Revision); err != nil {
		return domain.Field{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Field{}, fmt.Errorf("commit convert field: %w", err)
	}
	return updated, nil
}
