package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/Mahjong404/LoomTable-Server/internal/domain"
	loomrecord "github.com/Mahjong404/LoomTable-Server/internal/record"
)

func (r *Repository) PullHistory(ctx context.Context, actorID, tableID string, filter loomrecord.HistoryFilter, before int64, limit int) (loomrecord.StoredHistoryPage, error) {
	if r == nil || r.db == nil {
		return loomrecord.StoredHistoryPage{}, domain.ErrDependencyMissing
	}
	visible, err := activeTableVisible(ctx, r.db, actorID, tableID)
	if err != nil {
		return loomrecord.StoredHistoryPage{}, err
	}
	if !visible {
		return loomrecord.StoredHistoryPage{}, domain.ErrNotFound
	}
	var expiredThrough int64
	if err := r.db.QueryRowContext(ctx, `
		SELECT COALESCE((SELECT expired_through FROM change_retention_watermarks WHERE table_id = $1), 0)
	`, tableID).Scan(&expiredThrough); err != nil {
		return loomrecord.StoredHistoryPage{}, fmt.Errorf("read Change retention watermark: %w", err)
	}
	if before <= expiredThrough {
		return loomrecord.StoredHistoryPage{}, &domain.CursorExpiredError{}
	}
	builder := &querySQLBuilder{}
	parts := []string{"table_id = " + builder.add(tableID), "change_sequence < " + builder.add(before)}
	if filter.RecordID != "" {
		parts = append(parts, "record_id = "+builder.add(filter.RecordID))
	}
	if filter.Kind != "" {
		parts = append(parts, "kind = "+builder.add(filter.Kind))
	}
	if filter.ActorID != "" {
		parts = append(parts, "actor_id = "+builder.add(filter.ActorID))
	}
	if filter.Since != nil {
		parts = append(parts, "occurred_at >= "+builder.add(filter.Since.UTC()))
	}
	if filter.Until != nil {
		parts = append(parts, "occurred_at <= "+builder.add(filter.Until.UTC()))
	}
	if filter.FieldID != "" {
		parts = append(parts, "fields @> jsonb_build_array(jsonb_build_object('fieldId', ("+builder.add(filter.FieldID)+")::text))")
	}
	query := `
		SELECT change_sequence, id, kind, table_id, record_id, object_id, revision, actor_id, occurred_at, fields, primary_field_text
		FROM changes
		WHERE ` + strings.Join(parts, " AND ") + `
		ORDER BY change_sequence DESC
		LIMIT ` + builder.add(limit+1)
	rows, err := r.db.QueryContext(ctx, query, builder.args...)
	if err != nil {
		return loomrecord.StoredHistoryPage{}, fmt.Errorf("pull History: %w", err)
	}
	defer rows.Close()
	items := make([]loomrecord.Change, 0, limit+1)
	for rows.Next() {
		item, err := scanChange(rows)
		if err != nil {
			return loomrecord.StoredHistoryPage{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return loomrecord.StoredHistoryPage{}, fmt.Errorf("pull History: %w", err)
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	return loomrecord.StoredHistoryPage{Items: items, HasMore: hasMore}, nil
}
