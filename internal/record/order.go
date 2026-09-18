package record

import (
	"context"
	"fmt"

	"github.com/Mahjong404/LoomTable-Server/internal/domain"
	"github.com/Mahjong404/LoomTable-Server/internal/id"
)

type OrderStore interface {
	MoveRecord(context.Context, string, string, string, *string, *string) (StoredRecordOrderResult, error)
	DuplicateRecord(context.Context, string, string, string) (StoredRecordOrderResult, error)
}

func (s *Service) Move(ctx context.Context, actorID, tableID, recordID string, request MoveRequest) (RecordOrderResult, error) {
	if !id.Valid(id.TablePrefix, tableID) {
		return RecordOrderResult{}, &domain.BadRequestError{Message: "/tableId has an invalid typed ID"}
	}
	if !id.Valid(id.RecordPrefix, recordID) {
		return RecordOrderResult{}, &domain.BadRequestError{Message: "/recordId has an invalid typed ID"}
	}
	issues := make([]domain.ValidationIssue, 0, 2)
	if request.BeforeRecordID != "" && !id.Valid(id.RecordPrefix, request.BeforeRecordID) {
		issues = append(issues, domain.ValidationIssue{Path: "/beforeRecordId", Code: "format", Message: "beforeRecordId must be a typed Record ID"})
	}
	if request.AfterRecordID != "" && !id.Valid(id.RecordPrefix, request.AfterRecordID) {
		issues = append(issues, domain.ValidationIssue{Path: "/afterRecordId", Code: "format", Message: "afterRecordId must be a typed Record ID"})
	}
	if request.BeforeRecordID == recordID || request.AfterRecordID == recordID {
		issues = append(issues, domain.ValidationIssue{Path: "/", Code: "format", Message: "record cannot be moved relative to itself"})
	}
	if len(issues) > 0 {
		return RecordOrderResult{}, domain.NewValidationError(issues...)
	}
	if s == nil || s.store == nil {
		return RecordOrderResult{}, domain.ErrDependencyMissing
	}
	orderStore, ok := s.store.(OrderStore)
	if !ok {
		return RecordOrderResult{}, domain.ErrDependencyMissing
	}
	var before, after *string
	if request.BeforeRecordID != "" {
		before = &request.BeforeRecordID
	}
	if request.AfterRecordID != "" {
		after = &request.AfterRecordID
	}
	stored, err := orderStore.MoveRecord(ctx, actorID, tableID, recordID, before, after)
	if err != nil {
		return RecordOrderResult{}, err
	}
	return s.orderResult(ctx, actorID, tableID, stored)
}

func (s *Service) Duplicate(ctx context.Context, actorID, tableID, recordID string) (RecordOrderResult, error) {
	if !id.Valid(id.TablePrefix, tableID) {
		return RecordOrderResult{}, &domain.BadRequestError{Message: "/tableId has an invalid typed ID"}
	}
	if !id.Valid(id.RecordPrefix, recordID) {
		return RecordOrderResult{}, &domain.BadRequestError{Message: "/recordId has an invalid typed ID"}
	}
	if s == nil || s.store == nil {
		return RecordOrderResult{}, domain.ErrDependencyMissing
	}
	orderStore, ok := s.store.(OrderStore)
	if !ok {
		return RecordOrderResult{}, domain.ErrDependencyMissing
	}
	stored, err := orderStore.DuplicateRecord(ctx, actorID, tableID, recordID)
	if err != nil {
		return RecordOrderResult{}, err
	}
	return s.orderResult(ctx, actorID, tableID, stored)
}

func (s *Service) orderResult(ctx context.Context, actorID, tableID string, stored StoredRecordOrderResult) (RecordOrderResult, error) {
	signer, err := s.cursorSigner(ctx)
	if err != nil {
		return RecordOrderResult{}, err
	}
	changeCursor, err := signer.Encode("change", changeCursorPayload{ActorID: actorID, TableID: tableID, Sequence: stored.ChangeSequence})
	if err != nil {
		return RecordOrderResult{}, fmt.Errorf("encode change cursor: %w", err)
	}
	return RecordOrderResult{Record: stored.Record, ChangeCursor: changeCursor}, nil
}
