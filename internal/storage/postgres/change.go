package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/Mahjong404/LoomTable-Server/internal/domain"
	loomrecord "github.com/Mahjong404/LoomTable-Server/internal/record"
)

func (r *Repository) ChangeTail(ctx context.Context, actorID, tableID string) (int64, error) {
	if r == nil || r.db == nil {
		return 0, domain.ErrDependencyMissing
	}
	visible, err := activeTableVisible(ctx, r.db, actorID, tableID)
	if err != nil {
		return 0, err
	}
	if !visible {
		return 0, domain.ErrNotFound
	}
	var tail int64
	if err := r.db.QueryRowContext(ctx, "SELECT COALESCE(MAX(change_sequence), 0) FROM changes WHERE table_id = $1", tableID).Scan(&tail); err != nil {
		return 0, fmt.Errorf("read Change tail: %w", err)
	}
	return tail, nil
}

func (r *Repository) PullChanges(ctx context.Context, actorID, tableID string, after int64, limit int) (loomrecord.StoredChangePage, error) {
	if r == nil || r.db == nil {
		return loomrecord.StoredChangePage{}, domain.ErrDependencyMissing
	}
	visible, err := activeTableVisible(ctx, r.db, actorID, tableID)
	if err != nil {
		return loomrecord.StoredChangePage{}, err
	}
	if !visible {
		return loomrecord.StoredChangePage{}, domain.ErrNotFound
	}
	var tail int64
	if err := r.db.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(change_sequence), 0)
		FROM changes WHERE table_id = $1
	`, tableID).Scan(&tail); err != nil {
		return loomrecord.StoredChangePage{}, fmt.Errorf("read Change retention window: %w", err)
	}
	var expiredThrough int64
	if err := r.db.QueryRowContext(ctx, `
		SELECT COALESCE((SELECT expired_through FROM change_retention_watermarks WHERE table_id = $1), 0)
	`, tableID).Scan(&expiredThrough); err != nil {
		return loomrecord.StoredChangePage{}, fmt.Errorf("read Change retention watermark: %w", err)
	}
	if after > tail {
		if after > expiredThrough {
			return loomrecord.StoredChangePage{}, &domain.InvalidCursorError{}
		}
	}
	if after < expiredThrough {
		return loomrecord.StoredChangePage{}, &domain.CursorExpiredError{}
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT change_sequence, id, kind, table_id, record_id, object_id, revision, actor_id, occurred_at
		FROM changes
		WHERE table_id = $1 AND change_sequence > $2
		ORDER BY change_sequence ASC
		LIMIT $3
	`, tableID, after, limit+1)
	if err != nil {
		return loomrecord.StoredChangePage{}, fmt.Errorf("pull Changes: %w", err)
	}
	defer rows.Close()
	items := make([]loomrecord.Change, 0, limit+1)
	for rows.Next() {
		var item loomrecord.Change
		var recordID, objectID sql.NullString
		if err := rows.Scan(&item.Sequence, &item.ID, &item.Kind, &item.TableID, &recordID, &objectID, &item.Revision, &item.ActorID, &item.OccurredAt); err != nil {
			return loomrecord.StoredChangePage{}, fmt.Errorf("scan Change: %w", err)
		}
		if recordID.Valid {
			item.RecordID = recordID.String
		}
		if objectID.Valid {
			item.ObjectID = objectID.String
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return loomrecord.StoredChangePage{}, fmt.Errorf("pull Changes: %w", err)
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	next := after
	if len(items) > 0 {
		next = items[len(items)-1].Sequence
	}
	return loomrecord.StoredChangePage{Items: items, NextSequence: next, HasMore: hasMore}, nil
}
