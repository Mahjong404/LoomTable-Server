package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Mahjong404/LoomTable-Server/internal/domain"
	"github.com/Mahjong404/LoomTable-Server/internal/id"
	loomrecord "github.com/Mahjong404/LoomTable-Server/internal/record"
)

func (r *Repository) MoveRecord(
	ctx context.Context,
	actorID string,
	tableID string,
	recordID string,
	beforeID *string,
	afterID *string,
) (loomrecord.StoredRecordOrderResult, error) {
	if r == nil || r.db == nil {
		return loomrecord.StoredRecordOrderResult{}, domain.ErrDependencyMissing
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return loomrecord.StoredRecordOrderResult{}, fmt.Errorf("begin record move: %w", err)
	}
	defer tx.Rollback()
	if err := lockActor(ctx, tx, actorID); err != nil {
		return loomrecord.StoredRecordOrderResult{}, err
	}
	if err := lockActiveTable(ctx, tx, actorID, tableID); err != nil {
		return loomrecord.StoredRecordOrderResult{}, err
	}
	fields, err := loadFieldDefinitions(ctx, tx, tableID)
	if err != nil {
		return loomrecord.StoredRecordOrderResult{}, err
	}
	current, err := lockRecord(ctx, tx, tableID, recordID)
	if err != nil {
		return loomrecord.StoredRecordOrderResult{}, err
	}
	if current.DeletedAt != nil {
		return loomrecord.StoredRecordOrderResult{}, &domain.InvalidStateTransitionError{Resource: "record", ID: current.ID, Action: "move", Current: "deleted"}
	}

	target, err := moveTargetPosition(ctx, tx, tableID, recordID, beforeID, afterID)
	if err != nil {
		return loomrecord.StoredRecordOrderResult{}, err
	}
	moved, err := scanRecord(tx.QueryRowContext(ctx, `
		UPDATE records
		SET position = $1, updated_at = clock_timestamp()
		WHERE id = $2
		RETURNING id, table_id, revision, values, created_at, updated_at, deleted_at
	`, target, recordID))
	if err != nil {
		return loomrecord.StoredRecordOrderResult{}, fmt.Errorf("move record: %w", err)
	}
	if err := insertRecordChange(ctx, tx, actorID, moved, "recordMoved", nil, primaryFieldText(moved.Values, fields)); err != nil {
		return loomrecord.StoredRecordOrderResult{}, err
	}
	return finishRecordOrderChange(ctx, tx, tableID, moved)
}

func (r *Repository) DuplicateRecord(
	ctx context.Context,
	actorID string,
	tableID string,
	recordID string,
) (loomrecord.StoredRecordOrderResult, error) {
	if r == nil || r.db == nil {
		return loomrecord.StoredRecordOrderResult{}, domain.ErrDependencyMissing
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return loomrecord.StoredRecordOrderResult{}, fmt.Errorf("begin record duplicate: %w", err)
	}
	defer tx.Rollback()
	if err := lockActor(ctx, tx, actorID); err != nil {
		return loomrecord.StoredRecordOrderResult{}, err
	}
	if err := lockActiveTable(ctx, tx, actorID, tableID); err != nil {
		return loomrecord.StoredRecordOrderResult{}, err
	}
	fields, err := loadFieldDefinitions(ctx, tx, tableID)
	if err != nil {
		return loomrecord.StoredRecordOrderResult{}, err
	}
	current, err := lockRecord(ctx, tx, tableID, recordID)
	if err != nil {
		return loomrecord.StoredRecordOrderResult{}, err
	}
	if current.DeletedAt != nil {
		return loomrecord.StoredRecordOrderResult{}, &domain.InvalidStateTransitionError{Resource: "record", ID: current.ID, Action: "duplicate", Current: "deleted"}
	}
	values, queryValues, searchText, err := loomrecord.NormalizeCreateValues(current.Values, fields)
	if err != nil {
		return loomrecord.StoredRecordOrderResult{}, err
	}
	recordID2, err := id.New(id.RecordPrefix)
	if err != nil {
		return loomrecord.StoredRecordOrderResult{}, fmt.Errorf("generate record ID: %w", err)
	}
	created, err := insertRecord(ctx, tx, recordID2, tableID, values, queryValues, searchText)
	if err != nil {
		return loomrecord.StoredRecordOrderResult{}, err
	}
	if err := insertRecordChange(ctx, tx, actorID, created, "recordCreated", nil, primaryFieldText(created.Values, fields)); err != nil {
		return loomrecord.StoredRecordOrderResult{}, err
	}
	return finishRecordOrderChange(ctx, tx, tableID, created)
}

