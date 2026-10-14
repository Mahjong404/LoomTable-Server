package httpapi

import (
	"context"
	record "github.com/Mahjong404/LoomTable-Server/internal/record"
)

func (s *stubRecords) Audit(ctx context.Context, actor, table string, req record.AuditRequest) (record.AuditPage, error) {
	return record.AuditPage{Items: []record.AuditEvent{}}, nil
}
