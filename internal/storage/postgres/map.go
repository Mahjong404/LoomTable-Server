package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Mahjong404/LoomTable-Server/internal/domain"
	loomrecord "github.com/Mahjong404/LoomTable-Server/internal/record"
)

func (r *Repository) ResolveMap(ctx context.Context, actorID, viewID string) (loomrecord.QueryMetadata, error) {
	if r == nil || r.db == nil {
		return loomrecord.QueryMetadata{}, domain.ErrDependencyMissing
	}
	view, err := scanView(r.db.QueryRowContext(ctx, accessibleViewSQL+" AND v.deleted_at IS NULL", viewID, actorID))
	if errors.Is(err, sql.ErrNoRows) {
		return loomrecord.QueryMetadata{}, domain.ErrNotFound
	}
	if err != nil {
		return loomrecord.QueryMetadata{}, fmt.Errorf("resolve Map View: %w", err)
	}
	return r.ResolveQuery(ctx, actorID, view.TableID, viewID)
}

func (r *Repository) LoadMapSnapshot(
	ctx context.Context,
	actorID string,
	metadata loomrecord.QueryMetadata,
	plan loomrecord.QueryPlan,
	locationFieldID string,
	viewport *loomrecord.MapViewport,
) (loomrecord.StoredMapSnapshot, error) {
	if r == nil || r.db == nil {
		return loomrecord.StoredMapSnapshot{}, domain.ErrDependencyMissing
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return loomrecord.StoredMapSnapshot{}, fmt.Errorf("begin Map query: %w", err)
	}
	defer tx.Rollback()
	if err := locklessActiveTable(ctx, tx, actorID, metadata.TableID); err != nil {
		return loomrecord.StoredMapSnapshot{}, err
	}
	builder := &querySQLBuilder{}
	where, err := buildRecordWhere(builder, metadata.TableID, plan)
	if err != nil {
		return loomrecord.StoredMapSnapshot{}, err
	}
	locationParameter := "(" + builder.add(locationFieldID) + ")::text"
	latExpression := "(r.query_values -> " + locationParameter + " ->> 'lat')::double precision"
	lngExpression := "(r.query_values -> " + locationParameter + " ->> 'lng')::double precision"
	if viewport != nil {
		spatial := buildViewportPredicate(builder, latExpression, lngExpression, viewport.Boxes)
		where += " AND (" + spatial + ")"
	}
	primaryParameter := "(" + builder.add(metadata.PrimaryFieldID) + ")::text"
	rows, err := tx.QueryContext(ctx, `
		SELECT r.id, r.table_id, r.revision, r.created_at, r.updated_at, r.deleted_at,
		       `+latExpression+`, `+lngExpression+`, COALESCE(r.values ->> `+primaryParameter+`, '')
		FROM records r
		WHERE `+where+`
		ORDER BY r.id ASC
	`, builder.args...)
	if err != nil {
		return loomrecord.StoredMapSnapshot{}, fmt.Errorf("query Map records: %w", err)
	}
	defer rows.Close()
	records := make([]loomrecord.MapRecord, 0)
	for rows.Next() {
		var item loomrecord.MapRecord
		var lat, lng sql.NullFloat64
		if err := rows.Scan(
			&item.ID, &item.TableID, &item.Revision, &item.CreatedAt, &item.UpdatedAt, &item.DeletedAt,
			&lat, &lng, &item.PrimaryFieldText,
		); err != nil {
			return loomrecord.StoredMapSnapshot{}, fmt.Errorf("scan Map record: %w", err)
		}
		if lat.Valid && lng.Valid {
			item.Position = &loomrecord.MapCoordinate{Lat: lat.Float64, Lng: lng.Float64}
		}
		records = append(records, item)
	}
	if err := rows.Err(); err != nil {
		return loomrecord.StoredMapSnapshot{}, fmt.Errorf("query Map records: %w", err)
	}
	result := loomrecord.StoredMapSnapshot{Records: records}
	if err := tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(change_sequence), 0) FROM changes WHERE table_id = $1", metadata.TableID).Scan(&result.ChangeSequence); err != nil {
		return loomrecord.StoredMapSnapshot{}, fmt.Errorf("read Map Change tail: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return loomrecord.StoredMapSnapshot{}, fmt.Errorf("commit Map query: %w", err)
	}
	return result, nil
}

