package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Mahjong404/LoomTable-Server/internal/cursor"
	"github.com/Mahjong404/LoomTable-Server/internal/domain"
	"github.com/Mahjong404/LoomTable-Server/internal/id"
)

const conversionPreviewPurpose = "field-convert-preview"

type conversionModeSpec struct {
	ID    string
	Label string
}

var conversionModes = map[string]map[string][]conversionModeSpec{
	"text": {
		"longText":    {{"preserve", "Keep values"}},
		"number":      {{"parse", "Parse numeric text"}},
		"checkbox":    {{"parse", "Parse boolean text"}},
		"date":        {{"parse", "Parse YYYY-MM-DD text"}},
		"url":         {{"parse", "Keep valid URLs"}},
		"select":      {{"distinctOptions", "Create options from distinct values"}},
		"multiSelect": {{"distinctOptions", "Create options from distinct values"}},
	},
	"longText": {
		"text":        {{"dropOverlong", "Drop values over 10,000 characters"}},
		"number":      {{"parse", "Parse numeric text"}},
		"checkbox":    {{"parse", "Parse boolean text"}},
		"date":        {{"parse", "Parse YYYY-MM-DD text"}},
		"url":         {{"parse", "Keep valid URLs"}},
		"select":      {{"distinctOptions", "Create options from distinct values"}},
		"multiSelect": {{"distinctOptions", "Create options from distinct values"}},
	},
	"url": {
		"text":        {{"preserve", "Keep values"}},
		"longText":    {{"preserve", "Keep values"}},
		"select":      {{"distinctOptions", "Create options from distinct values"}},
		"multiSelect": {{"distinctOptions", "Create options from distinct values"}},
	},
	"number": {
		"text":        {{"format", "Format as text"}},
		"longText":    {{"format", "Format as text"}},
		"checkbox":    {{"nonzero", "Zero is unchecked, non-zero is checked"}, {"zeroOne", "Only 0 and 1 convert"}},
		"select":      {{"distinctOptions", "Create options from distinct values"}},
		"multiSelect": {{"distinctOptions", "Create options from distinct values"}},
	},
	"checkbox": {
		"text":        {{"format", "Format as text"}},
		"longText":    {{"format", "Format as text"}},
		"number":      {{"numeric", "Checked is 1, unchecked is 0"}},
		"select":      {{"distinctOptions", "Create options from distinct values"}},
		"multiSelect": {{"distinctOptions", "Create options from distinct values"}},
	},
	"date": {
		"text":        {{"format", "Format as text"}},
		"longText":    {{"format", "Format as text"}},
		"select":      {{"distinctOptions", "Create options from distinct values"}},
		"multiSelect": {{"distinctOptions", "Create options from distinct values"}},
	},
	"select": {
		"text":        {{"optionName", "Write option names"}},
		"longText":    {{"optionName", "Write option names"}},
		"multiSelect": {{"singleElement", "Wrap as single-option lists"}},
	},
	"multiSelect": {
		"text":     {{"joinNames", "Join option names"}},
		"longText": {{"joinNames", "Join option names"}},
		"select":   {{"firstOption", "Keep first option"}},
	},
}

type ConversionModeStats struct {
	OK             int64 `json:"ok"`
	Lossy          int64 `json:"lossy"`
	Lost           int64 `json:"lost"`
	Empty          int64 `json:"empty"`
	DistinctValues int64 `json:"distinctValues,omitempty"`
	NewOptions     int64 `json:"newOptions,omitempty"`
}

type ConversionMode struct {
	ID       string              `json:"id"`
	Label    string              `json:"label"`
	Disabled bool                `json:"disabled,omitempty"`
	Reason   string              `json:"reason,omitempty"`
	Stats    ConversionModeStats `json:"stats"`
}

type ConversionPreview struct {
	Supported    bool             `json:"supported"`
	Reason       string           `json:"reason,omitempty"`
	TotalRecords int64            `json:"totalRecords"`
	Modes        []ConversionMode `json:"modes,omitempty"`
	PreviewToken string           `json:"previewToken,omitempty"`
}

type ConversionRequest struct {
	TargetType       string
	Mode             string
	ExpectedRevision int64
	PreviewToken     string
}

type ConversionResult struct {
	Field domain.Field        `json:"field"`
	Stats ConversionModeStats `json:"stats"`
}

