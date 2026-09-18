package postgres_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	loomattachment "github.com/Mahjong404/LoomTable-Server/internal/attachment"
	loomauth "github.com/Mahjong404/LoomTable-Server/internal/auth"
	"github.com/Mahjong404/LoomTable-Server/internal/catalog"
	"github.com/Mahjong404/LoomTable-Server/internal/domain"
	"github.com/Mahjong404/LoomTable-Server/internal/id"
	loomrecord "github.com/Mahjong404/LoomTable-Server/internal/record"
	"github.com/Mahjong404/LoomTable-Server/internal/storage/postgres"
)

func TestRepositoryEndToEnd(t *testing.T) {
	databaseURL := os.Getenv("LOOMTABLE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("LOOMTABLE_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	db, err := postgres.Open(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var databaseName string
	if err := db.QueryRowContext(ctx, `SELECT current_database()`).Scan(&databaseName); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToLower(databaseName), "test") {
		t.Fatalf("refusing destructive integration setup for database %q: name must contain test", databaseName)
	}
	if _, err := db.ExecContext(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		t.Fatal(err)
	}
	migrationDirectory := filepath.Join("..", "..", "..", "migrations")
	if err := postgres.ApplyMigrations(ctx, db, migrationDirectory); err != nil {
		t.Fatal(err)
	}

	repository := postgres.NewRepository(db)
	admin := loomauth.NewAdmin(repository)
	bootstrap, err := admin.Bootstrap(ctx, "Primary")
	if err != nil {
		t.Fatal(err)
	}
	if !bootstrap.Created || bootstrap.Token == nil {
		t.Fatalf("bootstrap = %#v", bootstrap)
	}
	actorID, err := repository.Authenticate(ctx, bootstrap.Token.Secret)
	if err != nil || actorID != bootstrap.ActorID {
		t.Fatalf("Authenticate actor = %q, error = %v", actorID, err)
	}

	catalogService := catalog.New(repository)
	workspace, err := catalogService.CreateWorkspace(ctx, actorID, newMutationID(t), "Workspace")
	if err != nil {
		t.Fatal(err)
	}
	base, err := catalogService.CreateBase(ctx, actorID, newMutationID(t), workspace.ID, "Base")
	if err != nil {
		t.Fatal(err)
	}
	tableResult, err := catalogService.CreateTable(ctx, actorID, newMutationID(t), base.ID, "Places", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	locationDescription := " Geographic column "
	locationField, err := catalogService.CreateField(ctx, actorID, newMutationID(t), tableResult.Table.ID, catalog.FieldInput{
		Name: "Location", Type: "location", Config: domain.EmptyFieldConfig{}, Description: &locationDescription,
	})
	if err != nil {
		t.Fatal(err)
	}
	if locationField.Description != "Geographic column" {
		t.Fatalf("location description = %q", locationField.Description)
	}
	selectField, err := catalogService.CreateField(ctx, actorID, newMutationID(t), tableResult.Table.ID, catalog.FieldInput{
		Name: "Status", Type: "select", Config: catalog.SelectFieldConfigInput{Options: []catalog.SelectOptionInput{{Name: "Open", Color: "green"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	selectConfig := selectField.Config.(domain.SelectFieldConfig)
	attachmentField, err := catalogService.CreateField(ctx, actorID, newMutationID(t), tableResult.Table.ID, catalog.FieldInput{
		Name: "Attachments", Type: "attachment", Config: domain.AttachmentFieldConfig{MaxCount: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	clearedField, err := catalogService.UpdateField(ctx, actorID, locationField.ID, catalog.FieldUpdate{
		Type: "location", ExpectedRevision: locationField.Revision, DescriptionPresent: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if clearedField.Description != "" || clearedField.Revision != locationField.Revision+1 {
		t.Fatalf("cleared field = %#v", clearedField)
	}
	convertibleField, err := catalogService.CreateField(ctx, actorID, newMutationID(t), tableResult.Table.ID, catalog.FieldInput{
		Name: "Convertible", Type: "text", Config: domain.EmptyFieldConfig{},
	})
	if err != nil {
		t.Fatal(err)
	}
	numberField, err := catalogService.CreateField(ctx, actorID, newMutationID(t), tableResult.Table.ID, catalog.FieldInput{
		Name: "Score", Type: "number", Config: domain.EmptyFieldConfig{},
	})
	if err != nil {
		t.Fatal(err)
	}
	multiField, err := catalogService.CreateField(ctx, actorID, newMutationID(t), tableResult.Table.ID, catalog.FieldInput{
		Name: "Tags", Type: "multiSelect", Config: catalog.SelectFieldConfigInput{Options: []catalog.SelectOptionInput{{Name: "Urgent", Color: "red"}, {Name: "Later", Color: "gray"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	attachmentService := loomattachment.New(repository, loomattachment.NewFileStore(t.TempDir()), loomattachment.DefaultMaxBytes)
	attachment, err := attachmentService.Initialize(ctx, actorID, newMutationID(t), loomattachment.InitRequest{
		Source: "managed", Filename: "hello.txt", MimeType: "text/plain", Size: int64Pointer(5),
	})
	if err != nil {
		t.Fatal(err)
	}
	attachment, err = attachmentService.Upload(ctx, actorID, attachment.ID, "text/plain", bytes.NewReader([]byte("hello")))
	if err != nil {
		t.Fatal(err)
	}
	mapView, err := catalogService.CreateView(ctx, actorID, newMutationID(t), tableResult.Table.ID, catalog.ViewInput{
		Name: "Map", Type: "map", Config: domain.MapViewConfig{LocationFieldID: locationField.ID},
	})
	if err != nil {
		t.Fatal(err)
	}

	recordService := loomrecord.New(repository)
	mutation, err := recordService.Mutate(ctx, actorID, tableResult.Table.ID, newMutationID(t), []loomrecord.Command{
		{Kind: "createRecord", ValuesPresent: true, Values: map[string]any{
			tableResult.PrimaryField.ID: "Alpha Road", locationField.ID: map[string]any{"lat": 31.2, "lng": 121.5}, selectField.ID: selectConfig.Options[0].ID,
			attachmentField.ID:  []any{map[string]any{"id": attachment.ID, "source": attachment.Source, "filename": attachment.Filename, "mimeType": attachment.MimeType, "size": float64(*attachment.Size), "hash": attachment.Hash}},
			convertibleField.ID: "Open", numberField.ID: 2.5,
		}},
		{Kind: "createRecord", ValuesPresent: true, Values: map[string]any{
			tableResult.PrimaryField.ID: "Beta", locationField.ID: map[string]any{"lat": 31.3, "lng": 121.6}, convertibleField.ID: "Closed",
		}},
		{Kind: "createRecord", ValuesPresent: true, Values: map[string]any{
			tableResult.PrimaryField.ID: "Gamma", convertibleField.ID: "Open", numberField.ID: 5.0,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(mutation.Results) != 3 {
		t.Fatalf("mutation results = %d", len(mutation.Results))
	}

	preview, err := catalogService.PreviewFieldConversion(ctx, actorID, convertibleField.ID, "select")
	if err != nil {
		t.Fatal(err)
	}
	if !preview.Supported || len(preview.Modes) != 1 || preview.Modes[0].ID != "distinctOptions" {
		t.Fatalf("preview = %#v", preview)
	}
	if preview.Modes[0].Stats.OK != 3 || preview.Modes[0].Stats.DistinctValues != 2 || preview.Modes[0].Stats.NewOptions != 2 {
		t.Fatalf("preview stats = %#v", preview.Modes[0].Stats)
	}
	converted, err := catalogService.ConvertField(ctx, actorID, convertibleField.ID, catalog.ConversionRequest{
		TargetType: "select", Mode: "distinctOptions", ExpectedRevision: convertibleField.Revision, PreviewToken: preview.PreviewToken,
	})
	if err != nil {
		t.Fatal(err)
	}
	if converted.Field.Type != "select" || converted.Field.Revision != convertibleField.Revision+1 {
		t.Fatalf("converted field = %#v", converted.Field)
	}
	convertedConfig, ok := converted.Field.Config.(domain.SelectFieldConfig)
	if !ok || len(convertedConfig.Options) != 2 {
		t.Fatalf("converted config = %#v", converted.Field.Config)
	}
	optionIDs := make(map[string]string)
	for _, option := range convertedConfig.Options {
		optionIDs[option.Name] = option.ID
	}
	if optionIDs["Open"] == "" || optionIDs["Closed"] == "" {
		t.Fatalf("options = %#v", convertedConfig.Options)
	}
	convertedRecord, err := recordService.Get(ctx, actorID, mutation.Results[0].Record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if convertedRecord.Values[convertibleField.ID] != optionIDs["Open"] {
		t.Fatalf("converted value = %#v", convertedRecord.Values[convertibleField.ID])
	}

	query, err := recordService.Query(ctx, actorID, tableResult.Table.ID, loomrecord.QueryRequest{
		ProjectionPresent: true,
		Projection:        []string{tableResult.PrimaryField.ID},
		FilterPresent:     true,
		Filter: &domain.FilterNode{
			Kind: "rule", FieldID: tableResult.PrimaryField.ID, Operator: "contains", Value: json.RawMessage(`"ROAD"`),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(query.Items) != 1 || query.Items[0].Values[tableResult.PrimaryField.ID] != "Alpha Road" || query.TotalCount == nil || *query.TotalCount != 1 {
		t.Fatalf("query = %#v", query)
	}
	firstPage, err := recordService.Query(ctx, actorID, tableResult.Table.ID, loomrecord.QueryRequest{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !firstPage.HasMore || firstPage.NextCursor == "" || firstPage.TotalCount == nil || *firstPage.TotalCount != 3 {
		t.Fatalf("first page = %#v", firstPage)
	}
	secondPage, err := recordService.Query(ctx, actorID, tableResult.Table.ID, loomrecord.QueryRequest{Limit: 1, Cursor: firstPage.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	if len(secondPage.Items) != 1 || secondPage.TotalCount != nil || secondPage.Items[0].ID == firstPage.Items[0].ID {
		t.Fatalf("second page = %#v", secondPage)
	}
	if query.UnfilteredTotal == nil || *query.UnfilteredTotal != 3 {
		t.Fatalf("unfilteredTotal = %#v, want 3", query.UnfilteredTotal)
	}
	if secondPage.UnfilteredTotal != nil {
		t.Fatalf("second page unfilteredTotal = %#v, want nil", secondPage.UnfilteredTotal)
	}

	values, err := recordService.DistinctValues(ctx, actorID, tableResult.Table.ID, convertibleField.ID, loomrecord.DistinctValuesRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(values.Items) != 2 || values.EmptyCount != 0 {
		t.Fatalf("distinct values = %#v", values)
	}
	valueCounts := make(map[string]int64)
	for _, item := range values.Items {
		valueCounts[item.Value.(string)] = item.Count
	}
	if valueCounts[optionIDs["Open"]] != 2 || valueCounts[optionIDs["Closed"]] != 1 {
		t.Fatalf("distinct value counts = %#v", valueCounts)
	}
	selfFiltered, err := recordService.DistinctValues(ctx, actorID, tableResult.Table.ID, convertibleField.ID, loomrecord.DistinctValuesRequest{
		FilterPresent: true,
		Filter: &domain.FilterNode{
			Kind: "rule", FieldID: convertibleField.ID, Operator: "is", Value: json.RawMessage(`"` + optionIDs["Closed"] + `"`),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(selfFiltered.Items) != 2 {
		t.Fatalf("self-filtered values = %#v, want both options kept", selfFiltered.Items)
	}
	narrowed, err := recordService.DistinctValues(ctx, actorID, tableResult.Table.ID, convertibleField.ID, loomrecord.DistinctValuesRequest{
		FilterPresent: true,
		Filter: &domain.FilterNode{
			Kind: "rule", FieldID: tableResult.PrimaryField.ID, Operator: "contains", Value: json.RawMessage(`"alpha"`),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(narrowed.Items) != 1 || narrowed.Items[0].Count != 1 || narrowed.EmptyCount != 0 {
		t.Fatalf("filtered distinct values = %#v", narrowed)
	}
	numbers, err := recordService.DistinctValues(ctx, actorID, tableResult.Table.ID, numberField.ID, loomrecord.DistinctValuesRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(numbers.Items) != 2 || numbers.Items[0].Value != 2.5 || numbers.Items[1].Value != float64(5) || numbers.EmptyCount != 1 {
		t.Fatalf("number distinct values = %#v", numbers)
	}
	if _, err := recordService.DistinctValues(ctx, actorID, tableResult.Table.ID, locationField.ID, loomrecord.DistinctValuesRequest{}); err == nil {
		t.Fatal("location field must reject distinct values")
	}

	multiConfig := multiField.Config.(domain.SelectFieldConfig)
	gammaRecord := mutation.Results[2].Record
	if _, err := recordService.Mutate(ctx, actorID, tableResult.Table.ID, newMutationID(t), []loomrecord.Command{{
		Kind: "updateRecord", RecordID: gammaRecord.ID, ExpectedRevision: gammaRecord.Revision,
		SetPresent: true, Set: map[string]any{multiField.ID: []any{multiConfig.Options[0].ID, multiConfig.Options[1].ID}},
	}}); err != nil {
		t.Fatal(err)
	}
	multiValues, err := recordService.DistinctValues(ctx, actorID, tableResult.Table.ID, multiField.ID, loomrecord.DistinctValuesRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(multiValues.Items) != 2 || multiValues.EmptyCount != 2 ||
		multiValues.Items[0].Display != "Urgent" || multiValues.Items[1].Display != "Later" {
		t.Fatalf("multiSelect distinct values = %#v", multiValues)
	}

	aggregate, err := recordService.Aggregate(ctx, actorID, tableResult.Table.ID, loomrecord.AggregateRequest{
		FieldIDs:  []string{numberField.ID, tableResult.PrimaryField.ID},
		Functions: []string{"count", "sum", "avg", "min", "max"},
	})
	if err != nil {
		t.Fatal(err)
	}
	numberResults := aggregate.Results[numberField.ID]
	if numberResults["count"] != int64(2) || numberResults["sum"] != 7.5 || numberResults["avg"] != 3.75 ||
		numberResults["min"] != 2.5 || numberResults["max"] != float64(5) {
		t.Fatalf("number aggregate = %#v", numberResults)
	}
	textResults := aggregate.Results[tableResult.PrimaryField.ID]
	if textResults["count"] != int64(3) || textResults["sum"] != nil || textResults["min"] != nil {
		t.Fatalf("text aggregate = %#v", textResults)
	}
	filteredAggregate, err := recordService.Aggregate(ctx, actorID, tableResult.Table.ID, loomrecord.AggregateRequest{
		FieldIDs:  []string{numberField.ID},
		Functions: []string{"count", "sum"},
		Filter: &domain.FilterNode{
			Kind: "rule", FieldID: tableResult.PrimaryField.ID, Operator: "contains", Value: json.RawMessage(`"gamma"`),
		},
		FilterPresent: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if filteredAggregate.Results[numberField.ID]["count"] != int64(1) || filteredAggregate.Results[numberField.ID]["sum"] != float64(5) {
		t.Fatalf("filtered aggregate = %#v", filteredAggregate.Results)
	}

	gridView, err := catalogService.CreateView(ctx, actorID, newMutationID(t), tableResult.Table.ID, catalog.ViewInput{
		Name: "Manual", Type: "grid", Config: domain.GridViewConfig{
			Projection: []string{tableResult.PrimaryField.ID}, ColumnOrder: []string{tableResult.PrimaryField.ID},
			ColumnWidths: map[string]int{}, FrozenFieldIDs: []string{}, RowHeight: "standard", Sort: []domain.SortSpec{}, ManualSort: true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	manualQuery := func() []string {
		page, err := recordService.Query(ctx, actorID, tableResult.Table.ID, loomrecord.QueryRequest{ViewIDPresent: true, ViewID: gridView.ID})
		if err != nil {
			t.Fatal(err)
		}
		order := make([]string, len(page.Items))
		for index, item := range page.Items {
			order[index] = item.ID
		}
		return order
	}
	firstRecordID := mutation.Results[0].Record.ID
	betaRecordID := mutation.Results[1].Record.ID
	if order := manualQuery(); len(order) != 3 || order[0] != firstRecordID || order[1] != betaRecordID || order[2] != gammaRecord.ID {
		t.Fatalf("manual order = %v", order)
	}
	if _, err := recordService.Move(ctx, actorID, tableResult.Table.ID, gammaRecord.ID, loomrecord.MoveRequest{BeforeRecordID: betaRecordID}); err != nil {
		t.Fatal(err)
	}
	if order := manualQuery(); order[0] != firstRecordID || order[1] != gammaRecord.ID || order[2] != betaRecordID {
		t.Fatalf("order after move-before = %v", order)
	}
	if _, err := recordService.Move(ctx, actorID, tableResult.Table.ID, firstRecordID, loomrecord.MoveRequest{}); err != nil {
		t.Fatal(err)
	}
	if order := manualQuery(); order[0] != gammaRecord.ID || order[1] != betaRecordID || order[2] != firstRecordID {
		t.Fatalf("order after move-to-end = %v", order)
	}
	movedHistory, err := recordService.History(ctx, actorID, tableResult.Table.ID, loomrecord.HistoryRequest{Kind: "recordMoved"})
	if err != nil {
		t.Fatal(err)
	}
	if len(movedHistory.Items) != 2 || movedHistory.Items[0].RecordID != firstRecordID {
		t.Fatalf("recordMoved history = %#v", movedHistory.Items)
	}

	duplicated, err := recordService.Duplicate(ctx, actorID, tableResult.Table.ID, firstRecordID)
	if err != nil {
		t.Fatal(err)
	}
	if duplicated.Record.ID == firstRecordID || duplicated.Record.Values[tableResult.PrimaryField.ID] != "Alpha Road" {
		t.Fatalf("duplicated record = %#v", duplicated.Record)
	}
	sourceAttachment := mutation.Results[0].Record.Values[attachmentField.ID].([]any)[0].(map[string]any)
	copyAttachment := duplicated.Record.Values[attachmentField.ID].([]any)[0].(map[string]any)
	if copyAttachment["id"] != sourceAttachment["id"] {
		t.Fatalf("duplicated attachment = %#v, want shared reference", copyAttachment)
	}
	if order := manualQuery(); len(order) != 4 || order[3] != duplicated.Record.ID {
		t.Fatalf("order after duplicate = %v", order)
	}

	changeStart, err := recordService.Changes(ctx, actorID, tableResult.Table.ID, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	firstRecord := mutation.Results[0].Record
	secondRecord := mutation.Results[1].Record
	if _, err := recordService.Mutate(ctx, actorID, tableResult.Table.ID, newMutationID(t), []loomrecord.Command{{
		Kind: "updateRecord", RecordID: firstRecord.ID, ExpectedRevision: firstRecord.Revision,
		SetPresent: true, Set: map[string]any{tableResult.PrimaryField.ID: "Alpha Avenue"},
	}}); err != nil {
		t.Fatal(err)
	}
	changes, err := recordService.Changes(ctx, actorID, tableResult.Table.ID, changeStart.NextCursor, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes.Items) != 1 || changes.Items[0].Kind != "recordUpdated" {
		t.Fatalf("changes = %#v", changes)
	}
	if len(changes.Items[0].Fields) != 1 || changes.Items[0].Fields[0].FieldID != tableResult.PrimaryField.ID ||
		string(changes.Items[0].Fields[0].Before) != `"Alpha Road"` || string(changes.Items[0].Fields[0].After) != `"Alpha Avenue"` {
		t.Fatalf("change field diff = %#v", changes.Items[0].Fields)
	}

	history, err := recordService.History(ctx, actorID, tableResult.Table.ID, loomrecord.HistoryRequest{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(history.Items) < 2 || history.Items[0].Kind != "recordUpdated" {
		t.Fatalf("history = %#v", history)
	}
	for index := 1; index < len(history.Items); index++ {
		if history.Items[index].Sequence >= history.Items[index-1].Sequence {
			t.Fatalf("history is not descending: %#v", history.Items)
		}
	}
	updatedChange := history.Items[0]
	if len(updatedChange.Fields) != 1 || updatedChange.Fields[0].FieldID != tableResult.PrimaryField.ID ||
		string(updatedChange.Fields[0].Before) != `"Alpha Road"` || string(updatedChange.Fields[0].After) != `"Alpha Avenue"` ||
		updatedChange.PrimaryFieldText != "Alpha Avenue" {
		t.Fatalf("history entry = %#v", updatedChange)
	}
	var createdChange *loomrecord.Change
	for index := range history.Items {
		if history.Items[index].Kind == "recordCreated" && history.Items[index].RecordID == mutation.Results[2].Record.ID {
			createdChange = &history.Items[index]
		}
	}
	if createdChange == nil || createdChange.PrimaryFieldText != "Gamma" || len(createdChange.Fields) != 0 {
		t.Fatalf("recordCreated history entry = %#v", createdChange)
	}
	filtered, err := recordService.History(ctx, actorID, tableResult.Table.ID, loomrecord.HistoryRequest{
		RecordID: firstRecord.ID, Kind: "recordUpdated", FieldID: tableResult.PrimaryField.ID, Limit: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered.Items) != 1 || filtered.Items[0].RecordID != firstRecord.ID {
		t.Fatalf("filtered history = %#v", filtered.Items)
	}
	unrelatedField, err := recordService.History(ctx, actorID, tableResult.Table.ID, loomrecord.HistoryRequest{
		Kind: "recordUpdated", FieldID: selectField.ID, Limit: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(unrelatedField.Items) != 0 {
		t.Fatalf("unrelated field history = %#v", unrelatedField.Items)
	}
	paged, err := recordService.History(ctx, actorID, tableResult.Table.ID, loomrecord.HistoryRequest{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(paged.Items) != 1 || !paged.HasMore || paged.NextCursor == "" {
		t.Fatalf("history first page = %#v", paged)
	}
	rest, err := recordService.History(ctx, actorID, tableResult.Table.ID, loomrecord.HistoryRequest{Limit: 100, Cursor: paged.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	if len(rest.Items) != len(history.Items)-1 || rest.Items[0].Sequence >= paged.Items[0].Sequence {
		t.Fatalf("history second page = %#v", rest.Items)
	}

	replayMutationID := newMutationID(t)
	replayCommand := []loomrecord.Command{{Kind: "updateRecord", RecordID: secondRecord.ID, ExpectedRevision: secondRecord.Revision, SetPresent: true, Set: map[string]any{
		tableResult.PrimaryField.ID: "Beta Replay",
	}}}
	replayed, err := recordService.Mutate(ctx, actorID, tableResult.Table.ID, replayMutationID, replayCommand)
	if err != nil {
		t.Fatal(err)
	}
	replayedAgain, err := recordService.Mutate(ctx, actorID, tableResult.Table.ID, replayMutationID, replayCommand)
	if err != nil {
		t.Fatal(err)
	}
	if replayedAgain.Results[0].Record.ID != replayed.Results[0].Record.ID || replayedAgain.Results[0].Record.Revision != replayed.Results[0].Record.Revision {
		t.Fatalf("idempotent replay changed the Record: first=%#v replay=%#v", replayed.Results[0].Record, replayedAgain.Results[0].Record)
	}
	_, err = recordService.Mutate(ctx, actorID, tableResult.Table.ID, replayMutationID, []loomrecord.Command{{
		Kind: "updateRecord", RecordID: secondRecord.ID, ExpectedRevision: secondRecord.Revision, SetPresent: true, Set: map[string]any{tableResult.PrimaryField.ID: "Replay Twice"},
	}})
	var reused *domain.IdempotencyKeyReusedError
	if !errors.As(err, &reused) {
		t.Fatalf("different request reuse error = %v, want IdempotencyKeyReusedError", err)
	}

	staleMutationID := newMutationID(t)
	_, err = recordService.Mutate(ctx, actorID, tableResult.Table.ID, staleMutationID, []loomrecord.Command{{
		Kind: "updateRecord", RecordID: firstRecord.ID, ExpectedRevision: firstRecord.Revision,
		SetPresent: true, Set: map[string]any{tableResult.PrimaryField.ID: "Stale Client Value"},
	}})
	var stale *loomrecord.ConflictError
	if !errors.As(err, &stale) {
		t.Fatalf("stale revision error = %v, want ConflictError", err)
	}
	if stale.FailedCommandIndex != 0 || len(stale.Conflicts) != 1 || stale.Conflicts[0].CurrentRevision != firstRecord.Revision+1 {
		t.Fatalf("stale conflict = %#v", stale)
	}

	atomicMutationID := newMutationID(t)
	atomicStart, err := recordService.Changes(ctx, actorID, tableResult.Table.ID, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	_, err = recordService.Mutate(ctx, actorID, tableResult.Table.ID, atomicMutationID, []loomrecord.Command{
		{Kind: "updateRecord", RecordID: secondRecord.ID, ExpectedRevision: secondRecord.Revision + 1, SetPresent: true, Set: map[string]any{tableResult.PrimaryField.ID: "Should Not Persist"}},
		{Kind: "updateRecord", RecordID: firstRecord.ID, ExpectedRevision: firstRecord.Revision, SetPresent: true, Set: map[string]any{tableResult.PrimaryField.ID: "Should Not Persist"}},
	})
	if !errors.As(err, &stale) || stale.FailedCommandIndex != 1 {
		t.Fatalf("atomic conflict = %v, want failed command index 1", err)
	}
	rolledBack, err := recordService.Query(ctx, actorID, tableResult.Table.ID, loomrecord.QueryRequest{
		FilterPresent: true,
		Filter:        &domain.FilterNode{Kind: "rule", FieldID: tableResult.PrimaryField.ID, Operator: "is", Value: json.RawMessage(`"Should Not Persist"`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rolledBack.Items) != 0 {
		t.Fatalf("atomic mutation persisted a Record: %#v", rolledBack.Items)
	}
	postAtomicChanges, err := recordService.Changes(ctx, actorID, tableResult.Table.ID, atomicStart.NextCursor, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(postAtomicChanges.Items) != 0 {
		t.Fatalf("atomic mutation emitted changes: %#v", postAtomicChanges.Items)
	}
	if _, err := recordService.Mutate(ctx, actorID, tableResult.Table.ID, atomicMutationID, []loomrecord.Command{{
		Kind: "updateRecord", RecordID: secondRecord.ID, ExpectedRevision: secondRecord.Revision + 1, SetPresent: true, Set: map[string]any{tableResult.PrimaryField.ID: "Retry After Rollback"},
	}}); err != nil {
		t.Fatalf("rolled-back clientMutationId could not be retried: %v", err)
	}

	summary, err := recordService.SummarizeMap(ctx, actorID, mapView.ID)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Summary.MatchedRecordCount != 4 || summary.Summary.RenderableRecordCount != 3 || summary.Summary.UnlocatedRecordCount != 1 {
		t.Fatalf("summary = %#v", summary)
	}
	mapResult, err := recordService.QueryMap(ctx, actorID, mapView.ID, loomrecord.MapQueryRequest{
		Viewport: loomrecord.MapViewport{Boxes: []loomrecord.MapViewportBox{{West: 120, South: 30, East: 123, North: 33}}},
		Zoom:     8, PixelWidth: 1000, PixelHeight: 800,
	})
	if err != nil {
		t.Fatal(err)
	}
	if mapResult.ViewportRenderableRecordCount != 3 || len(mapResult.Features) != 3 {
		t.Fatalf("map result = %#v", mapResult)
	}

	additional, err := admin.Create(ctx, "Laptop")
	if err != nil {
		t.Fatal(err)
	}
	listed, err := admin.List(ctx)
	if err != nil || len(listed) != 2 {
		t.Fatalf("listed Tokens = %#v, error = %v", listed, err)
	}
	if _, err := admin.Revoke(ctx, additional.ID); err != nil {
		t.Fatal(err)
	}
	_, err = repository.Authenticate(ctx, additional.Secret)
	if !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatalf("revoked Token authentication error = %v", err)
	}
}

func newMutationID(t *testing.T) string {
	t.Helper()
	value, err := id.New(id.MutationPrefix)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func int64Pointer(value int64) *int64 {
	return &value
}
