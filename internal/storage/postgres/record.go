package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/Mahjong404/LoomTable-Server/internal/domain"
	"github.com/Mahjong404/LoomTable-Server/internal/id"
	loomrecord "github.com/Mahjong404/LoomTable-Server/internal/record"
)

func (r *Repository) GetRecord(ctx context.Context, actorID, recordID string) (loomrecord.Record, error) {
	if r == nil || r.db == nil {
		return loomrecord.Record{}, domain.ErrDependencyMissing
	}
	item, err := scanRecord(r.db.QueryRowContext(ctx, `
		SELECT r.id, r.table_id, r.revision, r.values, r.created_at, r.updated_at, r.deleted_at
		FROM records r
		JOIN tables t ON t.id = r.table_id
		JOIN bases b ON b.id = t.base_id
		JOIN workspaces w ON w.id = b.workspace_id
		WHERE r.id = $1 AND t.deleted_at IS NULL AND b.deleted_at IS NULL
		  AND w.actor_id = $2 AND w.deleted_at IS NULL
	`, recordID, actorID))
	if errors.Is(err, sql.ErrNoRows) {
		return loomrecord.Record{}, domain.ErrNotFound
	}
	if err != nil {
		return loomrecord.Record{}, fmt.Errorf("get record: %w", err)
	}
	return item, nil
}

func (r *Repository) MutateRecords(
	ctx context.Context,
	actorID string,
	tableID string,
	clientMutationID string,
	fingerprint [32]byte,
	commands []loomrecord.Command,
) (loomrecord.StoredMutationResult, error) {
	if r == nil || r.db == nil {
		return loomrecord.StoredMutationResult{}, domain.ErrDependencyMissing
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return loomrecord.StoredMutationResult{}, fmt.Errorf("begin record mutation: %w", err)
	}
	defer tx.Rollback()

	if err := lockActor(ctx, tx, actorID); err != nil {
		return loomrecord.StoredMutationResult{}, err
	}
	if replay, found, err := replayIdempotent[loomrecord.StoredMutationResult](ctx, tx, actorID, clientMutationID, fingerprint); err != nil {
		return loomrecord.StoredMutationResult{}, err
	} else if found {
		if err := tx.Commit(); err != nil {
			return loomrecord.StoredMutationResult{}, fmt.Errorf("commit mutation replay: %w", err)
		}
		return replay, nil
	}

	if err := lockActiveTable(ctx, tx, actorID, tableID); err != nil {
		return loomrecord.StoredMutationResult{}, err
	}
	fields, err := loadFieldDefinitions(ctx, tx, tableID)
	if err != nil {
		return loomrecord.StoredMutationResult{}, err
	}

	results := make([]loomrecord.CommandResult, 0, len(commands))
	for index, command := range commands {
		var result loomrecord.CommandResult
		switch command.Kind {
		case "createRecord":
			result, err = applyCreateRecord(ctx, tx, actorID, tableID, index, command, fields)
		case "updateRecord":
			result, err = applyUpdateRecord(ctx, tx, actorID, tableID, clientMutationID, index, command, fields)
		case "deleteRecord":
			result, err = applyRecordLifecycle(ctx, tx, actorID, tableID, clientMutationID, index, command, true)
		case "restoreRecord":
			result, err = applyRecordLifecycle(ctx, tx, actorID, tableID, clientMutationID, index, command, false)
		default:
			err = domain.NewValidationError(domain.ValidationIssue{Path: fmt.Sprintf("/commands/%d/kind", index), Code: "format", Message: "unsupported mutation command"})
		}
		if err != nil {
			return loomrecord.StoredMutationResult{}, err
		}
		results = append(results, result)
	}

	var changeSequence int64
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(change_sequence), 0)
		FROM changes
		WHERE table_id = $1
	`, tableID).Scan(&changeSequence); err != nil {
		return loomrecord.StoredMutationResult{}, fmt.Errorf("read table change tail: %w", err)
	}
	stored := loomrecord.StoredMutationResult{
		ClientMutationID: clientMutationID,
		Results:          results,
		ChangeSequence:   changeSequence,
	}
	if err := saveIdempotent(ctx, tx, actorID, clientMutationID, fingerprint, stored); err != nil {
		return loomrecord.StoredMutationResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return loomrecord.StoredMutationResult{}, fmt.Errorf("commit record mutation: %w", err)
	}
	return stored, nil
}

func lockActiveTable(ctx context.Context, tx *sql.Tx, actorID, tableID string) error {
	var lockedID string
	err := tx.QueryRowContext(ctx, `
		SELECT t.id
		FROM tables t
		JOIN bases b ON b.id = t.base_id
		JOIN workspaces w ON w.id = b.workspace_id
		WHERE t.id = $1 AND t.deleted_at IS NULL AND b.deleted_at IS NULL
		  AND w.actor_id = $2 AND w.deleted_at IS NULL
		FOR UPDATE OF t
	`, tableID, actorID).Scan(&lockedID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("lock active table: %w", err)
	}
	return nil
}

func loadFieldDefinitions(ctx context.Context, tx *sql.Tx, tableID string) (map[string]loomrecord.FieldDefinition, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT id, type, config, deleted_at, is_primary
		FROM fields
		WHERE table_id = $1
		FOR SHARE
	`, tableID)
	if err != nil {
		return nil, fmt.Errorf("load Field definitions: %w", err)
	}
	defer rows.Close()
	fields := make(map[string]loomrecord.FieldDefinition)
	for rows.Next() {
		var field loomrecord.FieldDefinition
		var config []byte
		if err := rows.Scan(&field.ID, &field.Type, &config, &field.DeletedAt, &field.IsPrimary); err != nil {
			return nil, fmt.Errorf("scan Field definition: %w", err)
		}
		field.Config = append(json.RawMessage(nil), config...)
		fields[field.ID] = field
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("load Field definitions: %w", err)
	}
	return fields, nil
}

