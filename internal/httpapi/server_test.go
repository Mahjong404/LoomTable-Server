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

	"github.com/Mahjong404/LoomTable-Server/internal/catalog"
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
	createdName       string
	actorID           string
	createdFieldInput catalog.FieldInput
	createdViewInput  catalog.ViewInput
}

type stubRecords struct {
	actorID      string
	tableID      string
	command      loomrecord.Command
	queryRequest loomrecord.QueryRequest
	mapRequest   loomrecord.MapQueryRequest
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

func (s *stubRecords) Query(_ context.Context, actorID, tableID string, request loomrecord.QueryRequest) (loomrecord.QueryResult, error) {
	s.actorID = actorID
	s.tableID = tableID
	s.queryRequest = request
	return loomrecord.QueryResult{Items: []loomrecord.Record{}, HasMore: false, ChangeCursor: "v1.change.payload.signature"}, nil
}

func (s *stubRecords) Changes(_ context.Context, actorID, tableID, _ string, _ int) (loomrecord.ChangePage, error) {
	s.actorID = actorID
	s.tableID = tableID
	return loomrecord.ChangePage{Items: []loomrecord.Change{}, NextCursor: "v1.change.payload.signature", HasMore: false}, nil
}

func (s *stubRecords) QueryMap(_ context.Context, actorID, viewID string, request loomrecord.MapQueryRequest) (loomrecord.MapQueryResult, error) {
	s.actorID = actorID
	s.tableID = viewID
	s.mapRequest = request
	return loomrecord.MapQueryResult{Features: []any{}, ViewRevision: 1, ChangeCursor: "v1.change.payload.signature"}, nil
}

func (s *stubRecords) SummarizeMap(_ context.Context, actorID, viewID string) (loomrecord.MapSummaryResult, error) {
	s.actorID = actorID
	s.tableID = viewID
	return loomrecord.MapSummaryResult{ViewRevision: 1, ChangeCursor: "v1.change.payload.signature"}, nil
}

func (s *stubRecords) QueryMapClusterRecords(_ context.Context, actorID, viewID string, _ loomrecord.MapClusterRecordsRequest) (loomrecord.QueryResult, error) {
	s.actorID = actorID
	s.tableID = viewID
	return loomrecord.QueryResult{Items: []loomrecord.Record{}, HasMore: false, ChangeCursor: "v1.change.payload.signature"}, nil
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
	return domain.Base{ID: "base_00000000000000000000000000", WorkspaceID: "ws_00000000000000000000000000", Revision: 1}, nil
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
	return domain.Table{ID: "tbl_00000000000000000000000000", BaseID: "base_00000000000000000000000000", Revision: 1}, nil
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

func (s *stubCatalog) ListFields(context.Context, string, string, string) ([]domain.Field, error) {
	return []domain.Field{}, nil
}

func (s *stubCatalog) CreateField(_ context.Context, actorID, _, tableID string, input catalog.FieldInput) (domain.Field, error) {
	s.actorID = actorID
	s.createdFieldInput = input
	return domain.Field{ID: "fld_00000000000000000000000000", TableID: tableID, Name: input.Name, Type: input.Type, Config: input.Config, Revision: 1}, nil
}

func (s *stubCatalog) UpdateField(context.Context, string, string, catalog.FieldUpdate) (domain.Field, error) {
	return domain.Field{}, nil
}

func (s *stubCatalog) DeleteField(context.Context, string, string, int64) error {
	return nil
}

func (s *stubCatalog) RestoreField(context.Context, string, string, int64) (domain.Field, error) {
	return domain.Field{}, nil
}

func (s *stubCatalog) ListViews(context.Context, string, string, string) ([]domain.View, error) {
	return []domain.View{}, nil
}

func (s *stubCatalog) GetView(context.Context, string, string) (domain.View, error) {
	return domain.View{ID: "view_00000000000000000000000000", TableID: "tbl_00000000000000000000000000", Type: "map", Config: domain.MapViewConfig{LocationFieldID: "fld_00000000000000000000000000"}, Revision: 1}, nil
}

func (s *stubCatalog) CreateView(_ context.Context, actorID, _, tableID string, input catalog.ViewInput) (domain.View, error) {
	s.actorID = actorID
	s.createdViewInput = input
	return domain.View{ID: "view_00000000000000000000000000", TableID: tableID, Name: input.Name, Type: input.Type, Config: input.Config, Revision: 1}, nil
}

func (s *stubCatalog) UpdateView(context.Context, string, string, catalog.ViewUpdate) (domain.View, error) {
	return domain.View{}, nil
}

func (s *stubCatalog) DeleteView(context.Context, string, string, int64) error {
	return nil
}

func (s *stubCatalog) RestoreView(context.Context, string, string, int64) (domain.View, error) {
	return domain.View{}, nil
}

func testConfig() config.Config {
	return config.Config{
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

func TestCreateSelectFieldRouteDecodesTypedConfig(t *testing.T) {
	catalogService := &stubCatalog{}
	server := New(testConfig(), func(context.Context) error { return nil }, Dependencies{
		Authenticator: fixedAuthenticator{},
		Catalog:       catalogService,
	})
	request := httptest.NewRequest(http.MethodPost, "/v1/tables/tbl_00000000000000000000000000/fields", strings.NewReader(`{
		"name":"Status",
		"type":"select",
		"config":{"options":[{"name":"Open","color":"blue"}]}
	}`))
	request.Header.Set("Authorization", "Bearer test-token")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "mut_00000000000000000000000000")
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("response = %d %s", recorder.Code, recorder.Body.String())
	}
	config, ok := catalogService.createdFieldInput.Config.(catalog.SelectFieldConfigInput)
	if !ok || len(config.Options) != 1 || config.Options[0].Name != "Open" {
		t.Fatalf("decoded config = %#v (%T)", catalogService.createdFieldInput.Config, catalogService.createdFieldInput.Config)
	}
}

func TestCreateFieldRouteRejectsUnknownNestedProperty(t *testing.T) {
	catalogService := &stubCatalog{}
	server := New(testConfig(), func(context.Context) error { return nil }, Dependencies{
		Authenticator: fixedAuthenticator{},
		Catalog:       catalogService,
	})
	request := httptest.NewRequest(http.MethodPost, "/v1/tables/tbl_00000000000000000000000000/fields", strings.NewReader(`{
		"name":"Status",
		"type":"select",
		"config":{"options":[{"name":"Open","color":"blue","temporary":true}]}
	}`))
	request.Header.Set("Authorization", "Bearer test-token")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "mut_00000000000000000000000000")
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnprocessableEntity || !strings.Contains(recorder.Body.String(), "unknownProperty") {
		t.Fatalf("response = %d %s", recorder.Code, recorder.Body.String())
	}
	if catalogService.createdFieldInput.Type != "" {
		t.Fatal("catalog must not be called for an invalid request")
	}
}

func TestCreateMapViewRouteDecodesTypedConfig(t *testing.T) {
	catalogService := &stubCatalog{}
	server := New(testConfig(), func(context.Context) error { return nil }, Dependencies{
		Authenticator: fixedAuthenticator{},
		Catalog:       catalogService,
	})
	request := httptest.NewRequest(http.MethodPost, "/v1/tables/tbl_00000000000000000000000000/views", strings.NewReader(`{
		"name":"Map",
		"type":"map",
		"config":{"locationFieldId":"fld_00000000000000000000000000","center":{"lat":31.2,"lng":121.5},"zoom":8}
	}`))
	request.Header.Set("Authorization", "Bearer test-token")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "mut_00000000000000000000000000")
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("response = %d %s", recorder.Code, recorder.Body.String())
	}
	config, ok := catalogService.createdViewInput.Config.(domain.MapViewConfig)
	if !ok || config.LocationFieldID != "fld_00000000000000000000000000" || config.Center == nil || config.Zoom == nil {
		t.Fatalf("decoded config = %#v (%T)", catalogService.createdViewInput.Config, catalogService.createdViewInput.Config)
	}
}

