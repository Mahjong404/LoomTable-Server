package domain

type ViewOrder struct {
	TableID  string   `json:"tableId"`
	Revision int64    `json:"revision"`
	ViewIDs  []string `json:"viewIds"`
}

type ViewListSnapshot struct {
	Items []View    `json:"items"`
	Order ViewOrder `json:"order"`
}

type ViewOrderResult struct {
	ClientMutationID string    `json:"clientMutationId"`
	Status           string    `json:"status"`
	Order            ViewOrder `json:"order"`
}

type ViewOrderConflictError struct {
	TableID               string    `json:"tableId"`
	ExpectedOrderRevision int64     `json:"expectedOrderRevision"`
	CurrentOrder          ViewOrder `json:"currentOrder"`
}

func (e *ViewOrderConflictError) Error() string {
	return "expectedOrderRevision does not match the current View Order revision"
}