type FieldConversionPlan struct {
	ExpectedRevision int64
	TargetType       string
	Scan             func(raw json.RawMessage) (json.RawMessage, bool, error)
	Verify           func() error
	BuildConfig      func() (json.RawMessage, error)
	ResolveValue     func(raw json.RawMessage) (json.RawMessage, bool)
}

type previewModeDigest struct {
	Stats     ConversionModeStats `json:"stats"`
	Distincts []string            `json:"distincts,omitempty"`
}

type convertPreviewToken struct {
	FieldID  string                       `json:"fieldId"`
	Revision int64                        `json:"revision"`
	Source   string                       `json:"source"`
	Target   string                       `json:"target"`
	Modes    map[string]previewModeDigest `json:"modes"`
}

type valueOutcome int

const (
	outcomeEmpty valueOutcome = iota
	outcomeOK
	outcomeLossy
	outcomeLost
)

type conversionContext struct {
	sourceType   string
	targetType   string
	optionNames  map[string]string
	textLimit    int
	optionTarget bool
}

type conversionAccumulator struct {
	context  conversionContext
	mode     string
	stats    ConversionModeStats
	distinct map[string]string
}

var fieldTypes = map[string]struct{}{
	"text": {}, "longText": {}, "number": {}, "checkbox": {}, "date": {},
	"url": {}, "location": {}, "attachment": {}, "select": {}, "multiSelect": {},
}

func isSelectTarget(fieldType string) bool {
	return fieldType == "select" || fieldType == "multiSelect"
}

