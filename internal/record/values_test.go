package record

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Mahjong404/LoomTable-Server/internal/domain"
)

type valuesTestStore struct {
	metadata         QueryMetadata
	distinct         StoredDistinctPage
	aggregate        StoredAggregateResult
	err              error
	gotDistinctPlan  DistinctPlan
	gotAggregatePlan AggregatePlan
}

func (s *valuesTestStore) GetRecord(context.Context, string, string) (Record, error) {
	return Record{}, domain.ErrNotFound
}

func (s *valuesTestStore) MutateRecords(context.Context, string, string, string, [32]byte, []Command) (StoredMutationResult, error) {
	return StoredMutationResult{}, nil
}

func (s *valuesTestStore) CursorKey(context.Context) ([]byte, error) {
	return []byte("01234567890123456789012345678901"), nil
}

func (s *valuesTestStore) ResolveQuery(context.Context, string, string, string) (QueryMetadata, error) {
	if s.err != nil {
		return QueryMetadata{}, s.err
	}
	return s.metadata, nil
}

func (s *valuesTestStore) DistinctValues(_ context.Context, _ string, _ string, plan DistinctPlan) (StoredDistinctPage, error) {
	s.gotDistinctPlan = plan
	return s.distinct, s.err
}

func (s *valuesTestStore) AggregateRecords(_ context.Context, _ string, _ string, plan AggregatePlan) (StoredAggregateResult, error) {
	s.gotAggregatePlan = plan
	return s.aggregate, s.err
}

const (
	valuesActor  = "act_00000000000000000000000000"
	valuesTable  = "tbl_00000000000000000000000000"
	valuesSelect = "fld_00000000000000000000000001"
	valuesText   = "fld_00000000000000000000000002"
	valuesNumber = "fld_00000000000000000000000003"
	valuesLoc    = "fld_00000000000000000000000004"
	valuesOption = "opt_00000000000000000000000001"
)

func valuesMetadata() QueryMetadata {
	return QueryMetadata{
		TableID:        valuesTable,
		PrimaryFieldID: valuesText,
		Fields: map[string]FieldDefinition{
			valuesSelect: {
				ID: valuesSelect, Type: "select", Revision: 1, Position: 1,
				Config: json.RawMessage(`{"options":[{"id":"opt_00000000000000000000000001","name":"Alpha","color":"red"},{"id":"opt_00000000000000000000000002","name":"Beta","color":"blue"},{"id":"opt_00000000000000000000000003","name":"Gamma","color":"green"}],"deletedOptions":[]}`),
			},
			valuesText:   {ID: valuesText, Type: "text", Revision: 1, Position: 2},
			valuesNumber: {ID: valuesNumber, Type: "number", Revision: 1, Position: 3},
			valuesLoc:    {ID: valuesLoc, Type: "location", Revision: 1, Position: 4},
		},
	}
}

func TestDistinctValuesSelectOrderedByRank(t *testing.T) {
	store := &valuesTestStore{
		metadata: valuesMetadata(),
		distinct: StoredDistinctPage{
			EmptyCount:     2,
			ChangeSequence: 9,
			Items: []StoredDistinctValue{
				{Value: "opt_00000000000000000000000003", Count: 1},
				{Value: "opt_00000000000000000000000001", Count: 5},
				{Value: "opt_00000000000000000000000002", Count: 3},
			},
		},
	}
	service := New(store)
	page, err := service.DistinctValues(context.Background(), valuesActor, valuesTable, valuesSelect, DistinctValuesRequest{})
	if err != nil {
		t.Fatalf("DistinctValues error: %v", err)
	}
	if len(page.Items) != 3 {
		t.Fatalf("DistinctValues items = %#v", page.Items)
	}
	want := []struct {
		value   string
		display string
		count   int64
	}{
		{"opt_00000000000000000000000001", "Alpha", 5},
		{"opt_00000000000000000000000002", "Beta", 3},
		{"opt_00000000000000000000000003", "Gamma", 1},
	}
	for index, item := range page.Items {
		if item.Value != want[index].value || item.Display != want[index].display || item.Count != want[index].count {
			t.Fatalf("item %d = %#v, want %#v", index, item, want[index])
		}
	}
	if page.EmptyCount != 2 || page.ChangeCursor == "" || page.HasMore {
		t.Fatalf("DistinctValues page = %#v", page)
	}
}

