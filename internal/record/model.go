package record

import (
	"encoding/json"
	"time"

	"github.com/Mahjong404/LoomTable-Server/internal/domain"
)

type Record struct {
	ID        string         `json:"id"`
	TableID   string         `json:"tableId"`
	Revision  int64          `json:"revision"`
	Values    map[string]any `json:"values"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
	DeletedAt *time.Time     `json:"deletedAt,omitempty"`
}

type Command struct {
	Kind               string         `json:"kind"`
	RecordID           string         `json:"recordId,omitempty"`
	ExpectedRevision   int64          `json:"expectedRevision,omitempty"`
	Values             map[string]any `json:"values,omitempty"`
	Set                map[string]any `json:"set,omitempty"`
	UnsetFieldIDs      []string       `json:"unsetFieldIds,omitempty"`
	ValuesPresent      bool           `json:"-"`
	SetPresent         bool           `json:"-"`
	UnsetFieldsPresent bool           `json:"-"`
}

type CommandResult struct {
	Index  int    `json:"index"`
	Status string `json:"status"`
	Record Record `json:"record"`
}

type MutationResult struct {
	ClientMutationID string          `json:"clientMutationId"`
	Results          []CommandResult `json:"results"`
	ChangeCursor     string          `json:"changeCursor"`
}

type StoredMutationResult struct {
	ClientMutationID string          `json:"clientMutationId"`
	Results          []CommandResult `json:"results"`
	ChangeSequence   int64           `json:"changeSequence"`
}

type FieldDefinition struct {
	ID        string
	Type      string
	Config    json.RawMessage
	DeletedAt *time.Time
	IsPrimary bool
	Revision  int64
	Position  int
}

type QueryRequest struct {
	ViewID            string
	ViewIDPresent     bool
	Lifecycle         string
	Cursor            string
	Limit             int
	Projection        []string
	ProjectionPresent bool
	Filter            *domain.FilterNode
	FilterPresent     bool
	Sort              []domain.SortSpec
	SortPresent       bool
	Search            string
	SearchPresent     bool
}

type QueryMetadata struct {
	TableID        string
	PrimaryFieldID string
	Fields         map[string]FieldDefinition
	View           *domain.View
}

type QueryPlan struct {
	Lifecycle   string
	Limit       int
	Projection  []string
	Filter      *domain.FilterNode
	Sort        []domain.SortSpec
	Search      string
	Fields      map[string]FieldDefinition
	Fingerprint string
	SchemaHash  string
}

type QueryPosition struct {
	SortValues []any  `json:"sortValues"`
	RecordID   string `json:"recordId"`
}

type StoredQueryPage struct {
	Items          []Record
	HasMore        bool
	NextPosition   *QueryPosition
	ChangeSequence int64
	TotalCount     *int64
}

type QueryResult struct {
	Items        []Record `json:"items"`
	NextCursor   string   `json:"nextCursor,omitempty"`
	HasMore      bool     `json:"hasMore"`
	ChangeCursor string   `json:"changeCursor"`
	TotalCount   *int64   `json:"totalCount,omitempty"`
}

type Change struct {
	ID         string    `json:"id"`
	Kind       string    `json:"kind"`
	TableID    string    `json:"tableId"`
	RecordID   string    `json:"recordId,omitempty"`
	ObjectID   string    `json:"objectId,omitempty"`
	Revision   int64     `json:"revision"`
	ActorID    string    `json:"actorId"`
	OccurredAt time.Time `json:"occurredAt"`
	Sequence   int64     `json:"-"`
}

type StoredChangePage struct {
	Items        []Change
	NextSequence int64
	HasMore      bool
}

type ChangePage struct {
	Items      []Change `json:"items"`
	NextCursor string   `json:"nextCursor"`
	HasMore    bool     `json:"hasMore"`
}

type MapCoordinate struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

type MapViewportBox struct {
	West  float64 `json:"west"`
	South float64 `json:"south"`
	East  float64 `json:"east"`
	North float64 `json:"north"`
}

type MapViewport struct {
	Boxes []MapViewportBox `json:"boxes"`
}

type MapQueryRequest struct {
	Viewport    MapViewport `json:"viewport"`
	Zoom        float64     `json:"zoom"`
	PixelWidth  int         `json:"pixelWidth"`
	PixelHeight int         `json:"pixelHeight"`
}

type MapPoint struct {
	Kind             string        `json:"kind"`
	RecordID         string        `json:"recordId"`
	Position         MapCoordinate `json:"position"`
	PrimaryFieldText string        `json:"primaryFieldText"`
}

type MapCluster struct {
	Kind              string        `json:"kind"`
	ClusterID         string        `json:"clusterId"`
	Position          MapCoordinate `json:"position"`
	Bounds            MapViewport   `json:"bounds"`
	PointCount        int           `json:"pointCount"`
	ExpansionZoom     *float64      `json:"expansionZoom,omitempty"`
	RecordsQueryToken string        `json:"recordsQueryToken"`
}

type MapQueryResult struct {
	Features                      []any  `json:"features"`
	ViewportRenderableRecordCount int64  `json:"viewportRenderableRecordCount"`
	ViewRevision                  int64  `json:"viewRevision"`
	ChangeCursor                  string `json:"changeCursor"`
}

type MapQuerySummary struct {
	MatchedRecordCount      int64        `json:"matchedRecordCount"`
	RenderableRecordCount   int64        `json:"renderableRecordCount"`
	UnlocatedRecordCount    int64        `json:"unlocatedRecordCount"`
	UnrenderableRecordCount int64        `json:"unrenderableRecordCount"`
	DataBounds              *MapViewport `json:"dataBounds,omitempty"`
}

type MapSummaryResult struct {
	Summary      MapQuerySummary `json:"summary"`
	ViewRevision int64           `json:"viewRevision"`
	ChangeCursor string          `json:"changeCursor"`
}

type MapRecord struct {
	Record
	Position         *MapCoordinate
	PrimaryFieldText string
}

type StoredMapSnapshot struct {
	Records        []MapRecord
	ChangeSequence int64
}

type MapClusterRecordsRequest struct {
	ClusterToken string
	Cursor       string
	Limit        int
}

type Conflict struct {
	RecordID          string         `json:"recordId"`
	ExpectedRevision  int64          `json:"expectedRevision"`
	CurrentRevision   int64          `json:"currentRevision"`
	CurrentValues     map[string]any `json:"currentValues"`
	SubmittedSet      map[string]any `json:"submittedSet,omitempty"`
	SubmittedUnsetIDs []string       `json:"submittedUnsetFieldIds,omitempty"`
}

type ConflictError struct {
	ClientMutationID   string
	FailedCommandIndex int
	Conflicts          []Conflict
}

func (e *ConflictError) Error() string {
	return "one or more records have a revision conflict"
}
