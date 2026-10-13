package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestViewOrderHTTPRoute(t *testing.T) {
	server := New(testConfig(), func(context.Context) error { return nil }, Dependencies{Authenticator: fixedAuthenticator{}, Catalog: &stubCatalog{}})
	r := httptest.NewRequest(http.MethodPut, "/v1/tables/tbl_00000000000000000000000000/views/order", strings.NewReader(`{"expectedOrderRevision":1,"viewIds":[]}`))
	r.Header.Set("Authorization", "Bearer test-token")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Idempotency-Key", "mut_00000000000000000000000000")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("Order route = %d %s", w.Code, w.Body.String())
	}
}

func TestViewOrderHTTPStrictPayloadAndListOptIn(t *testing.T) {
	server := New(testConfig(), func(context.Context) error { return nil }, Dependencies{Authenticator: fixedAuthenticator{}, Catalog: &stubCatalog{}})
	for _, body := range []string{`{}`, `{"expectedOrderRevision":null,"viewIds":[]}`, `{"expectedOrderRevision":1,"viewIds":null}`, `{"expectedOrderRevision":1,"viewIds":{},"extra":true}`, `{"expectedOrderRevision":1,"viewIds":[],"extra":true}`} {
		r := httptest.NewRequest(http.MethodPut, "/v1/tables/tbl_00000000000000000000000000/views/order", strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer test-token")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", "mut_00000000000000000000000000")
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, r)
		if w.Code != 422 {
			t.Fatalf("invalid body = %d %s", w.Code, w.Body.String())
		}
	}
	for _, query := range []string{"", "?includeOrder=false", "?includeOrder=true", "?includeOrder=wrong", "?includeOrder=true&includeOrder=false", "?includeOrder=true&other=1"} {
		r := httptest.NewRequest(http.MethodGet, "/v1/tables/tbl_00000000000000000000000000/views"+query, nil)
		r.Header.Set("Authorization", "Bearer test-token")
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, r)
		if query == "" || query == "?includeOrder=false" || query == "?includeOrder=true" {
			if w.Code != 200 || strings.Contains(w.Body.String(), `"order"`) != (query == "?includeOrder=true") {
				t.Fatalf("opt-in %s=%d %s", query, w.Code, w.Body.String())
			}
		} else if w.Code != 400 {
			t.Fatalf("bad query %s=%d", query, w.Code)
		}
	}
}