func TestDistinctValuesPrunesSelfRules(t *testing.T) {
	store := &valuesTestStore{metadata: valuesMetadata(), distinct: StoredDistinctPage{}}
	service := New(store)
	selfRule := domain.FilterNode{Kind: "rule", FieldID: valuesSelect, Operator: "is", Value: json.RawMessage(`"opt_00000000000000000000000001"`)}
	otherRule := domain.FilterNode{Kind: "rule", FieldID: valuesText, Operator: "contains", Value: json.RawMessage(`"road"`)}

	_, err := service.DistinctValues(context.Background(), valuesActor, valuesTable, valuesSelect, DistinctValuesRequest{
		FilterPresent: true,
		Filter:        &domain.FilterNode{Kind: "group", Operator: "and", Children: []domain.FilterNode{selfRule, otherRule}},
	})
	if err != nil {
		t.Fatalf("DistinctValues error: %v", err)
	}
	filter := store.gotDistinctPlan.Filter
	if filter == nil || len(filter.Children) != 1 || filter.Children[0].FieldID != valuesText {
		t.Fatalf("pruned filter = %#v, want only the text rule", filter)
	}

	_, err = service.DistinctValues(context.Background(), valuesActor, valuesTable, valuesSelect, DistinctValuesRequest{
		FilterPresent: true,
		Filter:        &selfRule,
	})
	if err != nil {
		t.Fatalf("DistinctValues error: %v", err)
	}
	if store.gotDistinctPlan.Filter != nil {
		t.Fatalf("self-only filter = %#v, want nil", store.gotDistinctPlan.Filter)
	}
}

func TestDistinctValuesSearchMatchesOptionName(t *testing.T) {
	store := &valuesTestStore{
		metadata: valuesMetadata(),
		distinct: StoredDistinctPage{Items: []StoredDistinctValue{
			{Value: "opt_00000000000000000000000001", Count: 5},
			{Value: "opt_00000000000000000000000002", Count: 3},
		}},
	}
	service := New(store)
	page, err := service.DistinctValues(context.Background(), valuesActor, valuesTable, valuesSelect, DistinctValuesRequest{
		Search: "alp", SearchPresent: true,
	})
	if err != nil {
		t.Fatalf("DistinctValues error: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].Value != "opt_00000000000000000000000001" {
		t.Fatalf("searched items = %#v, want only Alpha", page.Items)
	}
}

func TestDistinctValuesTextPagination(t *testing.T) {
	store := &valuesTestStore{
		metadata: valuesMetadata(),
		distinct: StoredDistinctPage{Items: []StoredDistinctValue{
			{Value: "beta", Display: "Beta", Count: 2},
			{Value: "alpha", Display: "Alpha", Count: 4},
			{Value: "gamma", Display: "Gamma", Count: 1},
		}},
	}
	service := New(store)
	first, err := service.DistinctValues(context.Background(), valuesActor, valuesTable, valuesText, DistinctValuesRequest{Limit: 2})
	if err != nil {
		t.Fatalf("DistinctValues error: %v", err)
	}
	if len(first.Items) != 2 || first.Items[0].Value != "alpha" || first.Items[0].Display != "Alpha" || !first.HasMore {
		t.Fatalf("first page = %#v", first.Items)
	}
	second, err := service.DistinctValues(context.Background(), valuesActor, valuesTable, valuesText, DistinctValuesRequest{Limit: 2, Cursor: first.NextCursor})
	if err != nil {
		t.Fatalf("DistinctValues second page error: %v", err)
	}
	if len(second.Items) != 1 || second.Items[0].Value != "gamma" || second.HasMore {
		t.Fatalf("second page = %#v", second.Items)
	}
	if _, err := service.DistinctValues(context.Background(), valuesActor, valuesTable, valuesNumber, DistinctValuesRequest{Limit: 2, Cursor: first.NextCursor}); err == nil {
		t.Fatal("cursor from another field must be rejected")
	} else {
		var invalid *domain.InvalidCursorError
		if !errors.As(err, &invalid) {
			t.Fatalf("DistinctValues error = %v, want InvalidCursorError", err)
		}
	}
}

func TestDistinctValuesRejectsUnsupportedType(t *testing.T) {
	store := &valuesTestStore{metadata: valuesMetadata()}
	service := New(store)
	_, err := service.DistinctValues(context.Background(), valuesActor, valuesTable, valuesLoc, DistinctValuesRequest{})
	var unsupported *domain.UnsupportedFieldTypeError
	if !errors.As(err, &unsupported) {
		t.Fatalf("DistinctValues error = %v, want UnsupportedFieldTypeError", err)
	}
	_, err = service.DistinctValues(context.Background(), valuesActor, valuesTable, "fld_99999999999999999999999999", DistinctValuesRequest{})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("DistinctValues error = %v, want NOT_FOUND", err)
	}
}

