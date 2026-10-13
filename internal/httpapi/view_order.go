package httpapi

import (
	"net/http"
	"strings"

	"github.com/Mahjong404/LoomTable-Server/internal/catalog"
	"github.com/Mahjong404/LoomTable-Server/internal/domain"
)

func (s *Server) reorderViews(w http.ResponseWriter, r *http.Request, tableID string) {
	if r.Method != http.MethodPut {
		writeAPIError(w, r, 404, "NOT_FOUND", "resource not found")
		return
	}
	if s.catalog == nil {
		writeDomainError(w, r, domain.ErrDependencyMissing)
		return
	}
	if len(r.URL.Query()) != 0 {
		writeAPIError(w, r, 400, "BAD_REQUEST", "View Order does not accept query parameters")
		return
	}
	var wire struct {
		ExpectedOrderRevision *int64    `json:"expectedOrderRevision"`
		ViewIDs               *[]string `json:"viewIds"`
	}
	if err := decodeJSONRequest(r, &wire); err != nil {
		writeDecodeError(w, r, err)
		return
	}
	if wire.ExpectedOrderRevision == nil || wire.ViewIDs == nil {
		writeDomainError(w, r, domain.NewValidationError(domain.ValidationIssue{Path: "", Code: "required", Message: "expectedOrderRevision and viewIds are required and cannot be null"}))
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		writeAPIError(w, r, 400, "BAD_REQUEST", "Idempotency-Key is required")
		return
	}
	result, err := s.catalog.ReorderViews(r.Context(), actorIDFrom(r), key, tableID,
		catalog.ViewOrderInput{ExpectedOrderRevision: *wire.ExpectedOrderRevision, ViewIDs: *wire.ViewIDs})
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
