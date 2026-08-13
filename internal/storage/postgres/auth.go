package postgres

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Mahjong404/LoomTable-Server/internal/auth"
	"github.com/Mahjong404/LoomTable-Server/internal/domain"
)

const cursorHMACKeyName = "cursor_hmac_v1"

func (r *Repository) Authenticate(ctx context.Context, token string) (string, error) {
	if r == nil || r.db == nil || token == "" {
		return "", domain.ErrUnauthenticated
	}

	var actorID string
	err := r.db.QueryRowContext(ctx, `
		SELECT actor_id
		FROM auth_tokens
		WHERE token_hash = $1 AND revoked_at IS NULL
	`, auth.HashToken(token)).Scan(&actorID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", domain.ErrUnauthenticated
	}
	if err != nil {
		return "", fmt.Errorf("authenticate token: %w", err)
	}
	return actorID, nil
}

func (r *Repository) BootstrapState(ctx context.Context) (string, error) {
	if r == nil || r.db == nil {
		return "unknown", domain.ErrDependencyMissing
	}
	var complete bool
	if err := r.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM actors)`).Scan(&complete); err != nil {
		return "unknown", fmt.Errorf("read bootstrap state: %w", err)
	}
	if complete {
		return "complete", nil
	}
	return "required", nil
}

func (r *Repository) CursorKey(ctx context.Context) ([]byte, error) {
	if r == nil || r.db == nil {
		return nil, domain.ErrDependencyMissing
	}
	generated := make([]byte, 32)
	if _, err := rand.Read(generated); err != nil {
		return nil, fmt.Errorf("generate cursor key: %w", err)
	}
	if _, err := r.db.ExecContext(ctx, `
		INSERT INTO server_secrets (name, secret)
		VALUES ($1, $2)
		ON CONFLICT (name) DO NOTHING
	`, cursorHMACKeyName, generated); err != nil {
		return nil, fmt.Errorf("initialize cursor key: %w", err)
	}
	var key []byte
	if err := r.db.QueryRowContext(ctx, `SELECT secret FROM server_secrets WHERE name = $1`, cursorHMACKeyName).Scan(&key); err != nil {
		return nil, fmt.Errorf("read cursor key: %w", err)
	}
	if len(key) != 32 {
		return nil, errors.New("stored cursor key has an invalid length")
	}
	return key, nil
}

func (r *Repository) BootstrapAuth(ctx context.Context, actorID, tokenID, name, nameKey, tokenHash string) (auth.TokenMetadata, bool, error) {
	if r == nil || r.db == nil {
		return auth.TokenMetadata{}, false, domain.ErrDependencyMissing
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return auth.TokenMetadata{}, false, fmt.Errorf("begin auth bootstrap: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext('loomtable-auth-bootstrap'))`); err != nil {
		return auth.TokenMetadata{}, false, fmt.Errorf("lock auth bootstrap: %w", err)
	}
	var initialized bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM actors)`).Scan(&initialized); err != nil {
		return auth.TokenMetadata{}, false, fmt.Errorf("check auth bootstrap: %w", err)
	}
	if initialized {
		if err := tx.Commit(); err != nil {
			return auth.TokenMetadata{}, false, fmt.Errorf("commit auth bootstrap status: %w", err)
		}
		return auth.TokenMetadata{}, false, nil
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO actors (id) VALUES ($1)`, actorID); err != nil {
		return auth.TokenMetadata{}, false, fmt.Errorf("insert bootstrap Actor: %w", err)
	}
	var metadata auth.TokenMetadata
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO auth_tokens (id, actor_id, name, name_key, token_hash)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, actor_id, name, created_at, revoked_at
	`, tokenID, actorID, name, nameKey, tokenHash).Scan(
		&metadata.ID, &metadata.ActorID, &metadata.Name, &metadata.CreatedAt, &metadata.RevokedAt,
	); err != nil {
		return auth.TokenMetadata{}, false, fmt.Errorf("insert bootstrap Token: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return auth.TokenMetadata{}, false, fmt.Errorf("commit auth bootstrap: %w", err)
	}
	return metadata, true, nil
}

func (r *Repository) CreateAuthToken(ctx context.Context, tokenID, name, nameKey, tokenHash string) (auth.TokenMetadata, error) {
	if r == nil || r.db == nil {
		return auth.TokenMetadata{}, domain.ErrDependencyMissing
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return auth.TokenMetadata{}, fmt.Errorf("begin create auth Token: %w", err)
	}
	defer tx.Rollback()
	actorID, err := lockPersonalActor(ctx, tx)
	if err != nil {
		return auth.TokenMetadata{}, err
	}
	var duplicate bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM auth_tokens
			WHERE actor_id = $1 AND name_key = $2 AND revoked_at IS NULL
		)
	`, actorID, nameKey).Scan(&duplicate); err != nil {
		return auth.TokenMetadata{}, fmt.Errorf("check Token name: %w", err)
	}
	if duplicate {
		return auth.TokenMetadata{}, domain.NewValidationError(domain.ValidationIssue{Path: "/name", Code: "duplicate", Message: "an active Token already uses this name"})
	}
	var metadata auth.TokenMetadata
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO auth_tokens (id, actor_id, name, name_key, token_hash)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, actor_id, name, created_at, revoked_at
	`, tokenID, actorID, name, nameKey, tokenHash).Scan(
		&metadata.ID, &metadata.ActorID, &metadata.Name, &metadata.CreatedAt, &metadata.RevokedAt,
	); err != nil {
		return auth.TokenMetadata{}, fmt.Errorf("insert auth Token: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return auth.TokenMetadata{}, fmt.Errorf("commit create auth Token: %w", err)
	}
	return metadata, nil
}

func (r *Repository) ListAuthTokens(ctx context.Context) ([]auth.TokenMetadata, error) {
	if r == nil || r.db == nil {
		return nil, domain.ErrDependencyMissing
	}
	actorID, err := personalActor(ctx, r.db)
	if err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, actor_id, name, created_at, revoked_at
		FROM auth_tokens
		WHERE actor_id = $1
		ORDER BY created_at ASC, id ASC
	`, actorID)
	if err != nil {
		return nil, fmt.Errorf("list auth Tokens: %w", err)
	}
	defer rows.Close()
	items := make([]auth.TokenMetadata, 0)
	for rows.Next() {
		var item auth.TokenMetadata
		if err := rows.Scan(&item.ID, &item.ActorID, &item.Name, &item.CreatedAt, &item.RevokedAt); err != nil {
			return nil, fmt.Errorf("scan auth Token: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list auth Tokens: %w", err)
	}
	return items, nil
}

