package record

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"time"

	"github.com/Mahjong404/LoomTable-Server/internal/domain"
	"github.com/Mahjong404/LoomTable-Server/internal/id"
)

type AuditFilter struct {
	Category         string     `json:"category"`
	RecordID         string     `json:"recordId,omitempty"`
	FieldID          string     `json:"fieldId,omitempty"`
	ViewID           string     `json:"viewId,omitempty"`
	ActorID          string     `json:"actorId,omitempty"`
	OperationID      string     `json:"operationId,omitempty"`
	ClientMutationID string     `json:"clientMutationId,omitempty"`
	Since            *time.Time `json:"since,omitempty"`
	Until            *time.Time `json:"until,omitempty"`
}
type AuditRequest struct {
	AuditFilter
	Cursor string
	Limit  int
}
type AuditEvent struct {
	ID               string          `json:"eventId"`
	OperationID      *string         `json:"operationId"`
	ChangeIDs        []string        `json:"changeIds"`
	TableID          string          `json:"tableId"`
	ObjectID         string          `json:"objectId"`
	RecordID         string          `json:"recordId,omitempty"`
	FieldID          string          `json:"fieldId,omitempty"`
	ViewID           string          `json:"viewId,omitempty"`
	ActorID          *string         `json:"actorId"`
	ClientMutationID string          `json:"clientMutationId,omitempty"`
	OccurredAt       time.Time       `json:"occurredAt"`
	Category         string          `json:"category"`
	Kind             string          `json:"kind"`
	PayloadVersion   int             `json:"payloadVersion"`
	Payload          json.RawMessage `json:"payload"`
	Sequence         int64           `json:"-"`
}
type AuditAvailability struct {
	LegacyExpiredThrough       int64      `json:"legacyExpiredThrough"`
	ExpiredThroughTime         *time.Time `json:"expiredThroughTime"`
	CapturedSince              time.Time  `json:"capturedSince"`
	EarliestReadableAt         *time.Time `json:"earliestReadableAt"`
	ExpiredThrough             int64      `json:"expiredThrough"`
	HasLegacyEvents            bool       `json:"hasLegacyEvents"`
	UninstrumentedWriter       bool       `json:"uninstrumentedWriter"`
	FullTableRollbackAvailable bool       `json:"fullTableRollbackAvailable"`
}
type AuditPage struct {
	Items        []AuditEvent      `json:"items"`
	Count        int64             `json:"count"`
	HasMore      bool              `json:"hasMore"`
	NextCursor   string            `json:"nextCursor,omitempty"`
	Availability AuditAvailability `json:"availability"`
}
type AuditPosition struct {
	High         int64
	Before       int64
	Watermark    int64
	Continuation bool
}
type StoredAuditPage struct {
	AuditPage
	Position AuditPosition
}
type AuditStore interface {
	ReadAudit(context.Context, string, string, AuditFilter, AuditPosition, int) (StoredAuditPage, error)
}
type auditCursor struct {
	ActorID    string `json:"actorId"`
	TableID    string `json:"tableId"`
	FilterHash string `json:"filterHash"`
	High       int64  `json:"high"`
	Before     int64  `json:"before"`
	Watermark  int64  `json:"watermark"`
	ExpiresAt  int64  `json:"expiresAt"`
}

var operationPattern = regexp.MustCompile(`^op_([0-9A-HJKMNP-TV-Z]{26}|[0-9a-f]{32})$`)

func (s *Service) Audit(ctx context.Context, actorID, tableID string, request AuditRequest) (AuditPage, error) {
	if !id.Valid(id.TablePrefix, tableID) {
		return AuditPage{}, &domain.BadRequestError{Message: "tableId has an invalid typed ID"}
	}
	if request.Category == "" {
		request.Category = "all"
	}
	if request.Category != "all" && request.Category != "record" {
		return AuditPage{}, &domain.BadRequestError{Message: "category must be all or record"}
	}
	for _, ref := range []struct{ prefix, value string }{{id.RecordPrefix, request.RecordID}, {id.FieldPrefix, request.FieldID}, {id.ViewPrefix, request.ViewID}, {id.ActorPrefix, request.ActorID}, {id.MutationPrefix, request.ClientMutationID}} {
		if ref.value != "" && !id.Valid(ref.prefix, ref.value) {
			return AuditPage{}, &domain.BadRequestError{Message: "invalid Audit filter ID"}
		}
	}
	if request.OperationID != "" && !operationPattern.MatchString(request.OperationID) {
		return AuditPage{}, &domain.BadRequestError{Message: "operationId is invalid"}
	}
	if request.Since != nil {
		v := request.Since.UTC()
		request.Since = &v
	}
	if request.Until != nil {
		v := request.Until.UTC()
		request.Until = &v
	}
	if request.Since != nil && request.Until != nil && request.Since.After(*request.Until) {
		return AuditPage{}, &domain.BadRequestError{Message: "since must not exceed until"}
	}
	if request.Limit == 0 {
		request.Limit = 100
	}
	if request.Limit < 1 || request.Limit > 500 {
		return AuditPage{}, &domain.BadRequestError{Message: "limit must be from 1 to 500"}
	}
	if s == nil || s.store == nil {
		return AuditPage{}, domain.ErrDependencyMissing
	}
	store, ok := s.store.(AuditStore)
	if !ok {
		return AuditPage{}, domain.ErrDependencyMissing
	}
	encoded, err := json.Marshal(request.AuditFilter)
	if err != nil {
		return AuditPage{}, err
	}
	hash := sha256.Sum256(encoded)
	fingerprint := hex.EncodeToString(hash[:])
	signer, err := s.cursorSigner(ctx)
	if err != nil {
		return AuditPage{}, err
	}
	position := AuditPosition{}
	expires := s.now().Add(queryCursorTTL).Unix()
	if request.Cursor != "" {
		var cursor auditCursor
		if err := signer.Decode("audit", request.Cursor, &cursor); err != nil || cursor.ActorID != actorID || cursor.TableID != tableID || cursor.FilterHash != fingerprint || cursor.High < 0 || cursor.Before < 0 || cursor.Before > cursor.High+1 || cursor.Watermark < 0 {
			return AuditPage{}, &domain.InvalidCursorError{}
		}
		if s.now().Unix() >= cursor.ExpiresAt {
			return AuditPage{}, &domain.CursorExpiredError{}
		}
		position = AuditPosition{High: cursor.High, Before: cursor.Before, Watermark: cursor.Watermark, Continuation: true}
		expires = cursor.ExpiresAt
	}
	stored, err := store.ReadAudit(ctx, actorID, tableID, request.AuditFilter, position, request.Limit)
	if err != nil {
		return AuditPage{}, err
	}
	if stored.HasMore {
		stored.NextCursor, err = signer.Encode("audit", auditCursor{ActorID: actorID, TableID: tableID, FilterHash: fingerprint, High: stored.Position.High, Before: stored.Items[len(stored.Items)-1].Sequence, Watermark: stored.Position.Watermark, ExpiresAt: expires})
		if err != nil {
			return AuditPage{}, err
		}
	}
	return stored.AuditPage, nil
}
