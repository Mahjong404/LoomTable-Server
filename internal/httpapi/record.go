package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/Mahjong404/LoomTable-Server/internal/domain"
	loomrecord "github.com/Mahjong404/LoomTable-Server/internal/record"
)

type mutationRequest struct {
	ClientMutationID *string            `json:"clientMutationId"`
	Commands         *[]json.RawMessage `json:"commands"`
}

func (s *Server) record(w http.ResponseWriter, r *http.Request) {
	if s.records == nil {
		writeDomainError(w, r, domain.ErrDependencyMissing)
		return
	}
	recordID, ok := singlePathID(r.URL.Path, "/v1/records/")
	if !ok || r.Method != http.MethodGet {
		writeAPIError(w, r, http.StatusNotFound, "NOT_FOUND", "resource not found")
		return
	}
	item, err := s.records.Get(r.Context(), actorIDFrom(r), recordID)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) mutateRecords(w http.ResponseWriter, r *http.Request, tableID string) {
	if s.records == nil {
		writeDomainError(w, r, domain.ErrDependencyMissing)
		return
	}
	if r.Method != http.MethodPost {
		writeAPIError(w, r, http.StatusNotFound, "NOT_FOUND", "resource not found")
		return
	}
	var request mutationRequest
	if err := decodeJSONRequest(r, &request); err != nil {
		writeDecodeError(w, r, err)
		return
	}
	issues := make([]domain.ValidationIssue, 0, 2)
	if request.ClientMutationID == nil {
		issues = append(issues, domain.ValidationIssue{Path: "/clientMutationId", Code: "required", Message: "clientMutationId is required"})
	}
	if request.Commands == nil {
		issues = append(issues, domain.ValidationIssue{Path: "/commands", Code: "required", Message: "commands is required"})
	}
	if len(issues) > 0 {
		writeDomainError(w, r, domain.NewValidationError(issues...))
		return
	}
	commands := make([]loomrecord.Command, 0, len(*request.Commands))
	for index, raw := range *request.Commands {
		command, err := decodeMutationCommand(raw)
		if err != nil {
			writeDecodeError(w, r, prefixCommandDecodeError(err, index))
			return
		}
		commands = append(commands, command)
	}
	result, err := s.records.Mutate(r.Context(), actorIDFrom(r), tableID, *request.ClientMutationID, commands)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func decodeMutationCommand(raw json.RawMessage) (loomrecord.Command, error) {
	var discriminant struct {
		Kind *string `json:"kind"`
	}
	if err := json.Unmarshal(raw, &discriminant); err != nil {
		return loomrecord.Command{}, &requestDecodeError{Status: http.StatusBadRequest, Code: "BAD_REQUEST", Message: "mutation command must be a JSON object"}
	}
	if discriminant.Kind == nil {
		validation := domain.NewValidationError(domain.ValidationIssue{Path: "/kind", Code: "required", Message: "kind is required"})
		return loomrecord.Command{}, validationDecodeError(validation)
	}

	switch *discriminant.Kind {
	case "createRecord":
		var input struct {
			Kind   string          `json:"kind"`
			Values json.RawMessage `json:"values"`
		}
		if err := decodeStrictJSONBytes(raw, &input); err != nil {
			return loomrecord.Command{}, err
		}
		command := loomrecord.Command{Kind: input.Kind, ValuesPresent: input.Values != nil}
		if input.Values != nil {
			if err := json.Unmarshal(input.Values, &command.Values); err != nil || command.Values == nil {
				validation := domain.NewValidationError(domain.ValidationIssue{Path: "/values", Code: "type", Message: "values must be an object"})
				return loomrecord.Command{}, validationDecodeError(validation)
			}
		}
		return command, nil
	case "updateRecord":
		var input struct {
			Kind             string          `json:"kind"`
			RecordID         *string         `json:"recordId"`
			ExpectedRevision *int64          `json:"expectedRevision"`
			Set              json.RawMessage `json:"set"`
			UnsetFieldIDs    *[]string       `json:"unsetFieldIds"`
		}
		if err := decodeStrictJSONBytes(raw, &input); err != nil {
			return loomrecord.Command{}, err
		}
		command := loomrecord.Command{
			Kind:               input.Kind,
			SetPresent:         input.Set != nil,
			UnsetFieldsPresent: input.UnsetFieldIDs != nil,
		}
		if input.RecordID != nil {
			command.RecordID = *input.RecordID
		}
		if input.ExpectedRevision != nil {
			command.ExpectedRevision = *input.ExpectedRevision
		}
		if input.Set != nil {
			if err := json.Unmarshal(input.Set, &command.Set); err != nil || command.Set == nil {
				validation := domain.NewValidationError(domain.ValidationIssue{Path: "/set", Code: "type", Message: "set must be an object"})
				return loomrecord.Command{}, validationDecodeError(validation)
			}
		}
		if input.UnsetFieldIDs != nil {
			command.UnsetFieldIDs = *input.UnsetFieldIDs
		}
		return command, nil
	case "deleteRecord", "restoreRecord":
		var input struct {
			Kind             string  `json:"kind"`
			RecordID         *string `json:"recordId"`
			ExpectedRevision *int64  `json:"expectedRevision"`
		}
		if err := decodeStrictJSONBytes(raw, &input); err != nil {
			return loomrecord.Command{}, err
		}
		command := loomrecord.Command{Kind: input.Kind}
		if input.RecordID != nil {
			command.RecordID = *input.RecordID
		}
		if input.ExpectedRevision != nil {
			command.ExpectedRevision = *input.ExpectedRevision
		}
		return command, nil
	default:
		validation := domain.NewValidationError(domain.ValidationIssue{Path: "/kind", Code: "format", Message: "unsupported mutation command"})
		return loomrecord.Command{}, validationDecodeError(validation)
	}
}

func validationDecodeError(validation *domain.ValidationError) *requestDecodeError {
	return &requestDecodeError{
		Status:  http.StatusUnprocessableEntity,
		Code:    "VALIDATION_ERROR",
		Message: validation.Error(),
		Details: map[string]any{"issues": validation.Issues},
	}
}

func prefixCommandDecodeError(err error, index int) error {
	var decodeError *requestDecodeError
	if !errors.As(err, &decodeError) {
		return err
	}
	copyError := *decodeError
	details, ok := decodeError.Details.(map[string]any)
	if !ok {
		return &copyError
	}
	issues, ok := details["issues"].([]domain.ValidationIssue)
	if !ok {
		return &copyError
	}
	prefixed := make([]domain.ValidationIssue, len(issues))
	for issueIndex, issue := range issues {
		issue.Path = fmt.Sprintf("/commands/%d%s", index, issue.Path)
		prefixed[issueIndex] = issue
	}
	copyError.Details = map[string]any{"issues": prefixed}
	return &copyError
}

func writeRecordConflict(w http.ResponseWriter, r *http.Request, conflict *loomrecord.ConflictError) {
	writeJSON(w, http.StatusConflict, map[string]any{
		"error": map[string]any{
			"code":               "CONFLICT",
			"message":            conflict.Error(),
			"requestId":          requestIDFrom(r),
			"clientMutationId":   conflict.ClientMutationID,
			"failedCommandIndex": conflict.FailedCommandIndex,
			"conflicts":          conflict.Conflicts,
		},
	})
}

func isRecordMutationPath(path string) bool {
	return strings.HasSuffix(path, "/records/mutate")
}
