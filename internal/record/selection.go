package record

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/Mahjong404/LoomTable-Server/internal/domain"
	"github.com/Mahjong404/LoomTable-Server/internal/id"
	"golang.org/x/text/unicode/norm"
)

const SelectionTTL = 5 * time.Minute
const SelectionMemberLimit = 100000

type SelectionQuery struct {
	ViewID        string             `json:"viewId,omitempty"`
	Filter        *domain.FilterNode `json:"filter,omitempty"`
	FilterPresent bool               `json:"filterPresent"`
	Search        string             `json:"search"`
}
type SelectionScope struct {
	ID                    string    `json:"id"`
	Token                 string    `json:"token"`
	TableID               string    `json:"tableId"`
	MembershipFingerprint string    `json:"membershipFingerprint"`
	SemanticDataVersion   string    `json:"semanticDataVersion"`
	Count                 int64     `json:"count"`
	CreatedAt             time.Time `json:"createdAt"`
	ExpiresAt             time.Time `json:"expiresAt"`
}
type SelectionMembership struct {
	RecordID string `json:"recordId"`
	InScope  bool   `json:"inScope"`
	Selected bool   `json:"selected"`
}
type SelectionResult struct {
	Scope         SelectionScope        `json:"scope"`
	ExcludedIDs   []string              `json:"excludedIds"`
	ExcludedCount int                   `json:"excludedCount"`
	SelectedCount int64                 `json:"selectedCount"`
	Membership    []SelectionMembership `json:"membership"`
}
type SelectionToken struct {
	ScopeID   string `json:"scopeId"`
	ActorID   string `json:"actorId"`
	TableID   string `json:"tableId"`
	ExpiresAt int64  `json:"expiresAt"`
}
type SelectionPage struct {
	ExpectedPageSequence *int64
	Token                SelectionToken
	Query                SelectionQuery
	ExcludedIDs          []string
	Now                  time.Time
}
type SelectionStore interface {
	AcquireSelection(context.Context, string, string, string, SelectionQuery, time.Time) (SelectionResult, error)
	ResolveSelection(context.Context, string, string, SelectionPage, []string) (SelectionResult, error)
}

type SelectionQueryStore interface {
	ResolveSelectionQuery(context.Context, string, string, SelectionPage) (QueryMetadata, error)
}

// Preserve a bounded pre-fold input for revalidation: case folding can expand
// a valid 500-code-point Search beyond the pre-fold Query input limit.
func CanonicalSelectionQuery(query SelectionQuery, plan QueryPlan) SelectionQuery {
	query.Search = norm.NFC.String(strings.TrimFunc(query.Search, unicode.IsSpace))
	query.Filter = plan.Filter
	if !query.FilterPresent {
		query.Filter = nil
	}
	return query
}

