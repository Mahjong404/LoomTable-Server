package httpapi

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/Mahjong404/LoomTable-Server/internal/domain"
	record "github.com/Mahjong404/LoomTable-Server/internal/record"
)

type selections interface {
	AcquireSelection(context.Context, string, string, string, record.SelectionQuery) (record.SelectionResult, error)
	ResolveSelection(context.Context, string, string, string, record.SelectionQuery, []string, []string) (record.SelectionResult, error)
}

func decodeSelectionQuery(raw json.RawMessage) (record.SelectionQuery, error) {
	var wire struct {
		ViewID json.RawMessage `json:"viewId"`
		Filter json.RawMessage `json:"filter"`
		Search json.RawMessage `json:"search"`
	}
	if len(raw) == 0 || string(raw) == "null" {
		return record.SelectionQuery{}, validationDecodeError(domain.NewValidationError(domain.ValidationIssue{Path: "/query", Code: "required", Message: "query object is required"}))
	}
	if err := decodeStrictJSONBytes(raw, &wire); err != nil {
		return record.SelectionQuery{}, prefixDecodeError(err, "/query")
	}
	query := record.SelectionQuery{}
	var err error
	query.ViewID, _, err = decodeOptionalJSONString(wire.ViewID, "/query/viewId")
	if err != nil {
		return query, err
	}
	query.Search, _, err = decodeOptionalJSONString(wire.Search, "/query/search")
	if err != nil {
		return query, err
	}
	if wire.Filter != nil {
		query.Filter, err = decodeFilterNode(wire.Filter, "/query/filter")
		query.FilterPresent = true
	}
	return query, err
}
func decodeSelectionIDs(raw json.RawMessage, path string) ([]string, error) {
	if raw == nil {
		return nil, nil
	}
	if len(raw) == 0 || raw[0] != '[' {
		return nil, validationDecodeError(domain.NewValidationError(domain.ValidationIssue{Path: path, Code: "type", Message: "array of Record IDs required"}))
	}
	var ids []string
	err := decodeStrictJSONBytes(raw, &ids)
	if err != nil {
		return nil, prefixDecodeError(err, path)
	}
	return ids, nil
}
func (s *Server) selectionScope(w http.ResponseWriter, r *http.Request, table string, resolve bool) {
	if r.Method != http.MethodPost {
		writeAPIError(w, r, 404, "NOT_FOUND", "resource not found")
		return
	}
	service, ok := s.records.(selections)
	if !ok {
		writeDomainError(w, r, domain.ErrDependencyMissing)
		return
	}
	if !resolve {
		var wire struct {
			RequestID *string         `json:"requestId"`
			Query     json.RawMessage `json:"query"`
		}
		if err := decodeJSONRequest(r, &wire); err != nil {
			writeDecodeError(w, r, err)
			return
		}
		if wire.RequestID == nil {
			writeDomainError(w, r, requiredValidation("/requestId", "requestId is required"))
			return
		}
		query, err := decodeSelectionQuery(wire.Query)
		if err != nil {
			writeDecodeError(w, r, err)
			return
		}
		result, err := service.AcquireSelection(r.Context(), actorIDFrom(r), table, *wire.RequestID, query)
		if err != nil {
			writeDomainError(w, r, err)
			return
		}
		writeJSON(w, 200, result)
		return
	}
	var wire struct {
		ScopeToken  *string         `json:"scopeToken"`
		Query       json.RawMessage `json:"query"`
		ExcludedIDs json.RawMessage `json:"excludedIds"`
		RecordIDs   json.RawMessage `json:"recordIds"`
	}
	if err := decodeJSONRequest(r, &wire); err != nil {
		writeDecodeError(w, r, err)
		return
	}
	if wire.ScopeToken == nil {
		writeDomainError(w, r, requiredValidation("/scopeToken", "scopeToken is required"))
		return
	}
	query, err := decodeSelectionQuery(wire.Query)
	if err != nil {
		writeDecodeError(w, r, err)
		return
	}
	excluded, err := decodeSelectionIDs(wire.ExcludedIDs, "/excludedIds")
	if err != nil {
		writeDecodeError(w, r, err)
		return
	}
	probes, err := decodeSelectionIDs(wire.RecordIDs, "/recordIds")
	if err != nil {
		writeDecodeError(w, r, err)
		return
	}
	result, err := service.ResolveSelection(r.Context(), actorIDFrom(r), table, *wire.ScopeToken, query, excluded, probes)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	writeJSON(w, 200, result)
}
