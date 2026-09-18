package record

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestDiffRecordValues(t *testing.T) {
	raw := func(t *testing.T, value string) json.RawMessage {
		t.Helper()
		return json.RawMessage(value)
	}
	cases := []struct {
		name   string
		before map[string]any
		after  map[string]any
		want   []FieldChange
	}{
		{
			name:   "both empty",
			before: map[string]any{},
			after:  map[string]any{},
			want:   nil,
		},
		{
			name:   "unchanged values are skipped",
			before: map[string]any{"fld_a": "x", "fld_b": float64(1)},
			after:  map[string]any{"fld_a": "x", "fld_b": float64(1)},
			want:   nil,
		},
		{
			name:   "scalar change",
			before: map[string]any{"fld_a": "old"},
			after:  map[string]any{"fld_a": "new"},
			want: []FieldChange{
				{FieldID: "fld_a", Before: raw(t, `"old"`), After: raw(t, `"new"`)},
			},
		},
		{
			name:   "set from unset",
			before: map[string]any{},
			after:  map[string]any{"fld_a": "x"},
			want: []FieldChange{
				{FieldID: "fld_a", After: raw(t, `"x"`)},
			},
		},
		{
			name:   "unset removes key",
			before: map[string]any{"fld_a": "x"},
			after:  map[string]any{},
			want: []FieldChange{
				{FieldID: "fld_a", Before: raw(t, `"x"`)},
			},
		},
		{
			name:   "explicit null is a change not an unset",
			before: map[string]any{"fld_a": "x"},
			after:  map[string]any{"fld_a": nil},
			want: []FieldChange{
				{FieldID: "fld_a", Before: raw(t, `"x"`), After: raw(t, `null`)},
			},
		},
		{
			name:   "null to value",
			before: map[string]any{"fld_a": nil},
			after:  map[string]any{"fld_a": "x"},
			want: []FieldChange{
				{FieldID: "fld_a", Before: raw(t, `null`), After: raw(t, `"x"`)},
			},
		},
		{
			name:   "null to unset",
			before: map[string]any{"fld_a": nil},
			after:  map[string]any{},
			want: []FieldChange{
				{FieldID: "fld_a", Before: raw(t, `null`)},
			},
		},
		{
			name:   "null equals null",
			before: map[string]any{"fld_a": nil},
			after:  map[string]any{"fld_a": nil},
			want:   nil,
		},
		{
			name:   "array and object values",
			before: map[string]any{"fld_a": []any{"opt_1", "opt_2"}, "fld_b": map[string]any{"lat": float64(1)}},
			after:  map[string]any{"fld_a": []any{"opt_2"}, "fld_b": map[string]any{"lat": float64(2)}},
			want: []FieldChange{
				{FieldID: "fld_a", Before: raw(t, `["opt_1","opt_2"]`), After: raw(t, `["opt_2"]`)},
				{FieldID: "fld_b", Before: raw(t, `{"lat":1}`), After: raw(t, `{"lat":2}`)},
			},
		},
		{
			name:   "multiple fields sorted deterministically",
			before: map[string]any{"fld_b": 1.0, "fld_a": "x", "fld_c": true},
			after:  map[string]any{"fld_b": 2.0, "fld_a": "y", "fld_c": true},
			want: []FieldChange{
				{FieldID: "fld_a", Before: raw(t, `"x"`), After: raw(t, `"y"`)},
				{FieldID: "fld_b", Before: raw(t, `1`), After: raw(t, `2`)},
			},
		},
		{
			name:   "numeric equality treats 1 and 1.0 as equal",
			before: map[string]any{"fld_a": float64(1)},
			after:  map[string]any{"fld_a": float64(1.0)},
			want:   nil,
		},
	}
	for _, current := range cases {
		t.Run(current.name, func(t *testing.T) {
			got, err := DiffRecordValues(current.before, current.after)
			if err != nil {
				t.Fatalf("DiffRecordValues error: %v", err)
			}
			if !reflect.DeepEqual(got, current.want) {
				gotJSON, _ := json.Marshal(got)
				wantJSON, _ := json.Marshal(current.want)
				t.Fatalf("DiffRecordValues = %s, want %s", gotJSON, wantJSON)
			}
		})
	}
}
