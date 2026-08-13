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
)

func (r *Repository) ListViews(ctx context.Context, actorID, tableID, lifecycle string) ([]domain.View, error) {
	if r == nil || r.db == nil {
		return nil, domain.ErrDependencyMissing
	}
	condition, err := lifecycleCondition("v.deleted_at", lifecycle)
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
		SELECT v.id, v.table_id, v.name, v.type, v.config, v.revision,
		       v.created_at, v.updated_at, v.deleted_at
		FROM views v
		WHERE v.table_id = $1 AND `+condition+`
		ORDER BY v.created_at ASC, v.id ASC
	`, tableID)
	if err != nil {
		return nil, fmt.Errorf("list views: %w", err)
	}
	defer rows.Close()
	items := make([]domain.View, 0)
	for rows.Next() {
		item, err := scanView(rows)
		if err != nil {
			return nil, fmt.Errorf("scan view: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list views: %w", err)
	}
	return items, nil
}

func (r *Repository) GetView(ctx context.Context, actorID, viewID string) (domain.View, error) {
	if r == nil || r.db == nil {
		return domain.View{}, domain.ErrDependencyMissing
	}
	item, err := scanView(r.db.QueryRowContext(ctx, accessibleViewSQL, viewID, actorID))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.View{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.View{}, fmt.Errorf("get view: %w", err)
	}
	return item, nil
}

func (r *Repository) CreateView(ctx context.Context, actorID, idempotencyKey string, fingerprint [32]byte, proposed domain.View) (domain.View, error) {
	if r == nil || r.db == nil {
		return domain.View{}, domain.ErrDependencyMissing
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.View{}, fmt.Errorf("begin create view: %w", err)
	}
	defer tx.Rollback()
	if err := lockActor(ctx, tx, actorID); err != nil {
		return domain.View{}, err
	}
	if response, found, err := replayIdempotent[domain.View](ctx, tx, actorID, idempotencyKey, fingerprint); err != nil {
		return domain.View{}, err
	} else if found {
		if err := tx.Commit(); err != nil {
			return domain.View{}, fmt.Errorf("commit view replay: %w", err)
		}
		return response, nil
	}
	if err := lockActiveTable(ctx, tx, actorID, proposed.TableID); err != nil {
		return domain.View{}, err
	}
	var count int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM views WHERE table_id = $1", proposed.TableID).Scan(&count); err != nil {
		return domain.View{}, fmt.Errorf("count views: %w", err)
	}
	if count >= catalog.ViewLimitPerTable {
		return domain.View{}, &domain.ResourceLimitError{Resource: "view", ParentType: "table", ParentID: proposed.TableID, Limit: catalog.ViewLimitPerTable}
	}
	encoded, err := json.Marshal(proposed.Config)
	if err != nil {
		return domain.View{}, fmt.Errorf("encode view config: %w", err)
	}
	created, err := scanView(tx.QueryRowContext(ctx, `
		INSERT INTO views (id, table_id, name, type, config, revision)
		VALUES ($1, $2, $3, $4, $5::jsonb, 1)
		RETURNING id, table_id, name, type, config, revision,
		          created_at, updated_at, deleted_at
	`, proposed.ID, proposed.TableID, proposed.Name, proposed.Type, string(encoded)))
	if err != nil {
		return domain.View{}, fmt.Errorf("insert view: %w", err)
	}
	if err := insertMetadataChange(ctx, tx, actorID, "viewChanged", created.TableID, created.ID, created.Revision); err != nil {
		return domain.View{}, err
	}
	if err := saveIdempotent(ctx, tx, actorID, idempotencyKey, fingerprint, created); err != nil {
		return domain.View{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.View{}, fmt.Errorf("commit create view: %w", err)
	}
	return created, nil
}

func (r *Repository) UpdateView(ctx context.Context, actorID, viewID string, expectedRevision int64, target domain.View) (domain.View, error) {
	if r == nil || r.db == nil {
		return domain.View{}, domain.ErrDependencyMissing
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.View{}, fmt.Errorf("begin update view: %w", err)
	}
	defer tx.Rollback()
	current, err := lockAccessibleView(ctx, tx, actorID, viewID)
	if err != nil {
		return domain.View{}, err
	}
	if err := checkViewRevision(current, expectedRevision); err != nil {
		return domain.View{}, err
	}
	if current.DeletedAt != nil {
		return domain.View{}, &domain.InvalidStateTransitionError{Resource: "view", ID: viewID, Action: "update", Current: "deleted"}
	}
	if current.Type != target.Type {
		return domain.View{}, domain.NewValidationError(domain.ValidationIssue{Path: "/type", Code: "format", Message: "View type is immutable in P0"})
	}
	if current.Name == target.Name && reflect.DeepEqual(current.Config, target.Config) {
		if err := tx.Commit(); err != nil {
			return domain.View{}, fmt.Errorf("commit view no-op: %w", err)
		}
		return current, nil
	}
	encoded, err := json.Marshal(target.Config)
	if err != nil {
		return domain.View{}, fmt.Errorf("encode view config: %w", err)
	}
	updated, err := scanView(tx.QueryRowContext(ctx, `
		UPDATE views
		SET name = $1, config = $2::jsonb, revision = revision + 1,
		    updated_at = clock_timestamp()
		WHERE id = $3
		RETURNING id, table_id, name, type, config, revision,
		          created_at, updated_at, deleted_at
	`, target.Name, string(encoded), viewID))
	if err != nil {
		return domain.View{}, fmt.Errorf("update view: %w", err)
	}
	if err := insertMetadataChange(ctx, tx, actorID, "viewChanged", updated.TableID, updated.ID, updated.Revision); err != nil {
		return domain.View{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.View{}, fmt.Errorf("commit update view: %w", err)
	}
	return updated, nil
}

func (r *Repository) DeleteView(ctx context.Context, actorID, viewID string, expectedRevision int64) error {
	if r == nil || r.db == nil {
		return domain.ErrDependencyMissing
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin delete view: %w", err)
	}
	defer tx.Rollback()
	current, err := lockAccessibleView(ctx, tx, actorID, viewID)
	if err != nil {
		return err
	}
	if err := checkViewRevision(current, expectedRevision); err != nil {
		return err
	}
	if current.DeletedAt != nil {
		return &domain.InvalidStateTransitionError{Resource: "view", ID: viewID, Action: "delete", Current: "deleted"}
	}
	deleted, err := scanView(tx.QueryRowContext(ctx, `
		UPDATE views
		SET deleted_at = clock_timestamp(), revision = revision + 1,
		    updated_at = clock_timestamp()
		WHERE id = $1
		RETURNING id, table_id, name, type, config, revision,
		          created_at, updated_at, deleted_at
	`, viewID))
	if err != nil {
		return fmt.Errorf("delete view: %w", err)
	}
	if err := insertMetadataChange(ctx, tx, actorID, "viewChanged", deleted.TableID, deleted.ID, deleted.Revision); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit delete view: %w", err)
	}
	return nil
}

func (r *Repository) RestoreView(ctx context.Context, actorID, viewID string, expectedRevision int64) (domain.View, error) {
	if r == nil || r.db == nil {
		return domain.View{}, domain.ErrDependencyMissing
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.View{}, fmt.Errorf("begin restore view: %w", err)
	}
	defer tx.Rollback()
	current, err := lockAccessibleView(ctx, tx, actorID, viewID)
	if err != nil {
		return domain.View{}, err
	}
	if err := checkViewRevision(current, expectedRevision); err != nil {
		return domain.View{}, err
	}
	if current.DeletedAt == nil {
		return domain.View{}, &domain.InvalidStateTransitionError{Resource: "view", ID: viewID, Action: "restore", Current: "active"}
	}
	restored, err := scanView(tx.QueryRowContext(ctx, `
		UPDATE views
		SET deleted_at = NULL, revision = revision + 1,
		    updated_at = clock_timestamp()
		WHERE id = $1
		RETURNING id, table_id, name, type, config, revision,
		          created_at, updated_at, deleted_at
	`, viewID))
	if err != nil {
		return domain.View{}, fmt.Errorf("restore view: %w", err)
	}
	if err := insertMetadataChange(ctx, tx, actorID, "viewChanged", restored.TableID, restored.ID, restored.Revision); err != nil {
		return domain.View{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.View{}, fmt.Errorf("commit restore view: %w", err)
	}
	return restored, nil
}

const accessibleViewSQL = `
	SELECT v.id, v.table_id, v.name, v.type, v.config, v.revision,
	       v.created_at, v.updated_at, v.deleted_at
	FROM views v
	JOIN tables t ON t.id = v.table_id
	JOIN bases b ON b.id = t.base_id
	JOIN workspaces w ON w.id = b.workspace_id
	WHERE v.id = $1 AND t.deleted_at IS NULL AND b.deleted_at IS NULL
	  AND w.actor_id = $2 AND w.deleted_at IS NULL
`

func scanView(row scanner) (domain.View, error) {
	var item domain.View
	var rawConfig []byte
	if err := row.Scan(
		&item.ID, &item.TableID, &item.Name, &item.Type, &rawConfig, &item.Revision,
		&item.CreatedAt, &item.UpdatedAt, &item.DeletedAt,
	); err != nil {
		return domain.View{}, err
	}
	config, err := decodeViewConfig(item.Type, rawConfig)
	if err != nil {
		return domain.View{}, err
	}
	item.Config = config
	return item, nil
}

func decodeViewConfig(viewType string, raw []byte) (any, error) {
	switch viewType {
	case "grid":
		var config domain.GridViewConfig
		if err := json.Unmarshal(raw, &config); err != nil {
			return nil, fmt.Errorf("decode Grid View config: %w", err)
		}
		if config.Projection == nil {
			config.Projection = make([]string, 0)
		}
		if config.ColumnOrder == nil {
			config.ColumnOrder = make([]string, 0)
		}
		if config.ColumnWidths == nil {
			config.ColumnWidths = make(map[string]int)
		}
		if config.FrozenFieldIDs == nil {
			config.FrozenFieldIDs = make([]string, 0)
		}
		if config.Sort == nil {
			config.Sort = make([]domain.SortSpec, 0)
		}
		return config, nil
	case "map":
		var config domain.MapViewConfig
		if err := json.Unmarshal(raw, &config); err != nil {
			return nil, fmt.Errorf("decode Map View config: %w", err)
		}
		return config, nil
	default:
		return nil, fmt.Errorf("unsupported persisted View type %q", viewType)
	}
}

func lockAccessibleView(ctx context.Context, tx *sql.Tx, actorID, viewID string) (domain.View, error) {
	item, err := scanView(tx.QueryRowContext(ctx, accessibleViewSQL+" FOR UPDATE OF v", viewID, actorID))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.View{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.View{}, fmt.Errorf("lock view: %w", err)
	}
	return item, nil
}

func checkViewRevision(current domain.View, expected int64) error {
	if current.Revision == expected {
		return nil
	}
	return &domain.RevisionConflictError{
		Resource: "view", ID: current.ID, ExpectedRevision: expected, CurrentRevision: current.Revision,
	}
}