func applyCreateRecord(
	ctx context.Context,
	tx *sql.Tx,
	actorID string,
	tableID string,
	index int,
	command loomrecord.Command,
	fields map[string]loomrecord.FieldDefinition,
) (loomrecord.CommandResult, error) {
	values, queryValues, searchText, err := loomrecord.NormalizeCreateValues(command.Values, fields)
	if err != nil {
		return loomrecord.CommandResult{}, prefixRecordValidation(err, fmt.Sprintf("/commands/%d", index))
	}
	if err := validateAttachmentReferences(ctx, tx, actorID, values, fields, "/values"); err != nil {
		return loomrecord.CommandResult{}, prefixRecordValidation(err, fmt.Sprintf("/commands/%d", index))
	}
	recordID, err := id.New(id.RecordPrefix)
	if err != nil {
		return loomrecord.CommandResult{}, fmt.Errorf("generate record ID: %w", err)
	}
	created, err := insertRecord(ctx, tx, recordID, tableID, values, queryValues, searchText)
	if err != nil {
		return loomrecord.CommandResult{}, err
	}
	if err := insertRecordChange(ctx, tx, actorID, created, "recordCreated"); err != nil {
		return loomrecord.CommandResult{}, err
	}
	return loomrecord.CommandResult{Index: index, Status: "applied", Record: created}, nil
}

func applyUpdateRecord(
	ctx context.Context,
	tx *sql.Tx,
	actorID string,
	tableID string,
	clientMutationID string,
	index int,
	command loomrecord.Command,
	fields map[string]loomrecord.FieldDefinition,
) (loomrecord.CommandResult, error) {
	current, err := lockRecord(ctx, tx, tableID, command.RecordID)
	if err != nil {
		return loomrecord.CommandResult{}, err
	}
	if current.Revision != command.ExpectedRevision {
		return loomrecord.CommandResult{}, recordConflict(clientMutationID, index, command, current)
	}
	if current.DeletedAt != nil {
		return loomrecord.CommandResult{}, &domain.InvalidStateTransitionError{Resource: "record", ID: current.ID, Action: "update", Current: "deleted"}
	}
	values, queryValues, searchText, err := loomrecord.NormalizeUpdatedValues(current.Values, command.Set, command.UnsetFieldIDs, fields)
	if err != nil {
		return loomrecord.CommandResult{}, prefixRecordValidation(err, fmt.Sprintf("/commands/%d", index))
	}
	if err := validateAttachmentReferences(ctx, tx, actorID, values, fields, "/set"); err != nil {
		return loomrecord.CommandResult{}, prefixRecordValidation(err, fmt.Sprintf("/commands/%d", index))
	}
	if reflect.DeepEqual(values, current.Values) {
		return loomrecord.CommandResult{Index: index, Status: "unchanged", Record: current}, nil
	}
	updated, err := updateRecordValues(ctx, tx, current.ID, values, queryValues, searchText)
	if err != nil {
		return loomrecord.CommandResult{}, err
	}
	if err := insertRecordChange(ctx, tx, actorID, updated, "recordUpdated"); err != nil {
		return loomrecord.CommandResult{}, err
	}
	return loomrecord.CommandResult{Index: index, Status: "applied", Record: updated}, nil
}

