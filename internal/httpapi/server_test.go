package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Mahjong404/LoomTable-Server/internal/auth"
	"github.com/Mahjong404/LoomTable-Server/internal/config"
	"github.com/Mahjong404/LoomTable-Server/internal/domain"
	loomrecord "github.com/Mahjong404/LoomTable-Server/internal/record"
	appstatus "github.com/Mahjong404/LoomTable-Server/internal/status"
)

type fixedAuthenticator struct{}

func (fixedAuthenticator) Authenticate(_ context.Context, token string) (string, error) {
	if token != "test-token" {
		return "", domain.ErrUnauthenticated
	}
	return "act_00000000000000000000000000", nil
}

type fixedBootstrapState string

func (state fixedBootstrapState) BootstrapState(context.Context) (string, error) {
	return string(state), nil
}

type stubCatalog struct {
	createdName string
	actorID     string
}

type stubRecords struct {
	actorID string
	tableID string
	command loomrecord.Command
}

func (s *stubRecords) Get(_ context.Context, actorID, recordID string) (loomrecord.Record, error) {
	s.actorID = actorID
	return loomrecord.Record{ID: recordID, TableID: "tbl_00000000000000000000000000", Revision: 1, Values: map[string]any{}, CreatedAt: time.Unix(1, 0).UTC(), UpdatedAt: time.Unix(1, 0).UTC()}, nil
}

func (s *stubRecords) Mutate(_ context.Context, actorID, tableID, mutationID string, commands []loomrecord.Command) (loomrecord.MutationResult, error) {
	s.actorID = actorID
	s.tableID = tableID
	if len(commands) > 0 {
		s.command = commands[0]
	}
	return loomrecord.MutationResult{ClientMutationID: mutationID, Results: []loomrecord.CommandResult{}, ChangeCursor: "v1.change.payload.signature"}, nil
}

func (s *stubCatalog) ListWorkspaces(context.Context, string) ([]domain.Workspace, error) {
	return []domain.Workspace{}, nil
}

func (s *stubCatalog) GetWorkspace(_ context.Context, actorID, workspaceID string) (domain.Workspace, error) {
	s.actorID = actorID
	return domain.Workspace{ID: workspaceID, Name: "Workspace", Revision: 1, CreatedAt: time.Unix(1, 0).UTC(), UpdatedAt: time.Unix(1, 0).UTC()}, nil
}

func (s *stubCatalog) CreateWorkspace(_ context.Context, actorID, _ string, name string) (domain.Workspace, error) {
	s.actorID = actorID
	s.createdName = name
	return domain.Workspace{ID: "ws_00000000000000000000000000", Name: name, Revision: 1, CreatedAt: time.Unix(1, 0).UTC(), UpdatedAt: time.Unix(1, 0).UTC()}, nil
}

func (s *stubCatalog) UpdateWorkspace(context.Context, string, string, int64, string) (domain.Workspace, error) {
	return domain.Workspace{}, nil
}

func (s *stubCatalog) ListBases(context.Context, string, string) ([]domain.Base, error) {
	return []domain.Base{}, nil
}

func (s *stubCatalog) GetBase(context.Context, string, string) (domain.Base, error) {
	return domain.Base{}, domain.ErrNotFound
}

func (s *stubCatalog) CreateBase(context.Context, string, string, string, string) (domain.Base, error) {
	return domain.Base{}, nil
}

func (s *stubCatalog) UpdateBase(context.Context, string, string, int64, string) (domain.Base, error) {
	return domain.Base{}, nil
}

func (s *stubCatalog) ListTables(context.Context, string, string, string) ([]domain.Table, error) {
	return []domain.Table{}, nil
}

func (s *stubCatalog) GetTable(context.Context, string, string) (domain.Table, error) {
	return domain.Table{}, domain.ErrNotFound
}

func (s *stubCatalog) CreateTable(context.Context, string, string, string, string, *string, *string) (domain.CreateTableResult, error) {
	return domain.CreateTableResult{}, nil
}

func (s *stubCatalog) UpdateTable(context.Context, string, string, int64, string) (domain.Table, error) {
	return domain.Table{}, nil
}

func (s *stubCatalog) DeleteTable(context.Context, string, string, int64) error {
	return nil
}

func (s *stubCatalog) RestoreTable(context.Context, string, string, int64) (domain.Table, error) {
	return domain.Table{}, nil
}

func testConfig() config.Config {
	return config.Config{
		AuthTokenHash:        auth.HashToken("test-token"),
		ServerVersion:        "test",
		APIVersion:           "v1",
		MinPluginVersion:     "0.1.0",
		Capabilities:         []string{"grid", "map"},
		ChangeRetention:      "30d",
		IdempotencyRetention: "30d",
	}
}

func TestPublicEndpointsAndAuthBoundary(t *testing.T) {
	server := New(testConfig(), func(context.Context) error { return nil })
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	response, err := http.Get(testServer.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("healthz status = %d, want 200", response.StatusCode)
	}
	_ = response.Body.Close()

	response, err = http.Get(testServer.URL + "/v1/meta")
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("meta status = %d, want 200", response.StatusCode)
	}
	_ = response.Body.Close()

	response, err = http.Get(testServer.URL + "/v1/workspaces")
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want 401", response.StatusCode)
	}
	_ = response.Body.Close()
}

