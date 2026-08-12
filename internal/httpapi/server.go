package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/Mahjong404/LoomTable-Server/internal/auth"
	"github.com/Mahjong404/LoomTable-Server/internal/config"
	"github.com/Mahjong404/LoomTable-Server/internal/domain"
	"github.com/Mahjong404/LoomTable-Server/internal/id"
	loomrecord "github.com/Mahjong404/LoomTable-Server/internal/record"
	appstatus "github.com/Mahjong404/LoomTable-Server/internal/status"
)

type ReadyChecker func(context.Context) error

type Authenticator interface {
	Authenticate(context.Context, string) (string, error)
}

type BootstrapStateChecker interface {
	BootstrapState(context.Context) (string, error)
}

type Catalog interface {
	ListWorkspaces(context.Context, string) ([]domain.Workspace, error)
	GetWorkspace(context.Context, string, string) (domain.Workspace, error)
	CreateWorkspace(context.Context, string, string, string) (domain.Workspace, error)
	UpdateWorkspace(context.Context, string, string, int64, string) (domain.Workspace, error)
	ListBases(context.Context, string, string) ([]domain.Base, error)
	GetBase(context.Context, string, string) (domain.Base, error)
	CreateBase(context.Context, string, string, string, string) (domain.Base, error)
	UpdateBase(context.Context, string, string, int64, string) (domain.Base, error)
	ListTables(context.Context, string, string, string) ([]domain.Table, error)
	GetTable(context.Context, string, string) (domain.Table, error)
	CreateTable(context.Context, string, string, string, string, *string, *string) (domain.CreateTableResult, error)
	UpdateTable(context.Context, string, string, int64, string) (domain.Table, error)
	DeleteTable(context.Context, string, string, int64) error
	RestoreTable(context.Context, string, string, int64) (domain.Table, error)
}

type Records interface {
	Get(context.Context, string, string) (loomrecord.Record, error)
	Mutate(context.Context, string, string, string, []loomrecord.Command) (loomrecord.MutationResult, error)
}

type Dependencies struct {
	Authenticator Authenticator
	Bootstrap     BootstrapStateChecker
	Catalog       Catalog
	Records       Records
}

type Server struct {
	config        config.Config
	ready         ReadyChecker
	authenticator Authenticator
	bootstrap     BootstrapStateChecker
	catalog       Catalog
	records       Records
	handler       http.Handler
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
	Details   any    `json:"details,omitempty"`
}

func New(cfg config.Config, ready ReadyChecker, provided ...Dependencies) *Server {
	var dependencies Dependencies
	if len(provided) > 0 {
		dependencies = provided[0]
	}
	server := &Server{
		config:        cfg,
		ready:         ready,
		authenticator: dependencies.Authenticator,
		bootstrap:     dependencies.Bootstrap,
		catalog:       dependencies.Catalog,
		records:       dependencies.Records,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", server.healthz)
	mux.HandleFunc("/readyz", server.readyz)
	mux.HandleFunc("/v1/meta", server.meta)
	mux.HandleFunc("/v1/workspaces", server.withAuth(server.workspaces))
	mux.HandleFunc("/v1/workspaces/", server.withAuth(server.workspace))
	mux.HandleFunc("/v1/bases", server.withAuth(server.bases))
	mux.HandleFunc("/v1/bases/", server.withAuth(server.base))
	mux.HandleFunc("/v1/tables", server.withAuth(server.tables))
	mux.HandleFunc("/v1/tables/", server.withAuth(server.table))
	mux.HandleFunc("/v1/records/", server.withAuth(server.record))
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
	bootstrapState := "unknown"
	if s.bootstrap != nil {
		if current, err := s.bootstrap.BootstrapState(r.Context()); err == nil {
			bootstrapState = current
		}
	}
	writeJSON(w, http.StatusOK, metaResponse{
		ServerVersion:        s.config.ServerVersion,
		APIVersion:           s.config.APIVersion,
		MinPluginVersion:     s.config.MinPluginVersion,
		Capabilities:         append([]string(nil), s.config.Capabilities...),
		ChangeRetention:      s.config.ChangeRetention,
		IdempotencyRetention: s.config.IdempotencyRetention,
		MigrationRequired:    migrationRequired,
		BootstrapState:       bootstrapState,
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
		header := r.Header.Get("Authorization")
		if s.authenticator != nil {
			token, ok := auth.BearerToken(header)
			if !ok {
				writeAPIError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "a valid Bearer Token is required")
				return
			}
			actorID, err := s.authenticator.Authenticate(r.Context(), token)
			if err != nil {
				if errors.Is(err, domain.ErrUnauthenticated) {
					writeAPIError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "a valid Bearer Token is required")
				} else {
					writeAPIError(w, r, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "authentication dependency is unavailable")
				}
				return
			}
			next(w, r.WithContext(context.WithValue(r.Context(), actorIDKey{}, actorID)))
			return
		}
		if !auth.VerifyBearer(header, s.config.AuthTokenHash) {
			writeAPIError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "a valid Bearer Token is required")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), actorIDKey{}, "act_legacy")))
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
type actorIDKey struct{}

func requestIDFrom(r *http.Request) string {
	if value, ok := r.Context().Value(requestIDKey{}).(string); ok && value != "" {
		return value
	}
	return id.RequestPrefix + "unknown"
}

func actorIDFrom(r *http.Request) string {
	if value, ok := r.Context().Value(actorIDKey{}).(string); ok {
		return value
	}
	return ""
}

func writeAPIError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	writeAPIErrorWithDetails(w, r, status, code, message, nil)
}

func writeAPIErrorWithDetails(w http.ResponseWriter, r *http.Request, status int, code, message string, details any) {
	writeJSON(w, status, errorResponse{Error: errorBody{
		Code:      code,
		Message:   message,
		RequestID: requestIDFrom(r),
		Details:   details,
	}})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
