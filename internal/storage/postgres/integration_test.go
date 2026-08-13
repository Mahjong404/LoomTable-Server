package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	loomauth "github.com/Mahjong404/LoomTable-Server/internal/auth"
	"github.com/Mahjong404/LoomTable-Server/internal/catalog"
	"github.com/Mahjong404/LoomTable-Server/internal/domain"
	"github.com/Mahjong404/LoomTable-Server/internal/id"
	loomrecord "github.com/Mahjong404/LoomTable-Server/internal/record"
	"github.com/Mahjong404/LoomTable-Server/internal/storage/postgres"
)

func TestRepositoryP0EndToEnd(t *testing.T) {
	databaseURL := os.Getenv("LOOMTABLE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("LOOMTABLE_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
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
	locationField, err := catalogService.CreateField(ctx, actorID, newMutationID(t), tableResult.Table.ID, catalog.FieldInput{
		Name: "Location", Type: "location", Config: domain.EmptyFieldConfig{},
	})
	if err != nil {
		t.Fatal(err)
	}
	selectField, err := catalogService.CreateField(ctx, actorID, newMutationID(t), tableResult.Table.ID, catalog.FieldInput{
		Name: "Status", Type: "select", Config: catalog.SelectFieldConfigInput{Options: []catalog.SelectOptionInput{{Name: "Open", Color: "green"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	selectConfig := selectField.Config.(domain.SelectFieldConfig)
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
		}},
		{Kind: "createRecord", ValuesPresent: true, Values: map[string]any{
			tableResult.PrimaryField.ID: "Beta", locationField.ID: map[string]any{"lat": 31.3, "lng": 121.6},
		}},
		{Kind: "createRecord", ValuesPresent: true, Values: map[string]any{
			tableResult.PrimaryField.ID: "Gamma",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(mutation.Results) != 3 {
		t.Fatalf("mutation results = %d", len(mutation.Results))
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

	changeStart, err := recordService.Changes(ctx, actorID, tableResult.Table.ID, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	firstRecord := mutation.Results[0].Record
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

	summary, err := recordService.SummarizeMap(ctx, actorID, mapView.ID)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Summary.MatchedRecordCount != 3 || summary.Summary.RenderableRecordCount != 2 || summary.Summary.UnlocatedRecordCount != 1 {
		t.Fatalf("summary = %#v", summary)
	}
	mapResult, err := recordService.QueryMap(ctx, actorID, mapView.ID, loomrecord.MapQueryRequest{
		Viewport: loomrecord.MapViewport{Boxes: []loomrecord.MapViewportBox{{West: 120, South: 30, East: 123, North: 33}}},
		Zoom:     8, PixelWidth: 1000, PixelHeight: 800,
	})
	if err != nil {
		t.Fatal(err)
	}
	if mapResult.ViewportRenderableRecordCount != 2 || len(mapResult.Features) != 2 {
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
