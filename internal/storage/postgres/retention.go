package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/Mahjong404/LoomTable-Server/internal/domain"
	"github.com/Mahjong404/LoomTable-Server/internal/maintenance"
)

func (r *Repository) CleanupRetention(
	ctx context.Context,
	changeAge time.Duration,
	idempotencyAge time.Duration,
	maxBatches int,
	batchSize int,
) (maintenance.CleanupStats, error) {
	if r == nil || r.db == nil {
		return maintenance.CleanupStats{}, domain.ErrDependencyMissing
	}
	// Short-lived scopes are independent of mutation/change retention settings.
	if _, err := r.db.ExecContext(ctx, `DELETE FROM selection_scopes WHERE id IN (SELECT id FROM selection_scopes WHERE expires_at<=clock_timestamp() ORDER BY expires_at LIMIT 8)`); err != nil {
		return maintenance.CleanupStats{}, err
	}
	if changeAge == 0 && idempotencyAge == 0 {
		return maintenance.CleanupStats{}, nil
	}
	connection, err := r.db.Conn(ctx)
	if err != nil {
		return maintenance.CleanupStats{}, fmt.Errorf("reserve retention connection: %w", err)
	}
	defer connection.Close()
	var acquired bool
	if err := connection.QueryRowContext(ctx, `SELECT pg_try_advisory_lock(hashtext('loomtable-retention-v1'))`).Scan(&acquired); err != nil {
		return maintenance.CleanupStats{}, fmt.Errorf("acquire retention lock: %w", err)
	}
	if !acquired {
		return maintenance.CleanupStats{}, nil
	}
	defer func() {
		connection.QueryRowContext(context.Background(), `SELECT pg_advisory_unlock(hashtext('loomtable-retention-v1'))`).Scan(new(bool))
	}()

	stats := maintenance.CleanupStats{}
	if changeAge > 0 {
		stats.AuditDeleted, err = cleanupAudit(ctx, connection, changeAge, maxBatches, batchSize)
		if err != nil {
			return maintenance.CleanupStats{}, err
		}
		stats.ChangesDeleted, err = cleanupChanges(ctx, connection, changeAge, maxBatches, batchSize)
		if err != nil {
			return maintenance.CleanupStats{}, err
		}
	}
	if idempotencyAge > 0 {
		stats.IdempotencyDeleted, err = cleanupRetentionTable(ctx, connection, "idempotency_keys", "created_at", idempotencyAge, maxBatches, batchSize)
		if err != nil {
			return maintenance.CleanupStats{}, err
		}
	}
	return stats, nil
}

func cleanupAudit(ctx context.Context, connection *sql.Conn, age time.Duration, maxBatches, batchSize int) (int64, error) {
	var total int64
	for batch := 0; batch < maxBatches; batch++ {
		var deleted int64
		err := connection.QueryRowContext(ctx, `
		 WITH doomed AS (
		  SELECT sequence FROM audit_events
		  WHERE occurred_at < clock_timestamp()-make_interval(secs => $1)
		  ORDER BY occurred_at, sequence LIMIT $2
		 ), deleted AS (
		  DELETE FROM audit_events target USING doomed WHERE target.sequence=doomed.sequence
		  RETURNING target.table_id,target.sequence,target.occurred_at
		 ), watermarks AS (
		  INSERT INTO audit_retention_watermarks(table_id,expired_through,expired_through_time)
		  SELECT table_id,MAX(sequence),MAX(occurred_at) FROM deleted GROUP BY table_id
		  ON CONFLICT(table_id) DO UPDATE SET expired_through=GREATEST(audit_retention_watermarks.expired_through,EXCLUDED.expired_through), generation=audit_retention_watermarks.generation+1, expired_through_time=GREATEST(audit_retention_watermarks.expired_through_time,EXCLUDED.expired_through_time)
		  RETURNING table_id
		 ) SELECT COUNT(*) FROM deleted
		`, int64(age/time.Second), batchSize).Scan(&deleted)
		if err != nil {
			return total, fmt.Errorf("cleanup audit: %w", err)
		}
		total += deleted
		if deleted < int64(batchSize) {
			break
		}
	}
	return total, nil
}

