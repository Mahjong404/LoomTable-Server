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
	"github.com/Mahjong404/LoomTable-Server/internal/id"
	record "github.com/Mahjong404/LoomTable-Server/internal/record"
	"github.com/Mahjong404/LoomTable-Server/internal/storage/postgres"
)

func TestSelectionRuntimeRestart(t *testing.T) {
	url := os.Getenv("LOOMTABLE_TEST_SELECTION_SERVER_URL")
	if url == "" {
		t.Skip("isolated restart runner is not configured")
	}
	if os.Getenv("LOOMTABLE_TEST_DATABASE_DISPOSABLE") != "true" || os.Getenv("LOOMTABLE_TEST_LEGACY_SERVER_DISPOSABLE") != "true" {
		t.Fatal("disposable runtime required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	db, err := postgres.Open(os.Getenv("LOOMTABLE_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var dbname string
	if err = db.QueryRowContext(ctx, `SELECT current_database()`).Scan(&dbname); err != nil || dbname != "loomtable_test_restore" {
		t.Fatal("restart only uses restored disposable DB", err)
	}
	repo := postgres.NewRepository(db)
	token, err := auth.NewAdmin(repo).Create(ctx, "G4 restart "+os.Getenv("LOOMTABLE_TEST_SELECTION_PHASE")+" synthetic")
	if err != nil {
		t.Fatal(err)
	}
	call := func(table, suffix string, body any) record.SelectionResult {
		t.Helper()
		raw, _ := json.Marshal(body)
		req, err := http.NewRequestWithContext(ctx, "POST", url+"/v1/tables/"+table+"/records/"+suffix, bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token.Secret)
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		payload, err := io.ReadAll(res.Body)
		if err != nil || res.StatusCode != 200 {
			t.Fatal("restart scope HTTP", res.StatusCode, err)
		}
		var value any
		if err = json.Unmarshal(payload, &value); err != nil {
			t.Fatal(err)
		}
		if err = compileOpenAPIResponseSchema(t, "#/components/schemas/SelectionResult").Validate(value); err != nil {
			t.Fatal(err)
		}
		var result record.SelectionResult
		json.Unmarshal(payload, &result)
		return result
	}
	switch os.Getenv("LOOMTABLE_TEST_SELECTION_PHASE") {
	case "seed":
		cat := catalog.New(repo)
		w, err := cat.CreateWorkspace(ctx, token.ActorID, newMutationID(t), "G4 restart")
		if err != nil {
			t.Fatal(err)
		}
		b, err := cat.CreateBase(ctx, token.ActorID, newMutationID(t), w.ID, "G4 restart")
		if err != nil {
			t.Fatal(err)
		}
		tab, err := cat.CreateTable(ctx, token.ActorID, newMutationID(t), b.ID, "G4 restart", nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = record.New(repo).Mutate(ctx, token.ActorID, tab.Table.ID, newMutationID(t), []record.Command{{Kind: "createRecord", ValuesPresent: true, Values: map[string]any{tab.PrimaryField.ID: "Alpha"}}}); err != nil {
			t.Fatal(err)
		}
		requestID, _ := id.New(id.RequestPrefix)
		scope := call(tab.Table.ID, "selection-scope", map[string]any{"requestId": requestID, "query": map[string]any{"search": "alpha"}})
		if scope.Scope.Count != 1 {
			t.Fatal("runtime scope members")
		}
		// Synthetic test proof, never an Access Token; stays only in the private DB.
		raw, _ := json.Marshal(scope.Scope)
		if _, err = db.ExecContext(ctx, `CREATE TABLE g4_qa_restart_proof(scope jsonb NOT NULL)`); err != nil {
			t.Fatal(err)
		}
		if _, err = db.ExecContext(ctx, `INSERT INTO g4_qa_restart_proof(scope) VALUES($1::jsonb)`, string(raw)); err != nil {
			t.Fatal(err)
		}
	case "verify":
		var raw []byte
		if err = db.QueryRowContext(ctx, `SELECT scope FROM g4_qa_restart_proof`).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var before record.SelectionScope
		if err = json.Unmarshal(raw, &before); err != nil {
			t.Fatal(err)
		}
		after := call(before.TableID, "selection-scope/resolve", map[string]any{"scopeToken": before.Token, "query": map[string]any{"search": "alpha"}})
		if after.Scope.ID != before.ID || after.Scope.Token != before.Token || after.Scope.SemanticDataVersion != before.SemanticDataVersion || after.Scope.Count != 1 {
			t.Fatal("actual process restart lost fixed membership/identity")
		}
		if _, err = db.ExecContext(ctx, `DROP TABLE g4_qa_restart_proof; DELETE FROM selection_scopes`); err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatal("restart phase required")
	}
}
