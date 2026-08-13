package record

import (
	"context"
	"fmt"

	"github.com/Mahjong404/LoomTable-Server/internal/domain"
	"github.com/Mahjong404/LoomTable-Server/internal/id"
)

type ChangeStore interface {
	ChangeTail(context.Context, string, string) (int64, error)
	PullChanges(context.Context, string, string, int64, int) (StoredChangePage, error)
}

func (s *Service) Changes(ctx context.Context, actorID, tableID, token string, limit int) (ChangePage, error) {
	if !id.Valid(id.TablePrefix, tableID) {
		return ChangePage{}, &domain.BadRequestError{Message: "/tableId has an invalid typed ID"}
	}
	if limit == 0 {
		limit = defaultQueryLimit
	}
	if limit < 1 || limit > maxQueryLimit {
		return ChangePage{}, &domain.BadRequestError{Message: "limit must be from 1 to 500"}
	}
	if s == nil || s.store == nil {
		return ChangePage{}, domain.ErrDependencyMissing
	}
	store, ok := s.store.(ChangeStore)
	if !ok {
		return ChangePage{}, domain.ErrDependencyMissing
	}
	signer, err := s.cursorSigner(ctx)
	if err != nil {
		return ChangePage{}, err
	}
	after := int64(0)
	if token == "" {
		after, err = store.ChangeTail(ctx, actorID, tableID)
		if err != nil {
			return ChangePage{}, err
		}
		encoded, err := signer.Encode("change", changeCursorPayload{ActorID: actorID, TableID: tableID, Sequence: after})
		if err != nil {
			return ChangePage{}, fmt.Errorf("encode Change cursor: %w", err)
		}
		return ChangePage{Items: []Change{}, NextCursor: encoded, HasMore: false}, nil
	}
	var payload changeCursorPayload
	if err := signer.Decode("change", token, &payload); err != nil || payload.ActorID != actorID || payload.TableID != tableID || payload.Sequence < 0 {
		return ChangePage{}, &domain.InvalidCursorError{}
	}
	after = payload.Sequence
	stored, err := store.PullChanges(ctx, actorID, tableID, after, limit)
	if err != nil {
		return ChangePage{}, err
	}
	next, err := signer.Encode("change", changeCursorPayload{ActorID: actorID, TableID: tableID, Sequence: stored.NextSequence})
	if err != nil {
		return ChangePage{}, fmt.Errorf("encode Change cursor: %w", err)
	}
	return ChangePage{Items: stored.Items, NextCursor: next, HasMore: stored.HasMore}, nil
}
