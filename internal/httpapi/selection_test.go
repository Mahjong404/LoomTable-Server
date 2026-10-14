package httpapi

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	record "github.com/Mahjong404/LoomTable-Server/internal/record"
)

func (s *stubRecords) AcquireSelection(context.Context, string, string, string, record.SelectionQuery) (record.SelectionResult, error) {
	return record.SelectionResult{}, nil
}
func (s *stubRecords) ResolveSelection(context.Context, string, string, string, record.SelectionQuery, []string, []string) (record.SelectionResult, error) {
	return record.SelectionResult{}, nil
}
func TestSelectionStrictRequests(t *testing.T) {
	for _, body := range []string{`{"query":null}`, `{"requestId":"req_00000000000000000000000000","query":{"sort":[]}}`, `{"requestId":"req_00000000000000000000000000","query":{"filter":{}}}`, `{"requestId":"req_00000000000000000000000000","query":{"search":null}}`} {
		server := New(testConfig(), func(context.Context) error { return nil }, Dependencies{Authenticator: fixedAuthenticator{}, Records: &stubRecords{}})
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
