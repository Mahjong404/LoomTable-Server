package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/Mahjong404/LoomTable-Server/internal/domain"
	loomrecord "github.com/Mahjong404/LoomTable-Server/internal/record"
)

type mutationRequest struct {
	ClientMutationID *string            `json:"clientMutationId"`
	Commands         *[]json.RawMessage `json:"commands"`
}

type recordQueryRequest struct {
	ViewID     json.RawMessage `json:"viewId"`
	Lifecycle  json.RawMessage `json:"lifecycle"`
	Cursor     json.RawMessage `json:"cursor"`
	Limit      json.RawMessage `json:"limit"`
	Projection json.RawMessage `json:"projection"`
	Filter     json.RawMessage `json:"filter"`
	Sort       json.RawMessage `json:"sort"`
	Search     json.RawMessage `json:"search"`
}

type mapQueryRequest struct {
	Viewport *struct {
		Boxes *[]struct {
			West  *float64 `json:"west"`
			South *float64 `json:"south"`
			East  *float64 `json:"east"`
			North *float64 `json:"north"`
		} `json:"boxes"`
	} `json:"viewport"`
	Zoom        *float64 `json:"zoom"`
	PixelWidth  *int     `json:"pixelWidth"`
	PixelHeight *int     `json:"pixelHeight"`
}

