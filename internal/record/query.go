package record

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Mahjong404/LoomTable-Server/internal/cursor"
	"github.com/Mahjong404/LoomTable-Server/internal/domain"
	"github.com/Mahjong404/LoomTable-Server/internal/id"
	"golang.org/x/text/unicode/norm"
)

const (
	defaultQueryLimit = 100
	maxQueryLimit     = 500
	queryCursorTTL    = 30 * time.Minute
)

type QueryStore interface {
	ResolveQuery(context.Context, string, string, string) (QueryMetadata, error)
	QueryRecords(context.Context, string, string, QueryPlan, *QueryPosition, bool) (StoredQueryPage, error)
}

type queryCursorPayload struct {
	ActorID     string        `json:"actorId"`
	TableID     string        `json:"tableId"`
	Fingerprint string        `json:"fingerprint"`
	SchemaHash  string        `json:"schemaHash"`
	Position    QueryPosition `json:"position"`
	IssuedAt    int64         `json:"issuedAt"`
	ExpiresAt   int64         `json:"expiresAt"`
}

func ValidateFilter(filter *domain.FilterNode, fields map[string]FieldDefinition) error {
	return validateFilter(filter, QueryMetadata{Fields: fields}, false)
}

func (s *Service) Query(ctx context.Context, actorID, tableID string, request QueryRequest) (QueryResult, error) {
	if !id.Valid(id.TablePrefix, tableID) {
		return QueryResult{}, &domain.BadRequestError{Message: "/tableId has an invalid typed ID"}
	}
	if request.ViewIDPresent && !id.Valid(id.ViewPrefix, request.ViewID) {
		return QueryResult{}, domain.NewValidationError(domain.ValidationIssue{Path: "/viewId", Code: "format", Message: "viewId must be a typed View ID"})
	}
	if s == nil || s.store == nil {
		return QueryResult{}, domain.ErrDependencyMissing
	}
	queryStore, ok := s.store.(QueryStore)
	if !ok {
		return QueryResult{}, domain.ErrDependencyMissing
	}
	metadata, err := queryStore.ResolveQuery(ctx, actorID, tableID, request.ViewID)
	if err != nil {
		return QueryResult{}, err
	}
	plan, err := buildQueryPlan(request, metadata)
	if err != nil {
		return QueryResult{}, err
	}

	signer, err := s.cursorSigner(ctx)
	if err != nil {
		return QueryResult{}, err
	}
	var position *QueryPosition
	if request.Cursor != "" {
		var payload queryCursorPayload
		if err := signer.Decode("query", request.Cursor, &payload); err != nil {
			return QueryResult{}, &domain.InvalidCursorError{}
		}
		if payload.ActorID != actorID || payload.TableID != tableID || payload.Fingerprint != plan.Fingerprint {
			return QueryResult{}, &domain.InvalidCursorError{}
		}
		if s.now().UTC().Unix() >= payload.ExpiresAt || payload.SchemaHash != plan.SchemaHash {
			return QueryResult{}, &domain.CursorExpiredError{}
		}
		position = &payload.Position
	}

	stored, err := queryStore.QueryRecords(ctx, actorID, tableID, plan, position, request.Cursor == "")
	if err != nil {
		return QueryResult{}, err
	}
	changeCursor, err := signer.Encode("change", changeCursorPayload{ActorID: actorID, TableID: tableID, Sequence: stored.ChangeSequence})
	if err != nil {
		return QueryResult{}, fmt.Errorf("encode change cursor: %w", err)
	}
	result := QueryResult{
		Items: stored.Items, HasMore: stored.HasMore, ChangeCursor: changeCursor, TotalCount: stored.TotalCount,
	}
	if stored.HasMore {
		if stored.NextPosition == nil {
			return QueryResult{}, errors.New("query store omitted the next keyset position")
		}
		now := s.now().UTC()
		result.NextCursor, err = signer.Encode("query", queryCursorPayload{
			ActorID: actorID, TableID: tableID, Fingerprint: plan.Fingerprint, SchemaHash: plan.SchemaHash,
			Position: *stored.NextPosition, IssuedAt: now.Unix(), ExpiresAt: now.Add(queryCursorTTL).Unix(),
		})
		if err != nil {
			return QueryResult{}, fmt.Errorf("encode query cursor: %w", err)
		}
	}
	return result, nil
}

