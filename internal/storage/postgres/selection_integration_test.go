package postgres_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	auth "github.com/Mahjong404/LoomTable-Server/internal/auth"
	"github.com/Mahjong404/LoomTable-Server/internal/catalog"
	"github.com/Mahjong404/LoomTable-Server/internal/config"
	"github.com/Mahjong404/LoomTable-Server/internal/domain"
	"github.com/Mahjong404/LoomTable-Server/internal/httpapi"
	"github.com/Mahjong404/LoomTable-Server/internal/id"
	record "github.com/Mahjong404/LoomTable-Server/internal/record"
	"github.com/Mahjong404/LoomTable-Server/internal/storage/postgres"
)

func TestWholeQuerySelection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	db := integrationDatabase(t, ctx)
	repo := postgres.NewRepository(db)
	boot, err := auth.NewAdmin(repo).Bootstrap(ctx, "G4 synthetic")
	if err != nil {
		t.Fatal(err)
	}
	actor := boot.ActorID
	cat := catalog.New(repo)
	records := record.New(repo)
	ws, err := cat.CreateWorkspace(ctx, actor, newMutationID(t), "G4")
	if err != nil {
		t.Fatal(err)
	}
	base, err := cat.CreateBase(ctx, actor, newMutationID(t), ws.ID, "G4")
	if err != nil {
		t.Fatal(err)
	}
	table := func(t *testing.T) domain.CreateTableResult {
		t.Helper()
		v, err := cat.CreateTable(ctx, actor, newMutationID(t), base.ID, "Selection", nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if _, err := db.ExecContext(ctx, `DELETE FROM selection_scopes WHERE table_id=$1`, v.Table.ID); err != nil {
				t.Error(err)
			}
		})
		return v
	}
	requestID := func() string {
		v, err := id.New(id.RequestPrefix)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	acquire := func(t *testing.T, tableID string, q record.SelectionQuery) record.SelectionResult {
		t.Helper()
		v, err := records.AcquireSelection(ctx, actor, tableID, requestID(), q)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	resolve := func(v record.SelectionResult, q record.SelectionQuery) (record.SelectionResult, error) {
		return records.ResolveSelection(ctx, actor, v.Scope.TableID, v.Scope.Token, q, nil, nil)
	}
	expired := func(t *testing.T, err error) {
		t.Helper()
		var e *domain.QuerySnapshotExpiredError
		if !errors.As(err, &e) {
			t.Fatalf("want Scope expired, got %v", err)
		}
	}
	create := func(t *testing.T, tab domain.CreateTableResult, text string) record.Record {
		t.Helper()
		v, err := records.Mutate(ctx, actor, tab.Table.ID, newMutationID(t), []record.Command{{Kind: "createRecord", Values: map[string]any{tab.PrimaryField.ID: text}, ValuesPresent: true}})
		if err != nil {
			t.Fatal(err)
		}
		return v.Results[0].Record
	}
	update := func(t *testing.T, item record.Record, field, text string) record.Record {
		t.Helper()
		v, err := records.Mutate(ctx, actor, item.TableID, newMutationID(t), []record.Command{{Kind: "updateRecord", RecordID: item.ID, ExpectedRevision: item.Revision, Set: map[string]any{field: text}, SetPresent: true}})
		if err != nil {
			t.Fatal(err)
		}
		return v.Results[0].Record
	}
	qAlpha := record.SelectionQuery{Search: "alpha"}

	t.Run("10000 members only 100 loaded all cursor pages and exclusions", func(t *testing.T) {
		tab := table(t)
		number, err := cat.CreateField(ctx, actor, newMutationID(t), tab.Table.ID, catalog.FieldInput{Name: "Number", Type: "number", Config: domain.NumberFieldConfig{}})
		if err != nil {
			t.Fatal(err)
		}
		_, err = db.ExecContext(ctx, `INSERT INTO records(id,table_id,values,query_values,search_text,position)
    SELECT 'rec_'||lpad(upper(to_hex(i)),26,'0'),$1,
    jsonb_build_object($2::text,CASE WHEN i=10002 THEN 'Beta' ELSE 'Alpha' END,$3::text,CASE WHEN i=10001 THEN 0 ELSE 1 END),
    jsonb_build_object($2::text,CASE WHEN i=10002 THEN 'beta' ELSE 'alpha' END,$3::text,CASE WHEN i=10001 THEN 0 ELSE 1 END),
    CASE WHEN i=10002 THEN 'beta' ELSE 'alpha' END,i*1024.0 FROM generate_series(1,10002) i`, tab.Table.ID, tab.PrimaryField.ID, number.ID)
		if err != nil {
			t.Fatal(err)
		}
		filter := &domain.FilterNode{Kind: "rule", FieldID: number.ID, Operator: "greaterThan", Value: json.RawMessage(`0`)}
		query := record.SelectionQuery{Filter: filter, FilterPresent: true, Search: "  ALPHA  "}
		start := time.Now()
		v := acquire(t, tab.Table.ID, query)
		t.Logf("10000-member acquisition: %s", time.Since(start))
		if v.Scope.Count != 10000 || len(v.Membership) != 0 || v.SelectedCount != 10000 {
			t.Fatal("loaded page substituted for whole query")
		}
		for _, test := range []struct {
			q     record.SelectionQuery
			count int64
		}{{record.SelectionQuery{Filter: filter, FilterPresent: true}, 10001}, {qAlpha, 10001}, {record.SelectionQuery{FilterPresent: true, Filter: &domain.FilterNode{Kind: "rule", FieldID: number.ID, Operator: "greaterThan", Value: json.RawMessage(`2`)}}, 0}} {
			s := acquire(t, tab.Table.ID, test.q)
			if s.Scope.Count != test.count {
				t.Fatal("Filter/Search/empty scope", s.Scope.Count)
			}
		}
		recID := func(i int) string { return fmt.Sprintf("rec_%026X", i) }
		selected, err := records.ResolveSelection(ctx, actor, tab.Table.ID, v.Scope.Token, query, []string{recID(1)}, []string{recID(1), recID(10000), recID(10002)})
		if err != nil {
			t.Fatal(err)
		}
		if selected.SelectedCount != 9999 || selected.ExcludedCount != 1 || selected.Membership[0].Selected || !selected.Membership[1].Selected || selected.Membership[2].InScope {
			t.Fatal("exclusion/membership proof")
		}
		seen := map[string]bool{}
		request := record.QueryRequest{Filter: filter, FilterPresent: true, Search: "alpha", Limit: 100, SelectionScopeToken: v.Scope.Token, SelectionExcludedIDs: []string{recID(1)}}
		start = time.Now()
		for page := 0; ; page++ {
			result, err := records.Query(ctx, actor, tab.Table.ID, request)
			if err != nil {
				t.Fatal(err)
			}
			if result.Selection == nil || result.Selection.SelectedCount != 9999 || len(result.Selection.Membership) != len(result.Items) {
				t.Fatal("scope/page snapshot missing")
			}
			if page == 0 && (len(result.Items) != 100 || result.TotalCount == nil || *result.TotalCount != 10000) {
				t.Fatal("first page count")
			}
			for _, r := range result.Items {
				if seen[r.ID] {
					t.Fatal("duplicate cursor member")
				}
				seen[r.ID] = true
			}
			if !result.HasMore {
				break
			}
			request.Cursor = result.NextCursor
		}
		if len(seen) != 10000 {
			t.Fatal("whole-query cursor coverage", len(seen))
		}
		t.Logf("100 pages with scope validation: %s", time.Since(start))
		if _, err := db.ExecContext(ctx, `ANALYZE records; ANALYZE selection_scope_members`); err != nil {
			t.Fatal(err)
		}
		var planJSON []byte
		if err := db.QueryRowContext(ctx, `EXPLAIN(ANALYZE,BUFFERS,FORMAT JSON) SELECT EXISTS(SELECT 1 FROM unnest($1::text[]) candidate(id) WHERE EXISTS(SELECT 1 FROM selection_scope_members m WHERE m.scope_id=$2 AND m.record_id=candidate.id) IS DISTINCT FROM EXISTS(SELECT 1 FROM records r WHERE r.id=candidate.id AND r.table_id=$3 AND r.deleted_at IS NULL AND strpos(r.search_text,'alpha')>0 AND (r.query_values->>$4::text)::double precision>0))`, []string{recID(10000)}, v.Scope.ID, tab.Table.ID, number.ID).Scan(&planJSON); err != nil {
			t.Fatal(err)
		}
		var plans []map[string]any
		if err := json.Unmarshal(planJSON, &plans); err != nil {
			t.Fatal(err)
		}
		indexed := false
		var inspect func(any)
		inspect = func(value any) {
			switch v := value.(type) {
			case map[string]any:
				if v["Relation Name"] == "records" {
					if v["Node Type"] == "Seq Scan" {
						t.Fatal("candidate detection scanned entire Table")
					}
					indexed = true
				}
				for _, child := range v {
					inspect(child)
				}
			case []any:
				for _, child := range v {
					inspect(child)
				}
			}
		}
		inspect(plans[0])
		if !indexed {
			t.Fatal("candidate query omitted Record index proof")
		}
		t.Logf("one changed ID detection EXPLAIN execution ms: %v", plans[0]["Execution Time"])
		request.Cursor = ""
		request.SortPresent = true
		request.Sort = []domain.SortSpec{{FieldID: number.ID, Direction: "desc"}}
		result, err := records.Query(ctx, actor, tab.Table.ID, request)
		if err != nil || result.Selection.Scope.ID != v.Scope.ID {
			t.Fatal("Sort changed scope", err)
		}
		unchanged, err := records.Get(ctx, actor, recID(10000))
		if err != nil {
			t.Fatal(err)
		}
		unchanged = update(t, unchanged, tab.PrimaryField.ID, "Alpha edited")
		still, err := resolve(v, query)
		if err != nil || still.Scope.SemanticDataVersion != v.Scope.SemanticDataVersion {
			t.Fatal("same-member value update invalidated scope", err)
		}
		outside, err := records.Get(ctx, actor, recID(10002))
		if err != nil {
			t.Fatal(err)
		}
		update(t, outside, tab.PrimaryField.ID, "Beta edited")
		if _, err = resolve(v, query); err != nil {
			t.Fatal("nonmember remained nonmember", err)
		}
		unchanged = update(t, unchanged, tab.PrimaryField.ID, "Beta")
		_, err = resolve(v, query)
		expired(t, err)
		update(t, unchanged, tab.PrimaryField.ID, "Alpha returned")
		_, err = resolve(v, query)
		expired(t, err)
	})

	t.Run("membership transitions new delete restore duplicate no automatic expansion", func(t *testing.T) {
		tab := table(t)
		a := create(t, tab, "Alpha")
		originalRequest := requestID()
		v, err := records.AcquireSelection(ctx, actor, tab.Table.ID, originalRequest, qAlpha)
		if err != nil {
			t.Fatal(err)
		}
		create(t, tab, "Beta")
		if _, err = resolve(v, qAlpha); err != nil {
			t.Fatal("nonmatching creation invalidates", err)
		}
		create(t, tab, "Alpha new")
		_, err = resolve(v, qAlpha)
		expired(t, err)
		v = acquire(t, tab.Table.ID, qAlpha)
		_, err = records.AcquireSelection(ctx, actor, tab.Table.ID, originalRequest, qAlpha)
		expired(t, err)
		deleted, err := records.Mutate(ctx, actor, tab.Table.ID, newMutationID(t), []record.Command{{Kind: "deleteRecord", RecordID: a.ID, ExpectedRevision: a.Revision}})
		if err != nil {
			t.Fatal(err)
		}
		_, err = resolve(v, qAlpha)
		expired(t, err)
		v = acquire(t, tab.Table.ID, qAlpha)
		_, err = records.Mutate(ctx, actor, tab.Table.ID, newMutationID(t), []record.Command{{Kind: "restoreRecord", RecordID: a.ID, ExpectedRevision: deleted.Results[0].Record.Revision}})
		if err != nil {
			t.Fatal(err)
		}
		_, err = resolve(v, qAlpha)
		expired(t, err)
		v = acquire(t, tab.Table.ID, qAlpha)
		if _, err = records.Duplicate(ctx, actor, tab.Table.ID, a.ID); err != nil {
			t.Fatal(err)
		}
		_, err = resolve(v, qAlpha)
		expired(t, err)
	})

	t.Run("rollback no-op replay unknown response and independent clients", func(t *testing.T) {
		tab := table(t)
		a := create(t, tab, "Alpha")
		key := requestID()
		v, err := records.AcquireSelection(ctx, actor, tab.Table.ID, key, qAlpha)
		if err != nil {
			t.Fatal(err)
		}
		again, err := record.New(postgres.NewRepository(db)).AcquireSelection(ctx, actor, tab.Table.ID, key, qAlpha)
		if err != nil || again.Scope.Token != v.Scope.Token {
			t.Fatal("restart/replay changed identity", err)
		}
		other := acquire(t, tab.Table.ID, qAlpha)
		if other.Scope.ID == v.Scope.ID {
			t.Fatal("two clients share allocation identity")
		}
		_, err = records.AcquireSelection(ctx, actor, tab.Table.ID, key, record.SelectionQuery{})
		var reuse *domain.IdempotencyKeyReusedError
		if !errors.As(err, &reuse) {
			t.Fatal("different body reused request", err)
		}
		a = update(t, a, tab.PrimaryField.ID, "Alpha")
		if _, err = resolve(v, qAlpha); err != nil {
			t.Fatal("no-op invalidated", err)
		}
		if _, err = db.ExecContext(ctx, `CREATE FUNCTION g4_fail_receipt() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic G4 rollback'; END $$; CREATE TRIGGER g4_fail_receipt BEFORE INSERT ON idempotency_keys FOR EACH ROW EXECUTE FUNCTION g4_fail_receipt()`); err != nil {
			t.Fatal(err)
		}
		_, err = records.Mutate(ctx, actor, tab.Table.ID, newMutationID(t), []record.Command{{Kind: "updateRecord", RecordID: a.ID, ExpectedRevision: a.Revision, Set: map[string]any{tab.PrimaryField.ID: "Beta"}, SetPresent: true}})
		if err == nil {
			t.Fatal("failure injector ignored")
		}
		if _, err = db.ExecContext(ctx, `DROP TRIGGER g4_fail_receipt ON idempotency_keys; DROP FUNCTION g4_fail_receipt()`); err != nil {
			t.Fatal(err)
		}
		if _, err = resolve(v, qAlpha); err != nil {
			t.Fatal("transaction failure partially invalidated", err)
		}
		a = update(t, a, tab.PrimaryField.ID, "Alpha new")
		if _, err = resolve(other, qAlpha); err != nil {
			t.Fatal(err)
		}
		_, err = records.ResolveSelection(ctx, actor, tab.Table.ID, v.Scope.Token, record.SelectionQuery{Search: "beta"}, nil, nil)
		expired(t, err)
		if _, err = resolve(v, qAlpha); err != nil {
			t.Fatal("another client query mismatch changed scope", err)
		}
	})

	t.Run("HTTP schema security expiry exclusions and legacy page", func(t *testing.T) {
		tab := table(t)
		a := create(t, tab, "Alpha")
		server := httpapi.New(config.Load(), postgres.ReadyChecker(db), httpapi.Dependencies{Authenticator: repo, Catalog: cat, Records: records})
		call := func(suffix string, body any, status int) *httptest.ResponseRecorder {
			t.Helper()
			raw, _ := json.Marshal(body)
			req := httptest.NewRequest("POST", "/v1/tables/"+tab.Table.ID+"/records/"+suffix, bytes.NewReader(raw))
			req.Header.Set("Authorization", "Bearer "+boot.Token.Secret)
			req.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			server.Handler().ServeHTTP(response, req)
			if response.Code != status {
				t.Fatalf("%s status %d, want %d", suffix, response.Code, status)
			}
			var value any
			if err := json.Unmarshal(response.Body.Bytes(), &value); err != nil {
				t.Fatal(err)
			}
			schema := "#/components/schemas/ErrorResponse"
			if status == 200 {
				schema = "#/components/schemas/SelectionResult"
				if suffix == "query" {
					schema = "#/components/schemas/QueryResult"
				}
			}
			if err := compileOpenAPIResponseSchema(t, schema).Validate(value); err != nil {
				t.Fatal("full response schema", err)
			}
			return response
		}
		body := map[string]any{"requestId": requestID(), "query": map[string]any{"search": "alpha"}}
		rawLost, _ := json.Marshal(body)
		lostRequest := httptest.NewRequest("POST", "/v1/tables/"+tab.Table.ID+"/records/selection-scope", bytes.NewReader(rawLost))
		lostRequest.Header.Set("Authorization", "Bearer "+boot.Token.Secret)
		lostRequest.Header.Set("Content-Type", "application/json")
		lost := &lostChoiceResponse{header: make(map[string][]string)}
		server.Handler().ServeHTTP(lost, lostRequest)
		if !lost.failed {
			t.Fatal("lost HTTP response injector did not run")
		}
		first := call("selection-scope", body, 200)
		again := call("selection-scope", body, 200)
		if !bytes.Equal(first.Body.Bytes(), again.Body.Bytes()) {
			t.Fatal("lost response retry changed Scope")
		}
		var v record.SelectionResult
		if err := json.Unmarshal(first.Body.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
		call("selection-scope/resolve", map[string]any{"scopeToken": v.Scope.Token, "query": map[string]any{"search": "alpha"}, "excludedIds": []string{a.ID}, "recordIds": []string{a.ID}}, 200)
		foreign, _ := id.New(id.RecordPrefix)
		call("selection-scope/resolve", map[string]any{"scopeToken": v.Scope.Token, "query": map[string]any{"search": "alpha"}, "excludedIds": []string{foreign}}, 422)
		call("selection-scope/resolve", map[string]any{"scopeToken": v.Scope.Token + "bad", "query": map[string]any{}}, 400)
		call("query", map[string]any{"search": "alpha", "selectionScopeToken": v.Scope.Token}, 200)
		legacy := call("query", map[string]any{"search": "alpha"}, 200)
		var legacyBody map[string]any
		json.Unmarshal(legacy.Body.Bytes(), &legacyBody)
		if _, ok := legacyBody["selection"]; ok {
			t.Fatal("legacy response changed")
		}
		_, err := records.ResolveSelection(ctx, "act_00000000000000000000000000", tab.Table.ID, v.Scope.Token, qAlpha, nil, nil)
		var bad *domain.InvalidCursorError
		if !errors.As(err, &bad) {
			t.Fatal("Actor binding", err)
		}
		other := table(t)
		_, err = records.ResolveSelection(ctx, actor, other.Table.ID, v.Scope.Token, qAlpha, nil, nil)
		if !errors.As(err, &bad) {
			t.Fatal("Table binding", err)
		}
		future := record.NewWithClock(repo, func() time.Time { return v.Scope.ExpiresAt })
		_, err = future.ResolveSelection(ctx, actor, tab.Table.ID, v.Scope.Token, qAlpha, nil, nil)
		expired(t, err)
		issued, err := auth.NewAdmin(repo).Create(ctx, "G4 revocation")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = auth.NewAdmin(repo).Revoke(ctx, issued.ID); err != nil {
			t.Fatal(err)
		}
		revokedRequest := httptest.NewRequest("POST", "/v1/tables/"+tab.Table.ID+"/records/selection-scope/resolve", bytes.NewBufferString("{}"))
		revokedRequest.Header.Set("Authorization", "Bearer "+issued.Secret)
		revokedRequest.Header.Set("Content-Type", "application/json")
		revokedResponse := httptest.NewRecorder()
		server.Handler().ServeHTTP(revokedResponse, revokedRequest)
		if revokedResponse.Code != 401 {
			t.Fatal("revoked token authorized selection")
		}
		var revokedBody any
		json.Unmarshal(revokedResponse.Body.Bytes(), &revokedBody)
		if err = compileOpenAPIResponseSchema(t, "#/components/schemas/ErrorResponse").Validate(revokedBody); err != nil {
			t.Fatal(err)
		}
		update(t, a, tab.PrimaryField.ID, "Beta")
		call("selection-scope/resolve", map[string]any{"scopeToken": v.Scope.Token, "query": map[string]any{"search": "alpha"}}, 410)
	})

	t.Run("query schema context presentation independent and ancestor revocation", func(t *testing.T) {
		tab := table(t)
		a := create(t, tab, "Alpha")
		v := acquire(t, tab.Table.ID, record.SelectionQuery{ViewID: tab.InitialView.ID, Search: "alpha"})
		rename := "Presentation rename"
		_, err := cat.UpdateView(ctx, actor, tab.InitialView.ID, catalog.ViewUpdate{ExpectedRevision: tab.InitialView.Revision, Name: &rename, Type: tab.InitialView.Type, Config: tab.InitialView.Config})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = resolve(v, record.SelectionQuery{ViewID: tab.InitialView.ID, Search: "alpha"}); err != nil {
			t.Fatal("View Revision alone invalidated", err)
		}
		if _, err = records.Move(ctx, actor, tab.Table.ID, a.ID, record.MoveRequest{}); err != nil {
			t.Fatal(err)
		}
		if _, err = resolve(v, record.SelectionQuery{ViewID: tab.InitialView.ID, Search: "alpha"}); err != nil {
			t.Fatal("Move invalidated", err)
		}
		_, err = cat.CreateField(ctx, actor, newMutationID(t), tab.Table.ID, catalog.FieldInput{Name: "Search field", Type: "text", Config: domain.EmptyFieldConfig{}})
		if err != nil {
			t.Fatal(err)
		}
		_, err = resolve(v, record.SelectionQuery{ViewID: tab.InitialView.ID, Search: "alpha"})
		expired(t, err)
		v = acquire(t, tab.Table.ID, qAlpha)
		if err = cat.DeleteTable(ctx, actor, tab.Table.ID, tab.Table.Revision); err != nil {
			t.Fatal(err)
		}
		_, err = resolve(v, qAlpha)
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatal("permission revocation", err)
		}
		if _, err = cat.RestoreTable(ctx, actor, tab.Table.ID, tab.Table.Revision+1); err != nil {
			t.Fatal(err)
		}
		_, err = resolve(v, qAlpha)
		expired(t, err)
	})

	t.Run("concurrent allocation and writer serialize without losing members", func(t *testing.T) {
		tab := table(t)
		create(t, tab, "Alpha")
		var scope record.SelectionResult
		var allocationErr, writerErr error
		var wg sync.WaitGroup
		wg.Add(2)
		start := make(chan struct{})
		go func() {
			defer wg.Done()
			<-start
			scope, allocationErr = records.AcquireSelection(ctx, actor, tab.Table.ID, requestID(), qAlpha)
		}()
		go func() {
			defer wg.Done()
			<-start
			_, writerErr = records.Mutate(ctx, actor, tab.Table.ID, newMutationID(t), []record.Command{{Kind: "createRecord", Values: map[string]any{tab.PrimaryField.ID: "Alpha concurrent"}, ValuesPresent: true}})
		}()
		close(start)
		wg.Wait()
		if allocationErr != nil || writerErr != nil {
			t.Fatal("concurrent operations", allocationErr, writerErr)
		}
		current, err := resolve(scope, qAlpha)
		if scope.Scope.Count == 1 {
			expired(t, err)
		} else if err != nil || scope.Scope.Count != 2 || current.Scope.Count != 2 {
			t.Fatal("silent membership overwrite", err)
		}
	})

	t.Run("page snapshot remains internally consistent across concurrent membership commit", func(t *testing.T) {
		tab := table(t)
		a := create(t, tab, "Alpha")
		scope := acquire(t, tab.Table.ID, qAlpha)
		blocker, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer blocker.Rollback()
		if _, err = blocker.ExecContext(ctx, `LOCK records IN ACCESS EXCLUSIVE MODE`); err != nil {
			t.Fatal(err)
		}
		type pageResult struct {
			page record.QueryResult
			err  error
		}
		pages := make(chan pageResult, 1)
		writes := make(chan error, 1)
		go func() {
			page, err := records.Query(ctx, actor, tab.Table.ID, record.QueryRequest{Search: "alpha", SelectionScopeToken: scope.Scope.Token})
			pages <- pageResult{page, err}
		}()
		waitForChoiceLock(t, ctx, db, "%count(*) FROM records r WHERE%")
		go func() {
			_, err := records.Mutate(ctx, actor, tab.Table.ID, newMutationID(t), []record.Command{{Kind: "updateRecord", RecordID: a.ID, ExpectedRevision: a.Revision, SetPresent: true, Set: map[string]any{tab.PrimaryField.ID: "Beta"}}})
			writes <- err
		}()
		waitForChoiceLock(t, ctx, db, "%FOR UPDATE%")
		if err = blocker.Commit(); err != nil {
			t.Fatal(err)
		}
		read := <-pages
		if read.err != nil {
			t.Fatal(read.err)
		}
		if err = <-writes; err != nil {
			t.Fatal(err)
		}
		if read.page.Selection == nil || read.page.Selection.Scope.Count != 1 || len(read.page.Items) != 1 || read.page.Items[0].Values[tab.PrimaryField.ID] != "Alpha" {
			t.Fatal("mixed page/order validity snapshots")
		}
		_, err = resolve(scope, qAlpha)
		expired(t, err)
	})

	t.Run("same members keep scope while changed sort values restart page cursor", func(t *testing.T) {
		tab := table(t)
		a := create(t, tab, "Alpha a")
		create(t, tab, "Alpha b")
		scope := acquire(t, tab.Table.ID, qAlpha)
		query := record.QueryRequest{Search: "alpha", Limit: 1, SortPresent: true, Sort: []domain.SortSpec{{FieldID: tab.PrimaryField.ID, Direction: "asc"}}, SelectionScopeToken: scope.Scope.Token}
		first, err := records.Query(ctx, actor, tab.Table.ID, query)
		if err != nil || !first.HasMore {
			t.Fatal("first page", err)
		}
		update(t, a, tab.PrimaryField.ID, "Alpha z")
		query.Cursor = first.NextCursor
		_, err = records.Query(ctx, actor, tab.Table.ID, query)
		var cursor *domain.CursorExpiredError
		if !errors.As(err, &cursor) {
			t.Fatal("ordering drift not caught", err)
		}
		if _, err = resolve(scope, qAlpha); err != nil {
			t.Fatal("Data Version alone invalidated scope", err)
		}
		query.Cursor = ""
		second, err := records.Query(ctx, actor, tab.Table.ID, query)
		if err != nil || second.Selection.Scope.ID != scope.Scope.ID {
			t.Fatal("cursor restart lost scope", err)
		}
		legacy, err := records.Query(ctx, actor, tab.Table.ID, record.QueryRequest{Limit: 1})
		if err != nil {
			t.Fatal(err)
		}
		query.Cursor = legacy.NextCursor
		_, err = records.Query(ctx, actor, tab.Table.ID, query)
		var invalid *domain.InvalidCursorError
		if !errors.As(err, &invalid) {
			t.Fatal("ordinary cursor impersonated scope page", err)
		}
	})

	t.Run("ancestor revoke serializes with new scope and restore never revives it", func(t *testing.T) {
		parent, err := cat.CreateBase(ctx, actor, newMutationID(t), ws.ID, "G4 ancestor race")
		if err != nil {
			t.Fatal(err)
		}
		tab, err := cat.CreateTable(ctx, actor, newMutationID(t), parent.ID, "Ancestor", nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		create(t, tab, "Alpha")
		blocker, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer blocker.Rollback()
		if _, err = blocker.ExecContext(ctx, `SELECT id FROM bases WHERE id=$1 FOR UPDATE`, parent.ID); err != nil {
			t.Fatal(err)
		}
		type scopeOutcome struct {
			scope record.SelectionResult
			err   error
		}
		acquired := make(chan scopeOutcome, 1)
		deleted := make(chan error, 1)
		go func() {
			s, err := records.AcquireSelection(ctx, actor, tab.Table.ID, requestID(), qAlpha)
			acquired <- scopeOutcome{s, err}
		}()
		waitForChoiceLock(t, ctx, db, "%FOR SHARE OF b,w%")
		go func() {
			_, err := db.ExecContext(ctx, `UPDATE bases SET deleted_at=clock_timestamp(),revision=revision+1 WHERE id=$1`, parent.ID)
			deleted <- err
		}()
		if err = blocker.Commit(); err != nil {
			t.Fatal(err)
		}
		scope := <-acquired
		if err = <-deleted; err != nil {
			t.Fatal(err)
		}
		if scope.err != nil && !errors.Is(scope.err, domain.ErrNotFound) {
			t.Fatal("ancestor allocation outcome", scope.err)
		}
		if _, err = db.ExecContext(ctx, `UPDATE bases SET deleted_at=NULL,revision=revision+1 WHERE id=$1`, parent.ID); err != nil {
			t.Fatal(err)
		}
		if scope.err == nil {
			_, err = resolve(scope.scope, qAlpha)
			expired(t, err)
		}
		fresh := acquire(t, tab.Table.ID, qAlpha)
		if fresh.Scope.Count != 1 {
			t.Fatal("explicit reselect after restored access")
		}
		if _, err = db.ExecContext(ctx, `DELETE FROM selection_scopes WHERE table_id=$1`, tab.Table.ID); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("Convert racing with Record update invalidates only semantic scopes", func(t *testing.T) {
		tab := table(t)
		field, err := cat.CreateField(ctx, actor, newMutationID(t), tab.Table.ID, catalog.FieldInput{Name: "Numeric text", Type: "text", Config: domain.EmptyFieldConfig{}})
		if err != nil {
			t.Fatal(err)
		}
		made, err := records.Mutate(ctx, actor, tab.Table.ID, newMutationID(t), []record.Command{{Kind: "createRecord", ValuesPresent: true, Values: map[string]any{tab.PrimaryField.ID: "Alpha", field.ID: "1"}}})
		if err != nil {
			t.Fatal(err)
		}
		a := made.Results[0].Record
		all := acquire(t, tab.Table.ID, record.SelectionQuery{})
		query := record.SelectionQuery{FilterPresent: true, Filter: &domain.FilterNode{Kind: "rule", FieldID: field.ID, Operator: "is", Value: json.RawMessage(`"1"`)}}
		filtered := acquire(t, tab.Table.ID, query)
		preview, err := cat.PreviewFieldConversion(ctx, actor, field.ID, "number")
		if err != nil {
			t.Fatal(err)
		}
		var convertErr, writeErr error
		var wg sync.WaitGroup
		wg.Add(2)
		start := make(chan struct{})
		go func() {
			defer wg.Done()
			<-start
			_, convertErr = cat.ConvertField(ctx, actor, field.ID, catalog.ConversionRequest{TargetType: "number", Mode: "parse", ExpectedRevision: field.Revision, PreviewToken: preview.PreviewToken})
		}()
		go func() {
			defer wg.Done()
			<-start
			_, writeErr = records.Mutate(ctx, actor, tab.Table.ID, newMutationID(t), []record.Command{{Kind: "updateRecord", RecordID: a.ID, ExpectedRevision: a.Revision, SetPresent: true, Set: map[string]any{tab.PrimaryField.ID: "Alpha changed"}}})
		}()
		close(start)
		wg.Wait()
		if convertErr != nil || writeErr != nil {
			t.Fatal("Convert/Record serialization", convertErr, writeErr)
		}
		_, err = resolve(filtered, query)
		expired(t, err)
		if _, err = resolve(all, record.SelectionQuery{}); err != nil {
			t.Fatal("Convert with same complete ID membership invalidated unfiltered scope", err)
		}
	})

	t.Run("Choice palette display writes preserve members Atomic Choice assignment invalidates", func(t *testing.T) {
		tab := table(t)
		field, err := cat.CreateField(ctx, actor, newMutationID(t), tab.Table.ID, catalog.FieldInput{Name: "Choice", Type: "select", Config: catalog.SelectFieldConfigInput{Options: []catalog.SelectOptionInput{{Name: "One", Color: "gray"}}}})
		if err != nil {
			t.Fatal(err)
		}
		option := field.Config.(domain.SelectFieldConfig).Options[0]
		made, err := records.Mutate(ctx, actor, tab.Table.ID, newMutationID(t), []record.Command{{Kind: "createRecord", ValuesPresent: true, Values: map[string]any{tab.PrimaryField.ID: "Alpha", field.ID: option.ID}}})
		if err != nil {
			t.Fatal(err)
		}
		a := made.Results[0].Record
		value, _ := json.Marshal(option.ID)
		query := record.SelectionQuery{FilterPresent: true, Filter: &domain.FilterNode{Kind: "rule", FieldID: field.ID, Operator: "is", Value: value}}
		scope := acquire(t, tab.Table.ID, query)
		for _, color := range []string{"lime", "indigo", "brown"} {
			field, err = cat.UpdateField(ctx, actor, field.ID, catalog.FieldUpdate{ExpectedRevision: field.Revision, Type: "select", Config: catalog.SelectFieldConfigInput{Options: []catalog.SelectOptionInput{{ID: &option.ID, Name: "Renamed", Color: color}}}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = resolve(scope, query); err != nil {
				t.Fatal("Choice color/label treated as membership change", err)
			}
		}
		_, err = records.Mutate(ctx, actor, tab.Table.ID, newMutationID(t), []record.Command{{Kind: "updateRecord", RecordID: a.ID, ExpectedRevision: a.Revision, ChoicesPresent: true, Choices: []record.ChoiceAssignment{{FieldID: field.ID, ExpectedFieldRevision: field.Revision, Options: []record.ChoiceOption{{Name: "Two", Color: "brown"}}}}}})
		if err != nil {
			t.Fatal(err)
		}
		_, err = resolve(scope, query)
		expired(t, err)
	})

	t.Run("inherited filter invalidation explicit override and display preservation", func(t *testing.T) {
		tab := table(t)
		create(t, tab, "Alpha")
		query := record.SelectionQuery{ViewID: tab.InitialView.ID, Search: "alpha"}
		inherited := acquire(t, tab.Table.ID, query)
		overrideQuery := query
		overrideQuery.FilterPresent = true
		override := acquire(t, tab.Table.ID, overrideQuery)
		view, err := cat.GetView(ctx, actor, tab.InitialView.ID)
		if err != nil {
			t.Fatal(err)
		}
		cfg := view.Config.(domain.GridViewConfig)
		cfg.ColumnWidths[tab.PrimaryField.ID] = 250
		cfg.RowHeight = "comfortable"
		cfg.Sort = []domain.SortSpec{{FieldID: tab.PrimaryField.ID, Direction: "desc"}}
		view, err = cat.UpdateView(ctx, actor, view.ID, catalog.ViewUpdate{ExpectedRevision: view.Revision, Type: view.Type, Config: cfg})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = resolve(inherited, query); err != nil {
			t.Fatal("Display/sort/width invalidated selection", err)
		}
		cfg.Filter = &domain.FilterNode{Kind: "rule", FieldID: tab.PrimaryField.ID, Operator: "contains", Value: json.RawMessage(`"beta"`)}
		if _, err = cat.UpdateView(ctx, actor, view.ID, catalog.ViewUpdate{ExpectedRevision: view.Revision, Type: view.Type, Config: cfg}); err != nil {
			t.Fatal(err)
		}
		_, err = resolve(inherited, query)
		expired(t, err)
		if _, err = resolve(override, overrideQuery); err != nil {
			t.Fatal("explicit filter lost to saved View", err)
		}
	})

	t.Run("009 reapply budgets and cleanup", func(t *testing.T) {
		tab := table(t)
		create(t, tab, "Alpha")
		for i := 0; i < 4; i++ {
			acquire(t, tab.Table.ID, qAlpha)
		}
		_, err := records.AcquireSelection(ctx, actor, tab.Table.ID, requestID(), qAlpha)
		var budget *domain.ValidationError
		if !errors.As(err, &budget) {
			t.Fatal("Scope allocation budget", err)
		}
		if _, err = db.ExecContext(ctx, `UPDATE selection_scopes SET expires_at=clock_timestamp()-interval '1 second',created_at=clock_timestamp()-interval '10 minutes' WHERE table_id=$1`, tab.Table.ID); err != nil {
			t.Fatal(err)
		}
		if _, err = records.AcquireSelection(ctx, actor, tab.Table.ID, requestID(), qAlpha); err != nil {
			t.Fatal("expired cleanup", err)
		}
		if err = postgres.ApplyMigrations(ctx, db, filepath.Join("..", "..", "..", "migrations")); err != nil {
			t.Fatal(err)
		}
		if err = postgres.ReadyChecker(db)(ctx); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("009 forward from eight migrations retains business rows and identity guards", func(t *testing.T) {
		tab := table(t)
		a := create(t, tab, "Alpha")
		order, err := cat.ListViewsSnapshot(ctx, actor, tab.Table.ID, "active")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = cat.ReorderViews(ctx, actor, newMutationID(t), tab.Table.ID, catalog.ViewOrderInput{ExpectedOrderRevision: order.Order.Revision, ViewIDs: order.Order.ViewIDs}); err != nil {
			t.Fatal(err)
		}
		var before int
		if err = db.QueryRowContext(ctx, `SELECT count(*) FROM mutation_identity_guards`).Scan(&before); err != nil || before == 0 {
			t.Fatal("guard fixture", err)
		}
		if _, err = db.ExecContext(ctx, `DROP TRIGGER tables_revoke_selection_scopes ON tables; DROP TRIGGER bases_revoke_selection_scopes ON bases; DROP TRIGGER workspaces_revoke_selection_scopes ON workspaces; DROP FUNCTION revoke_ancestor_selection_scopes(); DROP TABLE selection_scope_members; DROP TABLE selection_scopes; DELETE FROM schema_migrations WHERE version='009_selection_scopes'`); err != nil {
			t.Fatal(err)
		}
		if err = postgres.ReadyChecker(db)(ctx); err == nil {
			t.Fatal("009 must gate readiness")
		}
		for i := 0; i < 2; i++ {
			if err = postgres.ApplyMigrations(ctx, db, filepath.Join("..", "..", "..", "migrations")); err != nil {
				t.Fatal(err)
			}
		}
		var after, versions int
		if err = db.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM mutation_identity_guards),(SELECT count(*) FROM schema_migrations)`).Scan(&after, &versions); err != nil || after != before || versions != 11 {
			t.Fatal("migration changed guard/version", err)
		}
		got, err := records.Get(ctx, actor, a.ID)
		if err != nil || got.Revision != a.Revision || got.Values[tab.PrimaryField.ID] != "Alpha" {
			t.Fatal("009 changed business data", err)
		}
		acquire(t, tab.Table.ID, qAlpha)
	})

	t.Run("100001 member budget rolls back complete allocation", func(t *testing.T) {
		tab := table(t)
		prefix, err := id.New(id.RecordPrefix)
		if err != nil {
			t.Fatal(err)
		}
		prefix = prefix[:len(prefix)-8]
		if _, err = db.ExecContext(ctx, `INSERT INTO records(id,table_id,values,query_values,search_text,position) SELECT $2::text||lpad(upper(to_hex(i)),8,'0'),$1,jsonb_build_object($3::text,'Alpha'),jsonb_build_object($3::text,'alpha'),'alpha',i*1024.0 FROM generate_series(1,100001) i`, tab.Table.ID, prefix, tab.PrimaryField.ID); err != nil {
			t.Fatal(err)
		}
		_, err = records.AcquireSelection(ctx, actor, tab.Table.ID, requestID(), qAlpha)
		var budget *domain.ValidationError
		if !errors.As(err, &budget) || budget.Issues[0].Path != "/scopeMembers" {
			t.Fatal("member budget", err)
		}
		var persisted, recordsCount int
		if err = db.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM selection_scopes WHERE table_id=$1),(SELECT count(*) FROM records WHERE table_id=$1)`, tab.Table.ID).Scan(&persisted, &recordsCount); err != nil || persisted != 0 || recordsCount != 100001 {
			t.Fatal("allocation failure left partial scope or changed business records", err)
		}
	})

	t.Run("500 code point Search remains valid after expanding case folding", func(t *testing.T) {
		tab := table(t)
		text := strings.Repeat("ß", 500)
		a := create(t, tab, text)
		query := record.SelectionQuery{Search: "  " + text + "  "}
		key := requestID()
		scope, err := records.AcquireSelection(ctx, actor, tab.Table.ID, key, query)
		if err != nil || scope.Scope.Count != 1 {
			t.Fatal("valid expanding Search", err)
		}
		if _, err = resolve(scope, query); err != nil {
			t.Fatal("stored canonical Search violated original input limit", err)
		}
		again, err := records.AcquireSelection(ctx, actor, tab.Table.ID, key, record.SelectionQuery{Search: strings.Repeat("ẞ", 500)})
		if err != nil || again.Scope.ID != scope.Scope.ID {
			t.Fatal("canonical request replay", err)
		}
		update(t, a, tab.PrimaryField.ID, text+" changed")
		if _, err = resolve(scope, query); err != nil {
			t.Fatal("expanding Search falsely invalidated after same membership update", err)
		}
		tooLarge := record.SelectionQuery{FilterPresent: true, Filter: &domain.FilterNode{Kind: "rule", FieldID: tab.PrimaryField.ID, Operator: "contains", Value: json.RawMessage(`"` + strings.Repeat("a", 65537) + `"`)}}
		_, err = records.AcquireSelection(ctx, actor, tab.Table.ID, requestID(), tooLarge)
		var limit *domain.ValidationError
		if !errors.As(err, &limit) || limit.Issues[0].Path != "/query" {
			t.Fatal("stored query budget", err)
		}
	})

	t.Run("review HTTP null override invalid View and readiness full schemas", func(t *testing.T) {
		tab := table(t)
		create(t, tab, "Alpha")
		create(t, tab, "Beta")
		view, err := cat.GetView(ctx, actor, tab.InitialView.ID)
		if err != nil {
			t.Fatal(err)
		}
		cfg := view.Config.(domain.GridViewConfig)
		cfg.Filter = &domain.FilterNode{Kind: "rule", FieldID: tab.PrimaryField.ID, Operator: "contains", Value: json.RawMessage(`"beta"`)}
		view, err = cat.UpdateView(ctx, actor, view.ID, catalog.ViewUpdate{ExpectedRevision: view.Revision, Type: view.Type, Config: cfg})
		if err != nil {
			t.Fatal(err)
		}
		server := httpapi.New(config.Load(), postgres.ReadyChecker(db), httpapi.Dependencies{Authenticator: repo, Catalog: cat, Records: records})
		call := func(server *httpapi.Server, suffix string, body any, status int, code string) map[string]any {
			t.Helper()
			raw, _ := json.Marshal(body)
			if status == 200 {
				var input any
				json.Unmarshal(raw, &input)
				requestSchema := "#/components/schemas/SelectionScopeRequest"
				if suffix == "query" {
					requestSchema = "#/components/schemas/QueryRequest"
				} else if suffix == "selection-scope/resolve" {
					requestSchema = "#/components/schemas/SelectionResolveRequest"
				}
				if err := compileOpenAPIResponseSchema(t, requestSchema).Validate(input); err != nil {
					t.Fatal("full request schema", err)
				}
			}
			req := httptest.NewRequest("POST", "/v1/tables/"+tab.Table.ID+"/records/"+suffix, bytes.NewReader(raw))
			req.Header.Set("Authorization", "Bearer "+boot.Token.Secret)
			req.Header.Set("Content-Type", "application/json")
			res := httptest.NewRecorder()
			server.Handler().ServeHTTP(res, req)
			if res.Code != status {
				t.Fatalf("%s returned %d, want %d", suffix, res.Code, status)
			}
			var value map[string]any
			if err := json.Unmarshal(res.Body.Bytes(), &value); err != nil {
				t.Fatal(err)
			}
			schema := "#/components/schemas/ErrorResponse"
			if status == 200 {
				schema = "#/components/schemas/SelectionResult"
				if suffix == "query" {
					schema = "#/components/schemas/QueryResult"
				}
			}
			if err := compileOpenAPIResponseSchema(t, schema).Validate(value); err != nil {
				t.Fatal("full response schema", err)
			}
			if code != "" && value["error"].(map[string]any)["code"] != code {
				t.Fatal("wrong structured error code")
			}
			return value
		}
		query := map[string]any{"viewId": view.ID, "filter": nil}
		result := call(server, "selection-scope", map[string]any{"requestId": requestID(), "query": query}, 200, "")
		scope := result["scope"].(map[string]any)
		if scope["count"] != float64(2) {
			t.Fatal("null did not clear saved Filter")
		}
		token := scope["token"].(string)
		call(server, "selection-scope/resolve", map[string]any{"scopeToken": token, "query": query}, 200, "")
		page := call(server, "query", map[string]any{"viewId": view.ID, "filter": nil, "selectionScopeToken": token}, 200, "")
		if page["totalCount"] != float64(2) {
			t.Fatal("nullable scoped Query disagreed with Scope")
		}
		if err = cat.DeleteView(ctx, actor, view.ID, view.Revision); err != nil {
			t.Fatal(err)
		}
		call(server, "query", map[string]any{"viewId": view.ID, "filter": nil, "selectionScopeToken": token}, 410, "QUERY_SNAPSHOT_EXPIRED")
		call(server, "query", map[string]any{"viewId": view.ID}, 404, "NOT_FOUND")
		fresh := acquire(t, tab.Table.ID, record.SelectionQuery{})
		requests := []struct {
			suffix string
			body   any
		}{{"selection-scope", map[string]any{"requestId": requestID(), "query": map[string]any{}}}, {"selection-scope/resolve", map[string]any{"scopeToken": fresh.Scope.Token, "query": map[string]any{}}}, {"query", map[string]any{"selectionScopeToken": fresh.Scope.Token}}}
		pending := config.Load()
		pending.MigrationRequired = true
		explicit := httpapi.New(pending, postgres.ReadyChecker(db), httpapi.Dependencies{Authenticator: repo, Catalog: cat, Records: records})
		for _, req := range requests {
			call(explicit, req.suffix, req.body, 503, "MIGRATION_REQUIRED")
		}
		if _, err = db.ExecContext(ctx, `DROP TRIGGER tables_revoke_selection_scopes ON tables; DROP TRIGGER bases_revoke_selection_scopes ON bases; DROP TRIGGER workspaces_revoke_selection_scopes ON workspaces; DROP FUNCTION revoke_ancestor_selection_scopes(); DROP TABLE selection_scope_members; DROP TABLE selection_scopes; DELETE FROM schema_migrations WHERE version='009_selection_scopes'`); err != nil {
			t.Fatal(err)
		}
		for _, req := range requests {
			call(server, req.suffix, req.body, 503, "MIGRATION_REQUIRED")
		}
		call(server, "query", map[string]any{}, 200, "")
		if err = postgres.ApplyMigrations(ctx, db, filepath.Join("..", "..", "..", "migrations")); err != nil {
			t.Fatal(err)
		}
		acquire(t, tab.Table.ID, record.SelectionQuery{})
	})
}