func applyRecordLifecycle(
	ctx context.Context,
	tx *sql.Tx,
	actorID string,
	tableID string,
	clientMutationID string,
	index int,
	command loomrecord.Command,
	deleting bool,
) (loomrecord.CommandResult, error) {
	current, err := lockRecord(ctx, tx, tableID, command.RecordID)
	if err != nil {
		return loomrecord.CommandResult{}, err
	}
	if current.Revision != command.ExpectedRevision {
		return loomrecord.CommandResult{}, recordConflict(clientMutationID, index, command, current)
	}
	action := "restore"
	kind := "recordRestored"
	if deleting {
		action = "delete"
		kind = "recordDeleted"
		if current.DeletedAt != nil {
			return loomrecord.CommandResult{}, &domain.InvalidStateTransitionError{Resource: "record", ID: current.ID, Action: action, Current: "deleted"}
		}
	} else if current.DeletedAt == nil {
		return loomrecord.CommandResult{}, &domain.InvalidStateTransitionError{Resource: "record", ID: current.ID, Action: action, Current: "active"}
	}

	updated, err := setRecordDeleted(ctx, tx, current.ID, deleting)
	if err != nil {
		return loomrecord.CommandResult{}, err
	}
	if err := insertRecordChange(ctx, tx, actorID, updated, kind); err != nil {
		return loomrecord.CommandResult{}, err
	}
	return loomrecord.CommandResult{Index: index, Status: "applied", Record: updated}, nil
}

func insertRecord(ctx context.Context, tx *sql.Tx, recordID, tableID string, values, queryValues map[string]any, searchText string) (loomrecord.Record, error) {
	encodedValues, encodedQuery, err := encodeRecordValues(values, queryValues)
	if err != nil {
		return loomrecord.Record{}, err
	}
	created, err := scanRecord(tx.QueryRowContext(ctx, `
		INSERT INTO records (id, table_id, revision, values, query_values, search_text)
		VALUES ($1, $2, 1, $3::jsonb, $4::jsonb, $5)
		RETURNING id, table_id, revision, values, created_at, updated_at, deleted_at
	`, recordID, tableID, encodedValues, encodedQuery, searchText))
	if err != nil {
		return loomrecord.Record{}, fmt.Errorf("insert record: %w", err)
	}
	return created, nil
}

func updateRecordValues(ctx context.Context, tx *sql.Tx, recordID string, values, queryValues map[string]any, searchText string) (loomrecord.Record, error) {
	encodedValues, encodedQuery, err := encodeRecordValues(values, queryValues)
	if err != nil {
		return loomrecord.Record{}, err
	}
	updated, err := scanRecord(tx.QueryRowContext(ctx, `
		UPDATE records
		SET values = $1::jsonb, query_values = $2::jsonb, search_text = $3,
		    revision = revision + 1, updated_at = clock_timestamp()
		WHERE id = $4
		RETURNING id, table_id, revision, values, created_at, updated_at, deleted_at
	`, encodedValues, encodedQuery, searchText, recordID))
	if err != nil {
		return loomrecord.Record{}, fmt.Errorf("update record values: %w", err)
	}
	return updated, nil
}

func setRecordDeleted(ctx context.Context, tx *sql.Tx, recordID string, deleted bool) (loomrecord.Record, error) {
	expression := "NULL"
	if deleted {
		expression = "clock_timestamp()"
	}
	updated, err := scanRecord(tx.QueryRowContext(ctx, `
		UPDATE records
		SET deleted_at = `+expression+`, revision = revision + 1, updated_at = clock_timestamp()
		WHERE id = $1
		RETURNING id, table_id, revision, values, created_at, updated_at, deleted_at
	`, recordID))
	if err != nil {
		return loomrecord.Record{}, fmt.Errorf("update record lifecycle: %w", err)
	}
	return updated, nil
}

func lockRecord(ctx context.Context, tx *sql.Tx, tableID, recordID string) (loomrecord.Record, error) {
	item, err := scanRecord(tx.QueryRowContext(ctx, `
		SELECT id, table_id, revision, values, created_at, updated_at, deleted_at
		FROM records
		WHERE id = $1 AND table_id = $2
		FOR UPDATE
	`, recordID, tableID))
	if errors.Is(err, sql.ErrNoRows) {
		return loomrecord.Record{}, domain.ErrNotFound
	}
	if err != nil {
		return loomrecord.Record{}, fmt.Errorf("lock record: %w", err)
	}
	return item, nil
}

