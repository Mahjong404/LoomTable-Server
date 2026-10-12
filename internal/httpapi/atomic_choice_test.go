package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Mahjong404/LoomTable-Server/internal/config"
	"github.com/Mahjong404/LoomTable-Server/internal/domain"
	loomrecord "github.com/Mahjong404/LoomTable-Server/internal/record"
)

func TestAtomicChoiceHTTPPreservesExistingRecordPayload(t *testing.T) {
	store := &stubRecords{}
	server := New(testConfig(), func(context.Context) error { return nil }, Dependencies{Authenticator: fixedAuthenticator{}, Catalog: &stubCatalog{}, Records: store})
	body := `{"clientMutationId":"mut_00000000000000000000000000","commands":[{"kind":"updateRecord","recordId":"rec_00000000000000000000000000","expectedRevision":4,"choices":[{"fieldId":"fld_00000000000000000000000000","expectedFieldRevision":7,"options":[{"name":"  Straße  ","color":"lime"}]}]}]}`
	r := httptest.NewRequest(http.MethodPost, "/v1/tables/tbl_00000000000000000000000000/records/mutate", strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer test-token")
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("Choice request = %d %s", w.Code, w.Body.String())
	}
	encoded, err := json.Marshal(store.command)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"expectedFieldRevision":7`) || !strings.Contains(string(encoded), `"name":"  Straße  "`) {
		t.Fatalf("original Choice payload lost: %s", encoded)
	}
}

func TestAtomicChoiceHTTPRejectsMalformedNestedPayloadBeforeService(t *testing.T) {
	for _, choices := range []string{
		`null`, `{}`, `42`,
		`[{"fieldId":"fld_00000000000000000000000000","expectedFieldRevision":1,"options":null}]`,
		`[{"fieldId":"fld_00000000000000000000000000","expectedFieldRevision":1,"options":[{"optionId":null}]}]`,
		`[{"fieldId":"fld_00000000000000000000000000","expectedFieldRevision":1,"options":[{"optionId":"opt_00000000000000000000000000","name":""}]}]`,
		`[{"fieldId":"fld_00000000000000000000000000","expectedFieldRevision":1,"options":[{"name":"A","color":null}]}]`,
		`[{"fieldId":"fld_00000000000000000000000000","expectedFieldRevision":1,"options":[{"name":"A","color":"gray","id":"fake"}]}]`,
		`[{"fieldId":"fld_00000000000000000000000000","expectedFieldRevision":1,"extra":true,"options":[]}]`,
	} {
		store := &stubRecords{}
		server := New(testConfig(), func(context.Context) error { return nil }, Dependencies{Authenticator: fixedAuthenticator{}, Catalog: &stubCatalog{}, Records: store})
		r := httptest.NewRequest(http.MethodPost, "/v1/tables/tbl_00000000000000000000000000/records/mutate", strings.NewReader(`{"clientMutationId":"mut_00000000000000000000000000","commands":[{"kind":"createRecord","values":{},"choices":`+choices+`}]}`))
		r.Header.Set("Authorization", "Bearer test-token")
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, r)
		if w.Code != 422 || store.actorID != "" || !strings.Contains(w.Body.String(), "/commands/0/choices") {
			t.Fatalf("invalid choices %s: %d %s", choices, w.Code, w.Body.String())
		}
	}
}

func TestAtomicChoiceHTTPFieldConflictAndExpiredReceipt(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
		part   string
	}{
		{&loomrecord.ConflictError{ClientMutationID: "mut_00000000000000000000000000", FailedCommandIndex: 0, Conflicts: []loomrecord.Conflict{}, FieldConflicts: []loomrecord.FieldConflict{{FieldID: "fld_00000000000000000000000000", ExpectedRevision: 1, CurrentRevision: 2}}}, 409, `"fieldConflicts":[`},
		{&domain.MutationReceiptExpiredError{ClientMutationID: "mut_00000000000000000000000000"}, 410, `"operationCommitted":true`},
	} {
		server := New(testConfig(), func(context.Context) error { return nil }, Dependencies{Authenticator: fixedAuthenticator{}, Catalog: &stubCatalog{}, Records: &stubRecords{mutateErr: tc.err}})
		r := httptest.NewRequest(http.MethodPost, "/v1/tables/tbl_00000000000000000000000000/records/mutate", strings.NewReader(`{"clientMutationId":"mut_00000000000000000000000000","commands":[{"kind":"createRecord","values":{}}]}`))
		r.Header.Set("Authorization", "Bearer test-token")
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, r)
		if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.part) {
			t.Fatalf("G2 error = %d %s", w.Code, w.Body.String())
		}
	}
}

func TestAtomicChoiceCapabilityIndependentOfAttachments(t *testing.T) {
	for _, enabled := range []string{"true", "false"} {
		t.Setenv("LOOMTABLE_ATTACHMENTS_ENABLED", enabled)
		server := New(config.Load(), func(context.Context) error { return nil })
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/meta", nil))
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"atomicChoice"`) {
			t.Fatalf("meta = %d %s", w.Code, w.Body.String())
		}
	}
}
