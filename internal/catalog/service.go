package catalog

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"github.com/Mahjong404/LoomTable-Server/internal/domain"
	"github.com/Mahjong404/LoomTable-Server/internal/id"
)

const (
	WorkspaceLimitPerActor = 100
	BaseLimitPerWorkspace  = 500
	TableLimitPerBase      = 500
)

type Store interface {
	ListWorkspaces(context.Context, string) ([]domain.Workspace, error)
	GetWorkspace(context.Context, string, string) (domain.Workspace, error)
	CreateWorkspace(context.Context, string, string, [32]byte, domain.Workspace) (domain.Workspace, error)
	UpdateWorkspace(context.Context, string, string, int64, string) (domain.Workspace, error)

	ListBases(context.Context, string, string) ([]domain.Base, error)
	GetBase(context.Context, string, string) (domain.Base, error)
	CreateBase(context.Context, string, string, [32]byte, domain.Base) (domain.Base, error)
	UpdateBase(context.Context, string, string, int64, string) (domain.Base, error)

	ListTables(context.Context, string, string, string) ([]domain.Table, error)
	GetTable(context.Context, string, string) (domain.Table, error)
	CreateTable(context.Context, string, string, [32]byte, domain.CreateTableResult) (domain.CreateTableResult, error)
	UpdateTable(context.Context, string, string, int64, string) (domain.Table, error)
	DeleteTable(context.Context, string, string, int64) error
	RestoreTable(context.Context, string, string, int64) (domain.Table, error)
}

type IDGenerator func(string) (string, error)

type Service struct {
	store Store
	newID IDGenerator
}

func New(store Store) *Service {
	return NewWithIDGenerator(store, id.New)
}

func NewWithIDGenerator(store Store, newID IDGenerator) *Service {
	return &Service{store: store, newID: newID}
}

func (s *Service) ListWorkspaces(ctx context.Context, actorID string) ([]domain.Workspace, error) {
	if s == nil || s.store == nil {
		return nil, domain.ErrDependencyMissing
	}
	return s.store.ListWorkspaces(ctx, actorID)
}

func (s *Service) GetWorkspace(ctx context.Context, actorID, workspaceID string) (domain.Workspace, error) {
	if err := validateID("/workspaceId", id.WorkspacePrefix, workspaceID); err != nil {
		return domain.Workspace{}, err
	}
	if s == nil || s.store == nil {
		return domain.Workspace{}, domain.ErrDependencyMissing
	}
	return s.store.GetWorkspace(ctx, actorID, workspaceID)
}

func (s *Service) CreateWorkspace(ctx context.Context, actorID, idempotencyKey, name string) (domain.Workspace, error) {
	if err := validateID("/headers/Idempotency-Key", id.MutationPrefix, idempotencyKey); err != nil {
		return domain.Workspace{}, err
	}
	normalized, err := domain.NormalizeResourceName("/name", name)
	if err != nil {
		return domain.Workspace{}, err
	}
	if s == nil || s.store == nil {
		return domain.Workspace{}, domain.ErrDependencyMissing
	}
	workspaceID, err := s.newID(id.WorkspacePrefix)
	if err != nil {
		return domain.Workspace{}, fmt.Errorf("generate workspace ID: %w", err)
	}
	workspace := domain.Workspace{ID: workspaceID, Name: normalized, Revision: 1}
	fingerprint, err := requestFingerprint("POST", "/v1/workspaces", struct {
		Name string `json:"name"`
	}{Name: normalized})
	if err != nil {
		return domain.Workspace{}, err
	}
	return s.store.CreateWorkspace(ctx, actorID, idempotencyKey, fingerprint, workspace)
}

func (s *Service) UpdateWorkspace(ctx context.Context, actorID, workspaceID string, expectedRevision int64, name string) (domain.Workspace, error) {
	if err := validateID("/workspaceId", id.WorkspacePrefix, workspaceID); err != nil {
		return domain.Workspace{}, err
	}
	if expectedRevision < 1 {
		return domain.Workspace{}, domain.NewValidationError(domain.ValidationIssue{Path: "/expectedRevision", Code: "required", Message: "expectedRevision must be at least 1"})
	}
	normalized, err := domain.NormalizeResourceName("/name", name)
	if err != nil {
		return domain.Workspace{}, err
	}
	if s == nil || s.store == nil {
		return domain.Workspace{}, domain.ErrDependencyMissing
	}
	return s.store.UpdateWorkspace(ctx, actorID, workspaceID, expectedRevision, normalized)
}

func (s *Service) ListBases(ctx context.Context, actorID, workspaceID string) ([]domain.Base, error) {
	if err := validateID("/workspaceId", id.WorkspacePrefix, workspaceID); err != nil {
		return nil, err
	}
	if s == nil || s.store == nil {
		return nil, domain.ErrDependencyMissing
	}
	return s.store.ListBases(ctx, actorID, workspaceID)
}

