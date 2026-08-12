package record

import (
	"encoding/json"
	"time"
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