func (s *Service) cursorSigner(ctx context.Context) (*cursor.Signer, error) {
	if s == nil || s.store == nil {
		return nil, domain.ErrDependencyMissing
	}
	key, err := s.store.CursorKey(ctx)
	if err != nil {
		return nil, err
	}
	signer, err := cursor.NewSigner(key)
	if err != nil {
		return nil, fmt.Errorf("create cursor signer: %w", err)
	}
	return signer, nil
}

type changeCursorPayload struct {
	ActorID  string `json:"actorId"`
	TableID  string `json:"tableId"`
	Sequence int64  `json:"sequence"`
}

func buildQueryPlan(request QueryRequest, metadata QueryMetadata) (QueryPlan, error) {
	if metadata.TableID == "" {
		return QueryPlan{}, errors.New("query metadata does not identify the requested Table")
	}
	lifecycle := request.Lifecycle
	if lifecycle == "" {
		lifecycle = "active"
	}
	if lifecycle != "active" && lifecycle != "deleted" && lifecycle != "all" {
		return QueryPlan{}, domain.NewValidationError(domain.ValidationIssue{Path: "/lifecycle", Code: "format", Message: "lifecycle must be active, deleted, or all"})
	}
	limit := request.Limit
	if limit == 0 {
		limit = defaultQueryLimit
	}
	if limit < 1 || limit > maxQueryLimit {
		return QueryPlan{}, domain.NewValidationError(domain.ValidationIssue{Path: "/limit", Code: "limit", Message: "limit must be from 1 to 500"})
	}

	projection, filter, sortSpecs := viewQueryDefaults(metadata)
	projectionFromView := metadata.View != nil && projection != nil
	filterFromView := metadata.View != nil && filter != nil
	sortFromView := metadata.View != nil && sortSpecs != nil
	if request.ProjectionPresent {
		projection = append([]string(nil), request.Projection...)
		projectionFromView = false
	}
	if request.FilterPresent {
		filter = cloneFilter(request.Filter)
		filterFromView = false
	}
	if request.SortPresent {
		sortSpecs = append([]domain.SortSpec(nil), request.Sort...)
		sortFromView = false
	}
	if projection == nil {
		projection = activeFieldIDs(metadata.Fields)
	}

	if err := validateProjection(projection, metadata, projectionFromView); err != nil {
		return QueryPlan{}, err
	}
	if err := validateFilter(filter, metadata, filterFromView); err != nil {
		return QueryPlan{}, err
	}
	if err := normalizeSort(sortSpecs, metadata, sortFromView); err != nil {
		return QueryPlan{}, err
	}
	search, err := normalizeSearch(request.Search)
	if err != nil {
		return QueryPlan{}, err
	}

	plan := QueryPlan{
		Lifecycle: lifecycle, Limit: limit, Projection: projection, Filter: filter,
		Sort: sortSpecs, Search: search, Fields: metadata.Fields,
	}
	plan.Fingerprint, err = queryFingerprint(request.ViewID, plan)
	if err != nil {
		return QueryPlan{}, err
	}
	plan.SchemaHash, err = querySchemaHash(metadata, plan)
	if err != nil {
		return QueryPlan{}, err
	}
	return plan, nil
}

func viewQueryDefaults(metadata QueryMetadata) ([]string, *domain.FilterNode, []domain.SortSpec) {
	if metadata.View == nil {
		return nil, nil, nil
	}
	switch config := metadata.View.Config.(type) {
	case domain.GridViewConfig:
		return append([]string(nil), config.Projection...), cloneFilter(config.Filter), append([]domain.SortSpec(nil), config.Sort...)
	case domain.MapViewConfig:
		return nil, cloneFilter(config.Filter), nil
	default:
		return nil, nil, nil
	}
}