func (s *Service) GetBase(ctx context.Context, actorID, baseID string) (domain.Base, error) {
	if err := validateID("/baseId", id.BasePrefix, baseID); err != nil {
		return domain.Base{}, err
	}
	if s == nil || s.store == nil {
		return domain.Base{}, domain.ErrDependencyMissing
	}
	return s.store.GetBase(ctx, actorID, baseID)
}

func (s *Service) CreateBase(ctx context.Context, actorID, idempotencyKey, workspaceID, name string) (domain.Base, error) {
	if err := validateID("/headers/Idempotency-Key", id.MutationPrefix, idempotencyKey); err != nil {
		return domain.Base{}, err
	}
	if err := validateID("/workspaceId", id.WorkspacePrefix, workspaceID); err != nil {
		return domain.Base{}, err
	}
	normalized, err := domain.NormalizeResourceName("/name", name)
	if err != nil {
		return domain.Base{}, err
	}
	if s == nil || s.store == nil {
		return domain.Base{}, domain.ErrDependencyMissing
	}
	baseID, err := s.newID(id.BasePrefix)
	if err != nil {
		return domain.Base{}, fmt.Errorf("generate base ID: %w", err)
	}
	base := domain.Base{ID: baseID, WorkspaceID: workspaceID, Name: normalized, Revision: 1}
	fingerprint, err := requestFingerprint("POST", "/v1/bases", struct {
		WorkspaceID string `json:"workspaceId"`
		Name        string `json:"name"`
	}{WorkspaceID: workspaceID, Name: normalized})
	if err != nil {
		return domain.Base{}, err
	}
	return s.store.CreateBase(ctx, actorID, idempotencyKey, fingerprint, base)
}

func (s *Service) UpdateBase(ctx context.Context, actorID, baseID string, expectedRevision int64, name string) (domain.Base, error) {
	if err := validateID("/baseId", id.BasePrefix, baseID); err != nil {
		return domain.Base{}, err
	}
	if expectedRevision < 1 {
		return domain.Base{}, domain.NewValidationError(domain.ValidationIssue{Path: "/expectedRevision", Code: "required", Message: "expectedRevision must be at least 1"})
	}
	normalized, err := domain.NormalizeResourceName("/name", name)
	if err != nil {
		return domain.Base{}, err
	}
	if s == nil || s.store == nil {
		return domain.Base{}, domain.ErrDependencyMissing
	}
	return s.store.UpdateBase(ctx, actorID, baseID, expectedRevision, normalized)
}

func (s *Service) ListTables(ctx context.Context, actorID, baseID, lifecycle string) ([]domain.Table, error) {
	if err := validateID("/baseId", id.BasePrefix, baseID); err != nil {
		return nil, err
	}
	if err := validateLifecycle(lifecycle); err != nil {
		return nil, err
	}
	if s == nil || s.store == nil {
		return nil, domain.ErrDependencyMissing
	}
	return s.store.ListTables(ctx, actorID, baseID, lifecycle)
}

func (s *Service) GetTable(ctx context.Context, actorID, tableID string) (domain.Table, error) {
	if err := validateID("/tableId", id.TablePrefix, tableID); err != nil {
		return domain.Table{}, err
	}
	if s == nil || s.store == nil {
		return domain.Table{}, domain.ErrDependencyMissing
	}
	return s.store.GetTable(ctx, actorID, tableID)
}

