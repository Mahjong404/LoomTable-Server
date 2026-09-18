package record

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Mahjong404/LoomTable-Server/internal/cursor"
	"github.com/Mahjong404/LoomTable-Server/internal/domain"
)

type historyTestStore struct {
	tail      int64
	page      StoredHistoryPage
	err       error
	gotFilter HistoryFilter
	gotBefore int64
	gotLimit  int
}

func (s *historyTestStore) GetRecord(context.Context, string, string) (Record, error) {
	return Record{}, domain.ErrNotFound
}

func (s *historyTestStore) MutateRecords(context.Context, string, string, string, [32]byte, []Command) (StoredMutationResult, error) {
	return StoredMutationResult{}, nil
}

func (s *historyTestStore) CursorKey(context.Context) ([]byte, error) {
	return []byte("01234567890123456789012345678901"), nil
}

func (s *historyTestStore) ChangeTail(context.Context, string, string) (int64, error) {
	return s.tail, s.err
}

func (s *historyTestStore) PullHistory(_ context.Context, _ string, _ string, filter HistoryFilter, before int64, limit int) (StoredHistoryPage, error) {
	s.gotFilter, s.gotBefore, s.gotLimit = filter, before, limit
	return s.page, s.err
}

const (
	historyActor  = "act_00000000000000000000000000"
	historyTable  = "tbl_00000000000000000000000000"
	historyRecord = "rec_00000000000000000000000000"
	historyField  = "fld_00000000000000000000000000"
)

func historySigner(t *testing.T) *cursor.Signer {
	t.Helper()
	signer, err := cursor.NewSigner([]byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatal(err)
	}
	return signer
}

func TestHistoryValidation(t *testing.T) {
	since := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)
	until := since.Add(-time.Hour)
	cases := []struct {
		name    string
		request HistoryRequest
	}{
		{name: "bad recordId", request: HistoryRequest{RecordID: "nope"}},
		{name: "bad fieldId", request: HistoryRequest{FieldID: "nope"}},
		{name: "bad actorId", request: HistoryRequest{ActorID: "nope"}},
		{name: "bad kind", request: HistoryRequest{Kind: "fieldConverted"}},
		{name: "since after until", request: HistoryRequest{Since: &since, Until: &until}},
		{name: "limit too low", request: HistoryRequest{Limit: -1}},
		{name: "limit too high", request: HistoryRequest{Limit: 501}},
	}
	for _, current := range cases {
		t.Run(current.name, func(t *testing.T) {
			service := New(&historyTestStore{})
			_, err := service.History(context.Background(), historyActor, historyTable, current.request)
			var badRequest *domain.BadRequestError
			if !errors.As(err, &badRequest) {
				t.Fatalf("History error = %v, want BadRequestError", err)
			}
		})
	}
}

func TestHistoryFirstPageStartsAtTail(t *testing.T) {
	store := &historyTestStore{
		tail: 42,
		page: StoredHistoryPage{Items: []Change{
			{ID: "chg_1", Sequence: 42}, {ID: "chg_2", Sequence: 41},
		}, HasMore: true},
	}
	service := New(store)
	page, err := service.History(context.Background(), historyActor, historyTable, HistoryRequest{Limit: 2})
	if err != nil {
		t.Fatalf("History error: %v", err)
	}
	if store.gotBefore != 43 {
		t.Fatalf("PullHistory before = %d, want tail+1 = 43", store.gotBefore)
	}
	if store.gotLimit != 2 {
		t.Fatalf("PullHistory limit = %d, want 2", store.gotLimit)
	}
	if !page.HasMore || page.NextCursor == "" {
		t.Fatalf("History page = %#v, want hasMore with nextCursor", page)
	}
	if page.ChangeCursor == "" {
		t.Fatal("History page missing changeCursor")
	}
	var changePayload changeCursorPayload
	if err := historySigner(t).Decode("change", page.ChangeCursor, &changePayload); err != nil {
		t.Fatalf("decode changeCursor: %v", err)
	}
	if changePayload.Sequence != 42 || changePayload.ActorID != historyActor || changePayload.TableID != historyTable {
		t.Fatalf("changeCursor payload = %#v", changePayload)
	}
}