func activeFieldIDs(fields map[string]FieldDefinition) []string {
	items := make([]FieldDefinition, 0, len(fields))
	for _, field := range fields {
		if field.DeletedAt == nil {
			items = append(items, field)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Position == items[j].Position {
			return items[i].ID < items[j].ID
		}
		return items[i].Position < items[j].Position
	})
	result := make([]string, len(items))
	for index, field := range items {
		result[index] = field.ID
	}
	return result
}

func validateProjection(projection []string, metadata QueryMetadata, fromView bool) error {
	if len(projection) > 500 {
		return domain.NewValidationError(domain.ValidationIssue{Path: "/projection", Code: "limit", Message: "projection cannot contain more than 500 Fields"})
	}
	seen := make(map[string]struct{}, len(projection))
	invalid := make([]string, 0)
	issues := make([]domain.ValidationIssue, 0)
	for index, fieldID := range projection {
		path := fmt.Sprintf("/projection/%d", index)
		if _, duplicate := seen[fieldID]; duplicate {
			issues = append(issues, domain.ValidationIssue{Path: path, Code: "duplicate", Message: "Field ID appears more than once"})
			continue
		}
		seen[fieldID] = struct{}{}
		field, exists := metadata.Fields[fieldID]
		if !id.Valid(id.FieldPrefix, fieldID) || !exists || field.DeletedAt != nil {
			invalid = append(invalid, fieldID)
			if !fromView {
				issues = append(issues, domain.ValidationIssue{Path: path, Code: "invalidReference", Message: "Field is foreign, unknown, or deleted"})
			}
		}
	}
	if fromView && len(invalid) > 0 {
		return viewConfigurationError(metadata, invalid)
	}
	if len(issues) > 0 {
		return domain.NewValidationError(issues...)
	}
	return nil
}

func validateFilter(filter *domain.FilterNode, metadata QueryMetadata, fromView bool) error {
	if filter == nil {
		return nil
	}
	nodeCount := 0
	invalid := make([]string, 0)
	issues := make([]domain.ValidationIssue, 0)
	var walk func(*domain.FilterNode, string, int) error
	walk = func(node *domain.FilterNode, path string, depth int) error {
		nodeCount++
		if depth > 8 {
			issues = append(issues, domain.ValidationIssue{Path: path, Code: "limit", Message: "Filter depth cannot exceed 8"})
			return nil
		}
		if nodeCount > 100 {
			issues = append(issues, domain.ValidationIssue{Path: path, Code: "limit", Message: "Filter cannot contain more than 100 nodes"})
			return nil
		}
		if node.Kind == "" {
			issues = append(issues, requiredQueryIssue(path+"/kind", "kind is required"))
			return nil
		}
		switch node.Kind {
		case "group":
			if node.Operator == "" {
				issues = append(issues, requiredQueryIssue(path+"/operator", "operator is required"))
			} else if node.Operator != "and" && node.Operator != "or" {
				issues = append(issues, domain.ValidationIssue{Path: path + "/operator", Code: "format", Message: "group operator must be and or or"})
			}
			if len(node.Children) == 0 {
				issues = append(issues, requiredQueryIssue(path+"/children", "children must contain at least one node"))
			}
			if node.FieldID != "" || len(node.Value) > 0 {
				issues = append(issues, domain.ValidationIssue{Path: path, Code: "format", Message: "group cannot contain rule properties"})
			}
			for index := range node.Children {
				if err := walk(&node.Children[index], fmt.Sprintf("%s/children/%d", path, index), depth+1); err != nil {
					return err
				}
			}
		case "rule":
			if len(node.Children) > 0 {
				issues = append(issues, domain.ValidationIssue{Path: path + "/children", Code: "format", Message: "rule cannot contain children"})
			}
			if node.FieldID == "" {
				issues = append(issues, requiredQueryIssue(path+"/fieldId", "fieldId is required"))
				return nil
			}
			if node.Operator == "" {
				issues = append(issues, requiredQueryIssue(path+"/operator", "operator is required"))
				return nil
			}
			field, exists := metadata.Fields[node.FieldID]
			if !id.Valid(id.FieldPrefix, node.FieldID) || !exists || field.DeletedAt != nil {
				invalid = append(invalid, node.FieldID)
				if !fromView {
					issues = append(issues, domain.ValidationIssue{Path: path + "/fieldId", Code: "invalidReference", Message: "Field is foreign, unknown, or deleted"})
				}
				return nil
			}
			if err := validateFilterRule(path, node, field); err != nil {
				return err
			}
		default:
			issues = append(issues, domain.ValidationIssue{Path: path + "/kind", Code: "format", Message: "kind must be group or rule"})
		}
		return nil
	}
	if err := walk(filter, "/filter", 1); err != nil {
		return err
	}
	if fromView && len(invalid) > 0 {
		return viewConfigurationError(metadata, invalid)
	}
	if len(issues) > 0 {
		return domain.NewValidationError(issues...)
	}
	return nil
}