type mapClusterRecordsRequest struct {
	ClusterToken *string `json:"clusterToken"`
	Cursor       *string `json:"cursor"`
	Limit        *int    `json:"limit"`
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

func (s *Server) queryRecords(w http.ResponseWriter, r *http.Request, tableID string) {
	if s.records == nil {
		writeDomainError(w, r, domain.ErrDependencyMissing)
		return
	}
	if r.Method != http.MethodPost {
		writeAPIError(w, r, http.StatusNotFound, "NOT_FOUND", "resource not found")
		return
	}
	request, err := decodeRecordQueryRequest(r)
	if err != nil {
		writeDecodeError(w, r, err)
		return
	}
	result, err := s.records.Query(r.Context(), actorIDFrom(r), tableID, request)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) changes(w http.ResponseWriter, r *http.Request, tableID string) {
	if s.records == nil {
		writeDomainError(w, r, domain.ErrDependencyMissing)
		return
	}
	if r.Method != http.MethodGet {
		writeAPIError(w, r, http.StatusNotFound, "NOT_FOUND", "resource not found")
		return
	}
	query := r.URL.Query()
	if len(query) > 2 {
		writeAPIError(w, r, http.StatusBadRequest, "BAD_REQUEST", "unsupported query parameter")
		return
	}
	cursorToken := ""
	if values, present := query["cursor"]; present {
		if len(values) != 1 || values[0] == "" {
			writeAPIError(w, r, http.StatusBadRequest, "BAD_REQUEST", "cursor must appear once and cannot be empty")
			return
		}
		cursorToken = values[0]
	}
	limit := 0
	if values, present := query["limit"]; present {
		if len(values) != 1 {
			writeAPIError(w, r, http.StatusBadRequest, "BAD_REQUEST", "limit must appear once")
			return
		}
		parsed, err := strconv.Atoi(values[0])
		if err != nil || parsed < 1 || parsed > 500 {
			writeAPIError(w, r, http.StatusBadRequest, "BAD_REQUEST", "limit must be from 1 to 500")
			return
		}
		limit = parsed
	}
	for name := range query {
		if name != "cursor" && name != "limit" {
			writeAPIError(w, r, http.StatusBadRequest, "BAD_REQUEST", "unsupported query parameter")
			return
		}
	}
	page, err := s.records.Changes(r.Context(), actorIDFrom(r), tableID, cursorToken, limit)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (s *Server) queryMap(w http.ResponseWriter, r *http.Request, viewID string) {
	if s.records == nil {
		writeDomainError(w, r, domain.ErrDependencyMissing)
		return
	}
	if r.Method != http.MethodPost {
		writeAPIError(w, r, http.StatusNotFound, "NOT_FOUND", "resource not found")
		return
	}
	request, err := decodeMapQueryRequest(r)
	if err != nil {
		writeDecodeError(w, r, err)
		return
	}
	result, err := s.records.QueryMap(r.Context(), actorIDFrom(r), viewID, request)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) summarizeMap(w http.ResponseWriter, r *http.Request, viewID string) {
	if s.records == nil {
		writeDomainError(w, r, domain.ErrDependencyMissing)
		return
	}
	if r.Method != http.MethodPost {
		writeAPIError(w, r, http.StatusNotFound, "NOT_FOUND", "resource not found")
		return
	}
	if len(r.URL.Query()) != 0 || r.ContentLength != 0 {
		writeAPIError(w, r, http.StatusBadRequest, "BAD_REQUEST", "Map summary does not accept query parameters or a request body")
		return
	}
	result, err := s.records.SummarizeMap(r.Context(), actorIDFrom(r), viewID)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) queryMapClusterRecords(w http.ResponseWriter, r *http.Request, viewID string) {
	if s.records == nil {
		writeDomainError(w, r, domain.ErrDependencyMissing)
		return
	}
	if r.Method != http.MethodPost {
		writeAPIError(w, r, http.StatusNotFound, "NOT_FOUND", "resource not found")
		return
	}
	var wire mapClusterRecordsRequest
	if err := decodeJSONRequest(r, &wire); err != nil {
		writeDecodeError(w, r, err)
		return
	}
	if wire.ClusterToken == nil {
		writeDomainError(w, r, requiredValidation("/clusterToken", "clusterToken is required"))
		return
	}
	request := loomrecord.MapClusterRecordsRequest{ClusterToken: *wire.ClusterToken}
	if wire.Cursor != nil {
		request.Cursor = *wire.Cursor
	}
	if wire.Limit != nil {
		request.Limit = *wire.Limit
	}
	result, err := s.records.QueryMapClusterRecords(r.Context(), actorIDFrom(r), viewID, request)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func decodeMapQueryRequest(r *http.Request) (loomrecord.MapQueryRequest, error) {
	var wire mapQueryRequest
	if err := decodeJSONRequest(r, &wire); err != nil {
		return loomrecord.MapQueryRequest{}, err
	}
	issues := make([]domain.ValidationIssue, 0)
	if wire.Viewport == nil {
		issues = append(issues, requiredIssue("/viewport", "viewport is required"))
	} else if wire.Viewport.Boxes == nil {
		issues = append(issues, requiredIssue("/viewport/boxes", "boxes is required"))
	}
	if wire.Zoom == nil {
		issues = append(issues, requiredIssue("/zoom", "zoom is required"))
	}
	if wire.PixelWidth == nil {
		issues = append(issues, requiredIssue("/pixelWidth", "pixelWidth is required"))
	}
	if wire.PixelHeight == nil {
		issues = append(issues, requiredIssue("/pixelHeight", "pixelHeight is required"))
	}
	if len(issues) > 0 {
		return loomrecord.MapQueryRequest{}, validationDecodeError(domain.NewValidationError(issues...))
	}
	boxes := make([]loomrecord.MapViewportBox, 0, len(*wire.Viewport.Boxes))
	for index, box := range *wire.Viewport.Boxes {
		path := "/viewport/boxes/" + strconv.Itoa(index)
		boxIssues := make([]domain.ValidationIssue, 0, 4)
		if box.West == nil {
			boxIssues = append(boxIssues, requiredIssue(path+"/west", "west is required"))
		}
		if box.South == nil {
			boxIssues = append(boxIssues, requiredIssue(path+"/south", "south is required"))
		}
		if box.East == nil {
			boxIssues = append(boxIssues, requiredIssue(path+"/east", "east is required"))
		}
		if box.North == nil {
			boxIssues = append(boxIssues, requiredIssue(path+"/north", "north is required"))
		}
		issues = append(issues, boxIssues...)
		if len(boxIssues) == 0 {
			boxes = append(boxes, loomrecord.MapViewportBox{West: *box.West, South: *box.South, East: *box.East, North: *box.North})
		}
	}
	if len(issues) > 0 {
		return loomrecord.MapQueryRequest{}, validationDecodeError(domain.NewValidationError(issues...))
	}
	return loomrecord.MapQueryRequest{
		Viewport: loomrecord.MapViewport{Boxes: boxes}, Zoom: *wire.Zoom,
		PixelWidth: *wire.PixelWidth, PixelHeight: *wire.PixelHeight,
	}, nil
}

func decodeRecordQueryRequest(r *http.Request) (loomrecord.QueryRequest, error) {
	var wire recordQueryRequest
	if err := decodeJSONRequest(r, &wire); err != nil {
		return loomrecord.QueryRequest{}, err
	}
	request := loomrecord.QueryRequest{}
	if value, present, err := decodeOptionalJSONString(wire.ViewID, "/viewId"); err != nil {
		return loomrecord.QueryRequest{}, err
	} else if present {
		request.ViewID = value
		request.ViewIDPresent = true
	}
	if value, present, err := decodeOptionalJSONString(wire.Lifecycle, "/lifecycle"); err != nil {
		return loomrecord.QueryRequest{}, err
	} else if present {
		request.Lifecycle = value
	}
	if value, present, err := decodeOptionalJSONString(wire.Cursor, "/cursor"); err != nil {
		return loomrecord.QueryRequest{}, err
	} else if present {
		request.Cursor = value
	}
	if value, present, err := decodeOptionalJSONInt(wire.Limit, "/limit"); err != nil {
		return loomrecord.QueryRequest{}, err
	} else if present {
		request.Limit = value
	}
	if value, present, err := decodeOptionalJSONString(wire.Search, "/search"); err != nil {
		return loomrecord.QueryRequest{}, err
	} else if present {
		request.Search = value
		request.SearchPresent = true
	}
	if wire.Projection != nil {
		if !strings.HasPrefix(strings.TrimSpace(string(wire.Projection)), "[") {
			return loomrecord.QueryRequest{}, validationDecodeError(domain.NewValidationError(domain.ValidationIssue{Path: "/projection", Code: "type", Message: "projection must be an array"}))
		}
		if err := decodeStrictJSONBytes(wire.Projection, &request.Projection); err != nil {
			return loomrecord.QueryRequest{}, prefixDecodeError(err, "/projection")
		}
		request.ProjectionPresent = true
	}
	if wire.Filter != nil {
		filter, err := decodeFilterNode(wire.Filter, "/filter")
		if err != nil {
			return loomrecord.QueryRequest{}, err
		}
		request.Filter = filter
		request.FilterPresent = true
	}
	if wire.Sort != nil {
		if !strings.HasPrefix(strings.TrimSpace(string(wire.Sort)), "[") {
			return loomrecord.QueryRequest{}, validationDecodeError(domain.NewValidationError(domain.ValidationIssue{Path: "/sort", Code: "type", Message: "sort must be an array"}))
		}
		if err := decodeStrictJSONBytes(wire.Sort, &request.Sort); err != nil {
			return loomrecord.QueryRequest{}, prefixDecodeError(err, "/sort")
		}
		request.SortPresent = true
	}
	return request, nil
}

func decodeOptionalJSONString(raw json.RawMessage, path string) (string, bool, error) {
	if raw == nil {
		return "", false, nil
	}
	if string(raw) == "null" {
		return "", false, validationDecodeError(domain.NewValidationError(domain.ValidationIssue{Path: path, Code: "type", Message: "property must be a string"}))
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", false, validationDecodeError(domain.NewValidationError(domain.ValidationIssue{Path: path, Code: "type", Message: "property must be a string"}))
	}
	return value, true, nil
}

func decodeOptionalJSONInt(raw json.RawMessage, path string) (int, bool, error) {
	if raw == nil {
		return 0, false, nil
	}
	if string(raw) == "null" {
		return 0, false, validationDecodeError(domain.NewValidationError(domain.ValidationIssue{Path: path, Code: "type", Message: "property must be an integer"}))
	}
	var value int
	if err := json.Unmarshal(raw, &value); err != nil {
		return 0, false, validationDecodeError(domain.NewValidationError(domain.ValidationIssue{Path: path, Code: "type", Message: "property must be an integer"}))
	}
	return value, true, nil
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
