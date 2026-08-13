package catalog

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Mahjong404/LoomTable-Server/internal/domain"
	"github.com/Mahjong404/LoomTable-Server/internal/id"
)

type captureStore struct {
	createdWorkspace domain.Workspace
	createdBase      domain.Base
	createdField     domain.Field
	currentField     domain.Field
	updatedField     domain.Field
	fields           []domain.Field
	createdView      domain.View
	currentView      domain.View
	updatedView      domain.View
	fingerprint      [32]byte
	updateCalls      int
}

func (s *captureStore) ListWorkspaces(context.Context, string) ([]domain.Workspace, error) {
	return []domain.Workspace{}, nil
}

func (s *captureStore) GetWorkspace(context.Context, string, string) (domain.Workspace, error) {
	return domain.Workspace{}, domain.ErrNotFound
}

func (s *captureStore) CreateWorkspace(_ context.Context, _ string, _ string, fingerprint [32]byte, workspace domain.Workspace) (domain.Workspace, error) {
	s.createdWorkspace = workspace
	s.fingerprint = fingerprint
	workspace.CreatedAt = time.Unix(1, 0).UTC()
	workspace.UpdatedAt = workspace.CreatedAt
	return workspace, nil
}

func (s *captureStore) UpdateWorkspace(_ context.Context, _ string, _ string, _ int64, name string) (domain.Workspace, error) {
	s.updateCalls++
	return domain.Workspace{Name: name}, nil
}

func (s *captureStore) ListBases(context.Context, string, string) ([]domain.Base, error) {
	return []domain.Base{}, nil
}

func (s *captureStore) GetBase(context.Context, string, string) (domain.Base, error) {
	return domain.Base{}, domain.ErrNotFound
}

func (s *captureStore) CreateBase(_ context.Context, _ string, _ string, fingerprint [32]byte, base domain.Base) (domain.Base, error) {
	s.createdBase = base
	s.fingerprint = fingerprint
	return base, nil
}

func (s *captureStore) UpdateBase(context.Context, string, string, int64, string) (domain.Base, error) {
	return domain.Base{}, nil
}

func (s *captureStore) ListTables(context.Context, string, string, string) ([]domain.Table, error) {
	return []domain.Table{}, nil
}

func (s *captureStore) GetTable(context.Context, string, string) (domain.Table, error) {
	return domain.Table{}, domain.ErrNotFound
}

func (s *captureStore) CreateTable(_ context.Context, _ string, _ string, fingerprint [32]byte, result domain.CreateTableResult) (domain.CreateTableResult, error) {
	s.fingerprint = fingerprint
	return result, nil
}

func (s *captureStore) UpdateTable(context.Context, string, string, int64, string) (domain.Table, error) {
	return domain.Table{}, nil
}

func (s *captureStore) DeleteTable(context.Context, string, string, int64) error {
	return nil
}

func (s *captureStore) RestoreTable(context.Context, string, string, int64) (domain.Table, error) {
	return domain.Table{}, nil
}

func (s *captureStore) ListFields(context.Context, string, string, string) ([]domain.Field, error) {
	return s.fields, nil
}

func (s *captureStore) GetField(context.Context, string, string) (domain.Field, error) {
	if s.currentField.ID == "" {
		return domain.Field{}, domain.ErrNotFound
	}
	return s.currentField, nil
}

func (s *captureStore) CreateField(_ context.Context, _ string, _ string, fingerprint [32]byte, field domain.Field) (domain.Field, error) {
	s.createdField = field
	s.fingerprint = fingerprint
	return field, nil
}

func (s *captureStore) UpdateField(_ context.Context, _ string, _ string, _ int64, field domain.Field) (domain.Field, error) {
	s.updateCalls++
	s.updatedField = field
	return field, nil
}

func (s *captureStore) DeleteField(context.Context, string, string, int64) error {
	return nil
}

func (s *captureStore) RestoreField(context.Context, string, string, int64) (domain.Field, error) {
	return domain.Field{}, nil
}

func (s *captureStore) ListViews(context.Context, string, string, string) ([]domain.View, error) {
	return []domain.View{}, nil
}

func (s *captureStore) GetView(context.Context, string, string) (domain.View, error) {
	if s.currentView.ID == "" {
		return domain.View{}, domain.ErrNotFound
	}
	return s.currentView, nil
}

func (s *captureStore) CreateView(_ context.Context, _ string, _ string, fingerprint [32]byte, view domain.View) (domain.View, error) {
	s.createdView = view
	s.fingerprint = fingerprint
	return view, nil
}

func (s *captureStore) UpdateView(_ context.Context, _ string, _ string, _ int64, view domain.View) (domain.View, error) {
	s.updatedView = view
	return view, nil
}

func (s *captureStore) DeleteView(context.Context, string, string, int64) error {
	return nil
}

