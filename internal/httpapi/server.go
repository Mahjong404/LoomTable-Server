package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/Mahjong404/LoomTable-Server/internal/auth"
	"github.com/Mahjong404/LoomTable-Server/internal/config"
	"github.com/Mahjong404/LoomTable-Server/internal/id"
	appstatus "github.com/Mahjong404/LoomTable-Server/internal/status"
)

type ReadyChecker func(context.Context) error

type Server struct {
	config  config.Config
	ready   ReadyChecker
	handler http.Handler
}

type metaResponse struct {
	ServerVersion        string   `json:"serverVersion"`
	APIVersion           string   `json:"apiVersion"`
	MinPluginVersion     string   `json:"minPluginVersion"`
	Capabilities         []string `json:"capabilities"`
	ChangeRetention      string   `json:"changeRetention"`
	IdempotencyRetention string   `json:"idempotencyRetention"`
	MigrationRequired    bool     `json:"migrationRequired"`
	BootstrapState       string   `json:"bootstrapState"`
}

type errorResponse struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"requestId"`
}

func New(cfg config.Config, ready ReadyChecker) *Server {
	server := &Server{config: cfg, ready: ready}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", server.healthz)
	mux.HandleFunc("/readyz", server.readyz)
	mux.HandleFunc("/v1/meta", server.meta)
	mux.HandleFunc("/v1/attachments", server.withAuth(server.attachmentDisabled))
	mux.HandleFunc("/v1/attachments/", server.withAuth(server.attachmentDisabled))
	mux.HandleFunc("/v1/", server.withAuth(server.notFound))
	mux.HandleFunc("/", server.notFound)
	server.handler = server.withRequestID(mux)
	return server
}

func (s *Server) Handler() http.Handler {
	return s.handler
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, r, http.StatusNotFound, "NOT_FOUND", "resource not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) readyz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, r, http.StatusNotFound, "NOT_FOUND", "resource not found")
		return
	}

	if s.config.MigrationRequired {
		writeAPIError(w, r, http.StatusServiceUnavailable, "MIGRATION_REQUIRED", "an explicit migration is required")
		return
	}
	if s.ready == nil {
		writeAPIError(w, r, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "required dependencies are not configured")
		return
	}
	if err := s.ready(r.Context()); err != nil {
		code := "DEPENDENCY_UNAVAILABLE"
		message := "a required dependency is unavailable"
		if errors.Is(err, appstatus.ErrMigrationRequired) {
			code = "MIGRATION_REQUIRED"
			message = "an explicit migration is required"
		}
		writeAPIError(w, r, http.StatusServiceUnavailable, code, message)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) meta(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, r, http.StatusNotFound, "NOT_FOUND", "resource not found")
		return
	}
	migrationRequired := s.config.MigrationRequired
	if !migrationRequired && s.ready != nil {
		migrationRequired = errors.Is(s.ready(r.Context()), appstatus.ErrMigrationRequired)
	}
	writeJSON(w, http.StatusOK, metaResponse{
		ServerVersion:        s.config.ServerVersion,
		APIVersion:           s.config.APIVersion,
		MinPluginVersion:     s.config.MinPluginVersion,
		Capabilities:         append([]string(nil), s.config.Capabilities...),
		ChangeRetention:      s.config.ChangeRetention,
		IdempotencyRetention: s.config.IdempotencyRetention,
		MigrationRequired:    migrationRequired,
		BootstrapState:       "unknown",
	})
}

func (s *Server) attachmentDisabled(w http.ResponseWriter, r *http.Request) {
	writeAPIError(w, r, http.StatusNotImplemented, "CAPABILITY_NOT_ENABLED", "attachments capability is not enabled in P0")
}

func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/v1/") {
		writeAPIError(w, r, http.StatusNotFound, "NOT_FOUND", "resource not found")
		return
	}
	http.NotFound(w, r)
}

func (s *Server) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !auth.VerifyBearer(r.Header.Get("Authorization"), s.config.AuthTokenHash) {
			writeAPIError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "a valid Bearer Token is required")
			return
		}
		next(w, r)
	}
}

func (s *Server) withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID, err := id.New(id.RequestPrefix)
		if err != nil {
			requestID = id.RequestPrefix + "unknown"
		}
		w.Header().Set("X-Request-ID", requestID)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey{}, requestID)))
	})
}

type requestIDKey struct{}

func requestIDFrom(r *http.Request) string {
	if value, ok := r.Context().Value(requestIDKey{}).(string); ok && value != "" {
		return value
	}
	return id.RequestPrefix + "unknown"
}

func writeAPIError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	writeJSON(w, status, errorResponse{Error: errorBody{
		Code:      code,
		Message:   message,
		RequestID: requestIDFrom(r),
	}})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