func validateFilterRule(path string, node *domain.FilterNode, field FieldDefinition) error {
	allowed := map[string]map[string]bool{
		"text":        {"is": true, "isNot": true, "contains": true, "notContains": true, "startsWith": true, "endsWith": true, "isEmpty": true, "isNotEmpty": true},
		"longText":    {"is": true, "isNot": true, "contains": true, "notContains": true, "startsWith": true, "endsWith": true, "isEmpty": true, "isNotEmpty": true},
		"url":         {"is": true, "isNot": true, "contains": true, "notContains": true, "startsWith": true, "endsWith": true, "isEmpty": true, "isNotEmpty": true},
		"number":      {"is": true, "isNot": true, "greaterThan": true, "greaterOrEqual": true, "lessThan": true, "lessOrEqual": true, "isEmpty": true, "isNotEmpty": true},
		"date":        {"is": true, "isNot": true, "greaterThan": true, "greaterOrEqual": true, "lessThan": true, "lessOrEqual": true, "isEmpty": true, "isNotEmpty": true},
		"checkbox":    {"is": true, "isNot": true},
		"select":      {"is": true, "isNot": true, "isEmpty": true, "isNotEmpty": true},
		"multiSelect": {"includes": true, "excludes": true, "isEmpty": true, "isNotEmpty": true},
		"location":    {"isEmpty": true, "isNotEmpty": true},
	}
	if !allowed[field.Type][node.Operator] {
		return &domain.UnsupportedOperatorError{FieldID: field.ID, Operator: node.Operator}
	}
	emptyOperator := node.Operator == "isEmpty" || node.Operator == "isNotEmpty"
	if emptyOperator {
		if len(node.Value) > 0 {
			return domain.NewValidationError(domain.ValidationIssue{Path: path + "/value", Code: "format", Message: "value must be omitted for an empty operator"})
		}
		return nil
	}
	if len(node.Value) == 0 || string(node.Value) == "null" {
		return domain.NewValidationError(requiredQueryIssue(path+"/value", "value is required for this operator"))
	}
	var value any
	switch field.Type {
	case "text", "longText", "url":
		var text string
		if json.Unmarshal(node.Value, &text) != nil {
			return queryValueTypeError(path, "value must be a string")
		}
		value = domain.FoldKey(text)
	case "number":
		var number float64
		if json.Unmarshal(node.Value, &number) != nil || math.IsNaN(number) || math.IsInf(number, 0) {
			return queryValueTypeError(path, "value must be a finite number")
		}
		value = number
	case "checkbox":
		var checked bool
		if json.Unmarshal(node.Value, &checked) != nil {
			return queryValueTypeError(path, "value must be a boolean")
		}
		value = checked
	case "date":
		var date string
		if json.Unmarshal(node.Value, &date) != nil || !validDate(date) {
			return domain.NewValidationError(domain.ValidationIssue{Path: path + "/value", Code: "format", Message: "value must be a YYYY-MM-DD date"})
		}
		value = date
	case "select", "multiSelect":
		var optionID string
		if json.Unmarshal(node.Value, &optionID) != nil || !id.Valid(id.OptionPrefix, optionID) {
			return queryValueTypeError(path, "value must be an Option ID")
		}
		if !fieldOwnsOption(field, optionID) {
			return domain.NewValidationError(domain.ValidationIssue{Path: path + "/value", Code: "invalidReference", Message: "Option does not belong to this Field"})
		}
		value = optionID
	}
	normalized, err := json.Marshal(value)
	if err != nil {
		return err
	}
	node.Value = normalized
	return nil
}