func (s *Service) PreviewFieldConversion(ctx context.Context, actorID, fieldID, targetType string) (ConversionPreview, error) {
	if err := validateID("/fieldId", id.FieldPrefix, fieldID); err != nil {
		return ConversionPreview{}, err
	}
	if _, ok := fieldTypes[targetType]; !ok {
		return ConversionPreview{}, &domain.UnsupportedFieldTypeError{FieldType: targetType}
	}
	if s == nil || s.store == nil {
		return ConversionPreview{}, domain.ErrDependencyMissing
	}
	field, err := s.store.GetField(ctx, actorID, fieldID)
	if err != nil {
		return ConversionPreview{}, err
	}
	if field.DeletedAt != nil {
		return ConversionPreview{}, &domain.InvalidStateTransitionError{Resource: "field", ID: fieldID, Action: "convert", Current: "deleted"}
	}
	specs := conversionModes[field.Type][targetType]
	if len(specs) == 0 {
		return ConversionPreview{
			Supported: false,
			Reason:    fmt.Sprintf("Field type %s cannot be converted to %s", field.Type, targetType),
		}, nil
	}
	context := newConversionContext(field, targetType)
	accumulators := make([]*conversionAccumulator, 0, len(specs))
	for _, spec := range specs {
		accumulators = append(accumulators, &conversionAccumulator{
			context: context, mode: spec.ID, distinct: make(map[string]string),
		})
	}
	primary, err := s.store.ScanFieldValues(ctx, actorID, field, func(raw json.RawMessage) error {
		for _, accumulator := range accumulators {
			if _, _, err := accumulator.add(raw); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return ConversionPreview{}, err
	}
	if primary {
		return ConversionPreview{
			Supported: false,
			Reason:    "the primary Field cannot be converted",
		}, nil
	}
	preview := ConversionPreview{Supported: true, Modes: make([]ConversionMode, 0, len(specs))}
	digests := make(map[string]previewModeDigest, len(specs))
	for index, accumulator := range accumulators {
		accumulator.finalize()
		stats := accumulator.stats
		if index == 0 {
			preview.TotalRecords = stats.OK + stats.Lossy + stats.Lost + stats.Empty
		}
		mode := ConversionMode{ID: specs[index].ID, Label: specs[index].Label, Stats: stats}
		if stats.NewOptions > 500 {
			mode.Disabled = true
			mode.Reason = "distinct values exceed the 500 option limit"
		}
		preview.Modes = append(preview.Modes, mode)
		digests[specs[index].ID] = previewModeDigest{Stats: stats, Distincts: accumulator.sortedDistincts()}
	}
	signer, err := s.conversionSigner(ctx)
	if err != nil {
		return ConversionPreview{}, err
	}
	preview.PreviewToken, err = signer.Encode(conversionPreviewPurpose, convertPreviewToken{
		FieldID: field.ID, Revision: field.Revision, Source: field.Type, Target: targetType, Modes: digests,
	})
	if err != nil {
		return ConversionPreview{}, err
	}
	return preview, nil
}

func (s *Service) ConvertField(ctx context.Context, actorID, fieldID string, request ConversionRequest) (ConversionResult, error) {
	issues := make([]domain.ValidationIssue, 0, 4)
	if err := validateID("/fieldId", id.FieldPrefix, fieldID); err != nil {
		var validation *domain.ValidationError
		if !errors.As(err, &validation) {
			return ConversionResult{}, err
		}
		issues = append(issues, validation.Issues...)
	}
	if request.Mode == "" {
		issues = append(issues, domain.ValidationIssue{Path: "/mode", Code: "required", Message: "mode is required"})
	}
	if request.ExpectedRevision < 1 {
		issues = append(issues, domain.ValidationIssue{Path: "/expectedRevision", Code: "required", Message: "expectedRevision must be at least 1"})
	}
	if request.PreviewToken == "" {
		issues = append(issues, domain.ValidationIssue{Path: "/previewToken", Code: "required", Message: "previewToken is required"})
	}
	if len(issues) > 0 {
		return ConversionResult{}, domain.NewValidationError(issues...)
	}
	if _, ok := fieldTypes[request.TargetType]; !ok {
		return ConversionResult{}, &domain.UnsupportedFieldTypeError{FieldType: request.TargetType}
	}
	if s == nil || s.store == nil {
		return ConversionResult{}, domain.ErrDependencyMissing
	}
	signer, err := s.conversionSigner(ctx)
	if err != nil {
		return ConversionResult{}, err
	}
	var token convertPreviewToken
	if err := signer.Decode(conversionPreviewPurpose, request.PreviewToken, &token); err != nil {
		return ConversionResult{}, &domain.InvalidPreviewTokenError{}
	}
	if token.FieldID != fieldID || token.Target != request.TargetType {
		return ConversionResult{}, &domain.InvalidPreviewTokenError{}
	}
	field, err := s.store.GetField(ctx, actorID, fieldID)
	if err != nil {
		return ConversionResult{}, err
	}
	if field.DeletedAt != nil {
		return ConversionResult{}, &domain.InvalidStateTransitionError{Resource: "field", ID: fieldID, Action: "convert", Current: "deleted"}
	}
	if field.Type != token.Source || field.Revision != token.Revision {
		return ConversionResult{}, &domain.ConversionPreviewStaleError{}
	}
	specs := conversionModes[field.Type][request.TargetType]
	if len(specs) == 0 {
		return ConversionResult{}, &domain.UnsupportedConversionError{SourceType: field.Type, TargetType: request.TargetType}
	}
	digest, ok := token.Modes[request.Mode]
	if !ok {
		return ConversionResult{}, domain.NewValidationError(domain.ValidationIssue{Path: "/mode", Code: "format", Message: "mode is not offered by this conversion preview"})
	}
	context := newConversionContext(field, request.TargetType)
	accumulator := &conversionAccumulator{context: context, mode: request.Mode, distinct: make(map[string]string)}
	nameToID := make(map[string]string)
	plan := FieldConversionPlan{
		ExpectedRevision: request.ExpectedRevision,
		TargetType:       request.TargetType,
		Scan:             accumulator.add,
		Verify: func() error {
			accumulator.finalize()
			if accumulator.stats != digest.Stats || !equalStrings(accumulator.sortedDistincts(), digest.Distincts) {
				return &domain.ConversionPreviewStaleError{}
			}
			return nil
		},
		BuildConfig: func() (json.RawMessage, error) {
			return s.buildConvertedConfig(field, request.TargetType, accumulator.sortedNames(), nameToID)
		},
	}
	if context.optionTarget {
		plan.ResolveValue = func(raw json.RawMessage) (json.RawMessage, bool) {
			var name string
			if err := json.Unmarshal(raw, &name); err != nil {
				return nil, false
			}
			optionID, ok := nameToID[domain.FoldKey(name)]
			if !ok {
				return nil, false
			}
			var resolved []byte
			if request.TargetType == "multiSelect" {
				resolved, _ = json.Marshal([]string{optionID})
			} else {
				resolved, _ = json.Marshal(optionID)
			}
			return resolved, true
		}
	}
	updated, err := s.store.ConvertField(ctx, actorID, fieldID, plan)
	if err != nil {
		return ConversionResult{}, err
	}
	return ConversionResult{Field: updated, Stats: accumulator.stats}, nil
}

func (s *Service) conversionSigner(ctx context.Context) (*cursor.Signer, error) {
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

func newConversionContext(field domain.Field, targetType string) conversionContext {
	context := conversionContext{
		sourceType:   field.Type,
		targetType:   targetType,
		textLimit:    10000,
		optionTarget: isSelectTarget(targetType) && field.Type != "select" && field.Type != "multiSelect",
	}
	if targetType == "longText" {
		context.textLimit = 100000
	}
	if field.Type == "select" || field.Type == "multiSelect" {
		context.optionNames = make(map[string]string)
		if config, ok := field.Config.(domain.SelectFieldConfig); ok {
			for _, option := range config.Options {
				context.optionNames[option.ID] = option.Name
			}
			for _, option := range config.DeletedOptions {
				context.optionNames[option.ID] = option.Name
			}
		}
	}
	return context
}

func (a *conversionAccumulator) add(raw json.RawMessage) (json.RawMessage, bool, error) {
	var value any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, false, fmt.Errorf("decode stored value: %w", err)
		}
	}
	converted, outcome := a.context.convert(a.mode, value)
	switch outcome {
	case outcomeEmpty:
		a.stats.Empty++
		return nil, false, nil
	case outcomeOK:
		a.stats.OK++
	case outcomeLossy:
		a.stats.Lossy++
	case outcomeLost:
		a.stats.Lost++
		return nil, false, nil
	}
	if a.context.optionTarget {
		name, _ := converted.(string)
		key := domain.FoldKey(name)
		if _, exists := a.distinct[key]; !exists {
			a.distinct[key] = name
		}
	}
	encoded, err := json.Marshal(converted)
	if err != nil {
		return nil, false, fmt.Errorf("encode converted value: %w", err)
	}
	return encoded, true, nil
}

func (a *conversionAccumulator) finalize() {
	if a.context.optionTarget {
		a.stats.DistinctValues = int64(len(a.distinct))
		a.stats.NewOptions = int64(len(a.distinct))
	}
}

func (a *conversionAccumulator) sortedDistincts() []string {
	if len(a.distinct) == 0 {
		return nil
	}
	keys := make([]string, 0, len(a.distinct))
	for key := range a.distinct {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (a *conversionAccumulator) sortedNames() []string {
	keys := a.sortedDistincts()
	names := make([]string, 0, len(keys))
	for _, key := range keys {
		names = append(names, a.distinct[key])
	}
	return names
}

func (c conversionContext) convert(mode string, value any) (any, valueOutcome) {
	if value == nil {
		return nil, outcomeEmpty
	}
	switch mode {
	case "preserve":
		text, ok := value.(string)
		if !ok {
			return nil, outcomeLost
		}
		return text, outcomeOK
	case "dropOverlong":
		text, ok := value.(string)
		if !ok || utf8.RuneCountInString(text) > c.textLimit {
			return nil, outcomeLost
		}
		return text, outcomeOK
	case "parse":
		return c.parseText(value)
	case "distinctOptions":
		return c.distinctOption(value)
	case "format":
		return scalarString(value), outcomeOK
	case "numeric":
		checked, ok := value.(bool)
		if !ok {
			return nil, outcomeLost
		}
		if checked {
			return 1.0, outcomeOK
		}
		return 0.0, outcomeOK
	case "nonzero":
		number, ok := value.(float64)
		if !ok {
			return nil, outcomeLost
		}
		return number != 0, outcomeOK
	case "zeroOne":
		number, ok := value.(float64)
		if !ok || (number != 0 && number != 1) {
			return nil, outcomeLost
		}
		return number == 1, outcomeOK
	case "optionName":
		optionID, ok := value.(string)
		if !ok {
			return nil, outcomeLost
		}
		name, found := c.optionNames[optionID]
		if !found {
			return nil, outcomeLost
		}
		return name, outcomeOK
	case "joinNames":
		entries, ok := value.([]any)
		if !ok {
			return nil, outcomeLost
		}
		if len(entries) == 0 {
			return nil, outcomeEmpty
		}
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			optionID, ok := entry.(string)
			if !ok {
				return nil, outcomeLost
			}
			name, found := c.optionNames[optionID]
			if !found {
				return nil, outcomeLost
			}
			names = append(names, name)
		}
		joined := strings.Join(names, ", ")
		if utf8.RuneCountInString(joined) > c.textLimit {
			return nil, outcomeLost
		}
		return joined, outcomeOK
	case "singleElement":
		optionID, ok := value.(string)
		if !ok {
			return nil, outcomeLost
		}
		return []any{optionID}, outcomeOK
	case "firstOption":
		entries, ok := value.([]any)
		if !ok {
			return nil, outcomeLost
		}
		if len(entries) == 0 {
			return nil, outcomeEmpty
		}
		if len(entries) > 1 {
			return entries[0], outcomeLossy
		}
		return entries[0], outcomeOK
	default:
		return nil, outcomeLost
	}
}

func (c conversionContext) parseText(value any) (any, valueOutcome) {
	text, ok := value.(string)
	if !ok {
		return nil, outcomeLost
	}
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil, outcomeEmpty
	}
	switch c.targetType {
	case "number":
		number, err := strconv.ParseFloat(trimmed, 64)
		if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
			return nil, outcomeLost
		}
		return number, outcomeOK
	case "checkbox":
		switch strings.ToLower(trimmed) {
		case "true", "1", "yes":
			return true, outcomeOK
		case "false", "0", "no":
			return false, outcomeOK
		default:
			return nil, outcomeLost
		}
	case "date":
		parsed, err := time.Parse("2006-01-02", trimmed)
		if err != nil || parsed.Format("2006-01-02") != trimmed {
			return nil, outcomeLost
		}
		return trimmed, outcomeOK
	case "url":
		if utf8.RuneCountInString(trimmed) > 2048 {
			return nil, outcomeLost
		}
		parsed, err := url.Parse(trimmed)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return nil, outcomeLost
		}
		return trimmed, outcomeOK
	default:
		return nil, outcomeLost
	}
}

