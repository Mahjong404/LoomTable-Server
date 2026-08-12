package record

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Mahjong404/LoomTable-Server/internal/domain"
)

type serviceStore struct {
	stored StoredMutationResult
}

func (s *serviceStore) GetRecord(context.Context, string, string) (Record, error) {
	return Record{}, domain.ErrNotFound
}

func (s *serviceStore) MutateRecords(context.Context, string, string, string, [32]byte, []Command) (StoredMutationResult, error) {
	return s.stored, nil
}

func (s *serviceStore) CursorKey(context.Context) ([]byte, error) {
	return []byte("01234567890123456789012345678901"), nil
}

func TestMutateReturnsSignedChangeCursor(t *testing.T) {
	store := &serviceStore{stored: StoredMutationResult{
		ClientMutationID: "mut_00000000000000000000000000",
		Results:          []CommandResult{},
		ChangeSequence:   42,
	}}
	service := New(store)
	result, err := service.Mutate(
		context.Background(),
		"act_00000000000000000000000000",
		"tbl_00000000000000000000000000",
		"mut_00000000000000000000000000",
		[]Command{{Kind: "createRecord", Values: map[string]any{}, ValuesPresent: true}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(result.ChangeCursor, "v1.change.") {
		t.Fatalf("change cursor = %q", result.ChangeCursor)
	}
}

func TestMutateRejectsDuplicateRecordTargets(t *testing.T) {
	service := New(&serviceStore{})
	_, err := service.Mutate(
		context.Background(),
		"act_00000000000000000000000000",
		"tbl_00000000000000000000000000",
		"mut_00000000000000000000000000",
		[]Command{
			{Kind: "deleteRecord", RecordID: "rec_00000000000000000000000000", ExpectedRevision: 1},
			{Kind: "restoreRecord", RecordID: "rec_00000000000000000000000000", ExpectedRevision: 2},
		},
	)
	var validation *domain.ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("error = %v, want ValidationError", err)
	}
	if len(validation.Issues) != 1 || validation.Issues[0].Code != "duplicate" {
		t.Fatalf("issues = %#v", validation.Issues)
	}
}
