package domain

import "time"

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
	ID            string         `json:"id"`
	TableID       string         `json:"tableId"`
	Name          string         `json:"name"`
	Position      int            `json:"position"`
	SchemaVersion int            `json:"schemaVersion"`
	Revision      int64          `json:"revision"`
	Type          string         `json:"type"`
	Config        map[string]any `json:"config"`
	DeletedAt     *time.Time     `json:"deletedAt,omitempty"`
}

type SortSpec struct {
	FieldID   string `json:"fieldId"`
	Direction string `json:"direction"`
	Nulls     string `json:"nulls,omitempty"`
}

type GridViewConfig struct {
	Projection     []string       `json:"projection"`
	ColumnOrder    []string       `json:"columnOrder"`
	ColumnWidths   map[string]int `json:"columnWidths"`
	FrozenFieldIDs []string       `json:"frozenFieldIds"`
	RowHeight      string         `json:"rowHeight"`
	Sort           []SortSpec     `json:"sort"`
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
