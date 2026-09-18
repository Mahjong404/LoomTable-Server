package record

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/Mahjong404/LoomTable-Server/internal/domain"
	"github.com/Mahjong404/LoomTable-Server/internal/id"
)

var distinctFieldTypes = map[string]bool{
	"text": true, "longText": true, "url": true,
	"number": true, "date": true, "checkbox": true,
	"select": true, "multiSelect": true,
}

var aggregateFunctions = map[string]bool{
	"count": true, "sum": true, "avg": true, "min": true, "max": true,
}

type ValuesStore interface {
	ResolveQuery(context.Context, string, string, string) (QueryMetadata, error)
	DistinctValues(context.Context, string, string, DistinctPlan) (StoredDistinctPage, error)
	AggregateRecords(context.Context, string, string, AggregatePlan) (StoredAggregateResult, error)
}

type valuesCursorPayload struct {
	ActorID     string `json:"actorId"`
	TableID     string `json:"tableId"`
	FieldID     string `json:"fieldId"`
	Fingerprint string `json:"fingerprint"`
	SchemaHash  string `json:"schemaHash"`
	LastValue   string `json:"lastValue,omitempty"`
	LastIndex   int    `json:"lastIndex"`
	IssuedAt    int64  `json:"issuedAt"`
	ExpiresAt   int64  `json:"expiresAt"`
}

func (s *Service) DistinctValues(ctx context.Context, actorID, tableID, fieldID string, request DistinctValuesRequest) (DistinctValuesPage, error) {
	if !id.Valid(id.TablePrefix, tableID) {
		return DistinctValuesPage{}, &domain.BadRequestError{Message: "/tableId has an invalid typed ID"}
	}
	if !id.Valid(id.FieldPrefix, fieldID) {
		return DistinctValuesPage{}, &domain.BadRequestError{Message: "/fieldId has an invalid typed ID"}
	}
	if s == nil || s.store == nil {
		return DistinctValuesPage{}, domain.ErrDependencyMissing
	}
	valuesStore, ok := s.store.(ValuesStore)
	if !ok {
		return DistinctValuesPage{}, domain.ErrDependencyMissing
	}
	limit := request.Limit
	if limit == 0 {
		limit = defaultQueryLimit
	}
	if limit < 1 || limit > maxQueryLimit {
		return DistinctValuesPage{}, domain.NewValidationError(domain.ValidationIssue{Path: "/limit", Code: "limit", Message: "limit must be from 1 to 500"})
	}
	metadata, err := valuesStore.ResolveQuery(ctx, actorID, tableID, "")
	if err != nil {
		return DistinctValuesPage{}, err
	}
	field, exists := metadata.Fields[fieldID]
	if !exists || field.DeletedAt != nil {
		return DistinctValuesPage{}, domain.ErrNotFound
	}
	if !distinctFieldTypes[field.Type] {
		return DistinctValuesPage{}, &domain.UnsupportedFieldTypeError{FieldType: field.Type}
	}
	filter := pruneFieldRules(request.Filter, fieldID)
	if err := validateFilter(filter, metadata, false); err != nil {
		return DistinctValuesPage{}, err
	}
	search, err := normalizeSearch(request.Search)
	if err != nil {
		return DistinctValuesPage{}, err
	}
	plan := DistinctPlan{FieldID: fieldID, FieldType: field.Type, Filter: filter, Fields: metadata.Fields}
	fingerprint, err := distinctFingerprint(plan, search, limit)
	if err != nil {
		return DistinctValuesPage{}, err
	}
	schemaHash, err := querySchemaHash(metadata, QueryPlan{Projection: []string{fieldID}, Filter: filter, Fields: metadata.Fields})
	if err != nil {
		return DistinctValuesPage{}, err
	}

	signer, err := s.cursorSigner(ctx)
	if err != nil {
		return DistinctValuesPage{}, err
	}
	var cursorState *valuesCursorPayload
	if request.Cursor != "" {
		var payload valuesCursorPayload
		if err := signer.Decode("values", request.Cursor, &payload); err != nil {
			return DistinctValuesPage{}, &domain.InvalidCursorError{}
		}
		if payload.ActorID != actorID || payload.TableID != tableID || payload.FieldID != fieldID || payload.Fingerprint != fingerprint {
			return DistinctValuesPage{}, &domain.InvalidCursorError{}
		}
		if s.now().UTC().Unix() >= payload.ExpiresAt || payload.SchemaHash != schemaHash {
			return DistinctValuesPage{}, &domain.CursorExpiredError{}
		}
		cursorState = &payload
	}

	stored, err := valuesStore.DistinctValues(ctx, actorID, tableID, plan)
	if err != nil {
		return DistinctValuesPage{}, err
	}
	changeCursor, err := signer.Encode("change", changeCursorPayload{ActorID: actorID, TableID: tableID, Sequence: stored.ChangeSequence})
	if err != nil {
		return DistinctValuesPage{}, fmt.Errorf("encode change cursor: %w", err)
	}

	ordered := orderDistinctValues(stored.Items, field)
	if search != "" {
		filtered := make([]StoredDistinctValue, 0, len(ordered))
		for _, item := range ordered {
			candidate := distinctDisplay(field, item)
			if candidate == "" {
				candidate = item.Value
			}
			if strings.Contains(domain.FoldKey(candidate), search) {
				filtered = append(filtered, item)
			}
		}
		ordered = filtered
	}

	start := 0
	lastIndex := -1
	if cursorState != nil {
		if field.Type == "select" || field.Type == "multiSelect" {
			lastIndex = cursorState.LastIndex
			start = lastIndex + 1
		} else {
			for start < len(ordered) && ordered[start].Value <= cursorState.LastValue {
				start++
			}
		}
	}
	if start > len(ordered) {
		start = len(ordered)
	}
	remaining := ordered[start:]
	result := DistinctValuesPage{Items: make([]DistinctValue, 0, limit), EmptyCount: stored.EmptyCount, ChangeCursor: changeCursor}
	result.HasMore = len(remaining) > limit
	emitted := remaining
	if len(emitted) > limit {
		emitted = emitted[:limit]
	}
	var last StoredDistinctValue
	for index, item := range emitted {
		result.Items = append(result.Items, DistinctValue{
			Value:   typedDistinctValue(field.Type, item.Value),
			Display: distinctDisplay(field, item),
			Count:   item.Count,
		})
		last = item
		lastIndex = start + index
	}
	if result.HasMore {
		now := s.now().UTC()
		result.NextCursor, err = signer.Encode("values", valuesCursorPayload{
			ActorID: actorID, TableID: tableID, FieldID: fieldID,
			Fingerprint: fingerprint, SchemaHash: schemaHash,
			LastValue: last.Value, LastIndex: lastIndex,
			IssuedAt: now.Unix(), ExpiresAt: now.Add(queryCursorTTL).Unix(),
		})
		if err != nil {
			return DistinctValuesPage{}, fmt.Errorf("encode values cursor: %w", err)
		}
	}
	return result, nil
}