func (s *captureStore) RestoreView(context.Context, string, string, int64) (domain.View, error) {
	return domain.View{}, nil
}

func TestCreateWorkspaceNormalizesBeforePersistence(t *testing.T) {
	store := &captureStore{}
	service := NewWithIDGenerator(store, fixedID)

	created, err := service.CreateWorkspace(context.Background(), "act_test", "mut_00000000000000000000000000", " Cafe\u0301 ")
	if err != nil {
		t.Fatal(err)
	}
	if created.Name != "Café" || store.createdWorkspace.Name != "Café" {
		t.Fatalf("created workspace = %#v", created)
	}
	if store.createdWorkspace.Revision != 1 {
		t.Fatalf("revision = %d, want 1", store.createdWorkspace.Revision)
	}
	if store.fingerprint == ([32]byte{}) {
		t.Fatal("request fingerprint was not produced")
	}
}

func TestCreateBaseRejectsMalformedParentID(t *testing.T) {
	service := NewWithIDGenerator(&captureStore{}, fixedID)
	_, err := service.CreateBase(context.Background(), "act_test", "mut_00000000000000000000000000", "ws_bad", "Base")
	var badRequest *domain.BadRequestError
	if !errors.As(err, &badRequest) {
		t.Fatalf("error = %v, want BadRequestError", err)
	}
}

func TestUpdateWorkspaceValidatesBeforeStore(t *testing.T) {
	store := &captureStore{}
	service := NewWithIDGenerator(store, fixedID)
	_, err := service.UpdateWorkspace(context.Background(), "act_test", "ws_00000000000000000000000000", 0, "Name")
	var validation *domain.ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("error = %v, want ValidationError", err)
	}
	if store.updateCalls != 0 {
		t.Fatalf("store update calls = %d, want 0", store.updateCalls)
	}
}

