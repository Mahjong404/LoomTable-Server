package catalog

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/Mahjong404/LoomTable-Server/internal/domain"
	"github.com/Mahjong404/LoomTable-Server/internal/id"
	loomrecord "github.com/Mahjong404/LoomTable-Server/internal/record"
)

const (
	WorkspaceLimitPerActor = 100
	BaseLimitPerWorkspace  = 500
	TableLimitPerBase      = 500
	FieldLimitPerTable     = 500
	ViewLimitPerTable      = 100
)

type FieldInput struct {
	Name   string
	Type   string
	Config any
}

type SelectFieldConfigInput struct {
	Options []SelectOptionInput
}

type SelectOptionInput struct {
	ID    *string
	Name  string
	Color string
}

type FieldUpdate struct {
	Name             *string
	Type             string
	Config           any
	ExpectedRevision int64
}

type ViewInput struct {
	Name   string
	Type   string
	Config any
}

type ViewUpdate struct {
	Name             *string
	Type             string
	Config           any
	ExpectedRevision int64
}

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

	ListFields(context.Context, string, string, string) ([]domain.Field, error)
	GetField(context.Context, string, string) (domain.Field, error)
	CreateField(context.Context, string, string, [32]byte, domain.Field) (domain.Field, error)
	UpdateField(context.Context, string, string, int64, domain.Field) (domain.Field, error)
	DeleteField(context.Context, string, string, int64) error
	RestoreField(context.Context, string, string, int64) (domain.Field, error)

	ListViews(context.Context, string, string, string) ([]domain.View, error)
	GetView(context.Context, string, string) (domain.View, error)
	CreateView(context.Context, string, string, [32]byte, domain.View) (domain.View, error)
	UpdateView(context.Context, string, string, int64, domain.View) (domain.View, error)
	DeleteView(context.Context, string, string, int64) error
	RestoreView(context.Context, string, string, int64) (domain.View, error)
}

type IDGenerator func(string) (string, error)

type Service struct {
	store Store
	newID IDGenerator
	now   func() time.Time
}

func New(store Store) *Service {
	return NewWithIDGeneratorAndClock(store, id.New, time.Now)
}

func NewWithIDGenerator(store Store, newID IDGenerator) *Service {
	return NewWithIDGeneratorAndClock(store, newID, time.Now)
}

