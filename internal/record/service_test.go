package record

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Mahjong404/LoomTable-Server/internal/domain"
)

type serviceStore struct {
	stored      StoredMutationResult
	metadata    QueryMetadata
	page        StoredQueryPage
	tail        int64
	changes     StoredChangePage
	mapSnapshot StoredMapSnapshot
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

func (s *serviceStore) ResolveQuery(context.Context, string, string, string) (QueryMetadata, error) {
	return s.metadata, nil
}

func (s *serviceStore) QueryRecords(context.Context, string, string, QueryPlan, *QueryPosition, bool) (StoredQueryPage, error) {
	return s.page, nil
}

func (s *serviceStore) ChangeTail(context.Context, string, string) (int64, error) {
	return s.tail, nil
}

func (s *serviceStore) PullChanges(context.Context, string, string, int64, int) (StoredChangePage, error) {
	return s.changes, nil
}

func (s *serviceStore) ResolveMap(context.Context, string, string) (QueryMetadata, error) {
	return s.metadata, nil
}

func (s *serviceStore) LoadMapSnapshot(context.Context, string, QueryMetadata, QueryPlan, string, *MapViewport) (StoredMapSnapshot, error) {
	return s.mapSnapshot, nil
}

func (s *serviceStore) QueryMapClusterRecords(context.Context, string, QueryMetadata, QueryPlan, string, []MapViewportBox, *QueryPosition, int, bool) (StoredQueryPage, error) {
	return s.page, nil
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

func TestBuildQueryPlanUsesGridDefaultsAndAcceptsAndOrFilter(t *testing.T) {
	fields := map[string]FieldDefinition{
		textFieldID: {ID: textFieldID, Type: "text", Revision: 2, Position: 0},
		selectFieldID: {
			ID: selectFieldID, Type: "select", Revision: 3, Position: 1,
			Config: json.RawMessage(`{"options":[{"id":"` + activeOptionID + `"}],"deletedOptions":[]}`),
		},
	}
	filter := &domain.FilterNode{Kind: "group", Operator: "or", Children: []domain.FilterNode{
		{Kind: "rule", FieldID: textFieldID, Operator: "contains", Value: json.RawMessage(`"Straße"`)},
		{Kind: "group", Operator: "and", Children: []domain.FilterNode{{Kind: "rule", FieldID: selectFieldID, Operator: "is", Value: json.RawMessage(`"` + activeOptionID + `"`)}}},
	}}
	plan, err := buildQueryPlan(QueryRequest{}, QueryMetadata{
		TableID: "tbl_00000000000000000000000000", Fields: fields,
		View: &domain.View{ID: "view_00000000000000000000000000", Type: "grid", Revision: 4, Config: domain.GridViewConfig{
			Projection: []string{textFieldID}, Filter: filter, Sort: []domain.SortSpec{{FieldID: textFieldID, Direction: "asc"}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Projection) != 1 || plan.Projection[0] != textFieldID || plan.Sort[0].Nulls != "last" {
		t.Fatalf("plan = %#v", plan)
	}
	if got := string(plan.Filter.Children[0].Value); got != `"strasse"` {
		t.Fatalf("normalized filter value = %s", got)
	}
}

func TestQueryCursorRejectsMismatchAndExpiresOnSchemaChange(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	store := &serviceStore{
		metadata: QueryMetadata{TableID: "tbl_00000000000000000000000000", Fields: map[string]FieldDefinition{
			textFieldID: {ID: textFieldID, Type: "text", Revision: 1, Position: 0},
		}},
		page: StoredQueryPage{Items: []Record{}, HasMore: true, NextPosition: &QueryPosition{
			SortValues: []any{time.Unix(1, 0).UTC()}, RecordID: "rec_00000000000000000000000000",
		}},
	}
	service := NewWithClock(store, func() time.Time { return now })
	first, err := service.Query(context.Background(), "act_00000000000000000000000000", store.metadata.TableID, QueryRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if first.NextCursor == "" {
		t.Fatal("first page did not return a cursor")
	}

	_, err = service.Query(context.Background(), "act_00000000000000000000000000", store.metadata.TableID, QueryRequest{Cursor: first.NextCursor, Limit: 50})
	var invalid *domain.InvalidCursorError
	if !errors.As(err, &invalid) {
		t.Fatalf("mismatched query error = %v, want InvalidCursorError", err)
	}

	store.metadata.Fields[textFieldID] = FieldDefinition{ID: textFieldID, Type: "text", Revision: 2, Position: 0}
	_, err = service.Query(context.Background(), "act_00000000000000000000000000", store.metadata.TableID, QueryRequest{Cursor: first.NextCursor})
	var expired *domain.CursorExpiredError
	if !errors.As(err, &expired) {
		t.Fatalf("schema-changed query error = %v, want CursorExpiredError", err)
	}
}

func TestChangesWithoutCursorStartsAtCurrentTail(t *testing.T) {
	service := New(&serviceStore{tail: 42})
	page, err := service.Changes(context.Background(), "act_00000000000000000000000000", "tbl_00000000000000000000000000", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 0 || page.HasMore || !strings.HasPrefix(page.NextCursor, "v1.change.") {
		t.Fatalf("page = %#v", page)
	}
}

func TestMapBoundsUseTwoBoxesAcrossAntimeridian(t *testing.T) {
	bounds := minimalMapBounds([]MapCoordinate{{Lat: 10, Lng: 179}, {Lat: 12, Lng: -179}})
	if len(bounds.Boxes) != 2 || bounds.Boxes[0].West != 179 || bounds.Boxes[1].East != -179 {
		t.Fatalf("bounds = %#v", bounds)
	}
}

func TestQueryMapClustersWithoutDroppingRecords(t *testing.T) {
	locationID := "fld_00000000000000000000000001"
	viewID := "view_00000000000000000000000000"
	tableID := "tbl_00000000000000000000000000"
	records := make([]MapRecord, 501)
	for index := range records {
		position := MapCoordinate{Lat: 20 + float64(index%20)/100, Lng: 110 + float64(index%25)/100}
		records[index] = MapRecord{Record: Record{ID: fmt.Sprintf("rec_%026d", index)}, Position: &position}
	}
	store := &serviceStore{
		metadata: QueryMetadata{
			TableID: tableID, PrimaryFieldID: textFieldID,
			Fields: map[string]FieldDefinition{
				textFieldID: {ID: textFieldID, Type: "text", Revision: 1, Position: 0},
				locationID:  {ID: locationID, Type: "location", Revision: 1, Position: 1},
			},
			View: &domain.View{ID: viewID, TableID: tableID, Type: "map", Revision: 1, Config: domain.MapViewConfig{LocationFieldID: locationID}},
		},
		mapSnapshot: StoredMapSnapshot{Records: records, ChangeSequence: 7},
	}
	service := NewWithClock(store, func() time.Time { return time.Unix(1000, 0).UTC() })
	result, err := service.QueryMap(context.Background(), "act_00000000000000000000000000", viewID, MapQueryRequest{
		Viewport: MapViewport{Boxes: []MapViewportBox{{West: 100, South: 0, East: 120, North: 40}}},
		Zoom:     8, PixelWidth: 1000, PixelHeight: 800,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Features) > maxMapFeatures || result.ViewportRenderableRecordCount != 501 {
		t.Fatalf("features = %d, represented = %d", len(result.Features), result.ViewportRenderableRecordCount)
	}
	represented := 0
	for _, feature := range result.Features {
		switch typed := feature.(type) {
		case MapPoint:
			represented++
		case MapCluster:
			if typed.RecordsQueryToken == "" {
				t.Fatal("cluster omitted recordsQueryToken")
			}
			represented += typed.PointCount
		}
	}
	if represented != 501 {
		t.Fatalf("features represent %d Records, want 501", represented)
	}
}

func TestQueryMapExcludesUnlocatedAndUnrenderableRecords(t *testing.T) {
	locationID := "fld_00000000000000000000000001"
	viewID := "view_00000000000000000000000000"
	tableID := "tbl_00000000000000000000000000"
	renderable := MapCoordinate{Lat: 31.2, Lng: 121.5}
	unrenderable := MapCoordinate{Lat: 86, Lng: 121.5}
	store := &serviceStore{
		metadata: QueryMetadata{
			TableID: tableID, PrimaryFieldID: textFieldID,
			Fields: map[string]FieldDefinition{
				textFieldID: {ID: textFieldID, Type: "text", Revision: 1, Position: 0},
				locationID:  {ID: locationID, Type: "location", Revision: 1, Position: 1},
			},
			View: &domain.View{ID: viewID, TableID: tableID, Type: "map", Revision: 1, Config: domain.MapViewConfig{LocationFieldID: locationID}},
		},
		mapSnapshot: StoredMapSnapshot{Records: []MapRecord{
			{Record: Record{ID: "rec_00000000000000000000000001"}, Position: &renderable},
			{Record: Record{ID: "rec_00000000000000000000000002"}},
			{Record: Record{ID: "rec_00000000000000000000000003"}, Position: &unrenderable},
		}, ChangeSequence: 7},
	}
	service := NewWithClock(store, func() time.Time { return time.Unix(1000, 0).UTC() })
	result, err := service.QueryMap(context.Background(), "act_00000000000000000000000000", viewID, MapQueryRequest{
		Viewport: MapViewport{Boxes: []MapViewportBox{{West: 100, South: 0, East: 140, North: 40}}},
		Zoom:     8, PixelWidth: 1000, PixelHeight: 800,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ViewportRenderableRecordCount != 1 || len(result.Features) != 1 {
		t.Fatalf("map result = %#v", result)
	}
	point, ok := result.Features[0].(MapPoint)
	if !ok || point.RecordID != "rec_00000000000000000000000001" {
		t.Fatalf("features = %#v", result.Features)
	}
}

func TestMapBoundsKeepBothAntimeridianEndpoints(t *testing.T) {
	bounds := minimalMapBounds([]MapCoordinate{{Lat: 10, Lng: -180}, {Lat: 12, Lng: 180}})
	if len(bounds.Boxes) != 2 || bounds.Boxes[0].West != 180 || bounds.Boxes[1].East != -180 {
		t.Fatalf("bounds = %#v", bounds)
	}
}
