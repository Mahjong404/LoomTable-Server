package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"

	appstatus "github.com/Mahjong404/LoomTable-Server/internal/status"
)

func Open(databaseURL string) (*sql.DB, error) {
	if strings.TrimSpace(databaseURL) == "" {
		return nil, errors.New("database URL is required")
	}
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	return db, nil
}

func ReadyChecker(db *sql.DB) func(context.Context) error {
	return func(ctx context.Context) error {
		if db == nil {
			return appstatus.ErrDependencyUnavailable
		}
		if err := db.PingContext(ctx); err != nil {
			return fmt.Errorf("%w: %v", appstatus.ErrDependencyUnavailable, err)
		}

		var migrationTable sql.NullString
		err := db.QueryRowContext(ctx, `SELECT to_regclass('public.schema_migrations')`).Scan(&migrationTable)
		if err != nil {
			return fmt.Errorf("%w: check migrations: %v", appstatus.ErrDependencyUnavailable, err)
		}
		if !migrationTable.Valid {
			return appstatus.ErrMigrationRequired
		}

		var applied bool
		if err := db.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM schema_migrations WHERE version = '001_initial'
			)
		`).Scan(&applied); err != nil {
			return fmt.Errorf("%w: check migration version: %v", appstatus.ErrDependencyUnavailable, err)
		}
		if !applied {
			return appstatus.ErrMigrationRequired
		}
		return nil
	}
}

func ApplyMigrations(ctx context.Context, db *sql.DB, directory string) error {
	if db == nil {
		return appstatus.ErrDependencyUnavailable
	}
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		return fmt.Errorf("create migration table: %w", err)
	}

	entries, err := os.ReadDir(directory)
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	for _, entry := range entries {
		if entry.IsDir() || strings.ToLower(filepath.Ext(entry.Name())) != ".sql" {
			continue
		}
		version := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		var applied bool
		if err := db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, version).Scan(&applied); err != nil {
			return fmt.Errorf("check migration %s: %w", version, err)
		}
		if applied {
			continue
		}

		contents, err := os.ReadFile(filepath.Join(directory, entry.Name()))
		if err != nil {
			return fmt.Errorf("read migration %s: %w", version, err)
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration %s: %w", version, err)
		}
		if _, err := tx.ExecContext(ctx, string(contents)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply migration %s: %w", version, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, version); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %s: %w", version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", version, err)
		}
	}
	return nil
}