package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/Mahjong404/LoomTable-Server/internal/domain"
	record "github.com/Mahjong404/LoomTable-Server/internal/record"
	"strings"
)

func (r *Repository) ReadAudit(ctx context.Context, actorID, tableID string, filter record.AuditFilter, position record.AuditPosition, limit int) (record.StoredAuditPage, error) {
	if r == nil || r.db == nil {
		return record.StoredAuditPage{}, domain.ErrDependencyMissing
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return record.StoredAuditPage{}, err
	}
	defer tx.Rollback()
	visible, err := activeTableVisible(ctx, tx, actorID, tableID)
	if err != nil {
		return record.StoredAuditPage{}, err
	}
	if !visible {
		return record.StoredAuditPage{}, domain.ErrNotFound
	}
	result := record.StoredAuditPage{AuditPage: record.AuditPage{Items: []record.AuditEvent{}}}
	if err := tx.QueryRowContext(ctx, `SELECT captured_since,uninstrumented_writer FROM audit_coverage WHERE singleton`).Scan(&result.Availability.CapturedSince, &result.Availability.UninstrumentedWriter); err != nil {
		return result, err
	}
	var earliest sql.NullTime
	var high int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence),0),MIN(occurred_at),COALESCE(bool_or(payload_version=0),false) FROM audit_events WHERE table_id=$1`, tableID).Scan(&high, &earliest, &result.Availability.HasLegacyEvents); err != nil {
		return result, err
	}
	if earliest.Valid {
		result.Availability.EarliestReadableAt = &earliest.Time
	}
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE((SELECT expired_through FROM audit_retention_watermarks WHERE table_id=$1),0)`, tableID).Scan(&result.Availability.ExpiredThrough); err != nil {
		return result, err
	}
	var generation int64
	var expiredTime sql.NullTime
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE((SELECT generation FROM audit_retention_watermarks WHERE table_id=$1),0),(SELECT expired_through_time FROM audit_retention_watermarks WHERE table_id=$1),COALESCE((SELECT expired_through FROM change_retention_watermarks WHERE table_id=$1),0)`, tableID).Scan(&generation, &expiredTime, &result.Availability.LegacyExpiredThrough); err != nil {
		return result, err
	}
	if expiredTime.Valid {
		result.Availability.ExpiredThroughTime = &expiredTime.Time
	}
	if position.Continuation {
		if position.Watermark != generation {
			return result, &domain.CursorExpiredError{}
		}
	} else {
		position.High = high
		position.Before = high + 1
		position.Watermark = generation
	}
	result.Position = position
	builder := &querySQLBuilder{}
	parts := []string{"table_id=" + builder.add(tableID), "sequence<=" + builder.add(position.High)}
	if filter.Category == "record" {
		parts = append(parts, "category='record'")
	}
	for _, predicate := range []struct{ column, value string }{{"record_id", filter.RecordID}, {"view_id", filter.ViewID}, {"actor_id", filter.ActorID}, {"operation_id", filter.OperationID}} {
		if predicate.value != "" {
			parts = append(parts, predicate.column+"="+builder.add(predicate.value))
		}
	}
	if filter.FieldID != "" {
		value := builder.add(filter.FieldID)
		parts = append(parts, "(field_id="+value+" OR payload->'fields' @> jsonb_build_array(jsonb_build_object('fieldId',("+value+")::text)) OR payload->'legacyFields' @> jsonb_build_array(jsonb_build_object('fieldId',("+value+")::text)))")
	}
	if filter.ClientMutationID != "" {
		parts = append(parts, "client_mutation_id="+builder.add(filter.ClientMutationID), "actor_id="+builder.add(actorID))
	}
	if filter.Since != nil {
		parts = append(parts, "occurred_at>="+builder.add(*filter.Since))
	}
	if filter.Until != nil {
		parts = append(parts, "occurred_at<="+builder.add(*filter.Until))
	}
	where := strings.Join(parts, " AND ")
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM audit_events WHERE "+where, builder.args...).Scan(&result.Count); err != nil {
		return result, err
	}
	pageWhere := where + " AND sequence<" + builder.add(position.Before)
	rows, err := tx.QueryContext(ctx, `SELECT sequence,id,operation_id,array_to_json(change_ids),table_id,object_id,record_id,field_id,view_id,actor_id,client_mutation_id,occurred_at,category,kind,payload_version,payload FROM audit_events WHERE `+pageWhere+" ORDER BY sequence DESC LIMIT "+builder.add(limit+1), builder.args...)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var event record.AuditEvent
		var op, rid, fid, vid, actor, mutation sql.NullString
		var changes []byte
		if err := rows.Scan(&event.Sequence, &event.ID, &op, &changes, &event.TableID, &event.ObjectID, &rid, &fid, &vid, &actor, &mutation, &event.OccurredAt, &event.Category, &event.Kind, &event.PayloadVersion, &event.Payload); err != nil {
			rows.Close()
			return result, err
		}
		if op.Valid {
			value := op.String
			event.OperationID = &value
		}
		if actor.Valid {
			value := actor.String
			event.ActorID = &value
		}
		event.RecordID = rid.String
		event.FieldID = fid.String
		event.ViewID = vid.String
		event.ClientMutationID = mutation.String
		if err := json.Unmarshal(changes, &event.ChangeIDs); err != nil {
			rows.Close()
			return result, err
		}
		result.Items = append(result.Items, event)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, fmt.Errorf("read audit: %w", err)
	}
	if len(result.Items) > limit {
		result.HasMore = true
		result.Items = result.Items[:limit]
	}
	if err := tx.Commit(); err != nil {
		return result, err
	}
	return result, nil
}
