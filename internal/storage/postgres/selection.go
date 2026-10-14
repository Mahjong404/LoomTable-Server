package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/Mahjong404/LoomTable-Server/internal/domain"
	"github.com/Mahjong404/LoomTable-Server/internal/id"
	record "github.com/Mahjong404/LoomTable-Server/internal/record"
)

type selectionState struct {
	Scope               record.SelectionScope
	Query               record.SelectionQuery
	Fingerprint, Schema string
	RequestHash         []byte
	Invalid             bool
}

const selectionColumns = `id,table_id,query,membership_fingerprint,schema_hash,semantic_data_version,member_count,created_at,expires_at,request_hash,invalidated`

func scanSelection(row scanner) (selectionState, error) {
	var state selectionState
	var raw []byte
	err := row.Scan(&state.Scope.ID, &state.Scope.TableID, &raw, &state.Fingerprint, &state.Schema, &state.Scope.SemanticDataVersion, &state.Scope.Count, &state.Scope.CreatedAt, &state.Scope.ExpiresAt, &state.RequestHash, &state.Invalid)
	if err != nil {
		return state, err
	}
	err = json.Unmarshal(raw, &state.Query)
	state.Scope.MembershipFingerprint = state.Fingerprint
	return state, err
}
func selectionExpired() error { return &domain.QuerySnapshotExpiredError{} }
func selectionBudget(path string, limit int) error {
	return domain.NewValidationError(domain.ValidationIssue{Path: path, Code: "limit", Message: fmt.Sprintf("selection budget %d exceeded", limit)})
}
func normalizeSelectionContext(ctx context.Context, tx *sql.Tx, actor, table string, query record.SelectionQuery) (record.QueryPlan, string, string, error) {
	metadata, err := resolveQueryMetadata(ctx, tx, actor, table, query.ViewID)
	if err != nil {
		return record.QueryPlan{}, "", "", err
	}
	return record.SelectionPlan(query, metadata)
}
func selectionResult(ctx context.Context, tx *sql.Tx, state selectionState, excluded, probes []string) (record.SelectionResult, error) {
	result := record.SelectionResult{Scope: state.Scope, ExcludedIDs: append([]string{}, excluded...), Membership: make([]record.SelectionMembership, 0, len(probes))}
	sort.Strings(result.ExcludedIDs)
	ids := append(append([]string{}, excluded...), probes...)
	found := map[string]bool{}
	excludedSet := map[string]bool{}
	for _, v := range excluded {
		excludedSet[v] = true
	}
	if len(ids) > 0 {
		rows, err := tx.QueryContext(ctx, `SELECT record_id FROM selection_scope_members WHERE scope_id=$1 AND record_id=ANY($2::text[])`, state.Scope.ID, ids)
		if err != nil {
			return result, err
		}
		for rows.Next() {
			var v string
			if err := rows.Scan(&v); err != nil {
				rows.Close()
				return result, err
			}
			found[v] = true
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return result, err
		}
	}
	for _, v := range excluded {
		if !found[v] {
			return result, domain.NewValidationError(domain.ValidationIssue{Path: "/excludedIds", Code: "invalidReference", Message: "all exclusions must belong to the Scope"})
		}
	}
	result.ExcludedCount = len(excluded)
	result.SelectedCount = state.Scope.Count - int64(len(excluded))
	for _, v := range probes {
		result.Membership = append(result.Membership, record.SelectionMembership{RecordID: v, InScope: found[v], Selected: found[v] && !excludedSet[v]})
	}
	return result, nil
}

