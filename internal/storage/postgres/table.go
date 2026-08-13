package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Mahjong404/LoomTable-Server/internal/catalog"
	"github.com/Mahjong404/LoomTable-Server/internal/domain"
	"github.com/Mahjong404/LoomTable-Server/internal/id"
)

func (r *Repository) ListTables(ctx context.Context, actorID, baseID, lifecycle string) ([]domain.Table, error) {
	if r == nil || r.db == nil {
		return nil, domain.ErrDependencyMissing
	}
	condition := "t.deleted_at IS NULL"
	switch lifecycle {
	case "active":
	case "deleted":
		condition = "t.deleted_at IS NOT NULL"
	case "all":
		condition = "TRUE"
	default:
		return nil, &domain.BadRequestError{Message: "invalid lifecycle"}
	}

	var visible bool
	if err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM bases b
			JOIN workspaces w ON w.id = b.workspace_id
			WHERE b.id = $1 AND b.deleted_at IS NULL
			  AND w.actor_id = $2 AND w.deleted_at IS NULL
		)
	`, baseID, actorID).Scan(&visible); err != nil {
		return nil, fmt.Errorf("check base: %w", err)
	}
	if !visible {
		return nil, domain.ErrNotFound
	}

	rows, err := r.db.QueryContext(ctx, `
		SELECT t.id, t.base_id, t.name, t.primary_field_id, t.revision,
		       t.created_at, t.updated_at, t.deleted_at
		FROM tables t
		WHERE t.base_id = $1 AND `+condition+`
		ORDER BY t.created_at ASC, t.id ASC
	`, baseID)
	if err != nil {
		return nil, fmt.Errorf("list tables: %w", err)
	}
	defer rows.Close()

	items := make([]domain.Table, 0)
	for rows.Next() {
		item, err := scanTable(rows)
		if err != nil {
			return nil, fmt.Errorf("scan table: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list tables: %w", err)
	}
	return items, nil
}

func (r *Repository) GetTable(ctx context.Context, actorID, tableID string) (domain.Table, error) {
	if r == nil || r.db == nil {
		return domain.Table{}, domain.ErrDependencyMissing
	}
	item, err := scanTable(r.db.QueryRowContext(ctx, accessibleTableSQL, tableID, actorID))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Table{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Table{}, fmt.Errorf("get table: %w", err)
	}
	return item, nil
}

func (r *Repository) CreateTable(ctx context.Context, actorID, idempotencyKey string, fingerprint [32]byte, proposed domain.CreateTableResult) (domain.CreateTableResult, error) {
	if r == nil || r.db == nil {
		return domain.CreateTableResult{}, domain.ErrDependencyMissing
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.CreateTableResult{}, fmt.Errorf("begin create table: %w", err)
	}
	defer tx.Rollback()

	if err := lockActor(ctx, tx, actorID); err != nil {
		return domain.CreateTableResult{}, err
	}
	if response, found, err := replayIdempotent[domain.CreateTableResult](ctx, tx, actorID, idempotencyKey, fingerprint); err != nil {
		return domain.CreateTableResult{}, err
	} else if found {
		if err := tx.Commit(); err != nil {
			return domain.CreateTableResult{}, fmt.Errorf("commit table replay: %w", err)
		}
		return response, nil
	}

	var baseID string
	if err := tx.QueryRowContext(ctx, `
		SELECT b.id
		FROM bases b
		JOIN workspaces w ON w.id = b.workspace_id
		WHERE b.id = $1 AND b.deleted_at IS NULL
		  AND w.actor_id = $2 AND w.deleted_at IS NULL
		FOR UPDATE OF b
	`, proposed.Table.BaseID, actorID).Scan(&baseID); errors.Is(err, sql.ErrNoRows) {
		return domain.CreateTableResult{}, domain.ErrNotFound
	} else if err != nil {
		return domain.CreateTableResult{}, fmt.Errorf("lock parent base: %w", err)
	}

	var count int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM tables WHERE base_id = $1`, baseID).Scan(&count); err != nil {
		return domain.CreateTableResult{}, fmt.Errorf("count tables: %w", err)
	}
	if count >= catalog.TableLimitPerBase {
		return domain.CreateTableResult{}, &domain.ResourceLimitError{Resource: "table", ParentType: "base", ParentID: baseID, Limit: catalog.TableLimitPerBase}
	}

	createdTable, err := scanTable(tx.QueryRowContext(ctx, `
		INSERT INTO tables (id, base_id, name, primary_field_id, revision)
		VALUES ($1, $2, $3, $4, 1)
		RETURNING id, base_id, name, primary_field_id, revision, created_at, updated_at, deleted_at
	`, proposed.Table.ID, baseID, proposed.Table.Name, proposed.Table.PrimaryFieldID))
	if err != nil {
		return domain.CreateTableResult{}, fmt.Errorf("insert table: %w", err)
	}

	fieldConfig, err := json.Marshal(proposed.PrimaryField.Config)
	if err != nil {
		return domain.CreateTableResult{}, fmt.Errorf("encode primary field config: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO fields (
			id, table_id, name, type, position_index, schema_version,
			config, is_primary, revision
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, TRUE, 1)
	`, proposed.PrimaryField.ID, createdTable.ID, proposed.PrimaryField.Name,
		proposed.PrimaryField.Type, proposed.PrimaryField.Position,
		proposed.PrimaryField.SchemaVersion, string(fieldConfig)); err != nil {
		return domain.CreateTableResult{}, fmt.Errorf("insert primary field: %w", err)
	}

	viewConfig, err := json.Marshal(proposed.InitialView.Config)
	if err != nil {
		return domain.CreateTableResult{}, fmt.Errorf("encode initial view config: %w", err)
	}
	createdView := proposed.InitialView
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO views (id, table_id, name, type, config, revision)
		VALUES ($1, $2, $3, $4, $5::jsonb, 1)
		RETURNING created_at, updated_at
	`, createdView.ID, createdTable.ID, createdView.Name, createdView.Type, string(viewConfig)).Scan(&createdView.CreatedAt, &createdView.UpdatedAt); err != nil {
		return domain.CreateTableResult{}, fmt.Errorf("insert initial view: %w", err)
	}

	created := domain.CreateTableResult{
		Table:        createdTable,
		PrimaryField: proposed.PrimaryField,
		InitialView:  createdView,
	}
	if err := saveIdempotent(ctx, tx, actorID, idempotencyKey, fingerprint, created); err != nil {
		return domain.CreateTableResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.CreateTableResult{}, fmt.Errorf("commit create table: %w", err)
	}
	return created, nil
}

