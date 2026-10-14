package domain

import "context"

type auditContextKey struct{}

// AuditOperation is request-local and published only after the writer commits.
type AuditOperation struct {
	ID     string
	Replay bool
}

func WithAuditOperation(ctx context.Context) (context.Context, *AuditOperation) {
	operation := &AuditOperation{}
	return context.WithValue(ctx, auditContextKey{}, operation), operation
}

func SetAuditOperation(ctx context.Context, value string, replay bool) {
	if operation, ok := ctx.Value(auditContextKey{}).(*AuditOperation); ok {
		operation.ID, operation.Replay = value, replay
	}
}

func ReplayedAuditOperation(ctx context.Context) bool {
	operation, ok := ctx.Value(auditContextKey{}).(*AuditOperation)
	return ok && operation.Replay
}