// SelectionPlan shares Query normalization, while excluding presentation defaults.
func SelectionPlan(query SelectionQuery, metadata QueryMetadata) (QueryPlan, string, string, error) {
	if query.ViewID != "" && !id.Valid(id.ViewPrefix, query.ViewID) {
		return QueryPlan{}, "", "", domain.NewValidationError(domain.ValidationIssue{Path: "/query/viewId", Code: "format", Message: "viewId must be a typed View ID"})
	}
	plan, err := buildQueryPlan(QueryRequest{ViewID: query.ViewID, ViewIDPresent: query.ViewID != "", Filter: query.Filter, FilterPresent: query.FilterPresent, Search: query.Search, SortPresent: true, ProjectionPresent: true, Projection: []string{}}, metadata)
	if err != nil {
		return QueryPlan{}, "", "", err
	}
	plan.Filter = canonicalSelectionFilter(plan.Filter)
	fingerprint, err := selectionHash(struct {
		Version, Table, Lifecycle string
		Filter                    *domain.FilterNode
		Search                    string
	}{"selection-v1", metadata.TableID, "active", plan.Filter, plan.Search})
	if err != nil {
		return QueryPlan{}, "", "", err
	}
	refs := map[string]struct{}{}
	collectFilterFields(plan.Filter, refs)
	if plan.Search != "" {
		for fieldID, field := range metadata.Fields {
			if field.DeletedAt == nil && (field.Type == "text" || field.Type == "longText" || field.Type == "url") {
				refs[fieldID] = struct{}{}
			}
		}
	}
	type fieldContext struct{ ID, Type string }
	fields := make([]fieldContext, 0, len(refs))
	for fieldID := range refs {
		fields = append(fields, fieldContext{fieldID, metadata.Fields[fieldID].Type})
	}
	sort.Slice(fields, func(i, j int) bool { return fields[i].ID < fields[j].ID })
	schema, err := selectionHash(fields)
	return plan, fingerprint, schema, err
}
func selectionHash(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}
func canonicalSelectionFilter(node *domain.FilterNode) *domain.FilterNode {
	if node == nil {
		return nil
	}
	result := cloneFilter(node)
	for i := range result.Children {
		result.Children[i] = *canonicalSelectionFilter(&result.Children[i])
	}
	sort.Slice(result.Children, func(i, j int) bool {
		a, _ := json.Marshal(result.Children[i])
		b, _ := json.Marshal(result.Children[j])
		return string(a) < string(b)
	})
	children := result.Children[:0]
	previous := ""
	for _, child := range result.Children {
		encoded, _ := json.Marshal(child)
		if string(encoded) != previous {
			children = append(children, child)
			previous = string(encoded)
		}
	}
	result.Children = children
	return result
}
func ValidateSelectionIDs(excluded, probes []string) error {
	for _, list := range []struct {
		path  string
		ids   []string
		limit int
	}{{"/excludedIds", excluded, 5000}, {"/recordIds", probes, 500}} {
		if len(list.ids) > list.limit {
			return domain.NewValidationError(domain.ValidationIssue{Path: list.path, Code: "limit", Message: fmt.Sprintf("at most %d IDs", list.limit)})
		}
		seen := map[string]bool{}
		for _, v := range list.ids {
			if !id.Valid(id.RecordPrefix, v) || seen[v] {
				return domain.NewValidationError(domain.ValidationIssue{Path: list.path, Code: "format", Message: "unique typed Record IDs required"})
			}
			seen[v] = true
		}
	}
	return nil
}
func (s *Service) selectionToken(ctx context.Context, actor, table, token string) (SelectionToken, error) {
	signer, err := s.cursorSigner(ctx)
	if err != nil {
		return SelectionToken{}, err
	}
	var payload SelectionToken
	if signer.Decode("row-selection", token, &payload) != nil || payload.ActorID != actor || payload.TableID != table || !id.Valid(id.SelectionPrefix, payload.ScopeID) {
		return payload, &domain.InvalidCursorError{}
	}
	if s.now().UTC().UnixMicro() >= payload.ExpiresAt {
		return payload, &domain.QuerySnapshotExpiredError{}
	}
	return payload, nil
}
func (s *Service) decorateSelection(ctx context.Context, actor string, result *SelectionResult) error {
	signer, err := s.cursorSigner(ctx)
	if err != nil {
		return err
	}
	result.Scope.Token, err = signer.Encode("row-selection", SelectionToken{result.Scope.ID, actor, result.Scope.TableID, result.Scope.ExpiresAt.UnixMicro()})
	return err
}
func (s *Service) AcquireSelection(ctx context.Context, actor, table, requestID string, query SelectionQuery) (SelectionResult, error) {
	if !id.Valid(id.TablePrefix, table) || !id.Valid(id.RequestPrefix, requestID) {
		return SelectionResult{}, &domain.BadRequestError{Message: "typed Table and Request IDs required"}
	}
	store, ok := s.store.(SelectionStore)
	if !ok {
		return SelectionResult{}, domain.ErrDependencyMissing
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	result, err := store.AcquireSelection(ctx, actor, table, requestID, query, s.now().UTC())
	if err != nil {
		if ctx.Err() != nil {
			return result, domain.ErrDependencyMissing
		}
		return result, err
	}
	err = s.decorateSelection(ctx, actor, &result)
	return result, err
}
func (s *Service) ResolveSelection(ctx context.Context, actor, table, token string, query SelectionQuery, excluded, probes []string) (SelectionResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if !id.Valid(id.TablePrefix, table) {
		return SelectionResult{}, &domain.BadRequestError{Message: "typed Table ID required"}
	}
	if err := ValidateSelectionIDs(excluded, probes); err != nil {
		return SelectionResult{}, err
	}
	payload, err := s.selectionToken(ctx, actor, table, token)
	if err != nil {
		return SelectionResult{}, err
	}
	store, ok := s.store.(SelectionStore)
	if !ok {
		return SelectionResult{}, domain.ErrDependencyMissing
	}
	result, err := store.ResolveSelection(ctx, actor, table, SelectionPage{Token: payload, Query: query, ExcludedIDs: excluded, Now: s.now().UTC()}, probes)
	if err != nil {
		if ctx.Err() != nil {
			return result, domain.ErrDependencyMissing
		}
		return result, err
	}
	err = s.decorateSelection(ctx, actor, &result)
	return result, err
}
