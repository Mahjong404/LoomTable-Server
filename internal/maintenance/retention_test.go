package maintenance

import (
	"context"
	"testing"
	"time"
)

type retentionStore struct {
	calls int
}

func (s *retentionStore) CleanupRetention(context.Context, time.Duration, time.Duration, int, int) (CleanupStats, error) {
	s.calls++
	return CleanupStats{ChangesDeleted: 2, IdempotencyDeleted: 3}, nil
}

func TestRetentionRunnerSupportsConfiguredWindowsAndForever(t *testing.T) {
	store := &retentionStore{}
	runner, err := NewRetentionRunner(store, "90d", "365d", nil)
	if err != nil {
		t.Fatal(err)
	}
	stats, err := runner.RunOnce(context.Background())
	if err != nil || stats.ChangesDeleted != 2 || store.calls != 1 {
		t.Fatalf("stats = %#v, calls = %d, error = %v", stats, store.calls, err)
	}

	disabled, err := NewRetentionRunner(store, "forever", "forever", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := disabled.RunOnce(context.Background()); err != nil || store.calls != 1 {
		t.Fatalf("forever cleanup called store: calls = %d, error = %v", store.calls, err)
	}
}
