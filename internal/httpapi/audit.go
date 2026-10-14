package httpapi

import (
	"context"
	"github.com/Mahjong404/LoomTable-Server/internal/domain"
	record "github.com/Mahjong404/LoomTable-Server/internal/record"
	"net/http"
	"strconv"
	"time"
)

func (s *Server) audit(w http.ResponseWriter, r *http.Request, tableID string) {
	if r.Method != http.MethodGet {
		writeAPIError(w, r, http.StatusNotFound, "NOT_FOUND", "resource not found")
		return
	}
	if !s.requireReady(w, r) {
		return
	}
	reader, ok := s.records.(interface {
		Audit(context.Context, string, string, record.AuditRequest) (record.AuditPage, error)
	})
	if !ok {
		writeDomainError(w, r, domain.ErrDependencyMissing)
		return
	}
	q := r.URL.Query()
	request := record.AuditRequest{AuditFilter: record.AuditFilter{Category: q.Get("category"), RecordID: q.Get("recordId"), FieldID: q.Get("fieldId"), ViewID: q.Get("viewId"), ActorID: q.Get("actorId"), OperationID: q.Get("operationId"), ClientMutationID: q.Get("clientMutationId")}, Cursor: q.Get("cursor")}
	allowed := map[string]bool{"category": true, "recordId": true, "fieldId": true, "viewId": true, "actorId": true, "operationId": true, "clientMutationId": true, "since": true, "until": true, "cursor": true, "limit": true}
	for key, values := range q {
		if !allowed[key] || len(values) != 1 || values[0] == "" {
			writeDomainError(w, r, &domain.BadRequestError{Message: "invalid Audit query parameter"})
			return
		}
	}
	if raw := q.Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value == 0 {
			writeDomainError(w, r, &domain.BadRequestError{Message: "limit must be from 1 to 500"})
			return
		}
		request.Limit = value
	}
	for _, part := range []struct {
		name   string
		target **time.Time
	}{{"since", &request.Since}, {"until", &request.Until}} {
		if raw := q.Get(part.name); raw != "" {
			value, err := time.Parse(time.RFC3339Nano, raw)
			if err != nil {
				writeDomainError(w, r, &domain.BadRequestError{Message: "invalid Audit time filter"})
				return
			}
			*part.target = &value
		}
	}
	result, err := reader.Audit(r.Context(), actorIDFrom(r), tableID, request)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
