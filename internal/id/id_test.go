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

func TestValid(t *testing.T) {
	value, err := New(WorkspacePrefix)
	if err != nil {
		t.Fatal(err)
	}
	if !Valid(WorkspacePrefix, value) {
		t.Fatalf("Valid() rejected generated ID %q", value)
	}
	if Valid(BasePrefix, value) {
		t.Fatalf("Valid() accepted %q for the wrong type", value)
	}
	if Valid(WorkspacePrefix, WorkspacePrefix+strings.Repeat("I", 26)) {
		t.Fatal("Valid() accepted a character excluded by Crockford Base32")
	}
}