func TestQueryRecordsRouteDecodesAndOrFilterAndOverrides(t *testing.T) {
	records := &stubRecords{}
	server := New(testConfig(), func(context.Context) error { return nil }, Dependencies{
		Authenticator: fixedAuthenticator{}, Catalog: &stubCatalog{}, Records: records,
	})
	request := httptest.NewRequest(http.MethodPost, "/v1/tables/tbl_00000000000000000000000000/records/query", strings.NewReader(`{
		"lifecycle":"all",
		"limit":50,
		"projection":["fld_00000000000000000000000000"],
		"filter":{"kind":"group","operator":"or","children":[
			{"kind":"rule","fieldId":"fld_00000000000000000000000000","operator":"contains","value":"road"},
			{"kind":"rule","fieldId":"fld_00000000000000000000000001","operator":"isEmpty"}
		]},
		"sort":[]
	}`))
	request.Header.Set("Authorization", "Bearer test-token")
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("response = %d %s", recorder.Code, recorder.Body.String())
	}
	if !records.queryRequest.FilterPresent || records.queryRequest.Filter == nil || records.queryRequest.Filter.Operator != "or" {
		t.Fatalf("query request = %#v", records.queryRequest)
	}
	if !records.queryRequest.ProjectionPresent || !records.queryRequest.SortPresent || records.queryRequest.Limit != 50 {
		t.Fatalf("query override presence = %#v", records.queryRequest)
	}
}

func TestChangesRouteUsesAuthenticatedTableScope(t *testing.T) {
	records := &stubRecords{}
	server := New(testConfig(), func(context.Context) error { return nil }, Dependencies{
		Authenticator: fixedAuthenticator{}, Catalog: &stubCatalog{}, Records: records,
	})
	request := httptest.NewRequest(http.MethodGet, "/v1/tables/tbl_00000000000000000000000000/changes?limit=25", nil)
	request.Header.Set("Authorization", "Bearer test-token")
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK || records.tableID != "tbl_00000000000000000000000000" {
		t.Fatalf("response = %d %s, table = %q", recorder.Code, recorder.Body.String(), records.tableID)
	}
}

func TestMapQueryRouteDecodesViewport(t *testing.T) {
	records := &stubRecords{}
	server := New(testConfig(), func(context.Context) error { return nil }, Dependencies{
		Authenticator: fixedAuthenticator{}, Catalog: &stubCatalog{}, Records: records,
	})
	request := httptest.NewRequest(http.MethodPost, "/v1/views/view_00000000000000000000000000/map/query", strings.NewReader(`{
		"viewport":{"boxes":[{"west":100,"south":10,"east":120,"north":30}]},
		"zoom":8,"pixelWidth":1000,"pixelHeight":800
	}`))
	request.Header.Set("Authorization", "Bearer test-token")
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK || len(records.mapRequest.Viewport.Boxes) != 1 || records.mapRequest.PixelWidth != 1000 {
		t.Fatalf("response = %d %s, request = %#v", recorder.Code, recorder.Body.String(), records.mapRequest)
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
	server := New(testConfig(), func(context.Context) error { return nil }, Dependencies{Authenticator: fixedAuthenticator{}})
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