func (s *Service) Aggregate(ctx context.Context, actorID, tableID string, request AggregateRequest) (AggregateResult, error) {
	if !id.Valid(id.TablePrefix, tableID) {
		return AggregateResult{}, &domain.BadRequestError{Message: "/tableId has an invalid typed ID"}
	}
	if s == nil || s.store == nil {
		return AggregateResult{}, domain.ErrDependencyMissing
	}
	valuesStore, ok := s.store.(ValuesStore)
	if !ok {
		return AggregateResult{}, domain.ErrDependencyMissing
	}
	issues := make([]domain.ValidationIssue, 0)
	if len(request.FieldIDs) == 0 {
		issues = append(issues, domain.ValidationIssue{Path: "/fieldIds", Code: "required", Message: "fieldIds must contain at least one Field ID"})
	}
	if len(request.FieldIDs) > 50 {
		issues = append(issues, domain.ValidationIssue{Path: "/fieldIds", Code: "limit", Message: "fieldIds cannot contain more than 50 Fields"})
	}
	if len(request.Functions) == 0 {
		issues = append(issues, domain.ValidationIssue{Path: "/fns", Code: "required", Message: "fns must contain at least one aggregation function"})
	}
	seenFns := make(map[string]struct{}, len(request.Functions))
	for index, fn := range request.Functions {
		path := fmt.Sprintf("/fns/%d", index)
		if !aggregateFunctions[fn] {
			issues = append(issues, domain.ValidationIssue{Path: path, Code: "format", Message: "fn must be count, sum, avg, min, or max"})
			continue
		}
		if _, duplicate := seenFns[fn]; duplicate {
			issues = append(issues, domain.ValidationIssue{Path: path, Code: "duplicate", Message: "function appears more than once"})
		}
		seenFns[fn] = struct{}{}
	}
	if len(issues) > 0 {
		return AggregateResult{}, domain.NewValidationError(issues...)
	}
	metadata, err := valuesStore.ResolveQuery(ctx, actorID, tableID, "")
	if err != nil {
		return AggregateResult{}, err
	}
	seenFields := make(map[string]struct{}, len(request.FieldIDs))
	for index, fieldID := range request.FieldIDs {
		path := fmt.Sprintf("/fieldIds/%d", index)
		if _, duplicate := seenFields[fieldID]; duplicate {
			issues = append(issues, domain.ValidationIssue{Path: path, Code: "duplicate", Message: "Field ID appears more than once"})
			continue
		}
		seenFields[fieldID] = struct{}{}
		field, exists := metadata.Fields[fieldID]
		if !id.Valid(id.FieldPrefix, fieldID) || !exists || field.DeletedAt != nil {
			issues = append(issues, domain.ValidationIssue{Path: path, Code: "invalidReference", Message: "Field is foreign, unknown, or deleted"})
		}
	}
	if len(issues) > 0 {
		return AggregateResult{}, domain.NewValidationError(issues...)
	}
	filter := cloneFilter(request.Filter)
	if err := validateFilter(filter, metadata, false); err != nil {
		return AggregateResult{}, err
	}

	signer, err := s.cursorSigner(ctx)
	if err != nil {
		return AggregateResult{}, err
	}
	plan := AggregatePlan{Filter: filter, Fields: metadata.Fields}
	for _, fieldID := range request.FieldIDs {
		plan.Requests = append(plan.Requests, AggregateFieldRequest{
			FieldID:   fieldID,
			FieldType: metadata.Fields[fieldID].Type,
			Functions: append([]string(nil), request.Functions...),
		})
	}
	stored, err := valuesStore.AggregateRecords(ctx, actorID, tableID, plan)
	if err != nil {
		return AggregateResult{}, err
	}
	changeCursor, err := signer.Encode("change", changeCursorPayload{ActorID: actorID, TableID: tableID, Sequence: stored.ChangeSequence})
	if err != nil {
		return AggregateResult{}, fmt.Errorf("encode change cursor: %w", err)
	}
	return AggregateResult{Results: stored.Results, ChangeCursor: changeCursor}, nil
}

