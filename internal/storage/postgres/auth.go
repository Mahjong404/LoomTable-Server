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
