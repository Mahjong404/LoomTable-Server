package record

import (
	"encoding/json"
	"testing"

	"github.com/Mahjong404/LoomTable-Server/internal/domain"
)

func TestSelectionMembershipIdentity(t *testing.T) {
	primary := "fld_00000000000000000000000000"
	other := "fld_11111111111111111111111111"
	metadata := QueryMetadata{TableID: "tbl_00000000000000000000000000", PrimaryFieldID: primary, Fields: map[string]FieldDefinition{primary: {ID: primary, Type: "text", Revision: 1}, other: {ID: other, Type: "number", Revision: 1}}}
	textRule := domain.FilterNode{Kind: "rule", FieldID: primary, Operator: "contains", Value: json.RawMessage(`"ALPHA"`)}
	numberRule := domain.FilterNode{Kind: "rule", FieldID: other, Operator: "greaterThan", Value: json.RawMessage(`0`)}
	query := SelectionQuery{FilterPresent: true, Filter: &domain.FilterNode{Kind: "group", Operator: "and", Children: []domain.FilterNode{textRule, numberRule}}, Search: "  STRASSE  "}
	_, fp, schema, err := SelectionPlan(query, metadata)
	if err != nil {
		t.Fatal(err)
	}
	query.Filter.Children = []domain.FilterNode{numberRule, textRule, textRule}
	query.Search = "straße"
	metadata.Fields[primary] = FieldDefinition{ID: primary, Type: "text", Revision: 20}
	metadata.View = &domain.View{ID: "view_00000000000000000000000000", Revision: 50, Config: domain.GridViewConfig{Projection: []string{"fld_22222222222222222222222222"}, Sort: []domain.SortSpec{{FieldID: other, Direction: "desc"}}}}
	_, sameFP, sameSchema, err := SelectionPlan(query, metadata)
	if err != nil {
		t.Fatal(err)
	}
	if fp != sameFP || schema != sameSchema {
		t.Fatal("presentation or general revisions changed membership identity")
	}
	metadata.Fields["fld_22222222222222222222222222"] = FieldDefinition{ID: "fld_22222222222222222222222222", Type: "text"}
	_, _, changedSchema, err := SelectionPlan(query, metadata)
	if err != nil || schema == changedSchema {
		t.Fatal("Search schema change not bound", err)
	}
	query.Search = "other"
	_, different, _, err := SelectionPlan(query, metadata)
	if err != nil || fp == different {
		t.Fatal("Search did not change fingerprint", err)
	}
}
func TestSelectionIDBudgets(t *testing.T) {
	rec := "rec_00000000000000000000000000"
	for _, ids := range [][]string{{rec, rec}, {"tbl_00000000000000000000000000"}, make([]string, 5001)} {
		if ValidateSelectionIDs(ids, nil) == nil {
			t.Fatal("invalid exclusion accepted")
		}
	}
	if ValidateSelectionIDs([]string{rec}, []string{rec}) != nil {
		t.Fatal("same ID may be excluded and probed")
	}
	if ValidateSelectionIDs(nil, make([]string, 501)) == nil {
		t.Fatal("probe budget not enforced")
	}
}
