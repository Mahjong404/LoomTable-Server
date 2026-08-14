package httpapi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	loomattachment "github.com/Mahjong404/LoomTable-Server/internal/attachment"
	"github.com/Mahjong404/LoomTable-Server/internal/domain"
)

type attachmentHTTPStub struct {
	item        domain.Attachment
	initialized loomattachment.InitRequest
	deleted     int64
	uploadMime  string
}

func (s *attachmentHTTPStub) Initialize(_ context.Context, _ string, _ string, request loomattachment.InitRequest) (domain.Attachment, error) {
	s.initialized = request
	return s.item, nil
}

func (s *attachmentHTTPStub) Get(context.Context, string, string) (domain.Attachment, error) {
	return s.item, nil
}

func (s *attachmentHTTPStub) Delete(_ context.Context, _, _ string, revision int64) error {
	s.deleted = revision
	return nil
}

func (s *attachmentHTTPStub) Upload(_ context.Context, _, _, mimeType string, _ io.Reader) (domain.Attachment, error) {
	s.uploadMime = mimeType
	return s.item, nil
}

func (s *attachmentHTTPStub) Open(context.Context, string, string) (domain.Attachment, io.ReadCloser, error) {
	return s.item, io.NopCloser(strings.NewReader("hello")), nil
}

func TestAttachmentHTTPRoutesAndHeaders(t *testing.T) {
	stub := &attachmentHTTPStub{item: domain.Attachment{
		ID: "att_00000000000000000000000000", Source: "managed", Status: "ready", Filename: "hello.txt",
		MimeType: "text/plain", Size: int64PointerHTTP(5), Revision: 2, CreatedAt: time.Unix(1, 0).UTC(), UpdatedAt: time.Unix(2, 0).UTC(),
	}}
	cfg := testConfig()
	cfg.Capabilities = []string{"grid", "map", "attachments"}
	server := New(cfg, func(context.Context) error { return nil }, Dependencies{Authenticator: fixedAuthenticator{}, Attachments: stub})

	request := httptest.NewRequest(http.MethodPost, "/v1/attachments/init", strings.NewReader(`{"source":"managed","filename":"hello.txt"}`))
	request.Header.Set("Authorization", "Bearer test-token")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "mut_00000000000000000000000000")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusCreated || stub.initialized.Filename != "hello.txt" {
		t.Fatalf("initialize status = %d, body = %s, request = %+v", response.Code, response.Body.String(), stub.initialized)
	}

	request = httptest.NewRequest(http.MethodPut, "/v1/attachments/att_00000000000000000000000000/content", strings.NewReader("hello"))
	request.Header.Set("Authorization", "Bearer test-token")
	request.Header.Set("Content-Type", "text/plain")
	response = httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || stub.uploadMime != "text/plain" {
		t.Fatalf("upload status = %d, body = %s, mime = %q", response.Code, response.Body.String(), stub.uploadMime)
	}

	request = httptest.NewRequest(http.MethodGet, "/v1/attachments/att_00000000000000000000000000/content", nil)
	request.Header.Set("Authorization", "Bearer test-token")
	response = httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != "hello" || response.Header().Get("Content-Type") != "text/plain" {
		t.Fatalf("download = %d %q Content-Type=%q", response.Code, response.Body.String(), response.Header().Get("Content-Type"))
	}

	request = httptest.NewRequest(http.MethodDelete, "/v1/attachments/att_00000000000000000000000000?expectedRevision=2", nil)
	request.Header.Set("Authorization", "Bearer test-token")
	response = httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || stub.deleted != 2 {
		t.Fatalf("delete status = %d, revision = %d", response.Code, stub.deleted)
	}
}

func TestAttachmentHTTPRejectsCompressedUploadAndMissingIdempotencyKey(t *testing.T) {
	stub := &attachmentHTTPStub{item: domain.Attachment{ID: "att_00000000000000000000000000", Filename: "hello.txt"}}
	server := New(testConfig(), func(context.Context) error { return nil }, Dependencies{Authenticator: fixedAuthenticator{}, Attachments: stub})

	request := httptest.NewRequest(http.MethodPost, "/v1/attachments/init", strings.NewReader(`{"source":"managed","filename":"hello.txt"}`))
	request.Header.Set("Authorization", "Bearer test-token")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("missing Idempotency-Key status = %d, body = %s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodPut, "/v1/attachments/att_00000000000000000000000000/content", strings.NewReader("hello"))
	request.Header.Set("Authorization", "Bearer test-token")
	request.Header.Set("Content-Encoding", "gzip")
	response = httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("compressed upload status = %d, body = %s", response.Code, response.Body.String())
	}
}

var _ Attachments = (*attachmentHTTPStub)(nil)

func int64PointerHTTP(value int64) *int64 {
	return &value
}