func (r *Repository) RevokeAuthToken(ctx context.Context, tokenID string) (auth.TokenMetadata, error) {
	if r == nil || r.db == nil {
		return auth.TokenMetadata{}, domain.ErrDependencyMissing
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return auth.TokenMetadata{}, fmt.Errorf("begin revoke auth Token: %w", err)
	}
	defer tx.Rollback()
	actorID, err := lockPersonalActor(ctx, tx)
	if err != nil {
		return auth.TokenMetadata{}, err
	}
	var current auth.TokenMetadata
	err = tx.QueryRowContext(ctx, `
		SELECT id, actor_id, name, created_at, revoked_at
		FROM auth_tokens
		WHERE id = $1 AND actor_id = $2
		FOR UPDATE
	`, tokenID, actorID).Scan(&current.ID, &current.ActorID, &current.Name, &current.CreatedAt, &current.RevokedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return auth.TokenMetadata{}, domain.ErrNotFound
	}
	if err != nil {
		return auth.TokenMetadata{}, fmt.Errorf("lock auth Token: %w", err)
	}
	if current.RevokedAt != nil {
		return auth.TokenMetadata{}, &domain.InvalidStateTransitionError{Resource: "token", ID: tokenID, Action: "revoke", Current: "revoked"}
	}
	var revoked auth.TokenMetadata
	if err := tx.QueryRowContext(ctx, `
		UPDATE auth_tokens
		SET revoked_at = clock_timestamp()
		WHERE id = $1
		RETURNING id, actor_id, name, created_at, revoked_at
	`, tokenID).Scan(&revoked.ID, &revoked.ActorID, &revoked.Name, &revoked.CreatedAt, &revoked.RevokedAt); err != nil {
		return auth.TokenMetadata{}, fmt.Errorf("revoke auth Token: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return auth.TokenMetadata{}, fmt.Errorf("commit revoke auth Token: %w", err)
	}
	return revoked, nil
}

type actorQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func personalActor(ctx context.Context, queryer actorQueryer) (string, error) {
	rows, err := queryer.QueryContext(ctx, `SELECT id FROM actors ORDER BY created_at ASC, id ASC LIMIT 2`)
	if err != nil {
		return "", fmt.Errorf("read Personal Actor: %w", err)
	}
	defer rows.Close()
	actors := make([]string, 0, 2)
	for rows.Next() {
		var actorID string
		if err := rows.Scan(&actorID); err != nil {
			return "", fmt.Errorf("scan Personal Actor: %w", err)
		}
		actors = append(actors, actorID)
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("read Personal Actor: %w", err)
	}
	if len(actors) == 0 {
		return "", auth.ErrBootstrapRequired
	}
	if len(actors) > 1 {
		return "", errors.New("Personal mode database contains multiple Actors")
	}
	return actors[0], nil
}

func lockPersonalActor(ctx context.Context, tx *sql.Tx) (string, error) {
	actorID, err := personalActor(ctx, tx)
	if err != nil {
		return "", err
	}
	if err := tx.QueryRowContext(ctx, `SELECT id FROM actors WHERE id = $1 FOR UPDATE`, actorID).Scan(&actorID); err != nil {
		return "", fmt.Errorf("lock Personal Actor: %w", err)
	}
	return actorID, nil
}