func TestDistinctValuesEmptyCountAndTypedValues(t *testing.T) {
	store := &valuesTestStore{
		metadata: valuesMetadata(),
		distinct: StoredDistinctPage{EmptyCount: 7, Items: []StoredDistinctValue{
			{Value: "2.5", Count: 1}, {Value: "10", Count: 3},
		}},
	}
	service := New(store)
	page, err := service.DistinctValues(context.Background(), valuesActor, valuesTable, valuesNumber, DistinctValuesRequest{})
	if err != nil {
		t.Fatalf("DistinctValues error: %v", err)
	}
	if len(page.Items) != 2 || page.Items[0].Value != 2.5 || page.Items[1].Value != float64(10) {
		t.Fatalf("number items = %#v", page.Items)
	}
	if page.EmptyCount != 7 {
		t.Fatalf("emptyCount = %d, want 7", page.EmptyCount)
	}
}

func TestAggregatePassesPlanAndReturnsResults(t *testing.T) {
	store := &valuesTestStore{
		metadata: valuesMetadata(),
		aggregate: StoredAggregateResult{
			ChangeSequence: 12,
			Results: map[string]map[string]any{
				valuesNumber: {"count": int64(3), "sum": 9.0, "avg": 3.0, "min": 1.0, "max": 5.0},
				valuesText:   {"count": int64(4), "sum": nil, "avg": nil, "min": nil, "max": nil},
			},
		},
	}
	service := New(store)
	result, err := service.Aggregate(context.Background(), valuesActor, valuesTable, AggregateRequest{
		FieldIDs:  []string{valuesNumber, valuesText},
		Functions: []string{"count", "sum", "avg", "min", "max"},
		Filter: &domain.FilterNode{
			Kind: "rule", FieldID: valuesText, Operator: "contains", Value: json.RawMessage(`"x"`),
		},
		FilterPresent: true,
	})
	if err != nil {
		t.Fatalf("Aggregate error: %v", err)
	}
	if len(store.gotAggregatePlan.Requests) != 2 || store.gotAggregatePlan.Requests[0].FieldType != "number" {
		t.Fatalf("aggregate plan = %#v", store.gotAggregatePlan)
	}
	if store.gotAggregatePlan.Filter == nil {
		t.Fatal("aggregate plan lost the filter")
	}
	if result.Results[valuesNumber]["sum"] != 9.0 || result.Results[valuesText]["sum"] != nil {
		t.Fatalf("aggregate results = %#v", result.Results)
	}
	if result.ChangeCursor == "" {
		t.Fatal("aggregate result missing changeCursor")
	}
}

func TestAggregateValidation(t *testing.T) {
	service := New(&valuesTestStore{metadata: valuesMetadata()})
	cases := []struct {
		name    string
		request AggregateRequest
	}{
		{name: "no fields", request: AggregateRequest{Functions: []string{"count"}}},
		{name: "no fns", request: AggregateRequest{FieldIDs: []string{valuesNumber}}},
		{name: "bad fn", request: AggregateRequest{FieldIDs: []string{valuesNumber}, Functions: []string{"median"}}},
		{name: "duplicate field", request: AggregateRequest{FieldIDs: []string{valuesNumber, valuesNumber}, Functions: []string{"count"}}},
		{name: "unknown field", request: AggregateRequest{FieldIDs: []string{"fld_99999999999999999999999999"}, Functions: []string{"count"}}},
	}
	for _, current := range cases {
		t.Run(current.name, func(t *testing.T) {
			_, err := service.Aggregate(context.Background(), valuesActor, valuesTable, current.request)
			var validation *domain.ValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("Aggregate error = %v, want ValidationError", err)
			}
		})
	}
}