func (r *Repository) AcquireSelection(ctx context.Context, actor, table, requestID string, query record.SelectionQuery, now time.Time) (record.SelectionResult, error) {
	now = now.UTC().Truncate(time.Microsecond)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return record.SelectionResult{}, err
	}
	defer tx.Rollback()
	// Serializes allocations only. Business writers never acquire this lock.
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext('loomtable-selection-allocation-v1'))`); err != nil {
		return record.SelectionResult{}, err
	}
	if err = lockActor(ctx, tx, actor); err != nil {
		return record.SelectionResult{}, err
	}
	// Prevent an ancestor deletion from missing a concurrently inserted Scope.
	// These shared locks precede Table; existing writers never lock them after Scope.
	var visibleTable string
	err = tx.QueryRowContext(ctx, `SELECT t.id FROM tables t JOIN bases b ON b.id=t.base_id JOIN workspaces w ON w.id=b.workspace_id WHERE t.id=$1 AND w.actor_id=$2 AND t.deleted_at IS NULL AND b.deleted_at IS NULL AND w.deleted_at IS NULL FOR SHARE OF b,w`, table, actor).Scan(&visibleTable)
	if errors.Is(err, sql.ErrNoRows) {
		return record.SelectionResult{}, domain.ErrNotFound
	}
	if err != nil {
		return record.SelectionResult{}, err
	}
	if err = lockActiveTable(ctx, tx, actor, table); err != nil {
		return record.SelectionResult{}, err
	}
	previous, lookupErr := scanSelection(tx.QueryRowContext(ctx, `SELECT `+selectionColumns+` FROM selection_scopes WHERE actor_id=$1 AND request_id=$2`, actor, requestID))
	if lookupErr == nil && (previous.Invalid || !now.Before(previous.Scope.ExpiresAt)) {
		return record.SelectionResult{}, selectionExpired()
	}
	if lookupErr != nil && !errors.Is(lookupErr, sql.ErrNoRows) {
		return record.SelectionResult{}, lookupErr
	}
	plan, fp, schema, err := normalizeSelectionContext(ctx, tx, actor, table, query)
	if err != nil {
		return record.SelectionResult{}, err
	}
	query.Search = plan.Search
	query.Filter = plan.Filter
	// Retain whether the Filter was inherited; presentation defaults never enter it.
	if !query.FilterPresent {
		query.Filter = nil
	}
	raw, err := json.Marshal(query)
	if err != nil {
		return record.SelectionResult{}, err
	}
	hash := sha256.Sum256(append([]byte(table+"\n"), raw...))
	if lookupErr == nil {
		if hex.EncodeToString(previous.RequestHash) != hex.EncodeToString(hash[:]) {
			return record.SelectionResult{}, &domain.IdempotencyKeyReusedError{}
		}
		if previous.Invalid || !now.Before(previous.Scope.ExpiresAt) || previous.Fingerprint != fp || previous.Schema != schema {
			return record.SelectionResult{}, selectionExpired()
		}
		result, err := selectionResult(ctx, tx, previous, nil, nil)
		if err != nil {
			return result, err
		}
		return result, tx.Commit()
	}
	// Retain invalid request identities until expiry; reclaim only their members.
	if _, err = tx.ExecContext(ctx, `DELETE FROM selection_scopes WHERE expires_at <= $1`, now); err != nil {
		return record.SelectionResult{}, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM selection_scope_members m USING selection_scopes s WHERE m.scope_id=s.id AND s.invalidated`); err != nil {
		return record.SelectionResult{}, err
	}
	var actorCount, tableCount, totalCount, actorRequests, totalRequests int
	var totalMembers int64
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FILTER(WHERE actor_id=$1 AND NOT invalidated),count(*) FILTER(WHERE table_id=$2 AND NOT invalidated),count(*) FILTER(WHERE NOT invalidated),COALESCE(sum(member_count) FILTER(WHERE NOT invalidated),0),count(*) FILTER(WHERE actor_id=$1),count(*) FROM selection_scopes`, actor, table).Scan(&actorCount, &tableCount, &totalCount, &totalMembers, &actorRequests, &totalRequests); err != nil {
		return record.SelectionResult{}, err
	}
	for _, budget := range []struct{ count, limit int }{{actorCount, 8}, {tableCount, 4}, {totalCount, 64}, {actorRequests, 64}, {totalRequests, 256}} {
		if budget.count >= budget.limit {
			return record.SelectionResult{}, selectionBudget("/scopeBudget", budget.limit)
		}
	}
	scopeID, err := id.New(id.SelectionPrefix)
	if err != nil {
		return record.SelectionResult{}, err
	}
	state := selectionState{Scope: record.SelectionScope{ID: scopeID, TableID: table, MembershipFingerprint: fp, CreatedAt: now, ExpiresAt: now.Add(record.SelectionTTL)}, Query: query, Fingerprint: fp, Schema: schema}
	if _, err = tx.ExecContext(ctx, `INSERT INTO selection_scopes(id,actor_id,table_id,request_id,request_hash,query,membership_fingerprint,schema_hash,semantic_data_version,member_count,created_at,expires_at) VALUES($1,$2,$3,$4,$5,$6::jsonb,$7,$8,'pending',0,$9,$10)`, scopeID, actor, table, requestID, hash[:], string(raw), fp, schema, now, state.Scope.ExpiresAt); err != nil {
		return record.SelectionResult{}, err
	}
	builder := &querySQLBuilder{}
	where, err := buildRecordWhere(builder, table, plan)
	if err != nil {
		return record.SelectionResult{}, err
	}
	scopeParam := builder.add(scopeID)
	if _, err = tx.ExecContext(ctx, `INSERT INTO selection_scope_members(scope_id,record_id) SELECT `+scopeParam+`,r.id FROM records r WHERE `+where+` LIMIT 100001`, builder.args...); err != nil {
		return record.SelectionResult{}, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT record_id FROM selection_scope_members WHERE scope_id=$1 ORDER BY record_id`, scopeID)
	if err != nil {
		return record.SelectionResult{}, err
	}
	digest := sha256.New()
	digest.Write([]byte("selection-members-v1\n" + schema + "\n"))
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return record.SelectionResult{}, err
		}
		state.Scope.Count++
		digest.Write([]byte(v + "\n"))
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return record.SelectionResult{}, err
	}
	if state.Scope.Count > record.SelectionMemberLimit {
		return record.SelectionResult{}, selectionBudget("/scopeMembers", record.SelectionMemberLimit)
	}
	if totalMembers+state.Scope.Count > 1000000 {
		return record.SelectionResult{}, selectionBudget("/totalScopeMembers", 1000000)
	}
	state.Scope.SemanticDataVersion = hex.EncodeToString(digest.Sum(nil))
	if _, err = tx.ExecContext(ctx, `UPDATE selection_scopes SET member_count=$2,semantic_data_version=$3 WHERE id=$1`, scopeID, state.Scope.Count, state.Scope.SemanticDataVersion); err != nil {
		return record.SelectionResult{}, err
	}
	result, err := selectionResult(ctx, tx, state, nil, nil)
	if err != nil {
		return result, err
	}
	return result, tx.Commit()
}