func moveTargetPosition(ctx context.Context, tx *sql.Tx, tableID, recordID string, beforeID, afterID *string) (float64, error) {
	anchor := func(recordID string, path string) (float64, error) {
		var position float64
		err := tx.QueryRowContext(ctx, `
			SELECT position FROM records
			WHERE id = $1 AND table_id = $2 AND deleted_at IS NULL
		`, recordID, tableID).Scan(&position)
		if errors.Is(err, sql.ErrNoRows) {
			return 0, domain.NewValidationError(domain.ValidationIssue{Path: path, Code: "invalidReference", Message: "Record is foreign, unknown, or deleted"})
		}
		if err != nil {
			return 0, fmt.Errorf("read anchor record position: %w", err)
		}
		return position, nil
	}
	neighbor := func(query string, args ...any) (*float64, error) {
		var position float64
		err := tx.QueryRowContext(ctx, query, args...).Scan(&position)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("read neighbor record position: %w", err)
		}
		return &position, nil
	}

	switch {
	case beforeID == nil && afterID == nil:
		var target float64
		if err := tx.QueryRowContext(ctx, `
			SELECT COALESCE(max(position) + 1024.0, 1024.0)
			FROM records WHERE table_id = $1 AND deleted_at IS NULL
		`, tableID).Scan(&target); err != nil {
			return 0, fmt.Errorf("read table position tail: %w", err)
		}
		return target, nil
	case beforeID != nil && afterID != nil:
		before, err := anchor(*beforeID, "/beforeRecordId")
		if err != nil {
			return 0, err
		}
		after, err := anchor(*afterID, "/afterRecordId")
		if err != nil {
			return 0, err
		}
		if after >= before {
			return 0, domain.NewValidationError(domain.ValidationIssue{Path: "/", Code: "format", Message: "afterRecordId must be positioned before beforeRecordId"})
		}
		return (after + before) / 2, nil
	case beforeID != nil:
		before, err := anchor(*beforeID, "/beforeRecordId")
		if err != nil {
			return 0, err
		}
		previous, err := neighbor(`
			SELECT max(position) FROM records
			WHERE table_id = $1 AND deleted_at IS NULL AND position < $2 AND id <> $3
		`, tableID, before, recordID)
		if err != nil {
			return 0, err
		}
		lower := before - 1024.0
		if previous != nil {
			lower = *previous
		}
		return (lower + before) / 2, nil
	default:
		after, err := anchor(*afterID, "/afterRecordId")
		if err != nil {
			return 0, err
		}
		next, err := neighbor(`
			SELECT min(position) FROM records
			WHERE table_id = $1 AND deleted_at IS NULL AND position > $2 AND id <> $3
		`, tableID, after, recordID)
		if err != nil {
			return 0, err
		}
		upper := after + 1024.0
		if next != nil {
			upper = *next
		}
		return (after + upper) / 2, nil
	}
}

func finishRecordOrderChange(ctx context.Context, tx *sql.Tx, tableID string, item loomrecord.Record) (loomrecord.StoredRecordOrderResult, error) {
	var changeSequence int64
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(change_sequence), 0)
		FROM changes
		WHERE table_id = $1
	`, tableID).Scan(&changeSequence); err != nil {
		return loomrecord.StoredRecordOrderResult{}, fmt.Errorf("read table change tail: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return loomrecord.StoredRecordOrderResult{}, fmt.Errorf("commit record order change: %w", err)
	}
	return loomrecord.StoredRecordOrderResult{Record: item, ChangeSequence: changeSequence}, nil
}