func cleanupChanges(ctx context.Context, connection *sql.Conn, age time.Duration, maxBatches, batchSize int) (int64, error) {
	var deletedTotal int64
	for batch := 0; batch < maxBatches; batch++ {
		tx, err := connection.BeginTx(ctx, nil)
		if err != nil {
			return deletedTotal, fmt.Errorf("begin Change retention batch: %w", err)
		}
		rows, err := tx.QueryContext(ctx, `
			WITH doomed AS (
				SELECT change_sequence
				FROM changes
				WHERE occurred_at < clock_timestamp() - make_interval(secs => $1)
				ORDER BY occurred_at ASC, change_sequence ASC
				LIMIT $2
			), deleted AS (
				DELETE FROM changes target
				USING doomed
				WHERE target.change_sequence = doomed.change_sequence
				RETURNING target.table_id, target.change_sequence
			)
			SELECT table_id, MAX(change_sequence), COUNT(*)
			FROM deleted
			GROUP BY table_id
		`, int64(age/time.Second), batchSize)
		if err != nil {
			_ = tx.Rollback()
			return deletedTotal, fmt.Errorf("delete expired Changes: %w", err)
		}
		var batchDeleted int64
		type expiredGroup struct {
			tableID        string
			expiredThrough int64
		}
		groups := make([]expiredGroup, 0)
		for rows.Next() {
			var tableID string
			var expiredThrough, count int64
			if err := rows.Scan(&tableID, &expiredThrough, &count); err != nil {
				rows.Close()
				_ = tx.Rollback()
				return deletedTotal, fmt.Errorf("scan expired Change watermark: %w", err)
			}
			groups = append(groups, expiredGroup{tableID: tableID, expiredThrough: expiredThrough})
			batchDeleted += count
		}
		if err := rows.Close(); err != nil {
			_ = tx.Rollback()
			return deletedTotal, fmt.Errorf("close expired Change rows: %w", err)
		}
		if err := rows.Err(); err != nil {
			_ = tx.Rollback()
			return deletedTotal, fmt.Errorf("read expired Change rows: %w", err)
		}
		for _, group := range groups {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO change_retention_watermarks (table_id, expired_through, updated_at)
				VALUES ($1, $2, clock_timestamp())
				ON CONFLICT (table_id) DO UPDATE
				SET expired_through = GREATEST(change_retention_watermarks.expired_through, EXCLUDED.expired_through),
				    updated_at = clock_timestamp()
			`, group.tableID, group.expiredThrough); err != nil {
				_ = tx.Rollback()
				return deletedTotal, fmt.Errorf("save Change retention watermark: %w", err)
			}
		}
		if err := tx.Commit(); err != nil {
			return deletedTotal, fmt.Errorf("commit Change retention batch: %w", err)
		}
		deletedTotal += batchDeleted
		if batchDeleted < int64(batchSize) {
			break
		}
	}
	return deletedTotal, nil
}

func cleanupRetentionTable(
	ctx context.Context,
	connection *sql.Conn,
	table string,
	timestampColumn string,
	age time.Duration,
	maxBatches int,
	batchSize int,
) (int64, error) {
	var deletedTotal int64
	statement := `
		WITH doomed AS (
			SELECT ctid
			FROM ` + table + `
			WHERE ` + timestampColumn + ` < clock_timestamp() - make_interval(secs => $1)
			ORDER BY ` + timestampColumn + ` ASC, ctid ASC
			LIMIT $2
		)
		DELETE FROM ` + table + ` target
		USING doomed
		WHERE target.ctid = doomed.ctid
	`
	for batch := 0; batch < maxBatches; batch++ {
		tx, err := connection.BeginTx(ctx, nil)
		if err != nil {
			return deletedTotal, fmt.Errorf("begin %s retention batch: %w", table, err)
		}
		result, err := tx.ExecContext(ctx, statement, int64(age/time.Second), batchSize)
		if err != nil {
			_ = tx.Rollback()
			return deletedTotal, fmt.Errorf("delete expired %s: %w", table, err)
		}
		deleted, err := result.RowsAffected()
		if err != nil {
			_ = tx.Rollback()
			return deletedTotal, fmt.Errorf("count expired %s: %w", table, err)
		}
		if err := tx.Commit(); err != nil {
			return deletedTotal, fmt.Errorf("commit %s retention batch: %w", table, err)
		}
		deletedTotal += deleted
		if deleted < int64(batchSize) {
			break
		}
	}
	return deletedTotal, nil
}
