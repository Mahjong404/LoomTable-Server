package maintenance

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const (
	cleanupInterval  = 24 * time.Hour
	cleanupBatches   = 10
	cleanupBatchSize = 10_000
)

type CleanupStats struct {
	ChangesDeleted     int64
	IdempotencyDeleted int64
}

type RetentionStore interface {
	CleanupRetention(context.Context, time.Duration, time.Duration, int, int) (CleanupStats, error)
}

type Logger func(string, ...any)

type Runner struct {
	store          RetentionStore
	changeAge      time.Duration
	idempotencyAge time.Duration
	logger         Logger
}

func NewRetentionRunner(store RetentionStore, changeRetention, idempotencyRetention string, logger Logger) (*Runner, error) {
	changeAge, err := parseRetention(changeRetention)
	if err != nil {
		return nil, fmt.Errorf("change retention: %w", err)
	}
	idempotencyAge, err := parseRetention(idempotencyRetention)
	if err != nil {
		return nil, fmt.Errorf("idempotency retention: %w", err)
	}
	return &Runner{store: store, changeAge: changeAge, idempotencyAge: idempotencyAge, logger: logger}, nil
}

func (r *Runner) Start(ctx context.Context) {
	if r == nil || r.store == nil || (r.changeAge == 0 && r.idempotencyAge == 0) {
		return
	}
	go func() {
		r.runAndLog(ctx)
		ticker := time.NewTicker(cleanupInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				r.runAndLog(ctx)
			}
		}
	}()
}

func (r *Runner) RunOnce(ctx context.Context) (CleanupStats, error) {
	if r == nil || r.store == nil || (r.changeAge == 0 && r.idempotencyAge == 0) {
		return CleanupStats{}, nil
	}
	return r.store.CleanupRetention(ctx, r.changeAge, r.idempotencyAge, cleanupBatches, cleanupBatchSize)
}

func (r *Runner) runAndLog(ctx context.Context) {
	started := time.Now()
	stats, err := r.RunOnce(ctx)
	if r.logger == nil {
		return
	}
	if err != nil {
		r.logger("retention cleanup failed after %s: %v", time.Since(started), err)
		return
	}
	r.logger("retention cleanup completed in %s: changes=%d idempotency=%d", time.Since(started), stats.ChangesDeleted, stats.IdempotencyDeleted)
}

func parseRetention(value string) (time.Duration, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "forever":
		return 0, nil
	case "30d":
		return 30 * 24 * time.Hour, nil
	case "90d":
		return 90 * 24 * time.Hour, nil
	case "365d":
		return 365 * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("must be 30d, 90d, 365d, or forever")
	}
}