func TestHistoryCursorBindsFiltersAndPosition(t *testing.T) {
	store := &historyTestStore{
		tail: 42,
		page: StoredHistoryPage{Items: []Change{
			{ID: "chg_1", Sequence: 42}, {ID: "chg_2", Sequence: 40},
		}, HasMore: true},
	}
	service := New(store)
	request := HistoryRequest{RecordID: historyRecord, Kind: "recordUpdated", FieldID: historyField, Limit: 2}
	first, err := service.History(context.Background(), historyActor, historyTable, request)
	if err != nil {
		t.Fatalf("History error: %v", err)
	}
	if store.gotFilter != (HistoryFilter{RecordID: historyRecord, Kind: "recordUpdated", FieldID: historyField}) {
		t.Fatalf("PullHistory filter = %#v", store.gotFilter)
	}
	store.page = StoredHistoryPage{Items: []Change{{ID: "chg_3", Sequence: 39}}, HasMore: false}
	second, err := service.History(context.Background(), historyActor, historyTable, HistoryRequest{
		RecordID: historyRecord, Kind: "recordUpdated", FieldID: historyField, Limit: 2, Cursor: first.NextCursor,
	})
	if err != nil {
		t.Fatalf("History second page error: %v", err)
	}
	if store.gotBefore != 40 {
		t.Fatalf("PullHistory before = %d, want last item sequence 40", store.gotBefore)
	}
	if second.HasMore || second.NextCursor != "" {
		t.Fatalf("History second page = %#v, want terminal page", second)
	}
	if _, err := service.History(context.Background(), historyActor, historyTable, HistoryRequest{
		RecordID: historyRecord, Kind: "recordCreated", FieldID: historyField, Limit: 2, Cursor: first.NextCursor,
	}); err == nil {
		t.Fatal("History with changed filters must reject the cursor")
	} else {
		var invalid *domain.InvalidCursorError
		if !errors.As(err, &invalid) {
			t.Fatalf("History error = %v, want InvalidCursorError", err)
		}
	}
	if _, err := service.History(context.Background(), historyActor, "tbl_11111111111111111111111111", HistoryRequest{
		Cursor: first.NextCursor,
	}); err == nil {
		t.Fatal("History cursor from another table must be rejected")
	}
}

func TestHistoryCursorExpiry(t *testing.T) {
	store := &historyTestStore{tail: 10, page: StoredHistoryPage{
		Items: []Change{{ID: "chg_1", Sequence: 10}}, HasMore: true,
	}}
	now := time.Unix(1_800_000_000, 0)
	service := NewWithClock(store, func() time.Time { return now })
	first, err := service.History(context.Background(), historyActor, historyTable, HistoryRequest{})
	if err != nil {
		t.Fatalf("History error: %v", err)
	}
	var payload historyCursorPayload
	if err := historySigner(t).Decode("history", first.NextCursor, &payload); err != nil {
		t.Fatalf("decode history cursor: %v", err)
	}
	if payload.ExpiresAt != now.Unix()+int64(queryCursorTTL/time.Second) {
		t.Fatalf("history cursor expiresAt = %d, want %d", payload.ExpiresAt, now.Unix()+int64(queryCursorTTL/time.Second))
	}
	expired := NewWithClock(store, func() time.Time { return now.Add(queryCursorTTL + time.Second) })
	if _, err := expired.History(context.Background(), historyActor, historyTable, HistoryRequest{Cursor: first.NextCursor}); err == nil {
		t.Fatal("expired history cursor must fail")
	} else {
		var expiredError *domain.CursorExpiredError
		if !errors.As(err, &expiredError) {
			t.Fatalf("History error = %v, want CursorExpiredError", err)
		}
	}
}
