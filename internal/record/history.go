package record

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Mahjong404/LoomTable-Server/internal/domain"
	"github.com/Mahjong404/LoomTable-Server/internal/id"
)

type HistoryStore interface {
	ChangeTail(context.Context, string, string) (int64, error)
	PullHistory(context.Context, string, string, HistoryFilter, int64, int) (StoredHistoryPage, error)
}

type historyCursorPayload struct {
	ActorID    string `json:"actorId"`
	TableID    string `json:"tableId"`
	FilterHash string `json:"filterHash"`
	Before     int64  `json:"before"`
	IssuedAt   int64  `json:"issuedAt"`
	ExpiresAt  int64  `json:"expiresAt"`
}

func (s *Service) History(ctx context.Context, actorID, tableID string, request HistoryRequest) (HistoryPage, error) {
	if !id.Valid(id.TablePrefix, tableID) {
		return HistoryPage{}, &domain.BadRequestError{Message: "/tableId has an invalid typed ID"}
	}
	if request.RecordID != "" && !id.Valid(id.RecordPrefix, request.RecordID) {
		return HistoryPage{}, &domain.BadRequestError{Message: "recordId has an invalid typed ID"}
	}
	if request.FieldID != "" && !id.Valid(id.FieldPrefix, request.FieldID) {
		return HistoryPage{}, &domain.BadRequestError{Message: "fieldId has an invalid typed ID"}
	}
	if request.ActorID != "" && !id.Valid(id.ActorPrefix, request.ActorID) {
		return HistoryPage{}, &domain.BadRequestError{Message: "actorId has an invalid typed ID"}
	}
	if request.Kind != "" && !validHistoryKind(request.Kind) {
		return HistoryPage{}, &domain.BadRequestError{Message: "kind has an unsupported change kind"}
	}
	if request.Since != nil && request.Until != nil && request.Since.After(*request.Until) {
		return HistoryPage{}, &domain.BadRequestError{Message: "since must not be later than until"}
	}
	limit := request.Limit
	if limit == 0 {
		limit = defaultQueryLimit
	}
	if limit < 1 || limit > maxQueryLimit {
		return HistoryPage{}, &domain.BadRequestError{Message: "limit must be from 1 to 500"}
	}
	if s == nil || s.store == nil {
		return HistoryPage{}, domain.ErrDependencyMissing
	}
	store, ok := s.store.(HistoryStore)
	if !ok {
		return HistoryPage{}, domain.ErrDependencyMissing
	}
	signer, err := s.cursorSigner(ctx)
	if err != nil {
		return HistoryPage{}, err
	}
	filter := HistoryFilter{
		RecordID: request.RecordID, Kind: request.Kind, FieldID: request.FieldID,
		ActorID: request.ActorID, Since: request.Since, Until: request.Until,
	}
	filterHash, err := historyFilterHash(filter)
	if err != nil {
		return HistoryPage{}, err
	}
	tail, err := store.ChangeTail(ctx, actorID, tableID)
	if err != nil {
		return HistoryPage{}, err
	}
	before := tail + 1
	if request.Cursor != "" {
		var payload historyCursorPayload
		if err := signer.Decode("history", request.Cursor, &payload); err != nil ||
			payload.ActorID != actorID || payload.TableID != tableID || payload.FilterHash != filterHash {
			return HistoryPage{}, &domain.InvalidCursorError{}
		}
		if s.now().UTC().Unix() >= payload.ExpiresAt {
			return HistoryPage{}, &domain.CursorExpiredError{}
		}
		before = payload.Before
	}
	stored, err := store.PullHistory(ctx, actorID, tableID, filter, before, limit)
	if err != nil {
		return HistoryPage{}, err
	}
	changeCursor, err := signer.Encode("change", changeCursorPayload{ActorID: actorID, TableID: tableID, Sequence: tail})
	if err != nil {
		return HistoryPage{}, fmt.Errorf("encode change cursor: %w", err)
	}
	result := HistoryPage{Items: stored.Items, HasMore: stored.HasMore, ChangeCursor: changeCursor}
	if stored.HasMore {
		if len(stored.Items) == 0 {
			return HistoryPage{}, errors.New("history store returned an empty page with more results")
		}
		now := s.now().UTC()
		result.NextCursor, err = signer.Encode("history", historyCursorPayload{
			ActorID: actorID, TableID: tableID, FilterHash: filterHash,
			Before:   stored.Items[len(stored.Items)-1].Sequence,
			IssuedAt: now.Unix(), ExpiresAt: now.Add(queryCursorTTL).Unix(),
		})
		if err != nil {
			return HistoryPage{}, fmt.Errorf("encode history cursor: %w", err)
		}
	}
	return result, nil
}

func validHistoryKind(kind string) bool {
	switch kind {
	case "recordCreated", "recordUpdated", "recordDeleted", "recordRestored", "schemaChanged", "viewChanged":
		return true
	}
	return false
}

func historyFilterHash(filter HistoryFilter) (string, error) {
	var since, until string
	if filter.Since != nil {
		since = filter.Since.UTC().Format(time.RFC3339Nano)
	}
	if filter.Until != nil {
		until = filter.Until.UTC().Format(time.RFC3339Nano)
	}
	encoded, err := json.Marshal(struct {
		RecordID string `json:"recordId"`
		Kind     string `json:"kind"`
		FieldID  string `json:"fieldId"`
		ActorID  string `json:"actorId"`
		Since    string `json:"since"`
		Until    string `json:"until"`
	}{
		RecordID: filter.RecordID, Kind: filter.Kind, FieldID: filter.FieldID,
		ActorID: filter.ActorID, Since: since, Until: until,
	})
	if err != nil {
		return "", fmt.Errorf("canonicalize history filters: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}
