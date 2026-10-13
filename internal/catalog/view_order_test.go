package catalog

import (
	"context"
	"errors"
	"testing"

	"github.com/Mahjong404/LoomTable-Server/internal/domain"
)

func TestViewOrderServiceValidation(t *testing.T) {
	service := New(nil)
	for _, input := range []ViewOrderInput{
		{ExpectedOrderRevision: 0, ViewIDs: []string{}},
		{ExpectedOrderRevision: 1},
		{ExpectedOrderRevision: 1, ViewIDs: []string{"bad"}},
		{ExpectedOrderRevision: 1, ViewIDs: []string{"view_00000000000000000000000000", "view_00000000000000000000000000"}},
		{ExpectedOrderRevision: 1, ViewIDs: make([]string, 101)},
	} {
		_, err := service.ReorderViews(context.Background(), "actor", "mut_00000000000000000000000000", "tbl_00000000000000000000000000", input)
		var validation *domain.ValidationError
		if !errors.As(err, &validation) {
			t.Fatalf("invalid Order reached storage: %v", err)
		}
	}
	_, err := service.ReorderViews(context.Background(), "actor", "mut_00000000000000000000000000", "tbl_00000000000000000000000000", ViewOrderInput{ExpectedOrderRevision: 1, ViewIDs: []string{}})
	if !errors.Is(err, domain.ErrDependencyMissing) {
		t.Fatalf("empty permutation rejected before Table membership: %v", err)
	}
}