func scanRecord(row scanner) (loomrecord.Record, error) {
	var item loomrecord.Record
	var values []byte
	err := row.Scan(&item.ID, &item.TableID, &item.Revision, &values, &item.CreatedAt, &item.UpdatedAt, &item.DeletedAt)
	if err != nil {
		return loomrecord.Record{}, err
	}
	if err := json.Unmarshal(values, &item.Values); err != nil {
		return loomrecord.Record{}, fmt.Errorf("decode Record values: %w", err)
	}
	if item.Values == nil {
		item.Values = map[string]any{}
	}
	return item, nil
}

func encodeRecordValues(values, queryValues map[string]any) (string, string, error) {
	encodedValues, err := json.Marshal(values)
	if err != nil {
		return "", "", fmt.Errorf("encode Record values: %w", err)
	}
	encodedQuery, err := json.Marshal(queryValues)
	if err != nil {
		return "", "", fmt.Errorf("encode Record query values: %w", err)
	}
	return string(encodedValues), string(encodedQuery), nil
}

func insertRecordChange(ctx context.Context, tx *sql.Tx, actorID string, item loomrecord.Record, kind string) error {
	changeID, err := id.New(id.ChangePrefix)
	if err != nil {
		return fmt.Errorf("generate change ID: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO changes (id, kind, table_id, record_id, revision, actor_id)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, changeID, kind, item.TableID, item.ID, item.Revision, actorID); err != nil {
		return fmt.Errorf("insert Record change: %w", err)
	}
	return nil
}

func recordConflict(clientMutationID string, index int, command loomrecord.Command, current loomrecord.Record) error {
	return &loomrecord.ConflictError{
		ClientMutationID:   clientMutationID,
		FailedCommandIndex: index,
		Conflicts: []loomrecord.Conflict{{
			RecordID:          current.ID,
			ExpectedRevision:  command.ExpectedRevision,
			CurrentRevision:   current.Revision,
			CurrentValues:     current.Values,
			SubmittedSet:      command.Set,
			SubmittedUnsetIDs: command.UnsetFieldIDs,
		}},
	}
}

func prefixRecordValidation(err error, prefix string) error {
	var validation *domain.ValidationError
	if !errors.As(err, &validation) {
		return err
	}
	issues := make([]domain.ValidationIssue, len(validation.Issues))
	for index, issue := range validation.Issues {
		issue.Path = prefix + issue.Path
		issues[index] = issue
	}
	return domain.NewValidationError(issues...)
}

func validateAttachmentReferences(ctx context.Context, tx *sql.Tx, actorID string, values map[string]any, fields map[string]loomrecord.FieldDefinition, basePath string) error {
	fieldIDs := make([]string, 0)
	for fieldID, field := range fields {
		if field.Type == "attachment" {
			fieldIDs = append(fieldIDs, fieldID)
		}
	}
	sort.Strings(fieldIDs)
	issues := make([]domain.ValidationIssue, 0)
	for _, fieldID := range fieldIDs {
		value, present := values[fieldID]
		if !present || value == nil {
			continue
		}
		entries, ok := value.([]any)
		if !ok {
			return fmt.Errorf("normalized attachment value for %s is not an array", fieldID)
		}
		for index, entry := range entries {
			ref, ok := entry.(map[string]any)
			if !ok {
				return fmt.Errorf("normalized attachment reference for %s is not an object", fieldID)
			}
			attachmentID, _ := ref["id"].(string)
			path := fmt.Sprintf("%s/%s/%d", basePath, escapeJSONPointer(fieldID), index)
			var source, status, filename string
			err := tx.QueryRowContext(ctx, `
				SELECT source, status, filename
				FROM attachments
				WHERE id = $1 AND actor_id = $2 AND deleted_at IS NULL
			`, attachmentID, actorID).Scan(&source, &status, &filename)
			if errors.Is(err, sql.ErrNoRows) {
				issues = append(issues, domain.ValidationIssue{Path: path + "/id", Code: "invalidReference", Message: "Attachment is unknown, belongs to another Actor, deleted, or not ready"})
				continue
			}
			if err != nil {
				return fmt.Errorf("check attachment reference %s: %w", attachmentID, err)
			}
			refSource, sourceOK := ref["source"].(string)
			refFilename, filenameOK := ref["filename"].(string)
			if status != "ready" || !sourceOK || source != refSource || !filenameOK || filename != refFilename {
				issues = append(issues, domain.ValidationIssue{Path: path, Code: "invalidReference", Message: "Attachment metadata does not match a ready Attachment owned by the current Actor"})
				continue
			}
		}
	}
	if len(issues) > 0 {
		return domain.NewValidationError(issues...)
	}
	return nil
}

func escapeJSONPointer(value string) string {
	value = strings.ReplaceAll(value, "~", "~0")
	return strings.ReplaceAll(value, "/", "~1")
}

