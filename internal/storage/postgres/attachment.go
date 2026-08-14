package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Mahjong404/LoomTable-Server/internal/attachment"
	"github.com/Mahjong404/LoomTable-Server/internal/domain"
)

func (r *Repository) CreateAttachment(ctx context.Context, actorID, idempotencyKey string, fingerprint [32]byte, proposed domain.Attachment) (domain.Attachment, error) {
	if r == nil || r.db == nil {
		return domain.Attachment{}, domain.ErrDependencyMissing
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Attachment{}, fmt.Errorf("begin attachment creation: %w", err)
	}
	defer tx.Rollback()
	if err := lockActor(ctx, tx, actorID); err != nil {
		return domain.Attachment{}, err
	}
	if response, found, err := replayIdempotent[domain.Attachment](ctx, tx, actorID, idempotencyKey, fingerprint); err != nil {
		return domain.Attachment{}, err
	} else if found {
		if err := tx.Commit(); err != nil {
			return domain.Attachment{}, fmt.Errorf("commit attachment replay: %w", err)
		}
		return response, nil
	}
	created, err := scanAttachment(tx.QueryRowContext(ctx, `
		INSERT INTO attachments (
			id, actor_id, source, status, filename, mime_type, size_bytes,
			storage_key, vault_path, revision
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8, ''), NULLIF($9, ''), 1)
		RETURNING id, source, status, filename, mime_type, size_bytes,
		          storage_key, vault_path, sha256, width, height, revision,
		          created_at, updated_at, deleted_at
	`, proposed.ID, actorID, proposed.Source, proposed.Status, proposed.Filename, proposed.MimeType,
		nullableInt64(proposed.Size), proposed.StorageKey, proposed.VaultPath))
	if err != nil {
		return domain.Attachment{}, fmt.Errorf("insert attachment: %w", err)
	}
	if err := saveIdempotent(ctx, tx, actorID, idempotencyKey, fingerprint, created); err != nil {
		return domain.Attachment{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Attachment{}, fmt.Errorf("commit attachment creation: %w", err)
	}
	return created, nil
}

func (r *Repository) GetAttachment(ctx context.Context, actorID, attachmentID string) (domain.Attachment, error) {
	if r == nil || r.db == nil {
		return domain.Attachment{}, domain.ErrDependencyMissing
	}
	item, err := scanAttachment(r.db.QueryRowContext(ctx, `
		SELECT a.id, a.source, a.status, a.filename, a.mime_type, a.size_bytes,
		       a.storage_key, a.vault_path, a.sha256, a.width, a.height,
		       a.revision, a.created_at, a.updated_at, a.deleted_at
		FROM attachments a
		WHERE a.id = $1 AND a.actor_id = $2 AND a.deleted_at IS NULL
	`, attachmentID, actorID))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Attachment{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Attachment{}, fmt.Errorf("get attachment: %w", err)
	}
	return item, nil
}

func (r *Repository) MarkReady(ctx context.Context, actorID, attachmentID string, content attachment.Content) (domain.Attachment, error) {
	if r == nil || r.db == nil {
		return domain.Attachment{}, domain.ErrDependencyMissing
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Attachment{}, fmt.Errorf("begin attachment upload: %w", err)
	}
	defer tx.Rollback()
	current, err := lockAttachment(ctx, tx, actorID, attachmentID)
	if err != nil {
		return domain.Attachment{}, err
	}
	if current.Status != "pending" {
		return domain.Attachment{}, &domain.InvalidStateTransitionError{Resource: "attachment", ID: attachmentID, Action: "upload", Current: current.Status}
	}
	updated, err := scanAttachment(tx.QueryRowContext(ctx, `
		UPDATE attachments
		SET status = 'ready', size_bytes = $1, mime_type = $2, sha256 = $3,
		    width = $4, height = $5, revision = revision + 1, updated_at = clock_timestamp()
		WHERE id = $6 AND actor_id = $7
		RETURNING id, source, status, filename, mime_type, size_bytes,
		          storage_key, vault_path, sha256, width, height, revision,
		          created_at, updated_at, deleted_at
	`, content.Size, content.MimeType, content.Hash, nullableInt(content.Width), nullableInt(content.Height), attachmentID, actorID))
	if err != nil {
		return domain.Attachment{}, fmt.Errorf("mark attachment ready: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return domain.Attachment{}, fmt.Errorf("commit attachment upload: %w", err)
	}
	return updated, nil
}

func (r *Repository) DeleteAttachment(ctx context.Context, actorID, attachmentID string, expectedRevision int64) error {
	if r == nil || r.db == nil {
		return domain.ErrDependencyMissing
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin attachment deletion: %w", err)
	}
	defer tx.Rollback()
	current, err := lockAttachment(ctx, tx, actorID, attachmentID)
	if err != nil {
		return err
	}
	if current.DeletedAt != nil {
		return &domain.InvalidStateTransitionError{Resource: "attachment", ID: attachmentID, Action: "delete", Current: "deleted"}
	}
	if current.Revision != expectedRevision {
		return &domain.RevisionConflictError{Resource: "attachment", ID: attachmentID, ExpectedRevision: expectedRevision, CurrentRevision: current.Revision}
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE attachments
		SET deleted_at = clock_timestamp(), revision = revision + 1, updated_at = clock_timestamp()
		WHERE id = $1 AND actor_id = $2
	`, attachmentID, actorID); err != nil {
		return fmt.Errorf("delete attachment: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit attachment deletion: %w", err)
	}
	return nil
}

func lockAttachment(ctx context.Context, tx *sql.Tx, actorID, attachmentID string) (domain.Attachment, error) {
	item, err := scanAttachment(tx.QueryRowContext(ctx, `
		SELECT a.id, a.source, a.status, a.filename, a.mime_type, a.size_bytes,
		       a.storage_key, a.vault_path, a.sha256, a.width, a.height,
		       a.revision, a.created_at, a.updated_at, a.deleted_at
		FROM attachments a
		WHERE a.id = $1 AND a.actor_id = $2
		FOR UPDATE
	`, attachmentID, actorID))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Attachment{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Attachment{}, fmt.Errorf("lock attachment: %w", err)
	}
	return item, nil
}

func scanAttachment(row scanner) (domain.Attachment, error) {
	var item domain.Attachment
	var size sql.NullInt64
	var storageKey, vaultPath, mimeType, hash sql.NullString
	var width, height sql.NullInt64
	if err := row.Scan(
		&item.ID, &item.Source, &item.Status, &item.Filename, &mimeType, &size,
		&storageKey, &vaultPath, &hash, &width, &height, &item.Revision,
		&item.CreatedAt, &item.UpdatedAt, &item.DeletedAt,
	); err != nil {
		return domain.Attachment{}, err
	}
	item.MimeType = mimeType.String
	item.Hash = hash.String
	if size.Valid {
		item.Size = &size.Int64
	}
	if storageKey.Valid {
		item.StorageKey = storageKey.String
	}
	if vaultPath.Valid {
		item.VaultPath = vaultPath.String
	}
	if width.Valid {
		value := int(width.Int64)
		item.Width = &value
	}
	if height.Valid {
		value := int(height.Int64)
		item.Height = &value
	}
	return item, nil
}

func nullableInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

