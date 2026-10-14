package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/Mahjong404/LoomTable-Server/internal/catalog"
	"github.com/Mahjong404/LoomTable-Server/internal/domain"
)

func readViewOrder(ctx context.Context, tx *sql.Tx, tableID string) (domain.ViewOrder, error) {
	order := domain.ViewOrder{TableID: tableID}
	var encoded []byte
	if err := tx.QueryRowContext(ctx, `SELECT view_order_revision, view_order FROM tables WHERE id=$1`, tableID).Scan(&order.Revision, &encoded); err != nil {
		return order, fmt.Errorf("read View Order: %w", err)
	}
	if err := json.Unmarshal(encoded, &order.ViewIDs); err != nil {
		return order, fmt.Errorf("decode View Order: %w", err)
	}
	return order, nil
}

func (r *Repository) ListViewsSnapshot(ctx context.Context, actorID, tableID, lifecycle string) (domain.ViewListSnapshot, error) {
	if r == nil || r.db == nil {
		return domain.ViewListSnapshot{}, domain.ErrDependencyMissing
	}
	condition, err := lifecycleCondition("v.deleted_at", lifecycle)
	if err != nil {
		return domain.ViewListSnapshot{}, err
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return domain.ViewListSnapshot{}, fmt.Errorf("begin View list snapshot: %w", err)
	}
	defer tx.Rollback()
	visible, err := activeTableVisible(ctx, tx, actorID, tableID)
	if err != nil {
		return domain.ViewListSnapshot{}, err
	}
	if !visible {
		return domain.ViewListSnapshot{}, domain.ErrNotFound
	}
	order, err := readViewOrder(ctx, tx, tableID)
	if err != nil {
		return domain.ViewListSnapshot{}, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT v.id, v.table_id, v.name, v.type, v.config, v.is_default, v.revision,
		v.created_at, v.updated_at, v.deleted_at FROM views v WHERE v.table_id=$1 AND `+condition+`
		ORDER BY v.created_at, v.id`, tableID)
	if err != nil {
		return domain.ViewListSnapshot{}, fmt.Errorf("list View snapshot: %w", err)
	}
	items := make([]domain.View, 0)
	for rows.Next() {
		view, err := scanView(rows)
		if err != nil {
			rows.Close()
			return domain.ViewListSnapshot{}, err
		}
		items = append(items, view)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return domain.ViewListSnapshot{}, err
	}
	if lifecycle == "active" {
		byID := make(map[string]domain.View, len(items))
		for _, view := range items {
			byID[view.ID] = view
		}
		if len(order.ViewIDs) != len(items) {
			return domain.ViewListSnapshot{}, fmt.Errorf("View Order membership does not match active Views")
		}
		items = make([]domain.View, 0, len(order.ViewIDs))
		for _, viewID := range order.ViewIDs {
			view, ok := byID[viewID]
			if !ok {
				return domain.ViewListSnapshot{}, fmt.Errorf("invalid stored View Order member")
			}
			items = append(items, view)
			delete(byID, viewID)
		}
	}
	if err := tx.Commit(); err != nil {
		return domain.ViewListSnapshot{}, fmt.Errorf("commit View list snapshot: %w", err)
	}
	return domain.ViewListSnapshot{Items: items, Order: order}, nil
}

func (r *Repository) ReorderViews(ctx context.Context, actorID, key, tableID string, hash [32]byte, input catalog.ViewOrderInput) (domain.ViewOrderResult, error) {
	if r == nil || r.db == nil {
		return domain.ViewOrderResult{}, domain.ErrDependencyMissing
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.ViewOrderResult{}, fmt.Errorf("begin View Order: %w", err)
	}
	defer tx.Rollback()
	if err := beginAudit(ctx, tx, actorID); err != nil {
		return domain.ViewOrderResult{}, err
	}
	if err := lockActor(ctx, tx, actorID); err != nil {
		return domain.ViewOrderResult{}, err
	}
	if result, found, err := replayIdempotent[domain.ViewOrderResult](ctx, tx, actorID, key, hash); err != nil {
		return domain.ViewOrderResult{}, err
	} else if found {
		if err := commitAudit(ctx, tx); err != nil {
			return domain.ViewOrderResult{}, err
		}
		return result, nil
	}
	if err := lockActiveTable(ctx, tx, actorID, tableID); err != nil {
		return domain.ViewOrderResult{}, err
	}
	order, err := readViewOrder(ctx, tx, tableID)
	if err != nil {
		return domain.ViewOrderResult{}, err
	}
	if order.Revision != input.ExpectedOrderRevision {
		return domain.ViewOrderResult{}, &domain.ViewOrderConflictError{TableID: tableID, ExpectedOrderRevision: input.ExpectedOrderRevision, CurrentOrder: order}
	}
	members := make(map[string]bool, len(order.ViewIDs))
	for _, viewID := range order.ViewIDs {
		members[viewID] = true
	}
	valid := len(order.ViewIDs) == len(input.ViewIDs)
	for _, viewID := range input.ViewIDs {
		if !members[viewID] {
			valid = false
		}
		delete(members, viewID)
	}
	if !valid || len(members) != 0 {
		return domain.ViewOrderResult{}, domain.NewValidationError(domain.ValidationIssue{Path: "/viewIds", Code: "invalidReference", Message: "viewIds must be a complete active View permutation of this Table"})
	}
	result := domain.ViewOrderResult{ClientMutationID: key, Status: "unchanged", Order: order}
	if !slices.Equal(order.ViewIDs, input.ViewIDs) {
		encoded, err := json.Marshal(input.ViewIDs)
		if err != nil {
			return domain.ViewOrderResult{}, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE tables SET view_order=$1::jsonb, view_order_revision=view_order_revision+1 WHERE id=$2`, string(encoded), tableID); err != nil {
			return domain.ViewOrderResult{}, fmt.Errorf("save View Order: %w", err)
		}
		result.Status = "applied"
		result.Order = domain.ViewOrder{TableID: tableID, Revision: order.Revision + 1, ViewIDs: append([]string{}, input.ViewIDs...)}
		if err := insertMetadataChange(ctx, tx, actorID, "schemaChanged", tableID, tableID, result.Order.Revision); err != nil {
			return domain.ViewOrderResult{}, err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO mutation_identity_guards (actor_id, client_mutation_id, request_hash) VALUES ($1,$2,$3)`, actorID, key, hash[:]); err != nil {
		return domain.ViewOrderResult{}, fmt.Errorf("save View Order identity: %w", err)
	}
	if err := saveIdempotent(ctx, tx, actorID, key, hash, result); err != nil {
		return domain.ViewOrderResult{}, err
	}
	if err := commitAudit(ctx, tx); err != nil {
		return domain.ViewOrderResult{}, fmt.Errorf("commit View Order: %w", err)
	}
	return result, nil
}