func (s *Service) CreateTable(ctx context.Context, actorID, idempotencyKey, baseID, name string, primaryFieldName, initialViewName *string) (domain.CreateTableResult, error) {
	if err := validateID("/headers/Idempotency-Key", id.MutationPrefix, idempotencyKey); err != nil {
		return domain.CreateTableResult{}, err
	}
	if err := validateID("/baseId", id.BasePrefix, baseID); err != nil {
		return domain.CreateTableResult{}, err
	}
	normalizedName, err := domain.NormalizeResourceName("/name", name)
	if err != nil {
		return domain.CreateTableResult{}, err
	}
	primaryName := "Name"
	if primaryFieldName != nil {
		primaryName, err = domain.NormalizeResourceName("/primaryFieldName", *primaryFieldName)
		if err != nil {
			return domain.CreateTableResult{}, err
		}
	}
	viewName := "Grid"
	if initialViewName != nil {
		viewName, err = domain.NormalizeResourceName("/initialViewName", *initialViewName)
		if err != nil {
			return domain.CreateTableResult{}, err
		}
	}
	if s == nil || s.store == nil {
		return domain.CreateTableResult{}, domain.ErrDependencyMissing
	}

	tableID, err := s.newID(id.TablePrefix)
	if err != nil {
		return domain.CreateTableResult{}, fmt.Errorf("generate table ID: %w", err)
	}
	fieldID, err := s.newID(id.FieldPrefix)
	if err != nil {
		return domain.CreateTableResult{}, fmt.Errorf("generate primary field ID: %w", err)
	}
	viewID, err := s.newID(id.ViewPrefix)
	if err != nil {
		return domain.CreateTableResult{}, fmt.Errorf("generate initial view ID: %w", err)
	}

	result := domain.CreateTableResult{
		Table: domain.Table{
			ID:             tableID,
			BaseID:         baseID,
			Name:           normalizedName,
			PrimaryFieldID: fieldID,
			Revision:       1,
		},
		PrimaryField: domain.Field{
			ID:            fieldID,
			TableID:       tableID,
			Name:          primaryName,
			Position:      0,
			SchemaVersion: 1,
			Revision:      1,
			Type:          "text",
			Config:        map[string]any{},
		},
		InitialView: domain.GridView{
			ID:       viewID,
			TableID:  tableID,
			Name:     viewName,
			Type:     "grid",
			Revision: 1,
			Config: domain.GridViewConfig{
				Projection:     []string{fieldID},
				ColumnOrder:    []string{fieldID},
				ColumnWidths:   map[string]int{},
				FrozenFieldIDs: []string{},
				RowHeight:      "standard",
				Sort:           []domain.SortSpec{},
			},
		},
	}
	fingerprint, err := requestFingerprint("POST", "/v1/tables", struct {
		BaseID           string `json:"baseId"`
		Name             string `json:"name"`
		PrimaryFieldName string `json:"primaryFieldName"`
		InitialViewName  string `json:"initialViewName"`
	}{BaseID: baseID, Name: normalizedName, PrimaryFieldName: primaryName, InitialViewName: viewName})
	if err != nil {
		return domain.CreateTableResult{}, err
	}
	return s.store.CreateTable(ctx, actorID, idempotencyKey, fingerprint, result)
}

func (s *Service) UpdateTable(ctx context.Context, actorID, tableID string, expectedRevision int64, name string) (domain.Table, error) {
	if err := validateID("/tableId", id.TablePrefix, tableID); err != nil {
		return domain.Table{}, err
	}
	if expectedRevision < 1 {
		return domain.Table{}, domain.NewValidationError(domain.ValidationIssue{Path: "/expectedRevision", Code: "required", Message: "expectedRevision must be at least 1"})
	}
	normalized, err := domain.NormalizeResourceName("/name", name)
	if err != nil {
		return domain.Table{}, err
	}
	if s == nil || s.store == nil {
		return domain.Table{}, domain.ErrDependencyMissing
	}
	return s.store.UpdateTable(ctx, actorID, tableID, expectedRevision, normalized)
}

func (s *Service) DeleteTable(ctx context.Context, actorID, tableID string, expectedRevision int64) error {
	if err := validateID("/tableId", id.TablePrefix, tableID); err != nil {
		return err
	}
	if expectedRevision < 1 {
		return &domain.BadRequestError{Message: "expectedRevision must be at least 1"}
	}
	if s == nil || s.store == nil {
		return domain.ErrDependencyMissing
	}
	return s.store.DeleteTable(ctx, actorID, tableID, expectedRevision)
}

func (s *Service) RestoreTable(ctx context.Context, actorID, tableID string, expectedRevision int64) (domain.Table, error) {
	if err := validateID("/tableId", id.TablePrefix, tableID); err != nil {
		return domain.Table{}, err
	}
	if expectedRevision < 1 {
		return domain.Table{}, domain.NewValidationError(domain.ValidationIssue{Path: "/expectedRevision", Code: "required", Message: "expectedRevision must be at least 1"})
	}
	if s == nil || s.store == nil {
		return domain.Table{}, domain.ErrDependencyMissing
	}
	return s.store.RestoreTable(ctx, actorID, tableID, expectedRevision)
}

func validateID(path, prefix, value string) error {
	if !id.Valid(prefix, value) {
		return &domain.BadRequestError{Message: fmt.Sprintf("%s has an invalid typed ID", path)}
	}
	return nil
}

func validateLifecycle(value string) error {
	if value == "active" || value == "deleted" || value == "all" {
		return nil
	}
	return &domain.BadRequestError{Message: "lifecycle must be active, deleted, or all"}
}

func requestFingerprint(method, path string, body any) ([32]byte, error) {
	canonical, err := json.Marshal(struct {
		Method string `json:"method"`
		Path   string `json:"path"`
		Body   any    `json:"body"`
	}{Method: method, Path: path, Body: body})
	if err != nil {
		return [32]byte{}, fmt.Errorf("canonicalize request: %w", err)
	}
	return sha256.Sum256(canonical), nil
}
