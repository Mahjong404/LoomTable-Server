package catalog

import (
	"context"
	"fmt"

	"github.com/Mahjong404/LoomTable-Server/internal/domain"
	"github.com/Mahjong404/LoomTable-Server/internal/id"
)

type ViewOrderInput struct {
	ExpectedOrderRevision int64    `json:"expectedOrderRevision"`
	ViewIDs               []string `json:"viewIds"`
}

func (s *Service) ListViewsSnapshot(ctx context.Context, actorID, tableID, lifecycle string) (domain.ViewListSnapshot, error) {
	if err := validateID("/tableId", id.TablePrefix, tableID); err != nil {
		return domain.ViewListSnapshot{}, err
	}
	if err := validateLifecycle(lifecycle); err != nil {
		return domain.ViewListSnapshot{}, err
	}
	if s == nil || s.store == nil {
		return domain.ViewListSnapshot{}, domain.ErrDependencyMissing
	}
	return s.store.ListViewsSnapshot(ctx, actorID, tableID, lifecycle)
}

func (s *Service) ReorderViews(ctx context.Context, actorID, key, tableID string, input ViewOrderInput) (domain.ViewOrderResult, error) {
	if err := validateID("/headers/Idempotency-Key", id.MutationPrefix, key); err != nil {
		return domain.ViewOrderResult{}, err
	}
	if err := validateID("/tableId", id.TablePrefix, tableID); err != nil {
		return domain.ViewOrderResult{}, err
	}
	issues := make([]domain.ValidationIssue, 0)
	if input.ExpectedOrderRevision < 1 {
		issues = append(issues, domain.ValidationIssue{Path: "/expectedOrderRevision", Code: "required", Message: "expectedOrderRevision must be at least 1"})
	}
	if input.ViewIDs == nil {
		issues = append(issues, domain.ValidationIssue{Path: "/viewIds", Code: "required", Message: "viewIds must be an array"})
	}
	if len(input.ViewIDs) > ViewLimitPerTable {
		issues = append(issues, domain.ValidationIssue{Path: "/viewIds", Code: "limit", Message: "at most 100 Views"})
	}
	seen := make(map[string]bool)
	for i, viewID := range input.ViewIDs {
		p := fmt.Sprintf("/viewIds/%d", i)
		if !id.Valid(id.ViewPrefix, viewID) {
			issues = append(issues, domain.ValidationIssue{Path: p, Code: "format", Message: "viewId must be a typed View ID"})
		}
		if seen[viewID] {
			issues = append(issues, domain.ValidationIssue{Path: p, Code: "duplicate", Message: "View ID occurs more than once"})
		}
		seen[viewID] = true
	}
	if len(issues) > 0 {
		return domain.ViewOrderResult{}, domain.NewValidationError(issues...)
	}
	if s == nil || s.store == nil {
		return domain.ViewOrderResult{}, domain.ErrDependencyMissing
	}
	hash, err := requestFingerprint("PUT", "/v1/tables/"+tableID+"/views/order", input)
	if err != nil {
		return domain.ViewOrderResult{}, err
	}
	return s.store.ReorderViews(ctx, actorID, key, tableID, hash, input)
}