func (r *Repository) UpdateTable(ctx context.Context, actorID, tableID string, expectedRevision int64, name string) (domain.Table, error) {
	if r == nil || r.db == nil {
		return domain.Table{}, domain.ErrDependencyMissing
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Table{}, fmt.Errorf("begin update table: %w", err)
	}
	defer tx.Rollback()

	current, err := lockAccessibleTable(ctx, tx, actorID, tableID)
	if err != nil {
		return domain.Table{}, err
	}
	if err := checkTableRevision(current, expectedRevision); err != nil {
		return domain.Table{}, err
	}
	if current.DeletedAt != nil {
		return domain.Table{}, &domain.InvalidStateTransitionError{Resource: "table", ID: tableID, Action: "update", Current: "deleted"}
	}
	if current.Name == name {
		if err := tx.Commit(); err != nil {
			return domain.Table{}, fmt.Errorf("commit table no-op: %w", err)
		}
		return current, nil
	}

	updated, err := scanTable(tx.QueryRowContext(ctx, `
		UPDATE tables
		SET name = $1, revision = revision + 1, updated_at = clock_timestamp()
		WHERE id = $2
		RETURNING id, base_id, name, primary_field_id, revision, created_at, updated_at, deleted_at
	`, name, tableID))
	if err != nil {
		return domain.Table{}, fmt.Errorf("update table: %w", err)
	}
	if err := insertTableChange(ctx, tx, actorID, updated, "schemaChanged"); err != nil {
		return domain.Table{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Table{}, fmt.Errorf("commit update table: %w", err)
	}
	return updated, nil
}

func (r *Repository) DeleteTable(ctx context.Context, actorID, tableID string, expectedRevision int64) error {
	if r == nil || r.db == nil {
		return domain.ErrDependencyMissing
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin delete table: %w", err)
	}
	defer tx.Rollback()

	current, err := lockAccessibleTable(ctx, tx, actorID, tableID)
	if err != nil {
		return err
	}
	if err := checkTableRevision(current, expectedRevision); err != nil {
		return err
	}
	if current.DeletedAt != nil {
		return &domain.InvalidStateTransitionError{Resource: "table", ID: tableID, Action: "delete", Current: "deleted"}
	}

	deleted, err := scanTable(tx.QueryRowContext(ctx, `
		UPDATE tables
		SET deleted_at = clock_timestamp(), revision = revision + 1, updated_at = clock_timestamp()
		WHERE id = $1
		RETURNING id, base_id, name, primary_field_id, revision, created_at, updated_at, deleted_at
	`, tableID))
	if err != nil {
		return fmt.Errorf("delete table: %w", err)
	}
	if err := insertTableChange(ctx, tx, actorID, deleted, "schemaChanged"); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit delete table: %w", err)
	}
	return nil
}

