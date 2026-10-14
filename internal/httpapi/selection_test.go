package httpapi

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	record "github.com/Mahjong404/LoomTable-Server/internal/record"
	appstatus "github.com/Mahjong404/LoomTable-Server/internal/status"
)

func (s *stubRecords) AcquireSelection(context.Context, string, string, string, record.SelectionQuery) (record.SelectionResult, error) {
	return record.SelectionResult{}, nil
}

func TestSelectionNullFilterOverride(t *testing.T) {
	query, err := decodeSelectionQuery([]byte(`{"filter":null}`))
	if err != nil || !query.FilterPresent || query.Filter != nil {
		t.Fatal("null must explicitly override inherited Filter", err)
	}
	query, err = decodeSelectionQuery([]byte(`{}`))
	if err != nil || query.FilterPresent {
		t.Fatal("absent Filter must inherit", err)
	}
}

func TestSelectionRequiresReadiness(t *testing.T) {
	for _, mode := range []string{"explicit", "missing-migration", "dependency", "unconfigured"} {
		t.Run(mode, func(t *testing.T) {
			cfg := testConfig()
			var ready ReadyChecker = func(context.Context) error { return nil }
			switch mode {
			case "explicit":
				cfg.MigrationRequired = true
			case "missing-migration":
				ready = func(context.Context) error { return appstatus.ErrMigrationRequired }
			case "dependency":
				ready = func(context.Context) error { return appstatus.ErrDependencyUnavailable }
			case "unconfigured":
				ready = nil
			}
			server := New(cfg, ready, Dependencies{Authenticator: fixedAuthenticator{}, Catalog: &stubCatalog{}, Records: &stubRecords{}})
			for _, test := range []struct{ suffix, body string }{{"selection-scope", `{"requestId":"req_00000000000000000000000000","query":{}}`}, {"selection-scope/resolve", `{"scopeToken":"token","query":{}}`}, {"query", `{"selectionScopeToken":"token"}`}} {
				req := httptest.NewRequest("POST", "/v1/tables/tbl_00000000000000000000000000/records/"+test.suffix, strings.NewReader(test.body))
				req.Header.Set("Authorization", "Bearer test-token")
				req.Header.Set("Content-Type", "application/json")
				res := httptest.NewRecorder()
				server.Handler().ServeHTTP(res, req)
				if res.Code != 503 {
					t.Fatalf("%s bypassed %s readiness: %d", test.suffix, mode, res.Code)
				}
			}
		})
	}
}
func (s *stubRecords) ResolveSelection(context.Context, string, string, string, record.SelectionQuery, []string, []string) (record.SelectionResult, error) {
	return record.SelectionResult{}, nil
}
func TestSelectionStrictRequests(t *testing.T) {
	for _, body := range []string{`{"query":null}`, `{"requestId":"req_00000000000000000000000000","query":{"sort":[]}}`, `{"requestId":"req_00000000000000000000000000","query":{"filter":{}}}`, `{"requestId":"req_00000000000000000000000000","query":{"search":null}}`} {
		server := New(testConfig(), func(context.Context) error { return nil }, Dependencies{Authenticator: fixedAuthenticator{}, Catalog: &stubCatalog{}, Records: &stubRecords{}})
		request := httptest.NewRequest("POST", "/v1/tables/tbl_00000000000000000000000000/records/selection-scope", strings.NewReader(body))
		request.Header.Set("Authorization", "Bearer test-token")
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, request)
		if response.Code < 400 {
			t.Fatalf("invalid selection accepted: %s", body)
		}
	}
}
