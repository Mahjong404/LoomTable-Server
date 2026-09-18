package record

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Mahjong404/LoomTable-Server/internal/domain"
)

type orderTestStore struct {
	result     StoredRecordOrderResult
	err        error
	gotBefore  *string
	gotAfter   *string
	duplicated string
}

func (s *orderTestStore) GetRecord(context.Context, string, string) (Record, error) {
	return Record{}, domain.ErrNotFound
}

func (s *orderTestStore) MutateRecords(context.Context, string, string, string, [32]byte, []Command) (StoredMutationResult, error) {
	return StoredMutationResult{}, nil
}

func (s *orderTestStore) CursorKey(context.Context) ([]byte, error) {
	return []byte("01234567890123456789012345678901"), nil
}

func (s *orderTestStore) MoveRecord(_ context.Context, _, _, _ string, before, after *string) (StoredRecordOrderResult, error) {
	s.gotBefore, s.gotAfter = before, after
	return s.result, s.err
}

func (s *orderTestStore) DuplicateRecord(_ context.Context, _, _, recordID string) (StoredRecordOrderResult, error) {
	s.duplicated = recordID
	return s.result, s.err
}

const (
	orderTable  = "tbl_00000000000000000000000000"
	orderActor  = "act_00000000000000000000000000"
	orderRecord = "rec_00000000000000000000000000"
	orderAnchor = "rec_11111111111111111111111111"
)

func TestMoveValidation(t *testing.T) {
	service := New(&orderTestStore{})
	cases := []struct {
		name     string
		tableID  string
		recordID string
		request  MoveRequest
		want     any
	}{
		{name: "bad table", tableID: "nope", recordID: orderRecord},
		{name: "bad record", tableID: orderTable, recordID: "nope"},
		{name: "bad before", tableID: orderTable, recordID: orderRecord, request: MoveRequest{BeforeRecordID: "nope"}},
		{name: "bad after", tableID: orderTable, recordID: orderRecord, request: MoveRequest{AfterRecordID: "nope"}},
		{name: "self before", tableID: orderTable, recordID: orderRecord, request: MoveRequest{BeforeRecordID: orderRecord}},
		{name: "self after", tableID: orderTable, recordID: orderRecord, request: MoveRequest{AfterRecordID: orderRecord}},
	}
	for _, current := range cases {
		t.Run(current.name, func(t *testing.T) {
			_, err := service.Move(context.Background(), orderActor, current.tableID, current.recordID, current.request)
			if err == nil {
				t.Fatal("Move must reject invalid input")
			}
		})
	}
}

func TestMovePassesAnchorsAndReturnsCursor(t *testing.T) {
	store := &orderTestStore{result: StoredRecordOrderResult{
		Record:         Record{ID: orderRecord, TableID: orderTable, Revision: 3, CreatedAt: time.Unix(1, 0).UTC(), UpdatedAt: time.Unix(2, 0).UTC()},
		ChangeSequence: 44,
	}}
	service := New(store)
	result, err := service.Move(context.Background(), orderActor, orderTable, orderRecord, MoveRequest{
		BeforeRecordID: orderAnchor, AfterRecordID: "rec_22222222222222222222222222",
	})
	if err != nil {
		t.Fatalf("Move error: %v", err)
	}
	if store.gotBefore == nil || *store.gotBefore != orderAnchor || store.gotAfter == nil || *store.gotAfter != "rec_22222222222222222222222222" {
		t.Fatalf("Move anchors = %#v %#v", store.gotBefore, store.gotAfter)
	}
	if result.Record.ID != orderRecord || result.ChangeCursor == "" {
		t.Fatalf("Move result = %#v", result)
	}

	end, err := service.Move(context.Background(), orderActor, orderTable, orderRecord, MoveRequest{})
	if err != nil {
		t.Fatalf("Move to end error: %v", err)
	}
	if store.gotBefore != nil || store.gotAfter != nil {
		t.Fatalf("Move to end anchors = %#v %#v, want nil", store.gotBefore, store.gotAfter)
	}
	if end.ChangeCursor == "" {
		t.Fatal("Move to end missing changeCursor")
	}
}

func TestDuplicateCallsStoreAndReturnsRecord(t *testing.T) {
	store := &orderTestStore{result: StoredRecordOrderResult{
		Record:         Record{ID: "rec_99999999999999999999999999", TableID: orderTable, Revision: 1, CreatedAt: time.Unix(1, 0).UTC(), UpdatedAt: time.Unix(1, 0).UTC()},
		ChangeSequence: 45,
	}}
	service := New(store)
	result, err := service.Duplicate(context.Background(), orderActor, orderTable, orderRecord)
	if err != nil {
		t.Fatalf("Duplicate error: %v", err)
	}
	if store.duplicated != orderRecord {
		t.Fatalf("duplicated = %q, want %q", store.duplicated, orderRecord)
	}
	if result.Record.ID != "rec_99999999999999999999999999" || result.ChangeCursor == "" {
		t.Fatalf("Duplicate result = %#v", result)
	}
	if _, err := service.Duplicate(context.Background(), orderActor, orderTable, "nope"); err == nil {
		t.Fatal("Duplicate must reject an invalid recordId")
	}
	var invalid *domain.BadRequestError
	if _, err := service.Duplicate(context.Background(), orderActor, orderTable, "nope"); !errors.As(err, &invalid) {
		t.Fatalf("Duplicate error = %v, want BadRequestError", err)
	}
}
