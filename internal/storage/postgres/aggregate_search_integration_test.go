package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	loomauth "github.com/Mahjong404/LoomTable-Server/internal/auth"
	"github.com/Mahjong404/LoomTable-Server/internal/catalog"
	"github.com/Mahjong404/LoomTable-Server/internal/config"
	"github.com/Mahjong404/LoomTable-Server/internal/domain"
	"github.com/Mahjong404/LoomTable-Server/internal/httpapi"
	loomrecord "github.com/Mahjong404/LoomTable-Server/internal/record"
	"github.com/Mahjong404/LoomTable-Server/internal/storage/postgres"
)

func TestAggregateSearchDatabase(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	repository := postgres.NewRepository(integrationDatabase(t, ctx))
	bootstrap, err := loomauth.NewAdmin(repository).Bootstrap(ctx, "Aggregate QA")
	if err != nil {
		t.Fatal(err)
	}
	actorID := bootstrap.ActorID
	catalogService := catalog.New(repository)
	recordService := loomrecord.New(repository)
	workspace, err := catalogService.CreateWorkspace(ctx, actorID, newMutationID(t), "Aggregate QA")
	if err != nil {
		t.Fatal(err)
	}
	base, err := catalogService.CreateBase(ctx, actorID, newMutationID(t), workspace.ID, "Base")
	if err != nil {
		t.Fatal(err)
	}
	table, err := catalogService.CreateTable(ctx, actorID, newMutationID(t), base.ID, "Aggregate", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	createField := func(tableID, name, fieldType string) domain.Field {
		var cfg any = domain.EmptyFieldConfig{}
		if fieldType == "number" {
			cfg = domain.NumberFieldConfig{}
		}
		if fieldType == "select" {
			cfg = catalog.SelectFieldConfigInput{Options: []catalog.SelectOptionInput{{Name: "Choice-only", Color: "lime"}}}
		}
		field, err := catalogService.CreateField(ctx, actorID, newMutationID(t), tableID, catalog.FieldInput{Name: name, Type: fieldType, Config: cfg})
		if err != nil {
			t.Fatal(err)
		}
		return field
	}
	primary := table.PrimaryField.ID
	notes := createField(table.Table.ID, "Notes", "longText")
	url := createField(table.Table.ID, "URL", "url")
	number := createField(table.Table.ID, "Score", "number")
	date := createField(table.Table.ID, "Due", "date")
	choice := createField(table.Table.ID, "Status", "select")
	option := choice.Config.(domain.SelectFieldConfig).Options[0]
	rows := []map[string]any{
		{primary: "Alpha", notes.ID: "Straße Cafe\u0301", number.ID: 2.5, date.ID: "2026-10-12", choice.ID: option.ID},
		{primary: "Beta", notes.ID: "ALPHA", number.ID: 0.0, date.ID: "2026-10-14"},
		{primary: "Gamma", url.ID: "https://example.test/alpha", number.ID: 5.0, date.ID: "2026-10-10"},
		{primary: "Café", number.ID: nil, date.ID: nil},
		{primary: "alpha empty"},
		{primary: "zzz", number.ID: 100.0, date.ID: "2026-11-01"},
		{primary: "Alpha deleted", number.ID: 7.0, date.ID: "2026-10-13"},
		{primary: "Unrelated", notes.ID: "STRASSE", number.ID: -1.0, date.ID: "2026-09-01"},
		{primary: "literal %_ token", number.ID: 11.0},
	}
	commands := make([]loomrecord.Command, len(rows))
	for i, row := range rows {
		commands[i] = loomrecord.Command{Kind: "createRecord", ValuesPresent: true, Values: row}
	}
	created, err := recordService.Mutate(ctx, actorID, table.Table.ID, newMutationID(t), commands)
	if err != nil {
		t.Fatal(err)
	}
	deleted := created.Results[6].Record
	if _, err := recordService.Mutate(ctx, actorID, table.Table.ID, newMutationID(t), []loomrecord.Command{{Kind: "deleteRecord", RecordID: deleted.ID, ExpectedRevision: deleted.Revision}}); err != nil {
		t.Fatal(err)
	}
	positive := &domain.FilterNode{Kind: "rule", FieldID: number.ID, Operator: "greaterThan", Value: json.RawMessage(`0`)}
	for _, tc := range []struct {
		name, search   string
		filter         *domain.FilterNode
		matched, count int64
		sum            any
	}{
		{"all", "", nil, 8, 6, 117.5}, {"whitespace", "\u2003 \t", nil, 8, 6, 117.5},
		{"search across text types", " ALPHA ", nil, 4, 3, 7.5},
		{"filter and search", "alpha", positive, 2, 2, 7.5},
		{"filter only", "", positive, 4, 4, 118.5},
		{"NFC and accents", "CAFE\u0301", nil, 2, 1, 2.5},
		{"accent-sensitive", "cafe", nil, 0, 0, nil},
		{"Unicode fold", "Straße", nil, 2, 2, 1.5},
		{"literal wildcards", "%_", nil, 1, 1, 11.0},
		{"no matches", "missing", nil, 0, 0, nil},
		{"Option labels excluded", "Choice-only", nil, 0, 0, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			query, err := recordService.Query(ctx, actorID, table.Table.ID, loomrecord.QueryRequest{Search: tc.search, SearchPresent: true, Filter: tc.filter, FilterPresent: true, Limit: 1, Projection: []string{number.ID}, ProjectionPresent: true})
			if err != nil {
				t.Fatal(err)
			}
			result, err := recordService.Aggregate(ctx, actorID, table.Table.ID, loomrecord.AggregateRequest{Search: tc.search, Filter: tc.filter, FieldIDs: []string{primary, number.ID, date.ID}, Functions: []string{"count", "sum", "avg", "min", "max"}})
			if err != nil {
				t.Fatal(err)
			}
			if *query.TotalCount != tc.matched || result.Results[primary]["count"] != tc.matched || query.HasMore != (tc.matched > 1) {
				t.Fatalf("Query/aggregate membership: query=%#v results=%#v", query, result.Results)
			}
			if result.Results[number.ID]["count"] != tc.count || !reflect.DeepEqual(result.Results[number.ID]["sum"], tc.sum) {
				t.Fatalf("numeric results = %#v", result.Results[number.ID])
			}
			if result.ChangeCursor != query.ChangeCursor {
				t.Fatal("unchanged Table returned different Change tails")
			}
			if tc.name == "search across text types" {
				want := map[string]any{"count": int64(3), "sum": 7.5, "avg": 2.5, "min": 0.0, "max": 5.0}
				if !reflect.DeepEqual(result.Results[number.ID], want) {
					t.Fatalf("all numeric functions = %#v", result.Results[number.ID])
				}
				want = map[string]any{"count": int64(3), "sum": nil, "avg": nil, "min": "2026-10-10", "max": "2026-10-14"}
				if !reflect.DeepEqual(result.Results[date.ID], want) {
					t.Fatalf("date functions = %#v", result.Results[date.ID])
				}
			}
			if tc.matched == 0 {
				for _, fieldID := range []string{primary, number.ID, date.ID} {
					want := map[string]any{"count": int64(0), "sum": nil, "avg": nil, "min": nil, "max": nil}
					if !reflect.DeepEqual(result.Results[fieldID], want) {
						t.Fatalf("empty results = %#v", result.Results[fieldID])
					}
				}
			}
		})
	}
	// Names are metadata, not searchable Record text.
	renamed := "Metadata-only"
	if _, err := catalogService.UpdateField(ctx, actorID, choice.ID, catalog.FieldUpdate{Type: "select", Name: &renamed, ExpectedRevision: choice.Revision, Config: catalog.SelectFieldConfigInput{Options: []catalog.SelectOptionInput{{ID: &option.ID, Name: renamed, Color: option.Color}}}}); err != nil {
		t.Fatal(err)
	}
	result, err := recordService.Aggregate(ctx, actorID, table.Table.ID, loomrecord.AggregateRequest{Search: renamed, FieldIDs: []string{primary}, Functions: []string{"count"}})
	if err != nil || result.Results[primary]["count"] != int64(0) {
		t.Fatalf("metadata polluted Search: %#v %v", result, err)
	}
	_, err = recordService.Aggregate(ctx, "act_00000000000000000000000000", table.Table.ID, loomrecord.AggregateRequest{Search: "alpha", FieldIDs: []string{primary}, Functions: []string{"count"}})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("foreign Actor = %v", err)
	}
	server := httpapi.New(config.Load(), func(context.Context) error { return nil }, httpapi.Dependencies{Authenticator: repository, Catalog: catalogService, Records: recordService})
	for _, tc := range []struct {
		search string
		status int
	}{{"alpha", 200}, {strings.Repeat("界", 501), 422}} {
		body, _ := json.Marshal(map[string]any{"fieldIds": []string{number.ID}, "fns": []string{"sum"}, "search": tc.search})
		r := httptest.NewRequest(http.MethodPost, "/v1/tables/"+table.Table.ID+"/records/aggregate", strings.NewReader(string(body)))
		r.Header.Set("Authorization", "Bearer "+bootstrap.Token.Secret)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("real HTTP = %d %s", w.Code, w.Body.String())
		}
		if tc.status == 200 && !strings.Contains(w.Body.String(), `"sum":7.5`) {
			t.Fatalf("HTTP scope = %s", w.Body.String())
		}
	}

	t.Run("concurrent result and changeCursor snapshot", func(t *testing.T) {
		table, err := catalogService.CreateTable(ctx, actorID, newMutationID(t), base.ID, "Snapshot", nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		left := createField(table.Table.ID, "Left", "number")
		right := createField(table.Table.ID, "Right", "number")
		initial, err := recordService.Mutate(ctx, actorID, table.Table.ID, newMutationID(t), []loomrecord.Command{{Kind: "createRecord", ValuesPresent: true, Values: map[string]any{left.ID: 0.0, right.ID: 0.0}}})
		if err != nil {
			t.Fatal(err)
		}
		cursors := map[float64]string{0: initial.ChangeCursor}
		mutationIDs := make([]string, 30)
		for i := range mutationIDs {
			mutationIDs[i] = newMutationID(t)
		}
		writerCtx, cancelWriter := context.WithCancel(ctx)
		writerDone := make(chan struct{})
		var writerErr error
		t.Cleanup(func() {
			cancelWriter()
			<-writerDone
		})
		go func() {
			defer close(writerDone)
			record := initial.Results[0].Record
			for i := 1; i <= 30; i++ {
				mutation, err := recordService.Mutate(writerCtx, actorID, table.Table.ID, mutationIDs[i-1], []loomrecord.Command{{Kind: "updateRecord", RecordID: record.ID, ExpectedRevision: record.Revision, SetPresent: true, Set: map[string]any{left.ID: float64(i), right.ID: float64(i)}}})
				if err != nil {
					writerErr = err
					return
				}
				record = mutation.Results[0].Record
				cursors[float64(i)] = mutation.ChangeCursor
			}
		}()
		observed := make([]loomrecord.AggregateResult, 0, 30)
		for i := 0; i < 30; i++ {
			result, err := recordService.Aggregate(ctx, actorID, table.Table.ID, loomrecord.AggregateRequest{FieldIDs: []string{left.ID, right.ID}, Functions: []string{"sum"}})
			if err != nil {
				t.Fatal(err)
			}
			observed = append(observed, result)
		}
		<-writerDone
		if writerErr != nil {
			t.Fatal(writerErr)
		}
		for _, result := range observed {
			value := result.Results[left.ID]["sum"].(float64)
			if result.Results[right.ID]["sum"] != value || result.ChangeCursor != cursors[value] {
				t.Fatalf("torn snapshot = %#v", result)
			}
		}
	})
}
