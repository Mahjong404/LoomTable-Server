package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/Mahjong404/LoomTable-Server/internal/domain"
	loomrecord "github.com/Mahjong404/LoomTable-Server/internal/record"
)

type createWorkspaceRequest struct {
	Name *string `json:"name"`
}

type createBaseRequest struct {
	WorkspaceID *string `json:"workspaceId"`
	Name        *string `json:"name"`
}

type updateNameRequest struct {
	Name             *string `json:"name"`
	ExpectedRevision *int64  `json:"expectedRevision"`
}

type createTableRequest struct {
	BaseID           *string `json:"baseId"`
	Name             *string `json:"name"`
	PrimaryFieldName *string `json:"primaryFieldName"`
	InitialViewName  *string `json:"initialViewName"`
}

type restoreMetadataRequest struct {
	ExpectedRevision *int64 `json:"expectedRevision"`
}

func (s *Server) workspaces(w http.ResponseWriter, r *http.Request) {
	if s.catalog == nil {
		writeDomainError(w, r, domain.ErrDependencyMissing)
		return
	}
	switch r.Method {
	case http.MethodGet:
		if len(r.URL.Query()) != 0 {
			writeAPIError(w, r, http.StatusBadRequest, "BAD_REQUEST", "listWorkspaces does not accept query parameters")
			return
		}
		items, err := s.catalog.ListWorkspaces(r.Context(), actorIDFrom(r))
		if err != nil {
			writeDomainError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	case http.MethodPost:
		var request createWorkspaceRequest
		if err := decodeJSONRequest(r, &request); err != nil {
			writeDecodeError(w, r, err)
			return
		}
		if request.Name == nil {
			writeDomainError(w, r, domain.NewValidationError(domain.ValidationIssue{Path: "/name", Code: "required", Message: "name is required"}))
			return
		}
		key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		if key == "" {
			writeAPIError(w, r, http.StatusBadRequest, "BAD_REQUEST", "Idempotency-Key is required")
			return
		}
		created, err := s.catalog.CreateWorkspace(r.Context(), actorIDFrom(r), key, *request.Name)
		if err != nil {
			writeDomainError(w, r, err)
			return
		}
		writeJSON(w, http.StatusCreated, created)
	default:
		writeAPIError(w, r, http.StatusNotFound, "NOT_FOUND", "resource not found")
	}
}

func (s *Server) workspace(w http.ResponseWriter, r *http.Request) {
	if s.catalog == nil {
		writeDomainError(w, r, domain.ErrDependencyMissing)
		return
	}
	workspaceID, ok := singlePathID(r.URL.Path, "/v1/workspaces/")
	if !ok {
		writeAPIError(w, r, http.StatusNotFound, "NOT_FOUND", "resource not found")
		return
	}
	switch r.Method {
	case http.MethodGet:
		item, err := s.catalog.GetWorkspace(r.Context(), actorIDFrom(r), workspaceID)
		if err != nil {
			writeDomainError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, item)
	case http.MethodPatch:
		var request updateNameRequest
		if err := decodeJSONRequest(r, &request); err != nil {
			writeDecodeError(w, r, err)
			return
		}
		if err := validateUpdateNameRequest(request); err != nil {
			writeDomainError(w, r, err)
			return
		}
		item, err := s.catalog.UpdateWorkspace(r.Context(), actorIDFrom(r), workspaceID, *request.ExpectedRevision, *request.Name)
		if err != nil {
			writeDomainError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, item)
	default:
		writeAPIError(w, r, http.StatusNotFound, "NOT_FOUND", "resource not found")
	}
}

func (s *Server) bases(w http.ResponseWriter, r *http.Request) {
	if s.catalog == nil {
		writeDomainError(w, r, domain.ErrDependencyMissing)
		return
	}
	switch r.Method {
	case http.MethodGet:
		workspaceID, ok := requiredSingleQuery(r, "workspaceId")
		if !ok || len(r.URL.Query()) != 1 {
			writeAPIError(w, r, http.StatusBadRequest, "BAD_REQUEST", "exactly one workspaceId query parameter is required")
			return
		}
		items, err := s.catalog.ListBases(r.Context(), actorIDFrom(r), workspaceID)
		if err != nil {
			writeDomainError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	case http.MethodPost:
		var request createBaseRequest
		if err := decodeJSONRequest(r, &request); err != nil {
			writeDecodeError(w, r, err)
			return
		}
		issues := make([]domain.ValidationIssue, 0, 2)
		if request.WorkspaceID == nil {
			issues = append(issues, domain.ValidationIssue{Path: "/workspaceId", Code: "required", Message: "workspaceId is required"})
		}
		if request.Name == nil {
			issues = append(issues, domain.ValidationIssue{Path: "/name", Code: "required", Message: "name is required"})
		}
		if len(issues) > 0 {
			writeDomainError(w, r, domain.NewValidationError(issues...))
			return
		}
		key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		if key == "" {
			writeAPIError(w, r, http.StatusBadRequest, "BAD_REQUEST", "Idempotency-Key is required")
			return
		}
		created, err := s.catalog.CreateBase(r.Context(), actorIDFrom(r), key, *request.WorkspaceID, *request.Name)
		if err != nil {
			writeDomainError(w, r, err)
			return
		}
		writeJSON(w, http.StatusCreated, created)
	default:
		writeAPIError(w, r, http.StatusNotFound, "NOT_FOUND", "resource not found")
	}
}

func (s *Server) base(w http.ResponseWriter, r *http.Request) {
	if s.catalog == nil {
		writeDomainError(w, r, domain.ErrDependencyMissing)
		return
	}
	baseID, ok := singlePathID(r.URL.Path, "/v1/bases/")
	if !ok {
		writeAPIError(w, r, http.StatusNotFound, "NOT_FOUND", "resource not found")
		return
	}
	switch r.Method {
	case http.MethodGet:
		item, err := s.catalog.GetBase(r.Context(), actorIDFrom(r), baseID)
		if err != nil {
			writeDomainError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, item)
	case http.MethodPatch:
		var request updateNameRequest
		if err := decodeJSONRequest(r, &request); err != nil {
			writeDecodeError(w, r, err)
			return
		}
		if err := validateUpdateNameRequest(request); err != nil {
			writeDomainError(w, r, err)
			return
		}
		item, err := s.catalog.UpdateBase(r.Context(), actorIDFrom(r), baseID, *request.ExpectedRevision, *request.Name)
		if err != nil {
			writeDomainError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, item)
	default:
		writeAPIError(w, r, http.StatusNotFound, "NOT_FOUND", "resource not found")
	}
}

func (s *Server) tables(w http.ResponseWriter, r *http.Request) {
	if s.catalog == nil {
		writeDomainError(w, r, domain.ErrDependencyMissing)
		return
	}
	switch r.Method {
	case http.MethodGet:
		baseID, ok := requiredSingleQuery(r, "baseId")
		if !ok {
			writeAPIError(w, r, http.StatusBadRequest, "BAD_REQUEST", "exactly one baseId query parameter is required")
			return
		}
		lifecycle := "active"
		if values, present := r.URL.Query()["lifecycle"]; present {
			if len(values) != 1 || values[0] == "" {
				writeAPIError(w, r, http.StatusBadRequest, "BAD_REQUEST", "lifecycle must appear at most once")
				return
			}
			lifecycle = values[0]
		}
		if len(r.URL.Query()) > 2 {
			writeAPIError(w, r, http.StatusBadRequest, "BAD_REQUEST", "unsupported query parameter")
			return
		}
		items, err := s.catalog.ListTables(r.Context(), actorIDFrom(r), baseID, lifecycle)
		if err != nil {
			writeDomainError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	case http.MethodPost:
		var request createTableRequest
		if err := decodeJSONRequest(r, &request); err != nil {
			writeDecodeError(w, r, err)
			return
		}
		issues := make([]domain.ValidationIssue, 0, 2)
		if request.BaseID == nil {
			issues = append(issues, domain.ValidationIssue{Path: "/baseId", Code: "required", Message: "baseId is required"})
		}
		if request.Name == nil {
			issues = append(issues, domain.ValidationIssue{Path: "/name", Code: "required", Message: "name is required"})
		}
		if len(issues) > 0 {
			writeDomainError(w, r, domain.NewValidationError(issues...))
			return
		}
		key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		if key == "" {
			writeAPIError(w, r, http.StatusBadRequest, "BAD_REQUEST", "Idempotency-Key is required")
			return
		}
		created, err := s.catalog.CreateTable(r.Context(), actorIDFrom(r), key, *request.BaseID, *request.Name, request.PrimaryFieldName, request.InitialViewName)
		if err != nil {
			writeDomainError(w, r, err)
			return
		}
		writeJSON(w, http.StatusCreated, created)
	default:
		writeAPIError(w, r, http.StatusNotFound, "NOT_FOUND", "resource not found")
	}
}

func (s *Server) table(w http.ResponseWriter, r *http.Request) {
	if s.catalog == nil {
		writeDomainError(w, r, domain.ErrDependencyMissing)
		return
	}
	trimmed := strings.TrimPrefix(r.URL.Path, "/v1/tables/")
	if strings.HasSuffix(trimmed, "/records/mutate") {
		tableID := strings.TrimSuffix(trimmed, "/records/mutate")
		if tableID == "" || strings.Contains(tableID, "/") {
			writeAPIError(w, r, http.StatusNotFound, "NOT_FOUND", "resource not found")
			return
		}
		s.mutateRecords(w, r, tableID)
		return
	}
	if strings.HasSuffix(trimmed, "/restore") {
		tableID := strings.TrimSuffix(trimmed, "/restore")
		if tableID == "" || strings.Contains(tableID, "/") || r.Method != http.MethodPost {
			writeAPIError(w, r, http.StatusNotFound, "NOT_FOUND", "resource not found")
			return
		}
		var request restoreMetadataRequest
		if err := decodeJSONRequest(r, &request); err != nil {
			writeDecodeError(w, r, err)
			return
		}
		if request.ExpectedRevision == nil {
			writeDomainError(w, r, domain.NewValidationError(domain.ValidationIssue{Path: "/expectedRevision", Code: "required", Message: "expectedRevision is required"}))
			return
		}
		item, err := s.catalog.RestoreTable(r.Context(), actorIDFrom(r), tableID, *request.ExpectedRevision)
		if err != nil {
			writeDomainError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, item)
		return
	}

	tableID, ok := singlePathID(r.URL.Path, "/v1/tables/")
	if !ok {
		writeAPIError(w, r, http.StatusNotFound, "NOT_FOUND", "resource not found")
		return
	}
	switch r.Method {
	case http.MethodGet:
		item, err := s.catalog.GetTable(r.Context(), actorIDFrom(r), tableID)
		if err != nil {
			writeDomainError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, item)
	case http.MethodPatch:
		var request updateNameRequest
		if err := decodeJSONRequest(r, &request); err != nil {
			writeDecodeError(w, r, err)
			return
		}
		if err := validateUpdateNameRequest(request); err != nil {
			writeDomainError(w, r, err)
			return
		}
		item, err := s.catalog.UpdateTable(r.Context(), actorIDFrom(r), tableID, *request.ExpectedRevision, *request.Name)
		if err != nil {
			writeDomainError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, item)
	case http.MethodDelete:
		expectedRevision, ok := requiredPositiveRevisionQuery(r, "expectedRevision")
		if !ok || len(r.URL.Query()) != 1 {
			writeAPIError(w, r, http.StatusBadRequest, "BAD_REQUEST", "one positive expectedRevision query parameter is required")
			return
		}
		if err := s.catalog.DeleteTable(r.Context(), actorIDFrom(r), tableID, expectedRevision); err != nil {
			writeDomainError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeAPIError(w, r, http.StatusNotFound, "NOT_FOUND", "resource not found")
	}
}

func validateUpdateNameRequest(request updateNameRequest) error {
	issues := make([]domain.ValidationIssue, 0, 2)
	if request.Name == nil {
		issues = append(issues, domain.ValidationIssue{Path: "/name", Code: "required", Message: "name is required"})
	}
	if request.ExpectedRevision == nil {
		issues = append(issues, domain.ValidationIssue{Path: "/expectedRevision", Code: "required", Message: "expectedRevision is required"})
	}
	if len(issues) > 0 {
		return domain.NewValidationError(issues...)
	}
	return nil
}

func singlePathID(path, prefix string) (string, bool) {
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}
	value := strings.TrimPrefix(path, prefix)
	return value, value != "" && !strings.Contains(value, "/")
}

func requiredSingleQuery(r *http.Request, name string) (string, bool) {
	values, ok := r.URL.Query()[name]
	returnValue := ""
	if len(values) == 1 {
		returnValue = values[0]
	}
	return returnValue, ok && len(values) == 1 && returnValue != ""
}

func requiredPositiveRevisionQuery(r *http.Request, name string) (int64, bool) {
	value, ok := requiredSingleQuery(r, name)
	if !ok {
		return 0, false
	}
	revision, err := strconv.ParseInt(value, 10, 64)
	return revision, err == nil && revision > 0
}

func writeDomainError(w http.ResponseWriter, r *http.Request, err error) {
	var recordConflict *loomrecord.ConflictError
	if errors.As(err, &recordConflict) {
		writeRecordConflict(w, r, recordConflict)
		return
	}
	var badRequest *domain.BadRequestError
	if errors.As(err, &badRequest) {
		writeAPIError(w, r, http.StatusBadRequest, "BAD_REQUEST", badRequest.Error())
		return
	}
	var validation *domain.ValidationError
	if errors.As(err, &validation) {
		writeAPIErrorWithDetails(w, r, http.StatusUnprocessableEntity, "VALIDATION_ERROR", validation.Error(), map[string]any{"issues": validation.Issues})
		return
	}
	if errors.Is(err, domain.ErrNotFound) {
		writeAPIError(w, r, http.StatusNotFound, "NOT_FOUND", "resource not found")
		return
	}
	var conflict *domain.RevisionConflictError
	if errors.As(err, &conflict) {
		writeAPIError(w, r, http.StatusConflict, "CONFLICT", "expectedRevision does not match the current revision")
		return
	}
	var reused *domain.IdempotencyKeyReusedError
	if errors.As(err, &reused) {
		writeAPIError(w, r, http.StatusConflict, "IDEMPOTENCY_KEY_REUSED", reused.Error())
		return
	}
	var limit *domain.ResourceLimitError
	if errors.As(err, &limit) {
		details := map[string]any{
			"resource": limit.Resource,
			"parent":   map[string]string{"type": limit.ParentType, "id": limit.ParentID},
			"limit":    limit.Limit,
		}
		writeAPIErrorWithDetails(w, r, http.StatusUnprocessableEntity, "RESOURCE_LIMIT_EXCEEDED", limit.Error(), details)
		return
	}
	var state *domain.InvalidStateTransitionError
	if errors.As(err, &state) {
		details := map[string]any{
			"resource": map[string]string{"type": state.Resource, "id": state.ID},
			"action":   state.Action,
			"current":  state.Current,
		}
		writeAPIErrorWithDetails(w, r, http.StatusUnprocessableEntity, "INVALID_STATE_TRANSITION", state.Error(), details)
		return
	}
	if errors.Is(err, domain.ErrDependencyMissing) {
		writeAPIError(w, r, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "required application dependency is unavailable")
		return
	}
	writeAPIError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "an internal server error occurred")
}