func pruneFieldRules(filter *domain.FilterNode, fieldID string) *domain.FilterNode {
	if filter == nil {
		return nil
	}
	if filter.Kind == "rule" {
		if filter.FieldID == fieldID {
			return nil
		}
		return cloneFilter(filter)
	}
	children := make([]domain.FilterNode, 0, len(filter.Children))
	for index := range filter.Children {
		if pruned := pruneFieldRules(&filter.Children[index], fieldID); pruned != nil {
			children = append(children, *pruned)
		}
	}
	if len(children) == 0 {
		return nil
	}
	pruned := *filter
	pruned.Children = children
	return &pruned
}

func distinctFingerprint(plan DistinctPlan, search string, limit int) (string, error) {
	encoded, err := json.Marshal(struct {
		Version string             `json:"version"`
		FieldID string             `json:"fieldId"`
		Filter  *domain.FilterNode `json:"filter,omitempty"`
		Search  string             `json:"search"`
		Limit   int                `json:"limit"`
	}{Version: "v1", FieldID: plan.FieldID, Filter: plan.Filter, Search: search, Limit: limit})
	if err != nil {
		return "", fmt.Errorf("canonicalize distinct values query: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func orderDistinctValues(items []StoredDistinctValue, field FieldDefinition) []StoredDistinctValue {
	ordered := append([]StoredDistinctValue(nil), items...)
	switch field.Type {
	case "select", "multiSelect":
		rank := selectOptionRank(field.Config)
		sort.SliceStable(ordered, func(i, j int) bool {
			leftRank, leftKnown := rank[ordered[i].Value]
			rightRank, rightKnown := rank[ordered[j].Value]
			if leftKnown != rightKnown {
				return leftKnown
			}
			if leftRank != rightRank {
				return leftRank < rightRank
			}
			return ordered[i].Value < ordered[j].Value
		})
	case "number":
		sort.SliceStable(ordered, func(i, j int) bool {
			left, leftErr := strconv.ParseFloat(ordered[i].Value, 64)
			right, rightErr := strconv.ParseFloat(ordered[j].Value, 64)
			if (leftErr == nil) != (rightErr == nil) {
				return leftErr == nil
			}
			if left != right {
				return left < right
			}
			return ordered[i].Value < ordered[j].Value
		})
	default:
		sort.SliceStable(ordered, func(i, j int) bool {
			return ordered[i].Value < ordered[j].Value
		})
	}
	return ordered
}

func selectOptionRank(raw json.RawMessage) map[string]int {
	var config domain.SelectFieldConfig
	if err := json.Unmarshal(raw, &config); err != nil {
		return map[string]int{}
	}
	rank := make(map[string]int, len(config.Options)+len(config.DeletedOptions))
	position := 0
	for _, option := range config.Options {
		if _, exists := rank[option.ID]; !exists {
			rank[option.ID] = position
			position++
		}
	}
	for _, option := range config.DeletedOptions {
		if _, exists := rank[option.ID]; !exists {
			rank[option.ID] = position
			position++
		}
	}
	return rank
}

func selectOptionNames(raw json.RawMessage) map[string]string {
	var config domain.SelectFieldConfig
	if err := json.Unmarshal(raw, &config); err != nil {
		return map[string]string{}
	}
	names := make(map[string]string, len(config.Options)+len(config.DeletedOptions))
	for _, option := range config.Options {
		names[option.ID] = option.Name
	}
	for _, option := range config.DeletedOptions {
		names[option.ID] = option.Name
	}
	return names
}

func typedDistinctValue(fieldType, value string) any {
	switch fieldType {
	case "number":
		if number, err := strconv.ParseFloat(value, 64); err == nil {
			return number
		}
	case "checkbox":
		return value == "true"
	}
	return value
}

func distinctDisplay(field FieldDefinition, item StoredDistinctValue) string {
	if field.Type == "select" || field.Type == "multiSelect" {
		return selectOptionNames(field.Config)[item.Value]
	}
	return item.Display
}