func normalizeSort(specs []domain.SortSpec, metadata QueryMetadata, fromView bool) error {
	if len(specs) > 10 {
		return domain.NewValidationError(domain.ValidationIssue{Path: "/sort", Code: "limit", Message: "sort cannot contain more than 10 Fields"})
	}
	seen := make(map[string]struct{}, len(specs))
	invalid := make([]string, 0)
	issues := make([]domain.ValidationIssue, 0)
	for index := range specs {
		spec := &specs[index]
		path := fmt.Sprintf("/sort/%d", index)
		if _, duplicate := seen[spec.FieldID]; duplicate {
			issues = append(issues, domain.ValidationIssue{Path: path + "/fieldId", Code: "duplicate", Message: "Field ID appears more than once"})
			continue
		}
		seen[spec.FieldID] = struct{}{}
		field, exists := metadata.Fields[spec.FieldID]
		if !id.Valid(id.FieldPrefix, spec.FieldID) || !exists || field.DeletedAt != nil {
			invalid = append(invalid, spec.FieldID)
			if !fromView {
				issues = append(issues, domain.ValidationIssue{Path: path + "/fieldId", Code: "invalidReference", Message: "Field is foreign, unknown, or deleted"})
			}
			continue
		}
		if field.Type == "multiSelect" || field.Type == "location" {
			return &domain.UnsupportedSortError{FieldID: field.ID, FieldType: field.Type}
		}
		if spec.Direction != "asc" && spec.Direction != "desc" {
			issues = append(issues, domain.ValidationIssue{Path: path + "/direction", Code: "format", Message: "direction must be asc or desc"})
		}
		if spec.Nulls == "" {
			spec.Nulls = "last"
		} else if spec.Nulls != "first" && spec.Nulls != "last" {
			issues = append(issues, domain.ValidationIssue{Path: path + "/nulls", Code: "format", Message: "nulls must be first or last"})
		}
	}
	if fromView && len(invalid) > 0 {
		return viewConfigurationError(metadata, invalid)
	}
	if len(issues) > 0 {
		return domain.NewValidationError(issues...)
	}
	return nil
}

func normalizeSearch(value string) (string, error) {
	value = norm.NFC.String(strings.TrimFunc(value, unicode.IsSpace))
	if utf8.RuneCountInString(value) > 500 {
		return "", domain.NewValidationError(domain.ValidationIssue{Path: "/search", Code: "limit", Message: "search cannot exceed 500 Unicode code points"})
	}
	return domain.FoldKey(value), nil
}

