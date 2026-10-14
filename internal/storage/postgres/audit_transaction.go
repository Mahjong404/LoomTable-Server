package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/Mahjong404/LoomTable-Server/internal/domain"
	"github.com/Mahjong404/LoomTable-Server/internal/id"
)

func beginAudit(ctx context.Context, tx *sql.Tx, actorID string) error {
	operationID, err := id.New("op_")
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "SELECT set_config('loomtable.audit_actor',$1,true),set_config('loomtable.audit_operation',$2,true)", actorID, operationID)
	return err
}

func commitAudit(ctx context.Context, tx *sql.Tx) error {
	var operationID string
	if err := tx.QueryRowContext(ctx, "SELECT COALESCE(current_setting('loomtable.audit_operation',true),'')").Scan(&operationID); err != nil {
		return fmt.Errorf("read operation identity: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if !domain.ReplayedAuditOperation(ctx) {
		domain.SetAuditOperation(ctx, operationID, false)
	}
	return nil
}
