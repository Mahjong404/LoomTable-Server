package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Mahjong404/LoomTable-Server/internal/config"
)

func TestAggregateSearchHTTPDecoding(t *testing.T) {
	for _, tc := range []struct {
		json, want string
		status     int
	}{
		{"", "", 200}, {`,"search":""`, "", 200}, {`,"search":"  Straße  "`, "  Straße  ", 200},
		{`,"search":null`, "", 422}, {`,"search":42`, "", 422}, {`,"search":true`, "", 422},
		{`,"search":[]`, "", 422}, {`,"search":{}`, "", 422},
	} {
		store := &stubRecords{}
		server := New(testConfig(), func(context.Context) error { return nil }, Dependencies{Authenticator: fixedAuthenticator{}, Catalog: &stubCatalog{}, Records: store})
		request := httptest.NewRequest(http.MethodPost, "/v1/tables/tbl_00000000000000000000000000/records/aggregate", strings.NewReader(`{"fieldIds":["fld_00000000000000000000000000"],"fns":["count"]`+tc.json+`}`))
		request.Header.Set("Authorization", "Bearer test-token")
		request.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, request)
		if w.Code != tc.status {
			t.Fatalf("Search %s: %d %s", tc.json, w.Code, w.Body.String())
		}
		if store.aggregateRequest.Search != tc.want {
			t.Fatalf("decoded Search = %q, want %q", store.aggregateRequest.Search, tc.want)
		}
		if tc.status != 200 && store.actorID != "" {
			t.Fatal("invalid Search reached service")
		}
	}
}

func TestAggregateSearchCapabilityIndependentOfAttachments(t *testing.T) {
	for _, enabled := range []string{"true", "false"} {
		t.Setenv("LOOMTABLE_ATTACHMENTS_ENABLED", enabled)
		server := New(config.Load(), func(context.Context) error { return nil })
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/meta", nil))
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"aggregateSearch"`) {
			t.Fatalf("meta = %d %s", w.Code, w.Body.String())
		}
	}
}