func validateSelectionInTx(ctx context.Context, tx *sql.Tx, actor, table string, page record.SelectionPage) (selectionState, error) {
	// Authorization is always live in this request's snapshot, never inferred from Token.
	if err := locklessActiveTable(ctx, tx, actor, table); err != nil {
		return selectionState{}, err
	}
	state, err := scanSelection(tx.QueryRowContext(ctx, `SELECT `+selectionColumns+` FROM selection_scopes WHERE id=$1 AND actor_id=$2 AND table_id=$3`, page.Token.ScopeID, actor, table))
	if errors.Is(err, sql.ErrNoRows) {
		return state, selectionExpired()
	}
	if err != nil {
		return state, err
	}
	if state.Invalid || !page.Now.Before(state.Scope.ExpiresAt) || state.Scope.ExpiresAt.UnixMicro() != page.Token.ExpiresAt {
		return state, selectionExpired()
	}
	_, fp, schema, err := normalizeSelectionContext(ctx, tx, actor, table, page.Query)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return state, selectionExpired()
		}
		var validation *domain.ValidationError
		var config *domain.ViewConfigurationRequiredError
		if errors.As(err, &validation) || errors.As(err, &config) {
			return state, selectionExpired()
		}
		return state, err
	}
	if fp != state.Fingerprint || schema != state.Schema {
		return state, selectionExpired()
	}
	// Also validate the original inherited context if the caller supplied an override.
	_, originalFP, originalSchema, err := normalizeSelectionContext(ctx, tx, actor, table, state.Query)
	if err != nil || originalFP != state.Fingerprint || originalSchema != state.Schema {
		return state, selectionExpired()
	}
	return state, nil
}
func (r *Repository) ResolveSelection(ctx context.Context, actor, table string, page record.SelectionPage, probes []string) (record.SelectionResult, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return record.SelectionResult{}, err
	}
	defer tx.Rollback()
	state, err := validateSelectionInTx(ctx, tx, actor, table, page)
	if err != nil {
		return record.SelectionResult{}, err
	}
	result, err := selectionResult(ctx, tx, state, page.ExcludedIDs, probes)
	if err != nil {
		return result, err
	}
	return result, tx.Commit()
}

// Caller holds Table, after business changes and before receipt/commit. Immutable
// members + the existing predicate distinguish actual transitions from revisions.
func invalidateSelections(ctx context.Context, tx *sql.Tx, actor, table string, changedIDs []string) error {
	rows, err := tx.QueryContext(ctx, `SELECT `+selectionColumns+` FROM selection_scopes WHERE table_id=$1 AND NOT invalidated AND expires_at>clock_timestamp() ORDER BY id`, table)
	if err != nil {
		return err
	}
	states := []selectionState{}
	for rows.Next() {
		state, err := scanSelection(rows)
		if err != nil {
			rows.Close()
			return err
		}
		states = append(states, state)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, state := range states {
		plan, fp, schema, contextErr := normalizeSelectionContext(ctx, tx, actor, table, state.Query)
		invalid := false
		if contextErr != nil {
			var validation *domain.ValidationError
			var config *domain.ViewConfigurationRequiredError
			var unsupported *domain.UnsupportedOperatorError
			if errors.Is(contextErr, domain.ErrNotFound) || errors.As(contextErr, &validation) || errors.As(contextErr, &config) || errors.As(contextErr, &unsupported) {
				invalid = true
			} else {
				return contextErr
			}
		} else {
			invalid = fp != state.Fingerprint || schema != state.Schema
		}
		if !invalid && len(changedIDs) > 0 {
			builder := &querySQLBuilder{}
			where, err := buildRecordWhere(builder, table, plan)
			if err != nil {
				return err
			}
			ids := builder.add(changedIDs)
			scope := builder.add(state.Scope.ID)
			// Candidate lookup uses records.id and members' composite PK, not a Table scan.
			query := `SELECT EXISTS(SELECT 1 FROM unnest(` + ids + `::text[]) candidate(id) WHERE
    EXISTS(SELECT 1 FROM selection_scope_members m WHERE m.scope_id=` + scope + ` AND m.record_id=candidate.id)
    IS DISTINCT FROM EXISTS(SELECT 1 FROM records r WHERE r.id=candidate.id AND ` + where + `))`
			if err := tx.QueryRowContext(ctx, query, builder.args...).Scan(&invalid); err != nil {
				return err
			}
		}
		if invalid {
			if _, err := tx.ExecContext(ctx, `UPDATE selection_scopes SET invalidated=true WHERE id=$1`, state.Scope.ID); err != nil {
				return err
			}
		}
	}
	return nil
}