func queryFingerprint(viewID string, plan QueryPlan) (string, error) {
	encoded, err := json.Marshal(struct {
		Version    string             `json:"version"`
		ViewID     string             `json:"viewId,omitempty"`
		Lifecycle  string             `json:"lifecycle"`
		Limit      int                `json:"limit"`
		Projection []string           `json:"projection"`
		Filter     *domain.FilterNode `json:"filter,omitempty"`
		Sort       []domain.SortSpec  `json:"sort"`
		Search     string             `json:"search"`
	}{Version: "v1", ViewID: viewID, Lifecycle: plan.Lifecycle, Limit: plan.Limit, Projection: plan.Projection, Filter: plan.Filter, Sort: plan.Sort, Search: plan.Search})
	if err != nil {
		return "", fmt.Errorf("canonicalize query: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func querySchemaHash(metadata QueryMetadata, plan QueryPlan) (string, error) {
	references := make(map[string]struct{})
	for _, fieldID := range plan.Projection {
		references[fieldID] = struct{}{}
	}
	collectFilterFields(plan.Filter, references)
	for _, spec := range plan.Sort {
		references[spec.FieldID] = struct{}{}
	}
	if plan.Search != "" {
		for fieldID, field := range metadata.Fields {
			if field.DeletedAt == nil && (field.Type == "text" || field.Type == "longText" || field.Type == "url") {
				references[fieldID] = struct{}{}
			}
		}
	}
	ids := make([]string, 0, len(references))
	for fieldID := range references {
		ids = append(ids, fieldID)
	}
	sort.Strings(ids)
	type revision struct {
		ID       string `json:"id"`
		Revision int64  `json:"revision"`
	}
	revisions := make([]revision, 0, len(ids))
	for _, fieldID := range ids {
		revisions = append(revisions, revision{ID: fieldID, Revision: metadata.Fields[fieldID].Revision})
	}
	viewRevision := int64(0)
	if metadata.View != nil {
		viewRevision = metadata.View.Revision
	}
	encoded, err := json.Marshal(struct {
		ViewRevision int64      `json:"viewRevision"`
		Fields       []revision `json:"fields"`
	}{ViewRevision: viewRevision, Fields: revisions})
	if err != nil {
		return "", fmt.Errorf("canonicalize query schema: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func viewConfigurationError(metadata QueryMetadata, fieldIDs []string) error {
	sort.Strings(fieldIDs)
	fieldIDs = compactStrings(fieldIDs)
	viewID := ""
	if metadata.View != nil {
		viewID = metadata.View.ID
	}
	return &domain.ViewConfigurationRequiredError{ViewID: viewID, InvalidFieldIDs: fieldIDs}
}

func compactStrings(values []string) []string {
	if len(values) == 0 {
		return values
	}
	result := values[:1]
	for _, value := range values[1:] {
		if value != result[len(result)-1] {
			result = append(result, value)
		}
	}
	return result
}

func collectFilterFields(filter *domain.FilterNode, target map[string]struct{}) {
	if filter == nil {
		return
	}
	if filter.Kind == "rule" && filter.FieldID != "" {
		target[filter.FieldID] = struct{}{}
	}
	for index := range filter.Children {
		collectFilterFields(&filter.Children[index], target)
	}
}

func cloneFilter(filter *domain.FilterNode) *domain.FilterNode {
	if filter == nil {
		return nil
	}
	copyNode := *filter
	copyNode.Value = append(json.RawMessage(nil), filter.Value...)
	copyNode.Children = make([]domain.FilterNode, len(filter.Children))
	for index := range filter.Children {
		copyNode.Children[index] = *cloneFilter(&filter.Children[index])
	}
	return &copyNode
}

func fieldOwnsOption(field FieldDefinition, optionID string) bool {
	active, deleted, err := selectOptions(field.Config)
	if err != nil {
		return false
	}
	_, activeMatch := active[optionID]
	_, deletedMatch := deleted[optionID]
	return activeMatch || deletedMatch
}

func requiredQueryIssue(path, message string) domain.ValidationIssue {
	return domain.ValidationIssue{Path: path, Code: "required", Message: message}
}

func queryValueTypeError(path, message string) error {
	return domain.NewValidationError(domain.ValidationIssue{Path: path + "/value", Code: "type", Message: message})
}
