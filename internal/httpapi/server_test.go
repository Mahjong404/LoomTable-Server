package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Mahjong404/LoomTable-Server/internal/auth"
	"github.com/Mahjong404/LoomTable-Server/internal/config"
	appstatus "github.com/Mahjong404/LoomTable-Server/internal/status"
)

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