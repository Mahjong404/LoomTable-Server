package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Mahjong404/LoomTable-Server/internal/domain"
)

func conversionStore(field domain.Field, values ...string) *captureStore {
	raw := make([]json.RawMessage, 0, len(values))
	for _, value := range values {
		if value == "" {
			raw = append(raw, nil)
			continue
		}
		raw = append(raw, json.RawMessage(value))
	}
	return &captureStore{currentField: field, fieldValues: raw}
}

func textField() domain.Field {
	return domain.Field{
		ID: "fld_00000000000000000000000000", TableID: "tbl_00000000000000000000000000",
		Name: "Title", Type: "text", Revision: 3, Config: domain.EmptyFieldConfig{},
	}
}

func TestPreviewFieldConversionTextToSelect(t *testing.T) {
	store := conversionStore(textField(), `"Open"`, `"Closed"`, `"Open"`, "", `"  padded  "`)
	service := NewWithIDGenerator(store, fixedID)

	preview, err := service.PreviewFieldConversion(context.Background(), "act_test", store.currentField.ID, "select")
	if err != nil {
		t.Fatal(err)
	}
	if !preview.Supported || len(preview.Modes) != 1 {
		t.Fatalf("preview = %#v", preview)
	}
	mode := preview.Modes[0]
	if mode.ID != "distinctOptions" || mode.Disabled {
		t.Fatalf("mode = %#v", mode)
	}
	if mode.Stats.OK != 4 || mode.Stats.Empty != 1 || mode.Stats.DistinctValues != 3 || mode.Stats.NewOptions != 3 {
		t.Fatalf("stats = %#v", mode.Stats)
	}
	if preview.TotalRecords != 5 || preview.PreviewToken == "" {
		t.Fatalf("preview = %#v", preview)
	}
}

func TestPreviewFieldConversionUnsupportedPair(t *testing.T) {
	field := textField()
	field.Type = "location"
	store := conversionStore(field, `{"label":"here"}`)
	service := NewWithIDGenerator(store, fixedID)

	preview, err := service.PreviewFieldConversion(context.Background(), "act_test", field.ID, "number")
	if err != nil {
		t.Fatal(err)
	}
	if preview.Supported || preview.Reason == "" {
		t.Fatalf("preview = %#v", preview)
	}
}

func TestPreviewFieldConversionPrimaryField(t *testing.T) {
	store := conversionStore(textField(), `"abc"`)
	store.fieldPrimary = true
	service := NewWithIDGenerator(store, fixedID)

	preview, err := service.PreviewFieldConversion(context.Background(), "act_test", store.currentField.ID, "number")
	if err != nil {
		t.Fatal(err)
	}
	if preview.Supported || preview.Reason == "" {
		t.Fatalf("preview = %#v", preview)
	}
}

func TestPreviewFieldConversionUnknownType(t *testing.T) {
	store := conversionStore(textField())
	service := NewWithIDGenerator(store, fixedID)

	if _, err := service.PreviewFieldConversion(context.Background(), "act_test", store.currentField.ID, "bogus"); err == nil {
		t.Fatal("expected unsupported field type error")
	} else {
		var unsupported *domain.UnsupportedFieldTypeError
		if !errors.As(err, &unsupported) {
			t.Fatalf("error = %T %v", err, err)
		}
	}
}