func TestCreateTableBuildsMandatoryInitialMetadata(t *testing.T) {
	store := &captureStore{}
	service := NewWithIDGenerator(store, fixedID)

	result, err := service.CreateTable(
		context.Background(),
		"act_test",
		"mut_00000000000000000000000000",
		"base_00000000000000000000000000",
		" Contacts ",
		nil,
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Table.Name != "Contacts" || result.PrimaryField.Name != "Name" || result.InitialView.Name != "Grid" {
		t.Fatalf("result = %#v", result)
	}
	if result.Table.PrimaryFieldID != result.PrimaryField.ID || result.PrimaryField.TableID != result.Table.ID {
		t.Fatalf("initial metadata is not linked: %#v", result)
	}
	if len(result.InitialView.Config.Projection) != 1 || result.InitialView.Config.Projection[0] != result.PrimaryField.ID {
		t.Fatalf("initial grid config = %#v", result.InitialView.Config)
	}
}

func TestCreateSelectFieldNormalizesOptionsBeforePersistence(t *testing.T) {
	store := &captureStore{}
	service := NewWithIDGenerator(store, fixedID)

	created, err := service.CreateField(
		context.Background(),
		"act_test",
		"mut_00000000000000000000000000",
		"tbl_00000000000000000000000000",
		FieldInput{
			Name: " Status ",
			Type: "select",
			Config: SelectFieldConfigInput{Options: []SelectOptionInput{
				{Name: " Cafe\u0301 ", Color: "blue"},
			}},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if created.Name != "Status" || created.Type != "select" {
		t.Fatalf("created field = %#v", created)
	}
	config, ok := created.Config.(domain.SelectFieldConfig)
	if !ok {
		t.Fatalf("config type = %T, want domain.SelectFieldConfig", created.Config)
	}
	if len(config.Options) != 1 || config.Options[0].ID != "opt_00000000000000000000000000" || config.Options[0].Name != "Café" || config.Options[0].Color != "blue" {
		t.Fatalf("options = %#v", config.Options)
	}
	if config.DeletedOptions == nil || len(config.DeletedOptions) != 0 {
		t.Fatalf("deleted options = %#v, want empty array", config.DeletedOptions)
	}
	if store.fingerprint == ([32]byte{}) {
		t.Fatal("request fingerprint was not produced")
	}
}

func TestUpdateSelectFieldAppliesOptionLifecycle(t *testing.T) {
	now := time.Date(2026, time.August, 13, 10, 0, 0, 0, time.UTC)
	oldDeletedAt := now.Add(-time.Hour)
	activeA := "opt_00000000000000000000000001"
	activeB := "opt_00000000000000000000000002"
	deletedC := "opt_00000000000000000000000003"
	store := &captureStore{currentField: domain.Field{
		ID:       "fld_00000000000000000000000000",
		TableID:  "tbl_00000000000000000000000000",
		Name:     "Status",
		Type:     "select",
		Revision: 4,
		Config: domain.SelectFieldConfig{
			Options: []domain.SelectOption{
				{ID: activeA, Name: "A", Color: "red"},
				{ID: activeB, Name: "B", Color: "blue"},
			},
			DeletedOptions: []domain.DeletedSelectOption{
				{ID: deletedC, Name: "C", Color: "gray", DeletedAt: oldDeletedAt},
			},
		},
	}}
	service := NewWithIDGeneratorAndClock(store, fixedID, func() time.Time { return now })

	updated, err := service.UpdateField(
		context.Background(),
		"act_test",
		store.currentField.ID,
		FieldUpdate{
			Type:             "select",
			ExpectedRevision: 4,
			Config: SelectFieldConfigInput{Options: []SelectOptionInput{
				{ID: &activeB, Name: " B2 ", Color: "green"},
				{ID: &deletedC, Name: "C", Color: "gray"},
				{Name: "D", Color: "purple"},
			}},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	config := updated.Config.(domain.SelectFieldConfig)
	if len(config.Options) != 3 || config.Options[0].ID != activeB || config.Options[0].Name != "B2" || config.Options[1].ID != deletedC || config.Options[2].ID != "opt_00000000000000000000000000" {
		t.Fatalf("active options = %#v", config.Options)
	}
	if len(config.DeletedOptions) != 1 || config.DeletedOptions[0].ID != activeA || !config.DeletedOptions[0].DeletedAt.Equal(now) {
		t.Fatalf("deleted options = %#v", config.DeletedOptions)
	}
	if store.updateCalls != 1 {
		t.Fatalf("update calls = %d, want 1", store.updateCalls)
	}
}

func TestCreateMapViewRequiresActiveLocationField(t *testing.T) {
	locationFieldID := "fld_00000000000000000000000001"
	store := &captureStore{fields: []domain.Field{{
		ID: locationFieldID, TableID: "tbl_00000000000000000000000000", Type: "location",
	}}}
	service := NewWithIDGenerator(store, fixedID)
	zoom := 8.0

	created, err := service.CreateView(
		context.Background(),
		"act_test",
		"mut_00000000000000000000000000",
		"tbl_00000000000000000000000000",
		ViewInput{
			Name: " Places ",
			Type: "map",
			Config: domain.MapViewConfig{
				LocationFieldID: locationFieldID,
				Center:          &domain.MapCenter{Lat: 23.1291, Lng: 113.2644},
				Zoom:            &zoom,
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if created.Name != "Places" || created.Type != "map" || created.Config.(domain.MapViewConfig).LocationFieldID != locationFieldID {
		t.Fatalf("created view = %#v", created)
	}
	if store.fingerprint == ([32]byte{}) {
		t.Fatal("request fingerprint was not produced")
	}
}

func TestCreateMapViewValidatesSavedFilterOperators(t *testing.T) {
	locationFieldID := "fld_00000000000000000000000001"
	store := &captureStore{fields: []domain.Field{{
		ID: locationFieldID, TableID: "tbl_00000000000000000000000000", Type: "location", Config: domain.EmptyFieldConfig{},
	}}}
	service := NewWithIDGenerator(store, fixedID)
	_, err := service.CreateView(
		context.Background(), "act_test", "mut_00000000000000000000000000", "tbl_00000000000000000000000000",
		ViewInput{Name: "Map", Type: "map", Config: domain.MapViewConfig{
			LocationFieldID: locationFieldID,
			Filter:          &domain.FilterNode{Kind: "rule", FieldID: locationFieldID, Operator: "contains", Value: []byte(`"x"`)},
		}},
	)
	var unsupported *domain.UnsupportedOperatorError
	if !errors.As(err, &unsupported) {
		t.Fatalf("error = %v, want UnsupportedOperatorError", err)
	}
}

func TestUpdateViewChecksRevisionBeforeReplacementConfig(t *testing.T) {
	store := &captureStore{currentView: domain.View{
		ID:       "view_00000000000000000000000000",
		TableID:  "tbl_00000000000000000000000000",
		Name:     "Grid",
		Type:     "grid",
		Revision: 3,
		Config: domain.GridViewConfig{
			Projection: []string{}, ColumnOrder: []string{}, ColumnWidths: map[string]int{},
			FrozenFieldIDs: []string{}, RowHeight: "standard", Sort: []domain.SortSpec{},
		},
	}}
	service := NewWithIDGenerator(store, fixedID)

	_, err := service.UpdateView(context.Background(), "act_test", store.currentView.ID, ViewUpdate{
		Type:             "map",
		ExpectedRevision: 2,
		Config:           domain.MapViewConfig{},
	})
	var conflict *domain.RevisionConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("error = %v, want RevisionConflictError", err)
	}
}

func fixedID(prefix string) (string, error) {
	switch prefix {
	case id.WorkspacePrefix, id.BasePrefix, id.TablePrefix, id.FieldPrefix, id.ViewPrefix, id.OptionPrefix:
		return prefix + "00000000000000000000000000", nil
	default:
		return "", errors.New("unexpected prefix")
	}
}
