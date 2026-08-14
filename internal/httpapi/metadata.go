package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/Mahjong404/LoomTable-Server/internal/catalog"
	"github.com/Mahjong404/LoomTable-Server/internal/domain"
)

type createFieldRequest struct {
	Name   *string         `json:"name"`
	Type   *string         `json:"type"`
	Config json.RawMessage `json:"config"`
}

type updateFieldRequest struct {
	Name             *string         `json:"name"`
	Type             *string         `json:"type"`
	Config           json.RawMessage `json:"config"`
	ExpectedRevision *int64          `json:"expectedRevision"`
}

type selectFieldConfigRequest struct {
	Options *[]selectOptionRequest `json:"options"`
}

type selectOptionRequest struct {
	ID    json.RawMessage `json:"id"`
	Name  *string         `json:"name"`
	Color *string         `json:"color"`
}

type createViewRequest struct {
	Name   *string         `json:"name"`
	Type   *string         `json:"type"`
	Config json.RawMessage `json:"config"`
}

type updateViewRequest struct {
	Name             *string         `json:"name"`
	Type             *string         `json:"type"`
	Config           json.RawMessage `json:"config"`
	ExpectedRevision *int64          `json:"expectedRevision"`
}

func (s *Server) fields(w http.ResponseWriter, r *http.Request, tableID string) {
	if s.catalog == nil {
		writeDomainError(w, r, domain.ErrDependencyMissing)
		return
	}
	switch r.Method {
	case http.MethodGet:
		lifecycle, ok := lifecycleQuery(r)
		if !ok {
			writeAPIError(w, r, http.StatusBadRequest, "BAD_REQUEST", "only one lifecycle query parameter is accepted")
			return
		}
		items, err := s.catalog.ListFields(r.Context(), actorIDFrom(r), tableID, lifecycle)
		if err != nil {
			writeDomainError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	case http.MethodPost:
		input, err := decodeCreateFieldRequest(r)
		if err != nil {
			writeDecodeError(w, r, err)
			return
		}
		key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		if key == "" {
			writeAPIError(w, r, http.StatusBadRequest, "BAD_REQUEST", "Idempotency-Key is required")
			return
		}
		item, err := s.catalog.CreateField(r.Context(), actorIDFrom(r), key, tableID, input)
		if err != nil {
			writeDomainError(w, r, err)
			return
		}
		writeJSON(w, http.StatusCreated, item)
	default:
		writeAPIError(w, r, http.StatusNotFound, "NOT_FOUND", "resource not found")
	}
}

func (s *Server) field(w http.ResponseWriter, r *http.Request) {
	if s.catalog == nil {
		writeDomainError(w, r, domain.ErrDependencyMissing)
		return
	}
	trimmed := strings.TrimPrefix(r.URL.Path, "/v1/fields/")
	if strings.HasSuffix(trimmed, "/restore") {
		fieldID := strings.TrimSuffix(trimmed, "/restore")
		if fieldID == "" || strings.Contains(fieldID, "/") || r.Method != http.MethodPost {
			writeAPIError(w, r, http.StatusNotFound, "NOT_FOUND", "resource not found")
			return
		}
		var request restoreMetadataRequest
		if err := decodeJSONRequest(r, &request); err != nil {
			writeDecodeError(w, r, err)
			return
		}
		if request.ExpectedRevision == nil {
			writeDomainError(w, r, requiredValidation("/expectedRevision", "expectedRevision is required"))
			return
		}
		item, err := s.catalog.RestoreField(r.Context(), actorIDFrom(r), fieldID, *request.ExpectedRevision)
		if err != nil {
			writeDomainError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, item)
		return
	}
	fieldID, ok := singlePathID(r.URL.Path, "/v1/fields/")
	if !ok {
		writeAPIError(w, r, http.StatusNotFound, "NOT_FOUND", "resource not found")
		return
	}
	switch r.Method {
	case http.MethodPatch:
		update, err := decodeUpdateFieldRequest(r)
		if err != nil {
			writeDecodeError(w, r, err)
			return
		}
		item, err := s.catalog.UpdateField(r.Context(), actorIDFrom(r), fieldID, update)
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
		if err := s.catalog.DeleteField(r.Context(), actorIDFrom(r), fieldID, expectedRevision); err != nil {
			writeDomainError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeAPIError(w, r, http.StatusNotFound, "NOT_FOUND", "resource not found")
	}
}

func (s *Server) views(w http.ResponseWriter, r *http.Request, tableID string) {
	if s.catalog == nil {
		writeDomainError(w, r, domain.ErrDependencyMissing)
		return
	}
	switch r.Method {
	case http.MethodGet:
		lifecycle, ok := lifecycleQuery(r)
		if !ok {
			writeAPIError(w, r, http.StatusBadRequest, "BAD_REQUEST", "only one lifecycle query parameter is accepted")
			return
		}
		items, err := s.catalog.ListViews(r.Context(), actorIDFrom(r), tableID, lifecycle)
		if err != nil {
			writeDomainError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	case http.MethodPost:
		input, err := decodeCreateViewRequest(r)
		if err != nil {
			writeDecodeError(w, r, err)
			return
		}
		key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		if key == "" {
			writeAPIError(w, r, http.StatusBadRequest, "BAD_REQUEST", "Idempotency-Key is required")
			return
		}
		item, err := s.catalog.CreateView(r.Context(), actorIDFrom(r), key, tableID, input)
		if err != nil {
			writeDomainError(w, r, err)
			return
		}
		writeJSON(w, http.StatusCreated, item)
	default:
		writeAPIError(w, r, http.StatusNotFound, "NOT_FOUND", "resource not found")
	}
}

func (s *Server) view(w http.ResponseWriter, r *http.Request) {
	if s.catalog == nil {
		writeDomainError(w, r, domain.ErrDependencyMissing)
		return
	}
	trimmed := strings.TrimPrefix(r.URL.Path, "/v1/views/")
	for suffix, handler := range map[string]func(http.ResponseWriter, *http.Request, string){
		"/map/query":                 s.queryMap,
		"/map/summary":               s.summarizeMap,
		"/map/cluster-records/query": s.queryMapClusterRecords,
	} {
		if strings.HasSuffix(trimmed, suffix) {
			viewID := strings.TrimSuffix(trimmed, suffix)
			if viewID == "" || strings.Contains(viewID, "/") {
				writeAPIError(w, r, http.StatusNotFound, "NOT_FOUND", "resource not found")
				return
			}
			handler(w, r, viewID)
			return
		}
	}
	if strings.HasSuffix(trimmed, "/restore") {
		viewID := strings.TrimSuffix(trimmed, "/restore")
		if viewID == "" || strings.Contains(viewID, "/") || r.Method != http.MethodPost {
			writeAPIError(w, r, http.StatusNotFound, "NOT_FOUND", "resource not found")
			return
		}
		var request restoreMetadataRequest
		if err := decodeJSONRequest(r, &request); err != nil {
			writeDecodeError(w, r, err)
			return
		}
		if request.ExpectedRevision == nil {
			writeDomainError(w, r, requiredValidation("/expectedRevision", "expectedRevision is required"))
			return
		}
		item, err := s.catalog.RestoreView(r.Context(), actorIDFrom(r), viewID, *request.ExpectedRevision)
		if err != nil {
			writeDomainError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, item)
		return
	}
	viewID, ok := singlePathID(r.URL.Path, "/v1/views/")
	if !ok {
		writeAPIError(w, r, http.StatusNotFound, "NOT_FOUND", "resource not found")
		return
	}
	switch r.Method {
	case http.MethodGet:
		item, err := s.catalog.GetView(r.Context(), actorIDFrom(r), viewID)
		if err != nil {
			writeDomainError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, item)
	case http.MethodPatch:
		update, err := decodeUpdateViewRequest(r)
		if err != nil {
			writeDecodeError(w, r, err)
			return
		}
		item, err := s.catalog.UpdateView(r.Context(), actorIDFrom(r), viewID, update)
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
		if err := s.catalog.DeleteView(r.Context(), actorIDFrom(r), viewID, expectedRevision); err != nil {
			writeDomainError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeAPIError(w, r, http.StatusNotFound, "NOT_FOUND", "resource not found")
	}
}

func decodeCreateFieldRequest(r *http.Request) (catalog.FieldInput, error) {
	var request createFieldRequest
	if err := decodeJSONRequest(r, &request); err != nil {
		return catalog.FieldInput{}, err
	}
	issues := make([]domain.ValidationIssue, 0, 3)
	if request.Name == nil {
		issues = append(issues, requiredIssue("/name", "name is required"))
	}
	if request.Type == nil {
		issues = append(issues, requiredIssue("/type", "type is required"))
	}
	if request.Config == nil {
		issues = append(issues, requiredIssue("/config", "config is required"))
	}
	if len(issues) > 0 {
		return catalog.FieldInput{}, validationDecodeError(domain.NewValidationError(issues...))
	}
	config, err := decodeFieldConfig(*request.Type, request.Config)
	if err != nil {
		return catalog.FieldInput{}, err
	}
	return catalog.FieldInput{Name: *request.Name, Type: *request.Type, Config: config}, nil
}

func decodeUpdateFieldRequest(r *http.Request) (catalog.FieldUpdate, error) {
	var request updateFieldRequest
	if err := decodeJSONRequest(r, &request); err != nil {
		return catalog.FieldUpdate{}, err
	}
	issues := make([]domain.ValidationIssue, 0, 3)
	if request.Type == nil {
		issues = append(issues, requiredIssue("/type", "type is required"))
	}
	if request.ExpectedRevision == nil {
		issues = append(issues, requiredIssue("/expectedRevision", "expectedRevision is required"))
	}
	if request.Name == nil && request.Config == nil {
		issues = append(issues, requiredIssue("", "name or config is required"))
	}
	if len(issues) > 0 {
		return catalog.FieldUpdate{}, validationDecodeError(domain.NewValidationError(issues...))
	}
	var config any
	var err error
	if request.Config != nil {
		config, err = decodeFieldConfig(*request.Type, request.Config)
		if err != nil {
			return catalog.FieldUpdate{}, err
		}
	}
	return catalog.FieldUpdate{Name: request.Name, Type: *request.Type, Config: config, ExpectedRevision: *request.ExpectedRevision}, nil
}

func decodeFieldConfig(fieldType string, raw json.RawMessage) (any, error) {
	if !isJSONObject(raw) {
		return nil, validationDecodeError(domain.NewValidationError(domain.ValidationIssue{Path: "/config", Code: "type", Message: "config must be an object"}))
	}
	switch fieldType {
	case "text", "longText", "number", "checkbox", "date", "url", "location":
		var config domain.EmptyFieldConfig
		if err := decodeStrictJSONBytes(raw, &config); err != nil {
			return nil, prefixDecodeError(err, "/config")
		}
		return config, nil
	case "attachment":
		var config domain.AttachmentFieldConfig
		if err := decodeStrictJSONBytes(raw, &config); err != nil {
			return nil, prefixDecodeError(err, "/config")
		}
		return config, nil
	case "select", "multiSelect":
		var request selectFieldConfigRequest
		if err := decodeStrictJSONBytes(raw, &request); err != nil {
			return nil, prefixDecodeError(err, "/config")
		}
		if request.Options == nil {
			return nil, validationDecodeError(domain.NewValidationError(requiredIssue("/config/options", "options is required")))
		}
		options := make([]catalog.SelectOptionInput, 0, len(*request.Options))
		issues := make([]domain.ValidationIssue, 0)
		for index, option := range *request.Options {
			path := "/config/options/" + strconv.Itoa(index)
			var optionID *string
			if option.ID != nil {
				if string(option.ID) == "null" {
					issues = append(issues, domain.ValidationIssue{Path: path + "/id", Code: "type", Message: "id must be a string"})
				} else {
					var decoded string
					if err := json.Unmarshal(option.ID, &decoded); err != nil {
						issues = append(issues, domain.ValidationIssue{Path: path + "/id", Code: "type", Message: "id must be a string"})
					} else {
						optionID = &decoded
					}
				}
			}
			if option.Name == nil {
				issues = append(issues, requiredIssue(path+"/name", "name is required"))
			}
			if option.Color == nil {
				issues = append(issues, requiredIssue(path+"/color", "color is required"))
			}
			if option.Name != nil && option.Color != nil {
				options = append(options, catalog.SelectOptionInput{ID: optionID, Name: *option.Name, Color: *option.Color})
			}
		}
		if len(issues) > 0 {
			return nil, validationDecodeError(domain.NewValidationError(issues...))
		}
		return catalog.SelectFieldConfigInput{Options: options}, nil
	default:
		return nil, validationDecodeError(domain.NewValidationError(domain.ValidationIssue{Path: "/type", Code: "format", Message: "unsupported Field type"}))
	}
}

func decodeCreateViewRequest(r *http.Request) (catalog.ViewInput, error) {
	var request createViewRequest
	if err := decodeJSONRequest(r, &request); err != nil {
		return catalog.ViewInput{}, err
	}
	issues := make([]domain.ValidationIssue, 0, 3)
	if request.Name == nil {
		issues = append(issues, requiredIssue("/name", "name is required"))
	}
	if request.Type == nil {
		issues = append(issues, requiredIssue("/type", "type is required"))
	}
	if request.Config == nil {
		issues = append(issues, requiredIssue("/config", "config is required"))
	}
	if len(issues) > 0 {
		return catalog.ViewInput{}, validationDecodeError(domain.NewValidationError(issues...))
	}
	config, err := decodeViewConfig(*request.Type, request.Config)
	if err != nil {
		return catalog.ViewInput{}, err
	}
	return catalog.ViewInput{Name: *request.Name, Type: *request.Type, Config: config}, nil
}

func decodeUpdateViewRequest(r *http.Request) (catalog.ViewUpdate, error) {
	var request updateViewRequest
	if err := decodeJSONRequest(r, &request); err != nil {
		return catalog.ViewUpdate{}, err
	}
	issues := make([]domain.ValidationIssue, 0, 3)
	if request.Type == nil {
		issues = append(issues, requiredIssue("/type", "type is required"))
	}
	if request.Config == nil {
		issues = append(issues, requiredIssue("/config", "config is required"))
	}
	if request.ExpectedRevision == nil {
		issues = append(issues, requiredIssue("/expectedRevision", "expectedRevision is required"))
	}
	if len(issues) > 0 {
		return catalog.ViewUpdate{}, validationDecodeError(domain.NewValidationError(issues...))
	}
	config, err := decodeViewConfig(*request.Type, request.Config)
	if err != nil {
		return catalog.ViewUpdate{}, err
	}
	return catalog.ViewUpdate{Name: request.Name, Type: *request.Type, Config: config, ExpectedRevision: *request.ExpectedRevision}, nil
}

func decodeViewConfig(viewType string, raw json.RawMessage) (any, error) {
	if !isJSONObject(raw) {
		return nil, validationDecodeError(domain.NewValidationError(domain.ValidationIssue{Path: "/config", Code: "type", Message: "config must be an object"}))
	}
	switch viewType {
	case "grid":
		var request struct {
			Projection     *[]string          `json:"projection"`
			ColumnOrder    *[]string          `json:"columnOrder"`
			ColumnWidths   *map[string]int    `json:"columnWidths"`
			FrozenFieldIDs *[]string          `json:"frozenFieldIds"`
			RowHeight      *string            `json:"rowHeight"`
			Filter         json.RawMessage    `json:"filter"`
			Sort           *[]domain.SortSpec `json:"sort"`
		}
		if err := decodeStrictJSONBytes(raw, &request); err != nil {
			return nil, prefixDecodeError(err, "/config")
		}
		issues := make([]domain.ValidationIssue, 0, 6)
		if request.Projection == nil {
			issues = append(issues, requiredIssue("/config/projection", "projection is required"))
		}
		if request.ColumnOrder == nil {
			issues = append(issues, requiredIssue("/config/columnOrder", "columnOrder is required"))
		}
		if request.ColumnWidths == nil {
			issues = append(issues, requiredIssue("/config/columnWidths", "columnWidths is required"))
		}
		if request.FrozenFieldIDs == nil {
			issues = append(issues, requiredIssue("/config/frozenFieldIds", "frozenFieldIds is required"))
		}
		if request.RowHeight == nil {
			issues = append(issues, requiredIssue("/config/rowHeight", "rowHeight is required"))
		}
		if request.Sort == nil {
			issues = append(issues, requiredIssue("/config/sort", "sort is required"))
		}
		if len(issues) > 0 {
			return nil, validationDecodeError(domain.NewValidationError(issues...))
		}
		var filter *domain.FilterNode
		if request.Filter != nil {
			decoded, decodeErr := decodeFilterNode(request.Filter, "/config/filter")
			if decodeErr != nil {
				return nil, decodeErr
			}
			filter = decoded
		}
		return domain.GridViewConfig{Projection: *request.Projection, ColumnOrder: *request.ColumnOrder, ColumnWidths: *request.ColumnWidths, FrozenFieldIDs: *request.FrozenFieldIDs, RowHeight: *request.RowHeight, Filter: filter, Sort: *request.Sort}, nil
	case "map":
		var request struct {
			LocationFieldID *string         `json:"locationFieldId"`
			Filter          json.RawMessage `json:"filter"`
			Center          json.RawMessage `json:"center"`
			Zoom            json.RawMessage `json:"zoom"`
		}
		if err := decodeStrictJSONBytes(raw, &request); err != nil {
			return nil, prefixDecodeError(err, "/config")
		}
		issues := make([]domain.ValidationIssue, 0, 3)
		if request.LocationFieldID == nil {
			issues = append(issues, requiredIssue("/config/locationFieldId", "locationFieldId is required"))
		}
		var center *domain.MapCenter
		if request.Center != nil {
			if !isJSONObject(request.Center) {
				issues = append(issues, domain.ValidationIssue{Path: "/config/center", Code: "type", Message: "center must be an object"})
			} else {
				var wireCenter struct {
					Lat *float64 `json:"lat"`
					Lng *float64 `json:"lng"`
				}
				if err := decodeStrictJSONBytes(request.Center, &wireCenter); err != nil {
					return nil, prefixDecodeError(err, "/config/center")
				}
				if wireCenter.Lat == nil {
					issues = append(issues, requiredIssue("/config/center/lat", "lat is required"))
				}
				if wireCenter.Lng == nil {
					issues = append(issues, requiredIssue("/config/center/lng", "lng is required"))
				}
				if wireCenter.Lat != nil && wireCenter.Lng != nil {
					center = &domain.MapCenter{Lat: *wireCenter.Lat, Lng: *wireCenter.Lng}
				}
			}
		}
		var zoom *float64
		if request.Zoom != nil {
			var decoded float64
			if string(request.Zoom) == "null" || json.Unmarshal(request.Zoom, &decoded) != nil {
				issues = append(issues, domain.ValidationIssue{Path: "/config/zoom", Code: "type", Message: "zoom must be a number"})
			} else {
				zoom = &decoded
			}
		}
		if len(issues) > 0 {
			return nil, validationDecodeError(domain.NewValidationError(issues...))
		}
		var filter *domain.FilterNode
		if request.Filter != nil {
			decoded, decodeErr := decodeFilterNode(request.Filter, "/config/filter")
			if decodeErr != nil {
				return nil, decodeErr
			}
			filter = decoded
		}
		config := domain.MapViewConfig{LocationFieldID: *request.LocationFieldID, Filter: filter, Center: center, Zoom: zoom}
		return config, nil
	default:
		return nil, validationDecodeError(domain.NewValidationError(domain.ValidationIssue{Path: "/type", Code: "format", Message: "unsupported View type"}))
	}
}

func decodeFilterNode(raw json.RawMessage, path string) (*domain.FilterNode, error) {
	if !isJSONObject(raw) {
		return nil, validationDecodeError(domain.NewValidationError(domain.ValidationIssue{Path: path, Code: "type", Message: "Filter node must be an object"}))
	}
	var discriminant struct {
		Kind *string `json:"kind"`
	}
	if err := json.Unmarshal(raw, &discriminant); err != nil {
		return nil, &requestDecodeError{Status: http.StatusBadRequest, Code: "BAD_REQUEST", Message: "Filter node must be a JSON object"}
	}
	if discriminant.Kind == nil {
		return nil, validationDecodeError(domain.NewValidationError(requiredIssue(path+"/kind", "kind is required")))
	}
	switch *discriminant.Kind {
	case "group":
		var wire struct {
			Kind     string             `json:"kind"`
			Operator *string            `json:"operator"`
			Children *[]json.RawMessage `json:"children"`
		}
		if err := decodeStrictJSONBytes(raw, &wire); err != nil {
			return nil, prefixDecodeError(err, path)
		}
		issues := make([]domain.ValidationIssue, 0, 2)
		if wire.Operator == nil {
			issues = append(issues, requiredIssue(path+"/operator", "operator is required"))
		}
		if wire.Children == nil {
			issues = append(issues, requiredIssue(path+"/children", "children is required"))
		}
		if len(issues) > 0 {
			return nil, validationDecodeError(domain.NewValidationError(issues...))
		}
		children := make([]domain.FilterNode, 0, len(*wire.Children))
		for index, childRaw := range *wire.Children {
			child, err := decodeFilterNode(childRaw, path+"/children/"+strconv.Itoa(index))
			if err != nil {
				return nil, err
			}
			children = append(children, *child)
		}
		return &domain.FilterNode{Kind: wire.Kind, Operator: *wire.Operator, Children: children}, nil
	case "rule":
		var wire struct {
			Kind     string          `json:"kind"`
			FieldID  *string         `json:"fieldId"`
			Operator *string         `json:"operator"`
			Value    json.RawMessage `json:"value"`
		}
		if err := decodeStrictJSONBytes(raw, &wire); err != nil {
			return nil, prefixDecodeError(err, path)
		}
		issues := make([]domain.ValidationIssue, 0, 2)
		if wire.FieldID == nil {
			issues = append(issues, requiredIssue(path+"/fieldId", "fieldId is required"))
		}
		if wire.Operator == nil {
			issues = append(issues, requiredIssue(path+"/operator", "operator is required"))
		}
		if len(issues) > 0 {
			return nil, validationDecodeError(domain.NewValidationError(issues...))
		}
		return &domain.FilterNode{Kind: wire.Kind, FieldID: *wire.FieldID, Operator: *wire.Operator, Value: wire.Value}, nil
	default:
		return nil, validationDecodeError(domain.NewValidationError(domain.ValidationIssue{Path: path + "/kind", Code: "format", Message: "kind must be group or rule"}))
	}
}

func lifecycleQuery(r *http.Request) (string, bool) {
	if len(r.URL.Query()) == 0 {
		return "active", true
	}
	values, ok := r.URL.Query()["lifecycle"]
	if !ok || len(r.URL.Query()) != 1 || len(values) != 1 || values[0] == "" {
		return "", false
	}
	return values[0], true
}

func requiredValidation(path, message string) error {
	return domain.NewValidationError(requiredIssue(path, message))
}

func requiredIssue(path, message string) domain.ValidationIssue {
	return domain.ValidationIssue{Path: path, Code: "required", Message: message}
}

func isJSONObject(raw json.RawMessage) bool {
	return strings.HasPrefix(strings.TrimSpace(string(raw)), "{")
}

func prefixDecodeError(err error, prefix string) error {
	decodeError, ok := err.(*requestDecodeError)
	if !ok {
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
	for index, issue := range issues {
		issue.Path = prefix + issue.Path
		prefixed[index] = issue
	}
	copyError.Details = map[string]any{"issues": prefixed}
	return &copyError
}

