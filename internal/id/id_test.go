package id

import (
	"strings"
	"testing"
)

func TestNewProducesTypedULID(t *testing.T) {
	value, err := New(RecordPrefix)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if !strings.HasPrefix(value, RecordPrefix) {
		t.Fatalf("ID %q does not have prefix %q", value, RecordPrefix)
	}
	if len(strings.TrimPrefix(value, RecordPrefix)) != 26 {
		t.Fatalf("ULID body length = %d, want 26", len(strings.TrimPrefix(value, RecordPrefix)))
	}
}