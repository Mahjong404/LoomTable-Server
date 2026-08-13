package domain

import (
	"encoding/json"
	"fmt"
	"time"
)

type Workspace struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Revision  int64     `json:"revision"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type Base struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspaceId"`
	Name        string    `json:"name"`
	Revision    int64     `json:"revision"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type Table struct {
	ID             string     `json:"id"`
	BaseID         string     `json:"baseId"`
	Name           string     `json:"name"`
	PrimaryFieldID string     `json:"primaryFieldId"`
	Revision       int64      `json:"revision"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
	DeletedAt      *time.Time `json:"deletedAt,omitempty"`
}

type Field struct {
	ID            string     `json:"id"`
	TableID       string     `json:"tableId"`
	Name          string     `json:"name"`
	Position      int        `json:"position"`
	SchemaVersion int        `json:"schemaVersion"`
	Revision      int64      `json:"revision"`
	Type          string     `json:"type"`
	Config        any        `json:"config"`
	DeletedAt     *time.Time `json:"deletedAt,omitempty"`
}

func (field *Field) UnmarshalJSON(data []byte) error {
	type wireField Field
	var wire wireField
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	var envelope struct {
		Config json.RawMessage `json:"config"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return err
	}
	config, err := decodeFieldConfig(wire.Type, envelope.Config)
	if err != nil {
		return err
	}
	wire.Config = config
	*field = Field(wire)
	return nil
}

func decodeFieldConfig(fieldType string, raw json.RawMessage) (any, error) {
	switch fieldType {
	case "select", "multiSelect":
		var config SelectFieldConfig
		if err := json.Unmarshal(raw, &config); err != nil {
			return nil, fmt.Errorf("decode %s Field config: %w", fieldType, err)
		}
		return config, nil
	case "text", "longText", "number", "checkbox", "date", "url", "location":
		var config EmptyFieldConfig
		if err := json.Unmarshal(raw, &config); err != nil {
			return nil, fmt.Errorf("decode %s Field config: %w", fieldType, err)
		}
		return config, nil
	default:
		return nil, fmt.Errorf("unsupported Field type %q", fieldType)
	}
}

type EmptyFieldConfig struct{}

type SelectOption struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

type DeletedSelectOption struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Color     string    `json:"color"`
	DeletedAt time.Time `json:"deletedAt"`
}

type SelectFieldConfig struct {
	Options        []SelectOption        `json:"options"`
	DeletedOptions []DeletedSelectOption `json:"deletedOptions"`
}

type SortSpec struct {
	FieldID   string `json:"fieldId"`
	Direction string `json:"direction"`
	Nulls     string `json:"nulls,omitempty"`
}

type FilterNode struct {
	Kind     string          `json:"kind"`
	Operator string          `json:"operator,omitempty"`
	Children []FilterNode    `json:"children,omitempty"`
	FieldID  string          `json:"fieldId,omitempty"`
	Value    json.RawMessage `json:"value,omitempty"`
}

type GridViewConfig struct {
	Projection     []string       `json:"projection"`
	ColumnOrder    []string       `json:"columnOrder"`
	ColumnWidths   map[string]int `json:"columnWidths"`
	FrozenFieldIDs []string       `json:"frozenFieldIds"`
	RowHeight      string         `json:"rowHeight"`
	Filter         *FilterNode    `json:"filter,omitempty"`
	Sort           []SortSpec     `json:"sort"`
}

type MapCenter struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

type MapViewConfig struct {
	LocationFieldID string      `json:"locationFieldId"`
	Filter          *FilterNode `json:"filter,omitempty"`
	Center          *MapCenter  `json:"center,omitempty"`
	Zoom            *float64    `json:"zoom,omitempty"`
}

type View struct {
	ID        string     `json:"id"`
	TableID   string     `json:"tableId"`
	Name      string     `json:"name"`
	Type      string     `json:"type"`
	Config    any        `json:"config"`
	Revision  int64      `json:"revision"`
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`
	DeletedAt *time.Time `json:"deletedAt,omitempty"`
}

func (view *View) UnmarshalJSON(data []byte) error {
	type wireView View
	var wire wireView
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	var envelope struct {
		Config json.RawMessage `json:"config"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return err
	}
	var config any
	switch wire.Type {
	case "grid":
		var typed GridViewConfig
		if err := json.Unmarshal(envelope.Config, &typed); err != nil {
			return fmt.Errorf("decode Grid View config: %w", err)
		}
		config = typed
	case "map":
		var typed MapViewConfig
		if err := json.Unmarshal(envelope.Config, &typed); err != nil {
			return fmt.Errorf("decode Map View config: %w", err)
		}
		config = typed
	default:
		return fmt.Errorf("unsupported View type %q", wire.Type)
	}
	wire.Config = config
	*view = View(wire)
	return nil
}

type GridView struct {
	ID        string         `json:"id"`
	TableID   string         `json:"tableId"`
	Name      string         `json:"name"`
	Type      string         `json:"type"`
	Config    GridViewConfig `json:"config"`
	Revision  int64          `json:"revision"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
	DeletedAt *time.Time     `json:"deletedAt,omitempty"`
}

type CreateTableResult struct {
	Table        Table    `json:"table"`
	PrimaryField Field    `json:"primaryField"`
	InitialView  GridView `json:"initialView"`
}