func (r *Repository) RestoreTable(ctx context.Context, actorID, tableID string, expectedRevision int64) (domain.Table, error) {
	if r == nil || r.db == nil {
		return domain.Table{}, domain.ErrDependencyMissing
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Table{}, fmt.Errorf("begin restore table: %w", err)
	}
	defer tx.Rollback()

	current, err := lockAccessibleTable(ctx, tx, actorID, tableID)
	if err != nil {
		return domain.Table{}, err
	}
	if err := checkTableRevision(current, expectedRevision); err != nil {
		return domain.Table{}, err
	}
	if current.DeletedAt == nil {
		return domain.Table{}, &domain.InvalidStateTransitionError{Resource: "table", ID: tableID, Action: "restore", Current: "active"}
	}

	restored, err := scanTable(tx.QueryRowContext(ctx, `
		UPDATE tables
		SET deleted_at = NULL, revision = revision + 1, updated_at = clock_timestamp()
		WHERE id = $1
		RETURNING id, base_id, name, primary_field_id, revision, created_at, updated_at, deleted_at
	`, tableID))
	if err != nil {
		return domain.Table{}, fmt.Errorf("restore table: %w", err)
	}
	if err := insertTableChange(ctx, tx, actorID, restored, "schemaChanged"); err != nil {
		return domain.Table{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Table{}, fmt.Errorf("commit restore table: %w", err)
	}
	return restored, nil
}

const accessibleTableSQL = `
	SELECT t.id, t.base_id, t.name, t.primary_field_id, t.revision,
	       t.created_at, t.updated_at, t.deleted_at
	FROM tables t
	JOIN bases b ON b.id = t.base_id
	JOIN workspaces w ON w.id = b.workspace_id
	WHERE t.id = $1 AND b.deleted_at IS NULL
	  AND w.actor_id = $2 AND w.deleted_at IS NULL
`

func scanTable(row scanner) (domain.Table, error) {
	var item domain.Table
	err := row.Scan(
		&item.ID,
		&item.BaseID,
		&item.Name,
		&item.PrimaryFieldID,
		&item.Revision,
		&item.CreatedAt,
		&item.UpdatedAt,
		&item.DeletedAt,
	)
	return item, err
}

func lockAccessibleTable(ctx context.Context, tx *sql.Tx, actorID, tableID string) (domain.Table, error) {
	current, err := scanTable(tx.QueryRowContext(ctx, accessibleTableSQL+" FOR UPDATE OF t", tableID, actorID))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Table{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Table{}, fmt.Errorf("lock table: %w", err)
	}
	return current, nil
}

func checkTableRevision(current domain.Table, expected int64) error {
	if current.Revision == expected {
		return nil
	}
	return &domain.RevisionConflictError{
		Resource:         "table",
		ID:               current.ID,
		ExpectedRevision: expected,
		CurrentRevision:  current.Revision,
	}
}

func insertTableChange(ctx context.Context, tx *sql.Tx, actorID string, table domain.Table, kind string) error {
	changeID, err := id.New(id.ChangePrefix)
	if err != nil {
		return fmt.Errorf("generate change ID: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO changes (id, kind, table_id, object_id, revision, actor_id)
		VALUES ($1, $2, $3, $3, $4, $5)
	`, changeID, kind, table.ID, table.Revision, actorID); err != nil {
		return fmt.Errorf("insert table change: %w", err)
	}
	return nil
}