func TestConvertFieldTextToSelect(t *testing.T) {
	store := conversionStore(textField(), `"Open"`, `"Closed"`, `"Open"`, "")
	service := NewWithIDGenerator(store, fixedID)
	preview, err := service.PreviewFieldConversion(context.Background(), "act_test", store.currentField.ID, "select")
	if err != nil {
		t.Fatal(err)
	}

	result, err := service.ConvertField(context.Background(), "act_test", store.currentField.ID, ConversionRequest{
		TargetType: "select", Mode: "distinctOptions",
		ExpectedRevision: store.currentField.Revision, PreviewToken: preview.PreviewToken,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Stats.OK != 3 || result.Stats.Empty != 1 || result.Stats.NewOptions != 2 {
		t.Fatalf("stats = %#v", result.Stats)
	}
	if result.Field.Type != "select" || result.Field.Revision != 4 {
		t.Fatalf("field = %#v", result.Field)
	}
	config, ok := result.Field.Config.(json.RawMessage)
	if !ok {
		t.Fatalf("config type = %T", result.Field.Config)
	}
	var decoded domain.SelectFieldConfig
	if err := json.Unmarshal(config, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Options) != 2 {
		t.Fatalf("options = %#v", decoded.Options)
	}
	names := map[string]bool{}
	for _, option := range decoded.Options {
		if option.ID == "" || option.Color == "" {
			t.Fatalf("option = %#v", option)
		}
		names[option.Name] = true
	}
	if !names["Closed"] || !names["Open"] {
		t.Fatalf("options = %#v", decoded.Options)
	}
}

func TestConvertFieldRejectsStalePreview(t *testing.T) {
	store := conversionStore(textField(), `"1"`, `"2"`)
	service := NewWithIDGenerator(store, fixedID)
	preview, err := service.PreviewFieldConversion(context.Background(), "act_test", store.currentField.ID, "number")
	if err != nil {
		t.Fatal(err)
	}
	store.fieldValues = append(store.fieldValues, json.RawMessage(`"3"`))

	if _, err := service.ConvertField(context.Background(), "act_test", store.currentField.ID, ConversionRequest{
		TargetType: "number", Mode: "parse",
		ExpectedRevision: store.currentField.Revision, PreviewToken: preview.PreviewToken,
	}); err == nil {
		t.Fatal("expected stale preview error")
	} else {
		var stale *domain.ConversionPreviewStaleError
		if !errors.As(err, &stale) {
			t.Fatalf("error = %T %v", err, err)
		}
	}
}

func TestConvertFieldRejectsInvalidToken(t *testing.T) {
	store := conversionStore(textField(), `"1"`)
	service := NewWithIDGenerator(store, fixedID)

	if _, err := service.ConvertField(context.Background(), "act_test", store.currentField.ID, ConversionRequest{
		TargetType: "number", Mode: "parse",
		ExpectedRevision: store.currentField.Revision, PreviewToken: "garbage",
	}); err == nil {
		t.Fatal("expected invalid preview token error")
	} else {
		var invalid *domain.InvalidPreviewTokenError
		if !errors.As(err, &invalid) {
			t.Fatalf("error = %T %v", err, err)
		}
	}
}

func TestConvertFieldRejectsUnknownMode(t *testing.T) {
	store := conversionStore(textField(), `"1"`)
	service := NewWithIDGenerator(store, fixedID)
	preview, err := service.PreviewFieldConversion(context.Background(), "act_test", store.currentField.ID, "number")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.ConvertField(context.Background(), "act_test", store.currentField.ID, ConversionRequest{
		TargetType: "number", Mode: "bogus",
		ExpectedRevision: store.currentField.Revision, PreviewToken: preview.PreviewToken,
	}); err == nil {
		t.Fatal("expected validation error for unknown mode")
	}
}

func TestConvertLongTextToTextDropsOverlong(t *testing.T) {
	field := textField()
	field.Type = "longText"
	overlong := `"` + strings.Repeat("x", 10001) + `"`
	store := conversionStore(field, `"short"`, overlong)
	service := NewWithIDGenerator(store, fixedID)

	preview, err := service.PreviewFieldConversion(context.Background(), "act_test", field.ID, "text")
	if err != nil {
		t.Fatal(err)
	}
	mode := preview.Modes[0]
	if mode.ID != "dropOverlong" || mode.Stats.OK != 1 || mode.Stats.Lost != 1 {
		t.Fatalf("mode = %#v", mode)
	}
}

func TestConvertNumberToCheckboxModes(t *testing.T) {
	field := textField()
	field.Type = "number"
	store := conversionStore(field, `0`, `1`, `5`, "")
	service := NewWithIDGenerator(store, fixedID)

	preview, err := service.PreviewFieldConversion(context.Background(), "act_test", field.ID, "checkbox")
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Modes) != 2 {
		t.Fatalf("modes = %#v", preview.Modes)
	}
	var nonzero, zeroOne *ConversionMode
	for index := range preview.Modes {
		switch preview.Modes[index].ID {
		case "nonzero":
			nonzero = &preview.Modes[index]
		case "zeroOne":
			zeroOne = &preview.Modes[index]
		}
	}
	if nonzero == nil || zeroOne == nil {
		t.Fatalf("modes = %#v", preview.Modes)
	}
	if nonzero.Stats.OK != 3 || nonzero.Stats.Lost != 0 {
		t.Fatalf("nonzero stats = %#v", nonzero.Stats)
	}
	if zeroOne.Stats.OK != 2 || zeroOne.Stats.Lost != 1 {
		t.Fatalf("zeroOne stats = %#v", zeroOne.Stats)
	}
}

func TestConvertMultiSelectToSelectFirstOption(t *testing.T) {
	field := textField()
	field.Type = "multiSelect"
	field.Config = domain.SelectFieldConfig{
		Options: []domain.SelectOption{
			{ID: "opt_00000000000000000000000001", Name: "A", Color: "red"},
			{ID: "opt_00000000000000000000000002", Name: "B", Color: "blue"},
		},
		DeletedOptions: []domain.DeletedSelectOption{},
	}
	store := conversionStore(field,
		`["opt_00000000000000000000000001","opt_00000000000000000000000002"]`,
		`["opt_00000000000000000000000001"]`,
		`[]`,
	)
	service := NewWithIDGenerator(store, fixedID)

	preview, err := service.PreviewFieldConversion(context.Background(), "act_test", field.ID, "select")
	if err != nil {
		t.Fatal(err)
	}
	if !preview.Supported || len(preview.Modes) != 1 || preview.Modes[0].ID != "firstOption" {
		t.Fatalf("preview = %#v", preview)
	}
	stats := preview.Modes[0].Stats
	if stats.OK != 1 || stats.Lossy != 1 || stats.Empty != 1 {
		t.Fatalf("stats = %#v", stats)
	}
}

func TestParseTextValues(t *testing.T) {
	context := conversionContext{sourceType: "text", targetType: "number", textLimit: 10000}
	if value, outcome := context.convert("parse", " 1.5 "); outcome != outcomeOK || value != 1.5 {
		t.Fatalf("number parse = %v %d", value, outcome)
	}
	if _, outcome := context.convert("parse", "abc"); outcome != outcomeLost {
		t.Fatalf("outcome = %d", outcome)
	}
	if _, outcome := context.convert("parse", "   "); outcome != outcomeEmpty {
		t.Fatalf("outcome = %d", outcome)
	}

	context.targetType = "checkbox"
	if value, outcome := context.convert("parse", "YES"); outcome != outcomeOK || value != true {
		t.Fatalf("checkbox parse = %v %d", value, outcome)
	}
	if _, outcome := context.convert("parse", "maybe"); outcome != outcomeLost {
		t.Fatalf("outcome = %d", outcome)
	}

	context.targetType = "date"
	if _, outcome := context.convert("parse", "2026-02-30"); outcome != outcomeLost {
		t.Fatalf("outcome = %d", outcome)
	}
	if _, outcome := context.convert("parse", "2026-02-28"); outcome != outcomeOK {
		t.Fatalf("outcome = %d", outcome)
	}
}

func TestJoinNamesHonoursTextLimit(t *testing.T) {
	context := conversionContext{sourceType: "multiSelect", targetType: "text", textLimit: 10, optionNames: map[string]string{
		"opt_a": "alpha", "opt_b": "beta",
	}}
	if _, outcome := context.convert("joinNames", []any{"opt_a", "opt_b"}); outcome != outcomeLost {
		t.Fatalf("outcome = %d, want lost for overlong join", outcome)
	}
	context.textLimit = 10000
	if value, outcome := context.convert("joinNames", []any{"opt_a", "opt_b"}); outcome != outcomeOK || value != "alpha, beta" {
		t.Fatalf("join = %v %d", value, outcome)
	}
}

func TestDistinctOptionRejectsEmpty(t *testing.T) {
	context := conversionContext{sourceType: "text", targetType: "select", optionTarget: true, textLimit: 10000}
	if _, outcome := context.convert("distinctOptions", "   "); outcome != outcomeEmpty {
		t.Fatalf("outcome = %d", outcome)
	}
	long := make([]rune, 201)
	for index := range long {
		long[index] = 'x'
	}
	if _, outcome := context.convert("distinctOptions", string(long)); outcome != outcomeLost {
		t.Fatalf("outcome = %d, want lost for option name over limit", outcome)
	}
}
