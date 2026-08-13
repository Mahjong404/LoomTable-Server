package domain

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestFieldJSONRoundTripPreservesTypedConfig(t *testing.T) {
	want := Field{
		ID:   "fld_00000000000000000000000000",
		Type: "select",
		Config: SelectFieldConfig{Options: []SelectOption{{
			ID: "opt_00000000000000000000000000", Name: "Open", Color: "blue",
		}}, DeletedOptions: []DeletedSelectOption{}},
	}

	raw, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got Field
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Config, want.Config) {
		t.Fatalf("config = %#v (%T), want %#v (%T)", got.Config, got.Config, want.Config, want.Config)
	}
}

func TestViewJSONRoundTripPreservesTypedConfig(t *testing.T) {
	zoom := 6.5
	want := View{
		ID:   "view_00000000000000000000000000",
		Type: "map",
		Config: MapViewConfig{
			LocationFieldID: "fld_00000000000000000000000000",
			Center:          &MapCenter{Lat: 31.2, Lng: 121.5},
			Zoom:            &zoom,
		},
	}

	raw, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got View
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Config, want.Config) {
		t.Fatalf("config = %#v (%T), want %#v (%T)", got.Config, got.Config, want.Config, want.Config)
	}
}
