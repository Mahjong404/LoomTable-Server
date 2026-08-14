package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	"github.com/Mahjong404/LoomTable-Server/internal/catalog"
	"github.com/Mahjong404/LoomTable-Server/internal/domain"
	"github.com/Mahjong404/LoomTable-Server/internal/id"
)

func (r *Repository) ListFields(ctx context.Context, actorID, tableID, lifecycle string) ([]domain.Field, error) {
	if r == nil || r.db == nil {
		return nil, domain.ErrDependencyMissing
	}
	condition, err := lifecycleCondition("f.deleted_at", lifecycle)
	if err != nil {
		return nil, err
	}
	visible, err := activeTableVisible(ctx, r.db, actorID, tableID)
	if err != nil {
		return nil, err
	}
	if !visible {
		return nil, domain.ErrNotFound
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT f.id, f.table_id, f.name, f.position_index, f.schema_version,
		       f.revision, f.type, f.config, f.deleted_at
		FROM fields f
		WHERE f.table_id = $1 AND `+condition+`
		ORDER BY f.position_index ASC, f.id ASC
	`, tableID)
	if err != nil {
		return nil, fmt.Errorf("list fields: %w", err)
	}
	defer rows.Close()
	items := make([]domain.Field, 0)
	for rows.Next() {
		item, err := scanField(rows)
		if err != nil {
			return nil, fmt.Errorf("scan field: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list fields: %w", err)
	}
	return items, nil
}

func (r *Repository) GetField(ctx context.Context, actorID, fieldID string) (domain.Field, error) {
	if r == nil || r.db == nil {
		return domain.Field{}, domain.ErrDependencyMissing
	}
	item, err := scanField(r.db.QueryRowContext(ctx, accessibleFieldSQL, fieldID, actorID))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Field{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Field{}, fmt.Errorf("get field: %w", err)
	}
	return item, nil
}

func (r *Repository) CreateField(ctx context.Context, actorID, idempotencyKey string, fingerprint [32]byte, proposed domain.Field) (domain.Field, error) {
	if r == nil || r.db == nil {
		return domain.Field{}, domain.ErrDependencyMissing
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Field{}, fmt.Errorf("begin create field: %w", err)
	}
	defer tx.Rollback()
	if err := lockActor(ctx, tx, actorID); err != nil {
		return domain.Field{}, err
	}
	if response, found, err := replayIdempotent[domain.Field](ctx, tx, actorID, idempotencyKey, fingerprint); err != nil {
		return domain.Field{}, err
	} else if found {
		if err := tx.Commit(); err != nil {
			return domain.Field{}, fmt.Errorf("commit field replay: %w", err)
		}
		return response, nil
	}
	if err := lockActiveTable(ctx, tx, actorID, proposed.TableID); err != nil {
		return domain.Field{}, err
	}
	var count, position int
	if err := tx.QueryRowContext(ctx, `
		SELECT count(*), COALESCE(max(position_index) + 1, 0)
		FROM fields WHERE table_id = $1
	`, proposed.TableID).Scan(&count, &position); err != nil {
		return domain.Field{}, fmt.Errorf("count fields: %w", err)
	}
	if count >= catalog.FieldLimitPerTable {
		return domain.Field{}, &domain.ResourceLimitError{Resource: "field", ParentType: "table", ParentID: proposed.TableID, Limit: catalog.FieldLimitPerTable}
	}
	encoded, err := json.Marshal(proposed.Config)
	if err != nil {
		return domain.Field{}, fmt.Errorf("encode field config: %w", err)
	}
	created, err := scanField(tx.QueryRowContext(ctx, `
		INSERT INTO fields (
			id, table_id, name, type, position_index, schema_version,
			config, is_primary, revision
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, FALSE, 1)
		RETURNING id, table_id, name, position_index, schema_version,
		          revision, type, config, deleted_at
	`, proposed.ID, proposed.TableID, proposed.Name, proposed.Type, position,
		proposed.SchemaVersion, string(encoded)))
	if err != nil {
		return domain.Field{}, fmt.Errorf("insert field: %w", err)
	}
	if err := insertMetadataChange(ctx, tx, actorID, "schemaChanged", created.TableID, created.ID, created.Revision); err != nil {
		return domain.Field{}, err
	}
	if err := saveIdempotent(ctx, tx, actorID, idempotencyKey, fingerprint, created); err != nil {
		return domain.Field{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Field{}, fmt.Errorf("commit create field: %w", err)
	}
	return created, nil
}

func (r *Repository) UpdateField(ctx context.Context, actorID, fieldID string, expectedRevision int64, target domain.Field) (domain.Field, error) {
	if r == nil || r.db == nil {
		return domain.Field{}, domain.ErrDependencyMissing
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Field{}, fmt.Errorf("begin update field: %w", err)
	}
	defer tx.Rollback()
	current, _, err := lockAccessibleField(ctx, tx, actorID, fieldID)
	if err != nil {
		return domain.Field{}, err
	}
	if err := checkFieldRevision(current, expectedRevision); err != nil {
		return domain.Field{}, err
	}
	if current.DeletedAt != nil {
		return domain.Field{}, &domain.InvalidStateTransitionError{Resource: "field", ID: fieldID, Action: "update", Current: "deleted"}
	}
	if current.Type != target.Type {
		return domain.Field{}, domain.NewValidationError(domain.ValidationIssue{Path: "/type", Code: "format", Message: "Field type is immutable in P0"})
	}
	if current.Name == target.Name && reflect.DeepEqual(current.Config, target.Config) {
		if err := tx.Commit(); err != nil {
			return domain.Field{}, fmt.Errorf("commit field no-op: %w", err)
		}
		return current, nil
	}
	encoded, err := json.Marshal(target.Config)
	if err != nil {
		return domain.Field{}, fmt.Errorf("encode field config: %w", err)
	}
	updated, err := scanField(tx.QueryRowContext(ctx, `
		UPDATE fields
		SET name = $1, config = $2::jsonb, revision = revision + 1,
		    updated_at = clock_timestamp()
		WHERE id = $3
		RETURNING id, table_id, name, position_index, schema_version,
		          revision, type, config, deleted_at
	`, target.Name, string(encoded), fieldID))
	if err != nil {
		return domain.Field{}, fmt.Errorf("update field: %w", err)
	}
	if err := insertMetadataChange(ctx, tx, actorID, "schemaChanged", updated.TableID, updated.ID, updated.Revision); err != nil {
		return domain.Field{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Field{}, fmt.Errorf("commit update field: %w", err)
	}
	return updated, nil
}

func (r *Repository) DeleteField(ctx context.Context, actorID, fieldID string, expectedRevision int64) error {
	if r == nil || r.db == nil {
		return domain.ErrDependencyMissing
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin delete field: %w", err)
	}
	defer tx.Rollback()
	current, primary, err := lockAccessibleField(ctx, tx, actorID, fieldID)
	if err != nil {
		return err
	}
	if err := checkFieldRevision(current, expectedRevision); err != nil {
		return err
	}
	if current.DeletedAt != nil {
		return &domain.InvalidStateTransitionError{Resource: "field", ID: fieldID, Action: "delete", Current: "deleted"}
	}
	if primary {
		return &domain.InvalidStateTransitionError{Resource: "field", ID: fieldID, Action: "delete", Current: "primary"}
	}
	deleted, err := scanField(tx.QueryRowContext(ctx, `
		UPDATE fields
		SET deleted_at = clock_timestamp(), revision = revision + 1,
		    updated_at = clock_timestamp()
		WHERE id = $1
		RETURNING id, table_id, name, position_index, schema_version,
		          revision, type, config, deleted_at
	`, fieldID))
	if err != nil {
		return fmt.Errorf("delete field: %w", err)
	}
	if err := insertMetadataChange(ctx, tx, actorID, "schemaChanged", deleted.TableID, deleted.ID, deleted.Revision); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit delete field: %w", err)
	}
	return nil
}

func (r *Repository) RestoreField(ctx context.Context, actorID, fieldID string, expectedRevision int64) (domain.Field, error) {
	if r == nil || r.db == nil {
		return domain.Field{}, domain.ErrDependencyMissing
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Field{}, fmt.Errorf("begin restore field: %w", err)
	}
	defer tx.Rollback()
	current, _, err := lockAccessibleField(ctx, tx, actorID, fieldID)
	if err != nil {
		return domain.Field{}, err
	}
	if err := checkFieldRevision(current, expectedRevision); err != nil {
		return domain.Field{}, err
	}
	if current.DeletedAt == nil {
		return domain.Field{}, &domain.InvalidStateTransitionError{Resource: "field", ID: fieldID, Action: "restore", Current: "active"}
	}
	restored, err := scanField(tx.QueryRowContext(ctx, `
		UPDATE fields
		SET deleted_at = NULL, revision = revision + 1,
		    updated_at = clock_timestamp()
		WHERE id = $1
		RETURNING id, table_id, name, position_index, schema_version,
		          revision, type, config, deleted_at
	`, fieldID))
	if err != nil {
		return domain.Field{}, fmt.Errorf("restore field: %w", err)
	}
	if err := insertMetadataChange(ctx, tx, actorID, "schemaChanged", restored.TableID, restored.ID, restored.Revision); err != nil {
		return domain.Field{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Field{}, fmt.Errorf("commit restore field: %w", err)
	}
	return restored, nil
}

const accessibleFieldSQL = `
	SELECT f.id, f.table_id, f.name, f.position_index, f.schema_version,
	       f.revision, f.type, f.config, f.deleted_at
	FROM fields f
	JOIN tables t ON t.id = f.table_id
	JOIN bases b ON b.id = t.base_id
	JOIN workspaces w ON w.id = b.workspace_id
	WHERE f.id = $1 AND t.deleted_at IS NULL AND b.deleted_at IS NULL
	  AND w.actor_id = $2 AND w.deleted_at IS NULL
`

func scanField(row scanner) (domain.Field, error) {
	var item domain.Field
	var rawConfig []byte
	if err := row.Scan(
		&item.ID, &item.TableID, &item.Name, &item.Position, &item.SchemaVersion,
		&item.Revision, &item.Type, &rawConfig, &item.DeletedAt,
	); err != nil {
		return domain.Field{}, err
	}
	config, err := decodeFieldConfig(item.Type, rawConfig)
	if err != nil {
		return domain.Field{}, err
	}
	item.Config = config
	return item, nil
}

func decodeFieldConfig(fieldType string, raw []byte) (any, error) {
	switch fieldType {
	case "select", "multiSelect":
		var config domain.SelectFieldConfig
		if err := json.Unmarshal(raw, &config); err != nil {
			return nil, fmt.Errorf("decode %s Field config: %w", fieldType, err)
		}
		if config.Options == nil {
			config.Options = make([]domain.SelectOption, 0)
		}
		if config.DeletedOptions == nil {
			config.DeletedOptions = make([]domain.DeletedSelectOption, 0)
		}
		return config, nil
	case "attachment":
		var config domain.AttachmentFieldConfig
		if err := json.Unmarshal(raw, &config); err != nil {
			return nil, fmt.Errorf("decode %s Field config: %w", fieldType, err)
		}
		if config.MaxCount == 0 {
			config.MaxCount = 10
		}
		return config, nil
	case "text", "longText", "number", "checkbox", "date", "url", "location":
		return domain.EmptyFieldConfig{}, nil
	default:
		return nil, fmt.Errorf("unsupported persisted Field type %q", fieldType)
	}
}

func lockAccessibleField(ctx context.Context, tx *sql.Tx, actorID, fieldID string) (domain.Field, bool, error) {
	var item domain.Field
	var rawConfig []byte
	var primary bool
	err := tx.QueryRowContext(ctx, `
		SELECT f.id, f.table_id, f.name, f.position_index, f.schema_version,
		       f.revision, f.type, f.config, f.deleted_at, f.is_primary
		FROM fields f
		JOIN tables t ON t.id = f.table_id
		JOIN bases b ON b.id = t.base_id
		JOIN workspaces w ON w.id = b.workspace_id
		WHERE f.id = $1 AND t.deleted_at IS NULL AND b.deleted_at IS NULL
		  AND w.actor_id = $2 AND w.deleted_at IS NULL
		FOR UPDATE OF f
	`, fieldID, actorID).Scan(
		&item.ID, &item.TableID, &item.Name, &item.Position, &item.SchemaVersion,
		&item.Revision, &item.Type, &rawConfig, &item.DeletedAt, &primary,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Field{}, false, domain.ErrNotFound
	}
	if err != nil {
		return domain.Field{}, false, fmt.Errorf("lock field: %w", err)
	}
	item.Config, err = decodeFieldConfig(item.Type, rawConfig)
	if err != nil {
		return domain.Field{}, false, err
	}
	return item, primary, nil
}

func checkFieldRevision(current domain.Field, expected int64) error {
	if current.Revision == expected {
		return nil
	}
	return &domain.RevisionConflictError{
		Resource: "field", ID: current.ID, ExpectedRevision: expected, CurrentRevision: current.Revision,
	}
}

func lifecycleCondition(column, lifecycle string) (string, error) {
	switch lifecycle {
	case "active":
		return column + " IS NULL", nil
	case "deleted":
		return column + " IS NOT NULL", nil
	case "all":
		return "TRUE", nil
	default:
		return "", &domain.BadRequestError{Message: "invalid lifecycle"}
	}
}

type queryRower interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func activeTableVisible(ctx context.Context, db queryRower, actorID, tableID string) (bool, error) {
	var visible bool
	if err := db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM tables t
			JOIN bases b ON b.id = t.base_id
			JOIN workspaces w ON w.id = b.workspace_id
			WHERE t.id = $1 AND t.deleted_at IS NULL AND b.deleted_at IS NULL
			  AND w.actor_id = $2 AND w.deleted_at IS NULL
		)
	`, tableID, actorID).Scan(&visible); err != nil {
		return false, fmt.Errorf("check table: %w", err)
	}
	return visible, nil
}

func insertMetadataChange(ctx context.Context, tx *sql.Tx, actorID, kind, tableID, objectID string, revision int64) error {
	changeID, err := id.New(id.ChangePrefix)
	if err != nil {
		return fmt.Errorf("generate change ID: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO changes (id, kind, table_id, object_id, revision, actor_id)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, changeID, kind, tableID, objectID, revision, actorID); err != nil {
		return fmt.Errorf("insert metadata change: %w", err)
	}
	return nil
}

