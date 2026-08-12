package record

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/Mahjong404/LoomTable-Server/internal/domain"
)

const (
	textFieldID     = "fld_00000000000000000000000000"
	locationFieldID = "fld_00000000000000000000000001"
	selectFieldID   = "fld_00000000000000000000000002"
	deletedFieldID  = "fld_00000000000000000000000003"
	activeOptionID  = "opt_00000000000000000000000000"
	deletedOptionID = "opt_00000000000000000000000001"
)

func testFields() map[string]FieldDefinition {
	deletedAt := time.Unix(2, 0).UTC()
	return map[string]FieldDefinition{
		textFieldID:     {ID: textFieldID, Type: "text", Config: json.RawMessage(`{}`)},
		locationFieldID: {ID: locationFieldID, Type: "location", Config: json.RawMessage(`{}`)},
		selectFieldID: {
			ID:   selectFieldID,
			Type: "select",
			Config: json.RawMessage(`{
				"options":[{"id":"` + activeOptionID + `","name":"Active","color":"blue"}],
				"deletedOptions":[{"id":"` + deletedOptionID + `","name":"Old","color":"gray","deletedAt":"2026-01-01T00:00:00Z"}]
			}`),
		},
		deletedFieldID: {ID: deletedFieldID, Type: "text", Config: json.RawMessage(`{}`), DeletedAt: &deletedAt},
	}
}

func TestNormalizeCreateValuesAndProjection(t *testing.T) {
	canonical, query, search, err := NormalizeCreateValues(map[string]any{
		textFieldID: "Straße",
		locationFieldID: map[string]any{
			"label":     "  Cafe\u0301  ",
			"lat":       23.1,
			"lng":       113.2,
			"precision": "exact",
		},
		selectFieldID: activeOptionID,
	}, testFields())
	if err != nil {
		t.Fatal(err)
	}
	location := canonical[locationFieldID].(map[string]any)
	if location["label"] != "Café" {
		t.Fatalf("normalized Location = %#v", location)
	}
	if query[textFieldID] != "strasse" || search != "strasse" {
		t.Fatalf("query = %#v, search = %q", query, search)
	}
}

func TestNormalizeUpdatedValuesAllowsHistoricalReferencesButRejectsNewDeletedOption(t *testing.T) {
	current := map[string]any{
		selectFieldID:  deletedOptionID,
		deletedFieldID: "historical",
	}
	canonical, _, _, err := NormalizeUpdatedValues(current, map[string]any{selectFieldID: deletedOptionID}, nil, testFields())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(canonical, current) {
		t.Fatalf("canonical = %#v, want %#v", canonical, current)
	}

	_, _, _, err = NormalizeCreateValues(map[string]any{selectFieldID: deletedOptionID}, testFields())
	var validation *domain.ValidationError
	if !errors.As(err, &validation) || validation.Issues[0].Code != "invalidReference" {
		t.Fatalf("error = %#v, want invalidReference", err)
	}
}

func TestNormalizeUpdatedValuesRejectsSetUnsetOverlap(t *testing.T) {
	_, _, _, err := NormalizeUpdatedValues(
		map[string]any{},
		map[string]any{textFieldID: "new"},
		[]string{textFieldID},
		testFields(),
	)
	var validation *domain.ValidationError
	if !errors.As(err, &validation) || validation.Issues[0].Code != "duplicate" {
		t.Fatalf("error = %#v, want duplicate", err)
	}
}

func TestNormalizeCreateValuesRejectsInvalidLocation(t *testing.T) {
	_, _, _, err := NormalizeCreateValues(map[string]any{
		locationFieldID: map[string]any{"precision": "exact"},
	}, testFields())
	var validation *domain.ValidationError
	if !errors.As(err, &validation) || validation.Issues[0].Code != "required" {
		t.Fatalf("error = %#v, want required Location issue", err)
	}
}
