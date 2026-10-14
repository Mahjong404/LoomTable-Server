package postgres_test

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/Mahjong404/LoomTable-Server/internal/auth"
	"github.com/Mahjong404/LoomTable-Server/internal/catalog"
	"github.com/Mahjong404/LoomTable-Server/internal/storage/postgres"
	"io"
	"net/http"
	"os"
	"testing"
	"time"
)

// The private cloud runner supplies only its restored tmpfs database and owned runtime.
func TestAuditRuntimeRestart(t *testing.T) {
	endpoint := os.Getenv("LOOMTABLE_TEST_AUDIT_SERVER_URL")
	if endpoint == "" {
		t.Skip("owned cloud runtime not configured")
	}
	if os.Getenv("LOOMTABLE_TEST_DATABASE_DISPOSABLE") != "true" || os.Getenv("LOOMTABLE_TEST_LEGACY_SERVER_DISPOSABLE") != "true" {
		t.Fatal("owned disposable runtime required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	db, err := postgres.Open(os.Getenv("LOOMTABLE_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var dbname string
	if err := db.QueryRowContext(ctx, `SELECT current_database()`).Scan(&dbname); err != nil || dbname != "loomtable_test_restore" {
		t.Fatal("restored disposable DB required")
	}
	repo := postgres.NewRepository(db)
	phase := os.Getenv("LOOMTABLE_TEST_AUDIT_PHASE")
	token, err := auth.NewAdmin(repo).Create(ctx, "G5 runtime "+phase+" "+newMutationID(t))
	if err != nil {
		t.Fatal(err)
	}
	call := func(method, path string, body []byte) (*http.Response, map[string]any) {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, method, endpoint+path, bytes.NewReader(body))
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
		raw, err := io.ReadAll(res.Body)
		if err != nil || res.StatusCode != 200 {
			t.Fatalf("runtime=%d %s %v", res.StatusCode, raw, err)
		}
		var doc map[string]any
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatal(err)
		}
		return res, doc
	}
	var table, operation, cursor string
	var body []byte
	if phase == "seed" {
		cat := catalog.New(repo)
		workspaces, err := cat.ListWorkspaces(ctx, token.ActorID)
		if err != nil || len(workspaces) == 0 {
			t.Fatal(err)
		}
		base, err := cat.CreateBase(ctx, token.ActorID, newMutationID(t), workspaces[0].ID, "Runtime")
		if err != nil {
			t.Fatal(err)
		}
		created, err := cat.CreateTable(ctx, token.ActorID, newMutationID(t), base.ID, "Runtime", nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		table = created.Table.ID
		body, _ = json.Marshal(map[string]any{"clientMutationId": newMutationID(t), "commands": []any{map[string]any{"kind": "createRecord", "values": map[string]any{created.PrimaryField.ID: "Runtime"}}}})
		res, _ := call("POST", "/v1/tables/"+table+"/records/mutate", body)
		operation = res.Header.Get("X-LoomTable-Operation-Id")
		if operation == "" {
			t.Fatal("no operation identity")
		}
		_, page := call("GET", "/v1/tables/"+table+"/audit?limit=1", nil)
		cursor = page["nextCursor"].(string)
		if _, err := db.ExecContext(ctx, `CREATE TABLE g5_qa_restart_proof(table_id text,operation_id text,cursor text,body bytea)`); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO g5_qa_restart_proof VALUES($1,$2,$3,$4)`, table, operation, cursor, body); err != nil {
			t.Fatal(err)
		}
	} else if phase == "verify" {
		if err := db.QueryRowContext(ctx, `SELECT table_id,operation_id,cursor,body FROM g5_qa_restart_proof`).Scan(&table, &operation, &cursor, &body); err != nil {
			t.Fatal(err)
		}
		res, doc := call("POST", "/v1/tables/"+table+"/records/mutate", body)
		if res.Header.Get("X-LoomTable-Operation-Id") != operation {
			t.Fatal("restart/replay lost identity")
		}
		if err := mutationResponseSchema(t, 200).Validate(doc); err != nil {
			t.Fatal(err)
		}
		_, doc = call("GET", "/v1/tables/"+table+"/audit?operationId="+operation, nil)
		if doc["count"] != float64(1) {
			t.Fatal("restart/replay duplicated audit")
		}
		if err := compileOpenAPIResponseSchema(t, "#/components/schemas/AuditPage").Validate(doc); err != nil {
			t.Fatal(err)
		}
		_, doc = call("GET", "/v1/tables/"+table+"/audit?limit=1&cursor="+cursor, nil)
		if err := compileOpenAPIResponseSchema(t, "#/components/schemas/AuditPage").Validate(doc); err != nil {
			t.Fatal(err)
		}
	} else {
		t.Fatal("unknown runtime phase")
	}
}
