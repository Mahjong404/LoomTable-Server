package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeJSONRequestRejectsUnsupportedRepresentations(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		encoding    string
		body        string
		wantStatus  int
		wantCode    string
	}{
		{name: "wrong media type", contentType: "text/plain", body: `{}`, wantStatus: 415, wantCode: "UNSUPPORTED_MEDIA_TYPE"},
		{name: "wrong charset", contentType: "application/json; charset=iso-8859-1", body: `{}`, wantStatus: 415, wantCode: "UNSUPPORTED_MEDIA_TYPE"},
		{name: "compressed", contentType: "application/json", encoding: "gzip", body: `{}`, wantStatus: 415, wantCode: "UNSUPPORTED_MEDIA_TYPE"},
		{name: "multiple values", contentType: "application/json", body: `{} {}`, wantStatus: 400, wantCode: "BAD_REQUEST"},
		{name: "duplicate key", contentType: "application/json", body: `{"name":"one","name":"two"}`, wantStatus: 422, wantCode: "VALIDATION_ERROR"},
		{name: "unknown property", contentType: "application/json", body: `{"name":"one","extra":true}`, wantStatus: 422, wantCode: "VALIDATION_ERROR"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(test.body))
			request.Header.Set("Content-Type", test.contentType)
			if test.encoding != "" {
				request.Header.Set("Content-Encoding", test.encoding)
			}
			var decoded createWorkspaceRequest
			err := decodeJSONRequest(request, &decoded)
			decodeError, ok := err.(*requestDecodeError)
			if !ok {
				t.Fatalf("error = %T %v, want requestDecodeError", err, err)
			}
			if decodeError.Status != test.wantStatus || decodeError.Code != test.wantCode {
				t.Fatalf("decode error = %#v, want status %d code %s", decodeError, test.wantStatus, test.wantCode)
			}
		})
	}
}

func TestDecodeJSONRequestAcceptsUTF8JSON(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"工作区"}`))
	request.Header.Set("Content-Type", "application/json; charset=UTF-8")
	request.Header.Set("Content-Encoding", "identity")

	var decoded createWorkspaceRequest
	if err := decodeJSONRequest(request, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Name == nil || *decoded.Name != "工作区" {
		t.Fatalf("decoded = %#v", decoded)
	}
}

func TestDecodeJSONRequestEnforcesRawBodyLimit(t *testing.T) {
	body := `{"name":"` + strings.Repeat("a", maxJSONBodyBytes) + `"}`
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")

	var decoded createWorkspaceRequest
	err := decodeJSONRequest(request, &decoded)
	decodeError, ok := err.(*requestDecodeError)
	if !ok || decodeError.Status != http.StatusRequestEntityTooLarge || decodeError.Code != "PAYLOAD_TOO_LARGE" {
		t.Fatalf("error = %#v, want PAYLOAD_TOO_LARGE", err)
	}
}