func (c conversionContext) distinctOption(value any) (any, valueOutcome) {
	text := scalarString(value)
	if strings.TrimSpace(text) == "" {
		return nil, outcomeEmpty
	}
	name, err := domain.NormalizeOptionName("/value", text)
	if err != nil {
		return nil, outcomeLost
	}
	return name, outcomeOK
}

func scalarString(value any) string {
	switch current := value.(type) {
	case string:
		return current
	case float64:
		return strconv.FormatFloat(current, 'g', -1, 64)
	case bool:
		return strconv.FormatBool(current)
	default:
		return ""
	}
}

func (s *Service) buildConvertedConfig(field domain.Field, targetType string, names []string, nameToID map[string]string) (json.RawMessage, error) {
	if !isSelectTarget(targetType) {
		return json.Marshal(domain.EmptyFieldConfig{})
	}
	if (field.Type == "select" || field.Type == "multiSelect") && isSelectTarget(targetType) {
		config, _ := field.Config.(domain.SelectFieldConfig)
		if config.Options == nil {
			config.Options = make([]domain.SelectOption, 0)
		}
		if config.DeletedOptions == nil {
			config.DeletedOptions = make([]domain.DeletedSelectOption, 0)
		}
		return json.Marshal(config)
	}
	if len(names) > 500 {
		return nil, &domain.ResourceLimitError{Resource: "select option", ParentType: "field", ParentID: field.ID, Limit: 500}
	}
	palette := []string{"blue", "green", "red", "orange", "yellow", "purple", "pink", "cyan", "gray"}
	options := make([]domain.SelectOption, 0, len(names))
	for index, name := range names {
		optionID, err := s.newID(id.OptionPrefix)
		if err != nil {
			return nil, fmt.Errorf("generate option ID: %w", err)
		}
		options = append(options, domain.SelectOption{ID: optionID, Name: name, Color: palette[index%len(palette)]})
		nameToID[domain.FoldKey(name)] = optionID
	}
	return json.Marshal(domain.SelectFieldConfig{Options: options, DeletedOptions: make([]domain.DeletedSelectOption, 0)})
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