func TestReadyzReportsMigration(t *testing.T) {
	server := New(testConfig(), func(context.Context) error { return appstatus.ErrMigrationRequired })
	request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("readyz status = %d, want 503", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "MIGRATION_REQUIRED") {
		t.Fatalf("readyz body = %s, want migration code", recorder.Body.String())
	}
}

func TestMetaReflectsMigrationState(t *testing.T) {
	server := New(testConfig(), func(context.Context) error { return appstatus.ErrMigrationRequired })
	request := httptest.NewRequest(http.MethodGet, "/v1/meta", nil)
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("meta status = %d, want 200", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), `"migrationRequired":true`) {
		t.Fatalf("meta body = %s, want migrationRequired=true", recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"bootstrapState":"unknown"`) {
		t.Fatalf("meta body = %s, want bootstrapState=unknown", recorder.Body.String())
	}
}

func TestMetaUsesBootstrapStateDependency(t *testing.T) {
	server := New(testConfig(), func(context.Context) error { return nil }, Dependencies{Bootstrap: fixedBootstrapState("required")})
	request := httptest.NewRequest(http.MethodGet, "/v1/meta", nil)
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"bootstrapState":"required"`) {
		t.Fatalf("meta response = %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestCreateWorkspaceRouteAuthenticatesAndReturnsContractShape(t *testing.T) {
	catalog := &stubCatalog{}
	server := New(testConfig(), func(context.Context) error { return nil }, Dependencies{
		Authenticator: fixedAuthenticator{},
		Catalog:       catalog,
	})
	request := httptest.NewRequest(http.MethodPost, "/v1/workspaces", strings.NewReader(`{"name":"Workspace"}`))
	request.Header.Set("Authorization", "Bearer test-token")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "mut_00000000000000000000000000")
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if catalog.actorID != "act_00000000000000000000000000" || catalog.createdName != "Workspace" {
		t.Fatalf("catalog received actor=%q name=%q", catalog.actorID, catalog.createdName)
	}
	var response domain.Workspace
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.ID != "ws_00000000000000000000000000" || response.Revision != 1 {
		t.Fatalf("workspace response = %#v", response)
	}
}

func TestWorkspaceRouteRejectsMissingIdempotencyKey(t *testing.T) {
	server := New(testConfig(), func(context.Context) error { return nil }, Dependencies{
		Authenticator: fixedAuthenticator{},
		Catalog:       &stubCatalog{},
	})
	request := httptest.NewRequest(http.MethodPost, "/v1/workspaces", strings.NewReader(`{"name":"Workspace"}`))
	request.Header.Set("Authorization", "Bearer test-token")
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "BAD_REQUEST") {
		t.Fatalf("response = %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestMutateRecordsRouteDecodesStrictCommands(t *testing.T) {
	records := &stubRecords{}
	server := New(testConfig(), func(context.Context) error { return nil }, Dependencies{
		Authenticator: fixedAuthenticator{},
		Catalog:       &stubCatalog{},
		Records:       records,
	})
	body := `{"clientMutationId":"mut_00000000000000000000000000","commands":[{"kind":"createRecord","values":{}}]}`
	request := httptest.NewRequest(http.MethodPost, "/v1/tables/tbl_00000000000000000000000000/records/mutate", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer test-token")
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("response = %d %s", recorder.Code, recorder.Body.String())
	}
	if records.actorID != "act_00000000000000000000000000" || records.command.Kind != "createRecord" || !records.command.ValuesPresent {
		t.Fatalf("records call = %#v", records)
	}
}

func TestMutateRecordsRouteRejectsUnknownCommandProperty(t *testing.T) {
	server := New(testConfig(), func(context.Context) error { return nil }, Dependencies{
		Authenticator: fixedAuthenticator{},
		Catalog:       &stubCatalog{},
		Records:       &stubRecords{},
	})
	body := `{"clientMutationId":"mut_00000000000000000000000000","commands":[{"kind":"createRecord","values":{},"extra":true}]}`
	request := httptest.NewRequest(http.MethodPost, "/v1/tables/tbl_00000000000000000000000000/records/mutate", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer test-token")
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnprocessableEntity || !strings.Contains(recorder.Body.String(), `"path":"/commands/0/extra"`) {
		t.Fatalf("response = %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestReadyzReportsDependencyFailure(t *testing.T) {
	server := New(testConfig(), func(context.Context) error { return errors.New("database down") })
	request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("readyz status = %d, want 503", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "DEPENDENCY_UNAVAILABLE") {
		t.Fatalf("readyz body = %s, want dependency code", recorder.Body.String())
	}
}

func TestAttachmentCapabilityIsExplicitlyDisabled(t *testing.T) {
	server := New(testConfig(), func(context.Context) error { return nil })
	request := httptest.NewRequest(http.MethodGet, "/v1/attachments/att_test", nil)
	request.Header.Set("Authorization", "Bearer test-token")
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotImplemented {
		t.Fatalf("attachment status = %d, want 501", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "CAPABILITY_NOT_ENABLED") {
		t.Fatalf("attachment body = %s, want capability code", recorder.Body.String())
	}
}
