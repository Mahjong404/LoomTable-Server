package postgres_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	auth "github.com/Mahjong404/LoomTable-Server/internal/auth"
	"github.com/Mahjong404/LoomTable-Server/internal/catalog"
	"github.com/Mahjong404/LoomTable-Server/internal/storage/postgres"
)

// Only the private runner's restored synthetic DB and old binary are accepted.
// No reset, business migration, or cleanup of unrelated resources occurs here.
func TestSelectionLegacyBinaryCompatibility(t *testing.T) {
	url := os.Getenv("LOOMTABLE_TEST_LEGACY_SERVER_URL")
	if url == "" {
		t.Skip("isolated old-binary runner is not configured")
	}
	if os.Getenv("LOOMTABLE_TEST_DATABASE_DISPOSABLE") != "true" || os.Getenv("LOOMTABLE_TEST_LEGACY_SERVER_DISPOSABLE") != "true" {
		t.Fatal("requires isolated disposable runtime")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	db, err := postgres.Open(os.Getenv("LOOMTABLE_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var name string
	var scopes int
	if err = db.QueryRowContext(ctx, `SELECT current_database(),(SELECT count(*) FROM selection_scopes)`).Scan(&name, &scopes); err != nil || name != "loomtable_test_restore" || scopes != 0 {
		t.Fatal("downgrade requires restored disposable DB with cleared scopes", err)
	}
	repo := postgres.NewRepository(db)
	token, err := auth.NewAdmin(repo).Create(ctx, "G4 legacy synthetic")
	if err != nil {
		t.Fatal(err)
	}
	cat := catalog.New(repo)
	w, err := cat.CreateWorkspace(ctx, token.ActorID, newMutationID(t), "G4 legacy")
	if err != nil {
		t.Fatal(err)
	}
	b, err := cat.CreateBase(ctx, token.ActorID, newMutationID(t), w.ID, "G4 legacy")
	if err != nil {
		t.Fatal(err)
	}
	table, err := cat.CreateTable(ctx, token.ActorID, newMutationID(t), b.ID, "G4 legacy", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	call := func(suffix string, body any, status int) map[string]any {
		t.Helper()
		raw, _ := json.Marshal(body)
		req, err := http.NewRequestWithContext(ctx, "POST", url+"/v1/tables/"+table.Table.ID+"/records/"+suffix, bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token.Secret)
		req.Header.Set("Content-Type", "application/json")
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		payload, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != status {
			t.Fatalf("old binary %s HTTP %d, want %d", suffix, response.StatusCode, status)
		}
		var result map[string]any
		if err = json.Unmarshal(payload, &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	call("mutate", map[string]any{"clientMutationId": newMutationID(t), "commands": []any{map[string]any{"kind": "createRecord", "values": map[string]any{table.PrimaryField.ID: "Alpha"}}}}, 200)
	result := call("query", map[string]any{"search": "alpha"}, 200)
	if result["totalCount"] != float64(1) || result["selection"] != nil {
		t.Fatal("legacy request/response changed")
	}
	if err = compileOpenAPIResponseSchema(t, "#/components/schemas/QueryResult").Validate(result); err != nil {
		t.Fatal(err)
	}
	failure := call("query", map[string]any{"selectionScopeToken": "opaque"}, 422)
	if err = compileOpenAPIResponseSchema(t, "#/components/schemas/ErrorResponse").Validate(failure); err != nil {
		t.Fatal(err)
	}
	call("selection-scope", map[string]any{"requestId": "req_00000000000000000000000000", "query": map[string]any{}}, 404)
}
