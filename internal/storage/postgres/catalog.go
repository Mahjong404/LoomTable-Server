package postgres

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Mahjong404/LoomTable-Server/internal/catalog"
	"github.com/Mahjong404/LoomTable-Server/internal/domain"
)

type scanner interface {
	Scan(...any) error
}

func (r *Repository) ListWorkspaces(ctx context.Context, actorID string) ([]domain.Workspace, error) {
	if r == nil || r.db == nil {
		return nil, domain.ErrDependencyMissing
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, name, revision, created_at, updated_at
		FROM workspaces
		WHERE actor_id = $1 AND deleted_at IS NULL
		ORDER BY created_at ASC, id ASC
	`, actorID)
	if err != nil {
		return nil, fmt.Errorf("list workspaces: %w", err)
	}
	defer rows.Close()

	items := make([]domain.Workspace, 0)
	for rows.Next() {
		item, err := scanWorkspace(rows)
		if err != nil {
			return nil, fmt.Errorf("scan workspace: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list workspaces: %w", err)
	}
	return items, nil
}

func (r *Repository) GetWorkspace(ctx context.Context, actorID, workspaceID string) (domain.Workspace, error) {
	if r == nil || r.db == nil {
		return domain.Workspace{}, domain.ErrDependencyMissing
	}
	item, err := scanWorkspace(r.db.QueryRowContext(ctx, `
		SELECT id, name, revision, created_at, updated_at
		FROM workspaces
		WHERE id = $1 AND actor_id = $2 AND deleted_at IS NULL
	`, workspaceID, actorID))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Workspace{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Workspace{}, fmt.Errorf("get workspace: %w", err)
	}
	return item, nil
}

func (r *Repository) CreateWorkspace(ctx context.Context, actorID, idempotencyKey string, fingerprint [32]byte, proposed domain.Workspace) (domain.Workspace, error) {
	if r == nil || r.db == nil {
		return domain.Workspace{}, domain.ErrDependencyMissing
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Workspace{}, fmt.Errorf("begin create workspace: %w", err)
	}
	defer tx.Rollback()

	if err := lockActor(ctx, tx, actorID); err != nil {
		return domain.Workspace{}, err
	}
	if response, found, err := replayIdempotent[domain.Workspace](ctx, tx, actorID, idempotencyKey, fingerprint); err != nil {
		return domain.Workspace{}, err
	} else if found {
		if err := tx.Commit(); err != nil {
			return domain.Workspace{}, fmt.Errorf("commit workspace replay: %w", err)
		}
		return response, nil
	}

	var count int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM workspaces WHERE actor_id = $1`, actorID).Scan(&count); err != nil {
		return domain.Workspace{}, fmt.Errorf("count workspaces: %w", err)
	}
	if count >= catalog.WorkspaceLimitPerActor {
		return domain.Workspace{}, &domain.ResourceLimitError{Resource: "workspace", ParentType: "actor", ParentID: actorID, Limit: catalog.WorkspaceLimitPerActor}
	}

	created, err := scanWorkspace(tx.QueryRowContext(ctx, `
		INSERT INTO workspaces (id, actor_id, name, revision)
		VALUES ($1, $2, $3, 1)
		RETURNING id, name, revision, created_at, updated_at
	`, proposed.ID, actorID, proposed.Name))
	if err != nil {
		return domain.Workspace{}, fmt.Errorf("insert workspace: %w", err)
	}
	if err := saveIdempotent(ctx, tx, actorID, idempotencyKey, fingerprint, created); err != nil {
		return domain.Workspace{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Workspace{}, fmt.Errorf("commit create workspace: %w", err)
	}
	return created, nil
}

func (r *Repository) UpdateWorkspace(ctx context.Context, actorID, workspaceID string, expectedRevision int64, name string) (domain.Workspace, error) {
	if r == nil || r.db == nil {
		return domain.Workspace{}, domain.ErrDependencyMissing
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Workspace{}, fmt.Errorf("begin update workspace: %w", err)
	}
	defer tx.Rollback()

	current, err := scanWorkspace(tx.QueryRowContext(ctx, `
		SELECT id, name, revision, created_at, updated_at
		FROM workspaces
		WHERE id = $1 AND actor_id = $2 AND deleted_at IS NULL
		FOR UPDATE
	`, workspaceID, actorID))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Workspace{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Workspace{}, fmt.Errorf("lock workspace: %w", err)
	}
	if current.Revision != expectedRevision {
		return domain.Workspace{}, &domain.RevisionConflictError{Resource: "workspace", ID: workspaceID, ExpectedRevision: expectedRevision, CurrentRevision: current.Revision}
	}
	if current.Name == name {
		if err := tx.Commit(); err != nil {
			return domain.Workspace{}, fmt.Errorf("commit workspace no-op: %w", err)
		}
		return current, nil
	}

	updated, err := scanWorkspace(tx.QueryRowContext(ctx, `
		UPDATE workspaces
		SET name = $1, revision = revision + 1, updated_at = clock_timestamp()
		WHERE id = $2
		RETURNING id, name, revision, created_at, updated_at
	`, name, workspaceID))
	if err != nil {
		return domain.Workspace{}, fmt.Errorf("update workspace: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return domain.Workspace{}, fmt.Errorf("commit update workspace: %w", err)
	}
	return updated, nil
}

func (r *Repository) ListBases(ctx context.Context, actorID, workspaceID string) ([]domain.Base, error) {
	if r == nil || r.db == nil {
		return nil, domain.ErrDependencyMissing
	}
	var visible bool
	if err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM workspaces
			WHERE id = $1 AND actor_id = $2 AND deleted_at IS NULL
		)
	`, workspaceID, actorID).Scan(&visible); err != nil {
		return nil, fmt.Errorf("check workspace: %w", err)
	}
	if !visible {
		return nil, domain.ErrNotFound
	}

	rows, err := r.db.QueryContext(ctx, `
		SELECT b.id, b.workspace_id, b.name, b.revision, b.created_at, b.updated_at
		FROM bases b
		WHERE b.workspace_id = $1 AND b.deleted_at IS NULL
		ORDER BY b.created_at ASC, b.id ASC
	`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list bases: %w", err)
	}
	defer rows.Close()

	items := make([]domain.Base, 0)
	for rows.Next() {
		item, err := scanBase(rows)
		if err != nil {
			return nil, fmt.Errorf("scan base: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list bases: %w", err)
	}
	return items, nil
}

func (r *Repository) GetBase(ctx context.Context, actorID, baseID string) (domain.Base, error) {
	if r == nil || r.db == nil {
		return domain.Base{}, domain.ErrDependencyMissing
	}
	item, err := scanBase(r.db.QueryRowContext(ctx, `
		SELECT b.id, b.workspace_id, b.name, b.revision, b.created_at, b.updated_at
		FROM bases b
		JOIN workspaces w ON w.id = b.workspace_id
		WHERE b.id = $1 AND b.deleted_at IS NULL
		  AND w.actor_id = $2 AND w.deleted_at IS NULL
	`, baseID, actorID))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Base{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Base{}, fmt.Errorf("get base: %w", err)
	}
	return item, nil
}

func (r *Repository) CreateBase(ctx context.Context, actorID, idempotencyKey string, fingerprint [32]byte, proposed domain.Base) (domain.Base, error) {
	if r == nil || r.db == nil {
		return domain.Base{}, domain.ErrDependencyMissing
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Base{}, fmt.Errorf("begin create base: %w", err)
	}
	defer tx.Rollback()

	if err := lockActor(ctx, tx, actorID); err != nil {
		return domain.Base{}, err
	}
	if response, found, err := replayIdempotent[domain.Base](ctx, tx, actorID, idempotencyKey, fingerprint); err != nil {
		return domain.Base{}, err
	} else if found {
		if err := tx.Commit(); err != nil {
			return domain.Base{}, fmt.Errorf("commit base replay: %w", err)
		}
		return response, nil
	}

	var workspaceID string
	if err := tx.QueryRowContext(ctx, `
		SELECT id FROM workspaces
		WHERE id = $1 AND actor_id = $2 AND deleted_at IS NULL
		FOR UPDATE
	`, proposed.WorkspaceID, actorID).Scan(&workspaceID); errors.Is(err, sql.ErrNoRows) {
		return domain.Base{}, domain.ErrNotFound
	} else if err != nil {
		return domain.Base{}, fmt.Errorf("lock parent workspace: %w", err)
	}

	var count int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM bases WHERE workspace_id = $1`, workspaceID).Scan(&count); err != nil {
		return domain.Base{}, fmt.Errorf("count bases: %w", err)
	}
	if count >= catalog.BaseLimitPerWorkspace {
		return domain.Base{}, &domain.ResourceLimitError{Resource: "base", ParentType: "workspace", ParentID: workspaceID, Limit: catalog.BaseLimitPerWorkspace}
	}

	created, err := scanBase(tx.QueryRowContext(ctx, `
		INSERT INTO bases (id, workspace_id, name, revision)
		VALUES ($1, $2, $3, 1)
		RETURNING id, workspace_id, name, revision, created_at, updated_at
	`, proposed.ID, workspaceID, proposed.Name))
	if err != nil {
		return domain.Base{}, fmt.Errorf("insert base: %w", err)
	}
	if err := saveIdempotent(ctx, tx, actorID, idempotencyKey, fingerprint, created); err != nil {
		return domain.Base{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Base{}, fmt.Errorf("commit create base: %w", err)
	}
	return created, nil
}

func (r *Repository) UpdateBase(ctx context.Context, actorID, baseID string, expectedRevision int64, name string) (domain.Base, error) {
	if r == nil || r.db == nil {
		return domain.Base{}, domain.ErrDependencyMissing
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Base{}, fmt.Errorf("begin update base: %w", err)
	}
	defer tx.Rollback()

	current, err := scanBase(tx.QueryRowContext(ctx, `
		SELECT b.id, b.workspace_id, b.name, b.revision, b.created_at, b.updated_at
		FROM bases b
		JOIN workspaces w ON w.id = b.workspace_id
		WHERE b.id = $1 AND b.deleted_at IS NULL
		  AND w.actor_id = $2 AND w.deleted_at IS NULL
		FOR UPDATE OF b
	`, baseID, actorID))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Base{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Base{}, fmt.Errorf("lock base: %w", err)
	}
	if current.Revision != expectedRevision {
		return domain.Base{}, &domain.RevisionConflictError{Resource: "base", ID: baseID, ExpectedRevision: expectedRevision, CurrentRevision: current.Revision}
	}
	if current.Name == name {
		if err := tx.Commit(); err != nil {
			return domain.Base{}, fmt.Errorf("commit base no-op: %w", err)
		}
		return current, nil
	}

	updated, err := scanBase(tx.QueryRowContext(ctx, `
		UPDATE bases
		SET name = $1, revision = revision + 1, updated_at = clock_timestamp()
		WHERE id = $2
		RETURNING id, workspace_id, name, revision, created_at, updated_at
	`, name, baseID))
	if err != nil {
		return domain.Base{}, fmt.Errorf("update base: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return domain.Base{}, fmt.Errorf("commit update base: %w", err)
	}
	return updated, nil
}

func scanWorkspace(row scanner) (domain.Workspace, error) {
	var item domain.Workspace
	err := row.Scan(&item.ID, &item.Name, &item.Revision, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func scanBase(row scanner) (domain.Base, error) {
	var item domain.Base
	err := row.Scan(&item.ID, &item.WorkspaceID, &item.Name, &item.Revision, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func lockActor(ctx context.Context, tx *sql.Tx, actorID string) error {
	var lockedID string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM actors WHERE id = $1 FOR UPDATE`, actorID).Scan(&lockedID); errors.Is(err, sql.ErrNoRows) {
		return domain.ErrNotFound
	} else if err != nil {
		return fmt.Errorf("lock actor: %w", err)
	}
	return nil
}

func replayIdempotent[T any](ctx context.Context, tx *sql.Tx, actorID, key string, fingerprint [32]byte) (T, bool, error) {
	var zero T
	var storedHash []byte
	var response []byte
	err := tx.QueryRowContext(ctx, `
		SELECT request_hash, response
		FROM idempotency_keys
		WHERE actor_id = $1 AND client_mutation_id = $2
	`, actorID, key).Scan(&storedHash, &response)
	if errors.Is(err, sql.ErrNoRows) {
		return zero, false, nil
	}
	if err != nil {
		return zero, false, fmt.Errorf("read idempotency result: %w", err)
	}
	if !bytes.Equal(storedHash, fingerprint[:]) {
		return zero, false, &domain.IdempotencyKeyReusedError{}
	}
	var result T
	if err := json.Unmarshal(response, &result); err != nil {
		return zero, false, fmt.Errorf("decode idempotency result: %w", err)
	}
	return result, true, nil
}

func saveIdempotent(ctx context.Context, tx *sql.Tx, actorID, key string, fingerprint [32]byte, response any) error {
	encoded, err := json.Marshal(response)
	if err != nil {
		return fmt.Errorf("encode idempotency result: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO idempotency_keys (actor_id, client_mutation_id, request_hash, response)
		VALUES ($1, $2, $3, $4::jsonb)
	`, actorID, key, fingerprint[:], string(encoded)); err != nil {
		return fmt.Errorf("save idempotency result: %w", err)
	}
	return nil
}
