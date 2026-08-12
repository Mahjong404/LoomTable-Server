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

func fixedID(prefix string) (string, error) {
	switch prefix {
	case id.WorkspacePrefix, id.BasePrefix, id.TablePrefix, id.FieldPrefix, id.ViewPrefix:
		return prefix + "00000000000000000000000000", nil
	default:
		return "", errors.New("unexpected prefix")
	}
}