func (r *Repository) QueryMapClusterRecords(
	ctx context.Context,
	actorID string,
	metadata loomrecord.QueryMetadata,
	plan loomrecord.QueryPlan,
	locationFieldID string,
	boxes []loomrecord.MapViewportBox,
	position *loomrecord.QueryPosition,
	limit int,
	includeTotal bool,
) (loomrecord.StoredQueryPage, error) {
	if r == nil || r.db == nil {
		return loomrecord.StoredQueryPage{}, domain.ErrDependencyMissing
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return loomrecord.StoredQueryPage{}, fmt.Errorf("begin Map cluster query: %w", err)
	}
	defer tx.Rollback()
	if err := locklessActiveTable(ctx, tx, actorID, metadata.TableID); err != nil {
		return loomrecord.StoredQueryPage{}, err
	}
	builder := &querySQLBuilder{}
	where, err := buildRecordWhere(builder, metadata.TableID, plan)
	if err != nil {
		return loomrecord.StoredQueryPage{}, err
	}
	locationParameter := "(" + builder.add(locationFieldID) + ")::text"
	latExpression := "(r.query_values -> " + locationParameter + " ->> 'lat')::double precision"
	lngExpression := "(r.query_values -> " + locationParameter + " ->> 'lng')::double precision"
	where += " AND (" + buildViewportPredicate(builder, latExpression, lngExpression, boxes) + ")"
	countWhere := where
	countArgs := append([]any(nil), builder.args...)
	result := loomrecord.StoredQueryPage{}
	if includeTotal {
		var total int64
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM records r WHERE "+countWhere, countArgs...).Scan(&total); err != nil {
			return loomrecord.StoredQueryPage{}, fmt.Errorf("count Map cluster Records: %w", err)
		}
		result.TotalCount = &total
	}
	if position != nil {
		if len(position.SortValues) != 1 || position.RecordID == "" {
			return loomrecord.StoredQueryPage{}, &domain.InvalidCursorError{}
		}
		createdAt, ok := position.SortValues[0].(string)
		if !ok {
			return loomrecord.StoredQueryPage{}, &domain.InvalidCursorError{}
		}
		createdParameter := builder.add(createdAt)
		idParameter := builder.add(position.RecordID)
		where += " AND (r.created_at > " + createdParameter + " OR (r.created_at IS NOT DISTINCT FROM " + createdParameter + " AND r.id > " + idParameter + "))"
	}
	query := `
		SELECT r.id, r.table_id, r.revision, r.values, r.created_at, r.updated_at, r.deleted_at
		FROM records r
		WHERE ` + where + `
		ORDER BY r.created_at ASC, r.id ASC
		LIMIT ` + builder.add(limit+1)
	rows, err := tx.QueryContext(ctx, query, builder.args...)
	if err != nil {
		return loomrecord.StoredQueryPage{}, fmt.Errorf("query Map cluster Records: %w", err)
	}
	defer rows.Close()
	items := make([]loomrecord.Record, 0, limit+1)
	for rows.Next() {
		var item loomrecord.Record
		var values []byte
		if err := rows.Scan(&item.ID, &item.TableID, &item.Revision, &values, &item.CreatedAt, &item.UpdatedAt, &item.DeletedAt); err != nil {
			return loomrecord.StoredQueryPage{}, fmt.Errorf("scan Map cluster Record: %w", err)
		}
		if err := json.Unmarshal(values, &item.Values); err != nil {
			return loomrecord.StoredQueryPage{}, fmt.Errorf("decode Map cluster Record values: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return loomrecord.StoredQueryPage{}, fmt.Errorf("query Map cluster Records: %w", err)
	}
	result.HasMore = len(items) > limit
	if result.HasMore {
		items = items[:limit]
		last := items[len(items)-1]
		result.NextPosition = &loomrecord.QueryPosition{SortValues: []any{last.CreatedAt}, RecordID: last.ID}
	}
	result.Items = items
	if err := tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(change_sequence), 0) FROM changes WHERE table_id = $1", metadata.TableID).Scan(&result.ChangeSequence); err != nil {
		return loomrecord.StoredQueryPage{}, fmt.Errorf("read Map cluster Change tail: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return loomrecord.StoredQueryPage{}, fmt.Errorf("commit Map cluster query: %w", err)
	}
	return result, nil
}

func buildViewportPredicate(builder *querySQLBuilder, latExpression, lngExpression string, boxes []loomrecord.MapViewportBox) string {
	parts := make([]string, 0, len(boxes))
	for _, box := range boxes {
		parts = append(parts, "("+
			lngExpression+" >= "+builder.add(box.West)+" AND "+lngExpression+" <= "+builder.add(box.East)+" AND "+
			latExpression+" >= "+builder.add(box.South)+" AND "+latExpression+" <= "+builder.add(box.North)+")")
	}
	if len(parts) == 0 {
		return "FALSE"
	}
	return strings.Join(parts, " OR ")
}