func NewWithIDGeneratorAndClock(store Store, newID IDGenerator, now func() time.Time) *Service {
	return &Service{store: store, newID: newID, now: now}
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

func (s *Service) ListFields(ctx context.Context, actorID, tableID, lifecycle string) ([]domain.Field, error) {
	if err := validateID("/tableId", id.TablePrefix, tableID); err != nil {
		return nil, err
	}
	if err := validateLifecycle(lifecycle); err != nil {
		return nil, err
	}
	if s == nil || s.store == nil {
		return nil, domain.ErrDependencyMissing
	}
	return s.store.ListFields(ctx, actorID, tableID, lifecycle)
}

func (s *Service) CreateField(ctx context.Context, actorID, idempotencyKey, tableID string, input FieldInput) (domain.Field, error) {
	if err := validateID("/headers/Idempotency-Key", id.MutationPrefix, idempotencyKey); err != nil {
		return domain.Field{}, err
	}
	if err := validateID("/tableId", id.TablePrefix, tableID); err != nil {
		return domain.Field{}, err
	}
	name, err := domain.NormalizeResourceName("/name", input.Name)
	if err != nil {
		return domain.Field{}, err
	}
	if s == nil || s.store == nil {
		return domain.Field{}, domain.ErrDependencyMissing
	}
	config, fingerprintConfig, err := s.normalizeNewFieldConfig(input.Type, input.Config)
	if err != nil {
		return domain.Field{}, err
	}
	fieldID, err := s.newID(id.FieldPrefix)
	if err != nil {
		return domain.Field{}, fmt.Errorf("generate field ID: %w", err)
	}
	field := domain.Field{
		ID:            fieldID,
		TableID:       tableID,
		Name:          name,
		SchemaVersion: 1,
		Revision:      1,
		Type:          input.Type,
		Config:        config,
	}
	fingerprint, err := requestFingerprint("POST", "/v1/tables/"+tableID+"/fields", FieldInput{
		Name: name, Type: input.Type, Config: fingerprintConfig,
	})
	if err != nil {
		return domain.Field{}, err
	}
	return s.store.CreateField(ctx, actorID, idempotencyKey, fingerprint, field)
}

func (s *Service) UpdateField(ctx context.Context, actorID, fieldID string, update FieldUpdate) (domain.Field, error) {
	if err := validateID("/fieldId", id.FieldPrefix, fieldID); err != nil {
		return domain.Field{}, err
	}
	if update.ExpectedRevision < 1 {
		return domain.Field{}, domain.NewValidationError(domain.ValidationIssue{Path: "/expectedRevision", Code: "required", Message: "expectedRevision must be at least 1"})
	}
	if s == nil || s.store == nil {
		return domain.Field{}, domain.ErrDependencyMissing
	}
	current, err := s.store.GetField(ctx, actorID, fieldID)
	if err != nil {
		return domain.Field{}, err
	}
	if current.Revision != update.ExpectedRevision {
		return domain.Field{}, &domain.RevisionConflictError{
			Resource: "field", ID: fieldID, ExpectedRevision: update.ExpectedRevision, CurrentRevision: current.Revision,
		}
	}
	if current.DeletedAt != nil {
		return domain.Field{}, &domain.InvalidStateTransitionError{Resource: "field", ID: fieldID, Action: "update", Current: "deleted"}
	}
	if update.Type != current.Type {
		return domain.Field{}, domain.NewValidationError(domain.ValidationIssue{Path: "/type", Code: "format", Message: "Field type is immutable in P0"})
	}
	if update.Name == nil && update.Config == nil {
		return domain.Field{}, domain.NewValidationError(domain.ValidationIssue{Path: "", Code: "required", Message: "name or config is required"})
	}

	target := current
	if update.Name != nil {
		target.Name, err = domain.NormalizeResourceName("/name", *update.Name)
		if err != nil {
			return domain.Field{}, err
		}
	}
	if update.Config != nil {
		target.Config, err = s.normalizeUpdatedFieldConfig(current, update.Config)
		if err != nil {
			return domain.Field{}, err
		}
	}
	return s.store.UpdateField(ctx, actorID, fieldID, update.ExpectedRevision, target)
}

func (s *Service) DeleteField(ctx context.Context, actorID, fieldID string, expectedRevision int64) error {
	if err := validateID("/fieldId", id.FieldPrefix, fieldID); err != nil {
		return err
	}
	if expectedRevision < 1 {
		return &domain.BadRequestError{Message: "expectedRevision must be at least 1"}
	}
	if s == nil || s.store == nil {
		return domain.ErrDependencyMissing
	}
	return s.store.DeleteField(ctx, actorID, fieldID, expectedRevision)
}

func (s *Service) RestoreField(ctx context.Context, actorID, fieldID string, expectedRevision int64) (domain.Field, error) {
	if err := validateID("/fieldId", id.FieldPrefix, fieldID); err != nil {
		return domain.Field{}, err
	}
	if expectedRevision < 1 {
		return domain.Field{}, domain.NewValidationError(domain.ValidationIssue{Path: "/expectedRevision", Code: "required", Message: "expectedRevision must be at least 1"})
	}
	if s == nil || s.store == nil {
		return domain.Field{}, domain.ErrDependencyMissing
	}
	return s.store.RestoreField(ctx, actorID, fieldID, expectedRevision)
}

func (s *Service) CreateView(ctx context.Context, actorID, idempotencyKey, tableID string, input ViewInput) (domain.View, error) {
	if err := validateID("/headers/Idempotency-Key", id.MutationPrefix, idempotencyKey); err != nil {
		return domain.View{}, err
	}
	if err := validateID("/tableId", id.TablePrefix, tableID); err != nil {
		return domain.View{}, err
	}
	name, err := domain.NormalizeResourceName("/name", input.Name)
	if err != nil {
		return domain.View{}, err
	}
	if s == nil || s.store == nil {
		return domain.View{}, domain.ErrDependencyMissing
	}
	fields, err := s.store.ListFields(ctx, actorID, tableID, "all")
	if err != nil {
		return domain.View{}, err
	}
	config, err := normalizeViewConfig(input.Type, input.Config, fields)
	if err != nil {
		return domain.View{}, err
	}
	viewID, err := s.newID(id.ViewPrefix)
	if err != nil {
		return domain.View{}, fmt.Errorf("generate view ID: %w", err)
	}
	view := domain.View{
		ID: viewID, TableID: tableID, Name: name, Type: input.Type, Config: config, Revision: 1,
	}
	fingerprint, err := requestFingerprint("POST", "/v1/tables/"+tableID+"/views", ViewInput{
		Name: name, Type: input.Type, Config: config,
	})
	if err != nil {
		return domain.View{}, err
	}
	return s.store.CreateView(ctx, actorID, idempotencyKey, fingerprint, view)
}

func (s *Service) ListViews(ctx context.Context, actorID, tableID, lifecycle string) ([]domain.View, error) {
	if err := validateID("/tableId", id.TablePrefix, tableID); err != nil {
		return nil, err
	}
	if err := validateLifecycle(lifecycle); err != nil {
		return nil, err
	}
	if s == nil || s.store == nil {
		return nil, domain.ErrDependencyMissing
	}
	return s.store.ListViews(ctx, actorID, tableID, lifecycle)
}

func (s *Service) GetView(ctx context.Context, actorID, viewID string) (domain.View, error) {
	if err := validateID("/viewId", id.ViewPrefix, viewID); err != nil {
		return domain.View{}, err
	}
	if s == nil || s.store == nil {
		return domain.View{}, domain.ErrDependencyMissing
	}
	return s.store.GetView(ctx, actorID, viewID)
}

func (s *Service) UpdateView(ctx context.Context, actorID, viewID string, update ViewUpdate) (domain.View, error) {
	if err := validateID("/viewId", id.ViewPrefix, viewID); err != nil {
		return domain.View{}, err
	}
	if update.ExpectedRevision < 1 {
		return domain.View{}, domain.NewValidationError(domain.ValidationIssue{Path: "/expectedRevision", Code: "required", Message: "expectedRevision must be at least 1"})
	}
	if s == nil || s.store == nil {
		return domain.View{}, domain.ErrDependencyMissing
	}
	current, err := s.store.GetView(ctx, actorID, viewID)
	if err != nil {
		return domain.View{}, err
	}
	if current.Revision != update.ExpectedRevision {
		return domain.View{}, &domain.RevisionConflictError{
			Resource: "view", ID: viewID, ExpectedRevision: update.ExpectedRevision, CurrentRevision: current.Revision,
		}
	}
	if current.DeletedAt != nil {
		return domain.View{}, &domain.InvalidStateTransitionError{Resource: "view", ID: viewID, Action: "update", Current: "deleted"}
	}
	if update.Type != current.Type {
		return domain.View{}, domain.NewValidationError(domain.ValidationIssue{Path: "/type", Code: "format", Message: "View type is immutable in P0"})
	}
	if update.Config == nil {
		return domain.View{}, domain.NewValidationError(domain.ValidationIssue{Path: "/config", Code: "required", Message: "config is required"})
	}
	target := current
	if update.Name != nil {
		target.Name, err = domain.NormalizeResourceName("/name", *update.Name)
		if err != nil {
			return domain.View{}, err
		}
	}
	fields, err := s.store.ListFields(ctx, actorID, current.TableID, "all")
	if err != nil {
		return domain.View{}, err
	}
	target.Config, err = normalizeViewConfig(current.Type, update.Config, fields)
	if err != nil {
		return domain.View{}, err
	}
	return s.store.UpdateView(ctx, actorID, viewID, update.ExpectedRevision, target)
}

func (s *Service) DeleteView(ctx context.Context, actorID, viewID string, expectedRevision int64) error {
	if err := validateID("/viewId", id.ViewPrefix, viewID); err != nil {
		return err
	}
	if expectedRevision < 1 {
		return &domain.BadRequestError{Message: "expectedRevision must be at least 1"}
	}
	if s == nil || s.store == nil {
		return domain.ErrDependencyMissing
	}
	return s.store.DeleteView(ctx, actorID, viewID, expectedRevision)
}

func (s *Service) RestoreView(ctx context.Context, actorID, viewID string, expectedRevision int64) (domain.View, error) {
	if err := validateID("/viewId", id.ViewPrefix, viewID); err != nil {
		return domain.View{}, err
	}
	if expectedRevision < 1 {
		return domain.View{}, domain.NewValidationError(domain.ValidationIssue{Path: "/expectedRevision", Code: "required", Message: "expectedRevision must be at least 1"})
	}
	if s == nil || s.store == nil {
		return domain.View{}, domain.ErrDependencyMissing
	}
	return s.store.RestoreView(ctx, actorID, viewID, expectedRevision)
}

func (s *Service) normalizeNewFieldConfig(fieldType string, raw any) (any, any, error) {
	switch fieldType {
	case "text", "longText", "number", "checkbox", "date", "url", "location":
		if _, ok := raw.(domain.EmptyFieldConfig); !ok {
			return nil, nil, domain.NewValidationError(domain.ValidationIssue{Path: "/config", Code: "type", Message: "config must be an empty object"})
		}
		return domain.EmptyFieldConfig{}, domain.EmptyFieldConfig{}, nil
	case "select", "multiSelect":
		input, ok := raw.(SelectFieldConfigInput)
		if !ok {
			return nil, nil, domain.NewValidationError(domain.ValidationIssue{Path: "/config", Code: "type", Message: "config must contain an options array"})
		}
		return s.normalizeNewSelectConfig(input)
	default:
		return nil, nil, domain.NewValidationError(domain.ValidationIssue{Path: "/type", Code: "format", Message: "unsupported Field type"})
	}
}

func (s *Service) normalizeNewSelectConfig(input SelectFieldConfigInput) (domain.SelectFieldConfig, SelectFieldConfigInput, error) {
	if len(input.Options) > 500 {
		return domain.SelectFieldConfig{}, SelectFieldConfigInput{}, domain.NewValidationError(domain.ValidationIssue{Path: "/config/options", Code: "limit", Message: "at most 500 active options are allowed"})
	}
	options := make([]domain.SelectOption, 0, len(input.Options))
	normalizedInput := SelectFieldConfigInput{Options: make([]SelectOptionInput, 0, len(input.Options))}
	seenNames := make(map[string]struct{}, len(input.Options))
	issues := make([]domain.ValidationIssue, 0)
	for index, option := range input.Options {
		path := fmt.Sprintf("/config/options/%d", index)
		if option.ID != nil {
			issues = append(issues, domain.ValidationIssue{Path: path + "/id", Code: "invalidReference", Message: "an option ID cannot be supplied when creating a Field"})
			continue
		}
		name, err := domain.NormalizeOptionName(path+"/name", option.Name)
		if err != nil {
			var validation *domain.ValidationError
			if errors.As(err, &validation) {
				issues = append(issues, validation.Issues...)
			}
			continue
		}
		if !validOptionColor(option.Color) {
			issues = append(issues, domain.ValidationIssue{Path: path + "/color", Code: "format", Message: "unsupported option color"})
			continue
		}
		key := domain.FoldKey(name)
		if _, exists := seenNames[key]; exists {
			issues = append(issues, domain.ValidationIssue{Path: path + "/name", Code: "duplicate", Message: "active option names must be unique"})
			continue
		}
		seenNames[key] = struct{}{}
		optionID, err := s.newID(id.OptionPrefix)
		if err != nil {
			return domain.SelectFieldConfig{}, SelectFieldConfigInput{}, fmt.Errorf("generate option ID: %w", err)
		}
		options = append(options, domain.SelectOption{ID: optionID, Name: name, Color: option.Color})
		normalizedInput.Options = append(normalizedInput.Options, SelectOptionInput{Name: name, Color: option.Color})
	}
	if len(issues) > 0 {
		return domain.SelectFieldConfig{}, SelectFieldConfigInput{}, domain.NewValidationError(issues...)
	}
	return domain.SelectFieldConfig{
		Options:        options,
		DeletedOptions: make([]domain.DeletedSelectOption, 0),
	}, normalizedInput, nil
}

func (s *Service) normalizeUpdatedFieldConfig(current domain.Field, raw any) (any, error) {
	switch current.Type {
	case "text", "longText", "number", "checkbox", "date", "url", "location":
		if _, ok := raw.(domain.EmptyFieldConfig); !ok {
			return nil, domain.NewValidationError(domain.ValidationIssue{Path: "/config", Code: "type", Message: "config must be an empty object"})
		}
		return domain.EmptyFieldConfig{}, nil
	case "select", "multiSelect":
		input, ok := raw.(SelectFieldConfigInput)
		if !ok {
			return nil, domain.NewValidationError(domain.ValidationIssue{Path: "/config", Code: "type", Message: "config must contain an options array"})
		}
		currentConfig, ok := current.Config.(domain.SelectFieldConfig)
		if !ok {
			return nil, fmt.Errorf("decode current %s Field config", current.Type)
		}
		return s.normalizeUpdatedSelectConfig(currentConfig, input)
	default:
		return nil, fmt.Errorf("unsupported persisted Field type %q", current.Type)
	}
}

func (s *Service) normalizeUpdatedSelectConfig(current domain.SelectFieldConfig, input SelectFieldConfigInput) (domain.SelectFieldConfig, error) {
	if len(input.Options) > 500 {
		return domain.SelectFieldConfig{}, domain.NewValidationError(domain.ValidationIssue{Path: "/config/options", Code: "limit", Message: "at most 500 active options are allowed"})
	}
	activeByID := make(map[string]domain.SelectOption, len(current.Options))
	deletedByID := make(map[string]domain.DeletedSelectOption, len(current.DeletedOptions))
	for _, option := range current.Options {
		activeByID[option.ID] = option
	}
	for _, option := range current.DeletedOptions {
		deletedByID[option.ID] = option
	}

	active := make([]domain.SelectOption, 0, len(input.Options))
	seenIDs := make(map[string]struct{}, len(input.Options))
	seenNames := make(map[string]struct{}, len(input.Options))
	issues := make([]domain.ValidationIssue, 0)
	newCount := 0
	for index, option := range input.Options {
		path := fmt.Sprintf("/config/options/%d", index)
		name, err := domain.NormalizeOptionName(path+"/name", option.Name)
		if err != nil {
			var validation *domain.ValidationError
			if errors.As(err, &validation) {
				issues = append(issues, validation.Issues...)
			}
			continue
		}
		if !validOptionColor(option.Color) {
			issues = append(issues, domain.ValidationIssue{Path: path + "/color", Code: "format", Message: "unsupported option color"})
			continue
		}
		nameKey := domain.FoldKey(name)
		if _, exists := seenNames[nameKey]; exists {
			issues = append(issues, domain.ValidationIssue{Path: path + "/name", Code: "duplicate", Message: "active option names must be unique"})
			continue
		}
		seenNames[nameKey] = struct{}{}

		optionID := ""
		if option.ID == nil {
			newCount++
			optionID, err = s.newID(id.OptionPrefix)
			if err != nil {
				return domain.SelectFieldConfig{}, fmt.Errorf("generate option ID: %w", err)
			}
		} else {
			optionID = *option.ID
			if !id.Valid(id.OptionPrefix, optionID) {
				issues = append(issues, domain.ValidationIssue{Path: path + "/id", Code: "format", Message: "id must be a typed Option ID"})
				continue
			}
			if _, duplicate := seenIDs[optionID]; duplicate {
				issues = append(issues, domain.ValidationIssue{Path: path + "/id", Code: "duplicate", Message: "option ID is duplicated"})
				continue
			}
			if _, activeExists := activeByID[optionID]; !activeExists {
				if _, deletedExists := deletedByID[optionID]; !deletedExists {
					issues = append(issues, domain.ValidationIssue{Path: path + "/id", Code: "invalidReference", Message: "option ID does not belong to this Field"})
					continue
				}
			}
		}
		seenIDs[optionID] = struct{}{}
		active = append(active, domain.SelectOption{ID: optionID, Name: name, Color: option.Color})
	}
	if len(activeByID)+len(deletedByID)+newCount > 5000 {
		issues = append(issues, domain.ValidationIssue{Path: "/config/options", Code: "limit", Message: "a Field may contain at most 5000 total options"})
	}
	if len(issues) > 0 {
		return domain.SelectFieldConfig{}, domain.NewValidationError(issues...)
	}

	deleted := make([]domain.DeletedSelectOption, 0, len(current.DeletedOptions)+len(current.Options))
	for _, option := range current.DeletedOptions {
		if _, restored := seenIDs[option.ID]; !restored {
			deleted = append(deleted, option)
		}
	}
	deletedAt := s.now().UTC()
	for _, option := range current.Options {
		if _, retained := seenIDs[option.ID]; !retained {
			deleted = append(deleted, domain.DeletedSelectOption{
				ID: option.ID, Name: option.Name, Color: option.Color, DeletedAt: deletedAt,
			})
		}
	}
	sort.SliceStable(deleted, func(i, j int) bool {
		if deleted[i].DeletedAt.Equal(deleted[j].DeletedAt) {
			return deleted[i].ID < deleted[j].ID
		}
		return deleted[i].DeletedAt.Before(deleted[j].DeletedAt)
	})
	return domain.SelectFieldConfig{Options: active, DeletedOptions: deleted}, nil
}

func validOptionColor(value string) bool {
	switch value {
	case "gray", "red", "orange", "yellow", "green", "cyan", "blue", "purple", "pink":
		return true
	default:
		return false
	}
}

func normalizeViewConfig(viewType string, raw any, fields []domain.Field) (any, error) {
	fieldByID := make(map[string]domain.Field, len(fields))
	for _, field := range fields {
		fieldByID[field.ID] = field
	}
	switch viewType {
	case "grid":
		config, ok := raw.(domain.GridViewConfig)
		if !ok {
			return nil, domain.NewValidationError(domain.ValidationIssue{Path: "/config", Code: "type", Message: "config must be a Grid View configuration"})
		}
		if err := normalizeGridViewConfig(&config, fieldByID); err != nil {
			return nil, err
		}
		return config, nil
	case "map":
		config, ok := raw.(domain.MapViewConfig)
		if !ok {
			return nil, domain.NewValidationError(domain.ValidationIssue{Path: "/config", Code: "type", Message: "config must be a Map View configuration"})
		}
		if err := validateMapViewConfig(config, fieldByID); err != nil {
			return nil, err
		}
		return config, nil
	default:
		return nil, domain.NewValidationError(domain.ValidationIssue{Path: "/type", Code: "format", Message: "unsupported View type"})
	}
}

func normalizeGridViewConfig(config *domain.GridViewConfig, fields map[string]domain.Field) error {
	issues := make([]domain.ValidationIssue, 0)
	if len(config.Projection) > FieldLimitPerTable {
		issues = append(issues, domain.ValidationIssue{Path: "/config/projection", Code: "limit", Message: "at most 500 projected Fields are allowed"})
	}
	issues = append(issues, validateFieldIDList("/config/projection", config.Projection, fields)...)
	issues = append(issues, validateFieldIDList("/config/columnOrder", config.ColumnOrder, fields)...)
	issues = append(issues, validateFieldIDList("/config/frozenFieldIds", config.FrozenFieldIDs, fields)...)
	for fieldID, width := range config.ColumnWidths {
		if field, ok := fields[fieldID]; !ok || field.DeletedAt != nil || !id.Valid(id.FieldPrefix, fieldID) {
			issues = append(issues, domain.ValidationIssue{Path: "/config/columnWidths/" + escapePointer(fieldID), Code: "invalidReference", Message: "Field must be active and belong to the View Table"})
		}
		if width < 80 || width > 1000 {
			issues = append(issues, domain.ValidationIssue{Path: "/config/columnWidths/" + escapePointer(fieldID), Code: "limit", Message: "column width must be between 80 and 1000"})
		}
	}
	switch config.RowHeight {
	case "compact", "standard", "comfortable":
	default:
		issues = append(issues, domain.ValidationIssue{Path: "/config/rowHeight", Code: "format", Message: "unsupported row height"})
	}
	if len(config.Sort) > 10 {
		issues = append(issues, domain.ValidationIssue{Path: "/config/sort", Code: "limit", Message: "at most 10 Sort entries are allowed"})
	}
	seenSort := make(map[string]struct{}, len(config.Sort))
	for index := range config.Sort {
		spec := &config.Sort[index]
		path := fmt.Sprintf("/config/sort/%d", index)
		field, ok := fields[spec.FieldID]
		if !ok || field.DeletedAt != nil || !id.Valid(id.FieldPrefix, spec.FieldID) {
			issues = append(issues, domain.ValidationIssue{Path: path + "/fieldId", Code: "invalidReference", Message: "Field must be active and belong to the View Table"})
		} else if field.Type == "multiSelect" || field.Type == "location" {
			return &domain.UnsupportedSortError{FieldID: field.ID, FieldType: field.Type}
		}
		if _, duplicate := seenSort[spec.FieldID]; duplicate {
			issues = append(issues, domain.ValidationIssue{Path: path + "/fieldId", Code: "duplicate", Message: "Sort Field is duplicated"})
		}
		seenSort[spec.FieldID] = struct{}{}
		if spec.Direction != "asc" && spec.Direction != "desc" {
			issues = append(issues, domain.ValidationIssue{Path: path + "/direction", Code: "format", Message: "direction must be asc or desc"})
		}
		if spec.Nulls == "" {
			spec.Nulls = "last"
		} else if spec.Nulls != "first" && spec.Nulls != "last" {
			issues = append(issues, domain.ValidationIssue{Path: path + "/nulls", Code: "format", Message: "nulls must be first or last"})
		}
	}
	if config.Projection == nil {
		config.Projection = make([]string, 0)
	}
	if config.ColumnOrder == nil {
		config.ColumnOrder = make([]string, 0)
	}
	if config.ColumnWidths == nil {
		config.ColumnWidths = make(map[string]int)
	}
	if config.FrozenFieldIDs == nil {
		config.FrozenFieldIDs = make([]string, 0)
	}
	if config.Sort == nil {
		config.Sort = make([]domain.SortSpec, 0)
	}
	if len(issues) > 0 {
		return domain.NewValidationError(issues...)
	}
	return validateViewFilter(config.Filter, fields)
}

func validateMapViewConfig(config domain.MapViewConfig, fields map[string]domain.Field) error {
	issues := make([]domain.ValidationIssue, 0)
	field, ok := fields[config.LocationFieldID]
	if !ok || field.DeletedAt != nil || !id.Valid(id.FieldPrefix, config.LocationFieldID) {
		issues = append(issues, domain.ValidationIssue{Path: "/config/locationFieldId", Code: "invalidReference", Message: "Location Field must be active and belong to the View Table"})
	} else if field.Type != "location" {
		issues = append(issues, domain.ValidationIssue{Path: "/config/locationFieldId", Code: "invalidReference", Message: "Field must have type location"})
	}
	if (config.Center == nil) != (config.Zoom == nil) {
		issues = append(issues, domain.ValidationIssue{Path: "/config", Code: "required", Message: "center and zoom must be supplied together"})
	}
	if config.Center != nil {
		if math.IsNaN(config.Center.Lat) || math.IsInf(config.Center.Lat, 0) || config.Center.Lat < -85.0511287798066 || config.Center.Lat > 85.0511287798066 {
			issues = append(issues, domain.ValidationIssue{Path: "/config/center/lat", Code: "format", Message: "latitude is outside the renderable range"})
		}
		if math.IsNaN(config.Center.Lng) || math.IsInf(config.Center.Lng, 0) || config.Center.Lng < -180 || config.Center.Lng > 180 {
			issues = append(issues, domain.ValidationIssue{Path: "/config/center/lng", Code: "format", Message: "longitude must be between -180 and 180"})
		}
	}
	if config.Zoom != nil && (math.IsNaN(*config.Zoom) || math.IsInf(*config.Zoom, 0) || *config.Zoom < 0) {
		issues = append(issues, domain.ValidationIssue{Path: "/config/zoom", Code: "format", Message: "zoom must be a finite non-negative number"})
	}
	if len(issues) > 0 {
		return domain.NewValidationError(issues...)
	}
	return validateViewFilter(config.Filter, fields)
}

func validateViewFilter(filter *domain.FilterNode, fields map[string]domain.Field) error {
	if filter == nil {
		return nil
	}
	definitions := make(map[string]loomrecord.FieldDefinition, len(fields))
	for fieldID, field := range fields {
		config, err := json.Marshal(field.Config)
		if err != nil {
			return fmt.Errorf("encode Field config for View Filter validation: %w", err)
		}
		definitions[fieldID] = loomrecord.FieldDefinition{
			ID: field.ID, Type: field.Type, Config: config, DeletedAt: field.DeletedAt,
			Revision: field.Revision, Position: field.Position,
		}
	}
	err := loomrecord.ValidateFilter(filter, definitions)
	var validation *domain.ValidationError
	if !errors.As(err, &validation) {
		return err
	}
	issues := make([]domain.ValidationIssue, len(validation.Issues))
	for index, issue := range validation.Issues {
		issue.Path = "/config" + issue.Path
		issues[index] = issue
	}
	return domain.NewValidationError(issues...)
}

func validateFieldIDList(path string, values []string, fields map[string]domain.Field) []domain.ValidationIssue {
	issues := make([]domain.ValidationIssue, 0)
	seen := make(map[string]struct{}, len(values))
	for index, fieldID := range values {
		itemPath := fmt.Sprintf("%s/%d", path, index)
		field, ok := fields[fieldID]
		if !ok || field.DeletedAt != nil || !id.Valid(id.FieldPrefix, fieldID) {
			issues = append(issues, domain.ValidationIssue{Path: itemPath, Code: "invalidReference", Message: "Field must be active and belong to the View Table"})
		}
		if _, duplicate := seen[fieldID]; duplicate {
			issues = append(issues, domain.ValidationIssue{Path: itemPath, Code: "duplicate", Message: "Field ID is duplicated"})
		}
		seen[fieldID] = struct{}{}
	}
	return issues
}

func escapePointer(value string) string {
	value = strings.ReplaceAll(value, "~", "~0")
	return strings.ReplaceAll(value, "/", "~1")
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
