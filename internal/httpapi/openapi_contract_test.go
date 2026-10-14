package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/Mahjong404/LoomTable-Server/internal/catalog"
)

func TestOpenAPIOperationsHaveReachableHTTPRoutes(t *testing.T) {
	type operation struct {
		id         string
		method     string
		path       string
		body       string
		wantStatus int
	}
	operations := []operation{
		{"reorderViews", http.MethodPut, "/v1/tables/tbl_00000000000000000000000000/views/order", `{"expectedOrderRevision":1,"viewIds":[]}`, 200},
		{"healthz", http.MethodGet, "/healthz", "", 200},
		{"readyz", http.MethodGet, "/readyz", "", 200},
		{"getServerMeta", http.MethodGet, "/v1/meta", "", 200},
		{"listWorkspaces", http.MethodGet, "/v1/workspaces", "", 200},
		{"createWorkspace", http.MethodPost, "/v1/workspaces", `{"name":"Workspace"}`, 201},
		{"getWorkspace", http.MethodGet, "/v1/workspaces/ws_00000000000000000000000000", "", 200},
		{"updateWorkspace", http.MethodPatch, "/v1/workspaces/ws_00000000000000000000000000", `{"name":"New","expectedRevision":1}`, 200},
		{"listBases", http.MethodGet, "/v1/bases?workspaceId=ws_00000000000000000000000000", "", 200},
		{"createBase", http.MethodPost, "/v1/bases", `{"workspaceId":"ws_00000000000000000000000000","name":"Base"}`, 201},
		{"getBase", http.MethodGet, "/v1/bases/base_00000000000000000000000000", "", 200},
		{"updateBase", http.MethodPatch, "/v1/bases/base_00000000000000000000000000", `{"name":"New","expectedRevision":1}`, 200},
		{"listTables", http.MethodGet, "/v1/tables?baseId=base_00000000000000000000000000", "", 200},
		{"createTable", http.MethodPost, "/v1/tables", `{"baseId":"base_00000000000000000000000000","name":"Table"}`, 201},
		{"getTable", http.MethodGet, "/v1/tables/tbl_00000000000000000000000000", "", 200},
		{"updateTable", http.MethodPatch, "/v1/tables/tbl_00000000000000000000000000", `{"name":"New","expectedRevision":1}`, 200},
		{"deleteTable", http.MethodDelete, "/v1/tables/tbl_00000000000000000000000000?expectedRevision=1", "", 204},
		{"restoreTable", http.MethodPost, "/v1/tables/tbl_00000000000000000000000000/restore", `{"expectedRevision":1}`, 200},
		{"listFields", http.MethodGet, "/v1/tables/tbl_00000000000000000000000000/fields", "", 200},
		{"createField", http.MethodPost, "/v1/tables/tbl_00000000000000000000000000/fields", `{"name":"Text","type":"text","config":{}}`, 201},
		{"updateField", http.MethodPatch, "/v1/fields/fld_00000000000000000000000000", `{"name":"New","type":"text","expectedRevision":1}`, 200},
		{"deleteField", http.MethodDelete, "/v1/fields/fld_00000000000000000000000000?expectedRevision=1", "", 204},
		{"restoreField", http.MethodPost, "/v1/fields/fld_00000000000000000000000000/restore", `{"expectedRevision":1}`, 200},
		{"previewFieldConversion", http.MethodPost, "/v1/fields/fld_00000000000000000000000000/convert-preview", `{"type":"number"}`, 200},
		{"convertField", http.MethodPost, "/v1/fields/fld_00000000000000000000000000/convert", `{"type":"number","mode":"parse","expectedRevision":1,"previewToken":"tok"}`, 200},
		{"listViews", http.MethodGet, "/v1/tables/tbl_00000000000000000000000000/views", "", 200},
		{"createView", http.MethodPost, "/v1/tables/tbl_00000000000000000000000000/views", `{"name":"Map","type":"map","config":{"locationFieldId":"fld_00000000000000000000000000"}}`, 201},
		{"getView", http.MethodGet, "/v1/views/view_00000000000000000000000000", "", 200},
		{"updateView", http.MethodPatch, "/v1/views/view_00000000000000000000000000", `{"type":"map","config":{"locationFieldId":"fld_00000000000000000000000000"},"expectedRevision":1}`, 200},
		{"deleteView", http.MethodDelete, "/v1/views/view_00000000000000000000000000?expectedRevision=1", "", 204},
		{"restoreView", http.MethodPost, "/v1/views/view_00000000000000000000000000/restore", `{"expectedRevision":1}`, 200},
		{"setDefaultView", http.MethodPost, "/v1/views/view_00000000000000000000000000/default", `{"expectedRevision":1}`, 200},
		{"queryMap", http.MethodPost, "/v1/views/view_00000000000000000000000000/map/query", `{"viewport":{"boxes":[{"west":100,"south":10,"east":120,"north":30}]},"zoom":8,"pixelWidth":1000,"pixelHeight":800}`, 200},
		{"summarizeMap", http.MethodPost, "/v1/views/view_00000000000000000000000000/map/summary", "", 200},
		{"queryMapClusterRecords", http.MethodPost, "/v1/views/view_00000000000000000000000000/map/cluster-records/query", `{"clusterToken":"token"}`, 200},
		{"getRecord", http.MethodGet, "/v1/records/rec_00000000000000000000000000", "", 200},
		{"acquireSelectionScope", http.MethodPost, "/v1/tables/tbl_00000000000000000000000000/records/selection-scope", `{"requestId":"req_00000000000000000000000000","query":{}}`, 200},
		{"resolveSelectionScope", http.MethodPost, "/v1/tables/tbl_00000000000000000000000000/records/selection-scope/resolve", `{"scopeToken":"token","query":{}}`, 200},
		{"queryRecords", http.MethodPost, "/v1/tables/tbl_00000000000000000000000000/records/query", `{}`, 200},
		{"queryFieldValues", http.MethodPost, "/v1/tables/tbl_00000000000000000000000000/fields/fld_00000000000000000000000000/values/query", `{}`, 200},
		{"aggregateRecords", http.MethodPost, "/v1/tables/tbl_00000000000000000000000000/records/aggregate", `{"fieldIds":["fld_00000000000000000000000000"],"fns":["count"]}`, 200},
		{"moveRecord", http.MethodPost, "/v1/tables/tbl_00000000000000000000000000/records/rec_00000000000000000000000000/move", `{"afterRecordId":"rec_11111111111111111111111111"}`, 200},
		{"duplicateRecord", http.MethodPost, "/v1/tables/tbl_00000000000000000000000000/records/rec_00000000000000000000000000/duplicate", "", 200},
		{"mutateRecords", http.MethodPost, "/v1/tables/tbl_00000000000000000000000000/records/mutate", `{"clientMutationId":"mut_00000000000000000000000000","commands":[{"kind":"createRecord","values":{}}]}`, 200},
		{"pullChanges", http.MethodGet, "/v1/tables/tbl_00000000000000000000000000/changes", "", 200},
		{"pullTableAudit", http.MethodGet, "/v1/tables/tbl_00000000000000000000000000/audit", "", 200},
		{"pullHistory", http.MethodGet, "/v1/tables/tbl_00000000000000000000000000/history", "", 200},
		{"initializeAttachment", http.MethodPost, "/v1/attachments/init", `{}`, 501},
		{"getAttachment", http.MethodGet, "/v1/attachments/att_00000000000000000000000000", "", 501},
		{"deleteAttachment", http.MethodDelete, "/v1/attachments/att_00000000000000000000000000", "", 501},
		{"uploadAttachmentContent", http.MethodPut, "/v1/attachments/att_00000000000000000000000000/content", "", 501},
		{"downloadAttachmentContent", http.MethodGet, "/v1/attachments/att_00000000000000000000000000/content", "", 501},
	}

	specification, err := os.ReadFile(filepath.Join("..", "..", "docs", "api", "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	matches := regexp.MustCompile(`(?m)^\s+operationId:\s+(\S+)\s*$`).FindAllSubmatch(specification, -1)
	fromSpecification := make([]string, 0, len(matches))
	for _, match := range matches {
		fromSpecification = append(fromSpecification, string(match[1]))
	}
	fromTest := make([]string, len(operations))
	for index, current := range operations {
		fromTest[index] = current.id
	}
	sort.Strings(fromSpecification)
	sort.Strings(fromTest)
	if strings.Join(fromSpecification, "\n") != strings.Join(fromTest, "\n") {
		t.Fatalf("OpenAPI operations and route test table differ\nspec: %v\ntest: %v", fromSpecification, fromTest)
	}

	server := New(testConfig(), func(context.Context) error { return nil }, Dependencies{
		Authenticator: fixedAuthenticator{}, Catalog: &stubCatalog{}, Records: &stubRecords{},
	})
	for _, current := range operations {
		t.Run(current.id, func(t *testing.T) {
			request := httptest.NewRequest(current.method, current.path, strings.NewReader(current.body))
			if strings.HasPrefix(current.path, "/v1/") && current.path != "/v1/meta" {
				request.Header.Set("Authorization", "Bearer test-token")
			}
			if current.body != "" {
				request.Header.Set("Content-Type", "application/json")
			}
			if strings.HasPrefix(current.id, "create") || current.id == "initializeAttachment" || current.id == "reorderViews" {
				request.Header.Set("Idempotency-Key", "mut_00000000000000000000000000")
			}
			recorder := httptest.NewRecorder()
			server.Handler().ServeHTTP(recorder, request)
			if recorder.Code != current.wantStatus {
				t.Fatalf("%s %s = %d %s, want %d", current.method, current.path, recorder.Code, recorder.Body.String(), current.wantStatus)
			}
		})
	}
}

func TestSelectOptionColorEnumMatchesCatalogAllowance(t *testing.T) {
	specification, err := os.ReadFile(filepath.Join("..", "..", "docs", "api", "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	match := regexp.MustCompile(`(?ms)SelectOptionColor:\s*\n\s+type:\s*string\s*\n\s+enum:\s*\[([^\]]+)\]`).FindSubmatch(specification)
	if match == nil {
		t.Fatal("SelectOptionColor enum not found in openapi.yaml")
	}
	listed := strings.Split(string(match[1]), ",")
	for index := range listed {
		listed[index] = strings.TrimSpace(listed[index])
	}
	if !slices.Equal(listed, catalog.SelectOptionColors) {
		t.Fatalf("OpenAPI SelectOptionColor = %v, catalog.SelectOptionColors = %v", listed, catalog.SelectOptionColors)
	}
}

func TestAggregateSearchOpenAPIContract(t *testing.T) {
	spec, err := os.ReadFile(filepath.Join("..", "..", "docs", "api", "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	request := regexp.MustCompile(`(?ms)^    AggregateRequest:\n(.*?)^    AggregateResult:`).FindSubmatch(spec)
	if request == nil {
		t.Fatal("AggregateRequest schema missing")
	}
	if !strings.Contains(string(request[1]), "required: [fieldIds, fns]") || !regexp.MustCompile(`search:\s*\n\s+type: string\s*\n\s+x-loomtable-normalized-max-length: 500`).Match(request[1]) || strings.Contains(string(request[1]), "maxLength: 500") {
		t.Fatal("Aggregate Search must be an optional string bounded after Trim/NFC, without a raw string length restriction")
	}
	if !strings.Contains(string(spec), "`aggregateSearch`") {
		t.Fatal("Aggregate Search capability missing from Meta contract")
	}
}

func TestAtomicChoiceOpenAPIAdditiveContract(t *testing.T) {
	spec, err := os.ReadFile(filepath.Join("..", "..", "docs", "api", "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"CreateRecordCommand", "UpdateRecordCommand"} {
		match := regexp.MustCompile(`(?ms)^    ` + name + `:\n(.*?)^    [A-Z]`).FindSubmatch(spec)
		if match == nil || !strings.Contains(string(match[1]), "#/components/schemas/ChoiceAssignments") {
			t.Fatalf("%s must accept optional choices", name)
		}
		if strings.Contains(string(match[1]), "required: [choices, ") {
			t.Fatal("legacy commands must remain valid without choices")
		}
	}
	for _, name := range []string{"DeleteRecordCommand", "RestoreRecordCommand"} {
		match := regexp.MustCompile(`(?ms)^    ` + name + `:\n(.*?)^    [A-Z]`).FindSubmatch(spec)
		if match == nil || strings.Contains(string(match[1]), "ChoiceAssignments") {
			t.Fatalf("%s scope changed", name)
		}
	}
	for _, part := range []string{"version: 0.8.0", "`atomicChoice`", "ChoiceFieldResult:", "fieldConflicts:", "MUTATION_RECEIPT_EXPIRED", "MutationReceiptExpiredDetails:", "x-loomtable-normalized-max-length: 100"} {
		if !strings.Contains(string(spec), part) {
			t.Fatalf("G2 contract missing %s", part)
		}
	}
	if strings.Contains(string(spec), "operationId: createChoice") {
		t.Fatal("G2 must use mutateRecords rather than a new endpoint")
	}
}
