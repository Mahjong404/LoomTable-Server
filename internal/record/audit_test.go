package record

import (
	"context"
	"errors"
	"github.com/Mahjong404/LoomTable-Server/internal/domain"
	"testing"
	"time"
)

type auditTestStore struct {
	historyTestStore
	position AuditPosition
	filter   AuditFilter
}

func (s *auditTestStore) ReadAudit(_ context.Context, _ string, _ string, filter AuditFilter, position AuditPosition, _ int) (StoredAuditPage, error) {
	s.position = position
	s.filter = filter
	return StoredAuditPage{AuditPage: AuditPage{Items: []AuditEvent{{Sequence: 3}}, Count: 4, HasMore: true}, Position: AuditPosition{High: 9, Watermark: 2}}, nil
}
func TestAuditCursorBindingsAndExpiry(t *testing.T) {
	now := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	store := &auditTestStore{}
	service := NewWithClock(store, func() time.Time { return now })
	first, err := service.Audit(context.Background(), historyActor, historyTable, AuditRequest{AuditFilter: AuditFilter{Category: "record"}, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	request := AuditRequest{AuditFilter: AuditFilter{Category: "record"}, Cursor: first.NextCursor, Limit: 2}
	_, err = service.Audit(context.Background(), historyActor, historyTable, request)
	if err != nil || !store.position.Continuation || store.position.High != 9 || store.position.Before != 3 || store.position.Watermark != 2 {
		t.Fatalf("position=%#v %v", store.position, err)
	}
	for _, category := range []string{"all", "invalid"} {
		request.Category = category
		_, err = service.Audit(context.Background(), historyActor, historyTable, request)
		if err == nil {
			t.Fatal("changed filter accepted")
		}
	}
	request.Category = "record"
	now = now.Add(31 * time.Minute)
	_, err = service.Audit(context.Background(), historyActor, historyTable, request)
	var expired *domain.CursorExpiredError
	if !errors.As(err, &expired) {
		t.Fatalf("expiry=%v", err)
	}
}
