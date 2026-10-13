package postgres_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	loomauth "github.com/Mahjong404/LoomTable-Server/internal/auth"
	"github.com/Mahjong404/LoomTable-Server/internal/catalog"
	"github.com/Mahjong404/LoomTable-Server/internal/config"
	"github.com/Mahjong404/LoomTable-Server/internal/domain"
	"github.com/Mahjong404/LoomTable-Server/internal/httpapi"
	appstatus "github.com/Mahjong404/LoomTable-Server/internal/status"
	"github.com/Mahjong404/LoomTable-Server/internal/storage/postgres"
)

func TestViewOrderTransactions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	db := integrationDatabase(t, ctx)
	repo := postgres.NewRepository(db)
	bootstrap, err := loomauth.NewAdmin(repo).Bootstrap(ctx, "G3 synthetic")
	if err != nil {
		t.Fatal(err)
	}
	actor := bootstrap.ActorID
	cat := catalog.New(repo)
	w, err := cat.CreateWorkspace(ctx, actor, newMutationID(t), "G3")
	if err != nil {
		t.Fatal(err)
	}
	b, err := cat.CreateBase(ctx, actor, newMutationID(t), w.ID, "G3")
	if err != nil {
		t.Fatal(err)
	}
	makeTable := func(t *testing.T) domain.CreateTableResult {
		t.Helper()
		table, err := cat.CreateTable(ctx, actor, newMutationID(t), b.ID, "Order", nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		return table
	}
	getInitial := func(t *testing.T, table domain.CreateTableResult) domain.View {
		t.Helper()
		v, err := cat.GetView(ctx, actor, table.InitialView.ID)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	create := func(t *testing.T, tableID, key string, source domain.View) domain.View {
		t.Helper()
		v, err := cat.CreateView(ctx, actor, key, tableID, catalog.ViewInput{Name: "Copy", Type: source.Type, Config: source.Config})
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	list := func(t *testing.T, tableID string) domain.ViewListSnapshot {
		t.Helper()
		s, err := cat.ListViewsSnapshot(ctx, actor, tableID, "active")
		if err != nil {
			t.Fatal(err)
		}
		ids := make([]string, 0, len(s.Items))
		seen := map[string]bool{}
		for _, v := range s.Items {
			if v.DeletedAt != nil || seen[v.ID] {
				t.Fatal("split/duplicate membership")
			}
			seen[v.ID] = true
			ids = append(ids, v.ID)
		}
		if !slices.Equal(ids, s.Order.ViewIDs) {
			t.Fatal("List and Order differ")
		}
		return s
	}
	reorder := func(tableID, key string, order domain.ViewOrder, ids []string) (domain.ViewOrderResult, error) {
		return cat.ReorderViews(ctx, actor, key, tableID, catalog.ViewOrderInput{ExpectedOrderRevision: order.Revision, ViewIDs: ids})
	}
	noReceipt := func(t *testing.T, key string) {
		t.Helper()
		var receipt, guard bool
		if err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM idempotency_keys WHERE actor_id=$1 AND client_mutation_id=$2), EXISTS(SELECT 1 FROM mutation_identity_guards WHERE actor_id=$1 AND client_mutation_id=$2)`, actor, key).Scan(&receipt, &guard); err != nil {
			t.Fatal(err)
		}
		if receipt || guard {
			t.Fatal("rejected Order persisted identity")
		}
	}
	server := httpapi.New(config.Load(), postgres.ReadyChecker(db), httpapi.Dependencies{Authenticator: repo, Catalog: cat})
	call := func(t *testing.T, method, route, key string, body any, status int) (*httptest.ResponseRecorder, map[string]any) {
		t.Helper()
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(method, route, bytes.NewReader(encoded))
		r.Header.Set("Authorization", "Bearer "+bootstrap.Token.Secret)
		r.Header.Set("Content-Type", "application/json")
		if key != "" {
			r.Header.Set("Idempotency-Key", key)
		}
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, r)
		if response.Code != status {
			t.Fatalf("HTTP %s=%d want%d %s", route, response.Code, status, response.Body.String())
		}
		var value map[string]any
		if err := json.Unmarshal(response.Body.Bytes(), &value); err != nil {
			t.Fatal(err)
		}
		pointer := "#/components/schemas/ErrorResponse"
		if status == 200 && method == http.MethodPut {
			pointer = "#/components/schemas/ViewOrderResult"
		}
		if status == 200 && method == http.MethodGet {
			pointer = "#/paths/~1v1~1tables~1{tableId}~1views/get/responses/200/content/application~1json/schema"
		}
		if err := compileOpenAPIResponseSchema(t, pointer).Validate(value); err != nil {
			t.Fatalf("complete response schema: %v %s", err, response.Body.String())
		}
		return response, value
	}

	t.Run("lifecycle no-op replay CAS and HTTP", func(t *testing.T) {
		table := makeTable(t)
		a := getInitial(t, table)
		initial := list(t, table.Table.ID)
		if initial.Order.Revision != 2 || !slices.Equal(initial.Order.ViewIDs, []string{a.ID}) {
			t.Fatalf("first Order=%v", initial.Order)
		}
		key := newMutationID(t)
		v := create(t, table.Table.ID, key, a)
		if got := create(t, table.Table.ID, key, a); got.ID != v.ID {
			t.Fatal("Create replay identity changed")
		}
		c := create(t, table.Table.ID, newMutationID(t), a)
		s := list(t, table.Table.ID)
		if s.Order.Revision != 4 || !slices.Equal(s.Order.ViewIDs, []string{a.ID, v.ID, c.ID}) {
			t.Fatal(s.Order)
		}
		wanted := []string{c.ID, a.ID, v.ID}
		key = newMutationID(t)
		body := catalog.ViewOrderInput{ExpectedOrderRevision: s.Order.Revision, ViewIDs: wanted}
		route := "/v1/tables/" + table.Table.ID + "/views/order"
		response, _ := call(t, http.MethodPut, route, key, body, 200)
		replayed, _ := call(t, http.MethodPut, route, key, body, 200)
		if !bytes.Equal(response.Body.Bytes(), replayed.Body.Bytes()) {
			t.Fatal("Receipt replay changed")
		}
		applied := list(t, table.Table.ID)
		if applied.Order.Revision != 5 || !slices.Equal(applied.Order.ViewIDs, wanted) {
			t.Fatal(applied.Order)
		}
		for _, item := range applied.Items {
			if item.Revision != 1 {
				t.Fatal("Order changed View config revision")
			}
		}
		currentTable, err := cat.GetTable(ctx, actor, table.Table.ID)
		if err != nil || currentTable.Revision != table.Table.Revision {
			t.Fatal("Order changed Table ordinary revision")
		}
		rename := "Renamed"
		if _, err := cat.UpdateView(ctx, actor, c.ID, catalog.ViewUpdate{Name: &rename, Type: c.Type, Config: c.Config, ExpectedRevision: c.Revision}); err != nil {
			t.Fatal(err)
		}
		if list(t, table.Table.ID).Order.Revision != 5 {
			t.Fatal("View config changed Order revision")
		}
		_, err = reorder(table.Table.ID, newMutationID(t), applied.Order, wanted)
		if err != nil {
			t.Fatal(err)
		}
		if list(t, table.Table.ID).Order.Revision != 5 {
			t.Fatal("no-op bumped revision")
		}
		key2 := newMutationID(t)
		_, errorBody := call(t, http.MethodPut, route, key2, body, 409)
		if errorBody["error"].(map[string]any)["code"] != "CONFLICT" {
			t.Fatal(errorBody)
		}
		noReceipt(t, key2)
		call(t, http.MethodPut, route, key, catalog.ViewOrderInput{ExpectedOrderRevision: 5, ViewIDs: wanted}, 409)
		for _, ids := range [][]string{{}, {a.ID, v.ID}, {a.ID, a.ID, c.ID}, {a.ID, v.ID, "view_00000000000000000000000000"}} {
			key2 = newMutationID(t)
			call(t, http.MethodPut, route, key2, catalog.ViewOrderInput{ExpectedOrderRevision: 5, ViewIDs: ids}, 422)
			noReceipt(t, key2)
		}
		foreign := makeTable(t)
		key2 = newMutationID(t)
		call(t, http.MethodPut, route, key2, catalog.ViewOrderInput{ExpectedOrderRevision: 5, ViewIDs: []string{a.ID, v.ID, foreign.InitialView.ID}}, 422)
		noReceipt(t, key2)
		_, legacy := call(t, http.MethodGet, "/v1/tables/"+table.Table.ID+"/views", "", nil, 200)
		if _, ok := legacy["order"]; ok {
			t.Fatal("legacy response gained Order")
		}
		call(t, http.MethodGet, "/v1/tables/"+table.Table.ID+"/views?includeOrder=true", "", nil, 200)
		if _, err := cat.SetDefaultView(ctx, actor, v.ID, 1); err != nil {
			t.Fatal(err)
		}
		if list(t, table.Table.ID).Order.Revision != 5 {
			t.Fatal("default reordered")
		}
		v, err = cat.GetView(ctx, actor, v.ID)
		if err != nil {
			t.Fatal(err)
		}
		if err := cat.DeleteView(ctx, actor, v.ID, v.Revision); err != nil {
			t.Fatal(err)
		}
		deleted, err := cat.GetView(ctx, actor, v.ID)
		if err != nil {
			t.Fatal(err)
		}
		call(t, http.MethodPost, "/v1/views/"+v.ID+"/default", "", map[string]any{"expectedRevision": deleted.Revision}, 422)
		if !slices.Equal(list(t, table.Table.ID).Order.ViewIDs, []string{c.ID, a.ID}) {
			t.Fatal("survivor relative order changed")
		}
		restored, err := cat.RestoreView(ctx, actor, v.ID, deleted.Revision)
		if err != nil {
			t.Fatal(err)
		}
		if !restored.CreatedAt.Equal(v.CreatedAt) || restored.IsDefault || !slices.Equal(list(t, table.Table.ID).Order.ViewIDs, []string{c.ID, a.ID, v.ID}) {
			t.Fatal("restore did not append old identity")
		}
		if _, err := cat.RestoreView(ctx, actor, v.ID, deleted.Revision); err == nil {
			t.Fatal("Restore retried unexpectedly applied")
		}
		if list(t, table.Table.ID).Order.Revision != 7 {
			t.Fatal("Restore replay appended twice")
		}
		// Historical receipt remains stable after lifecycle changes.
		again, _ := call(t, http.MethodPut, route, key, body, 200)
		if !bytes.Equal(response.Body.Bytes(), again.Body.Bytes()) {
			t.Fatal("replay used current order")
		}
		// Real transport loss after commit, followed by exact-key retry.
		before := list(t, table.Table.ID)
		lostKey := newMutationID(t)
		lostInput := catalog.ViewOrderInput{ExpectedOrderRevision: before.Order.Revision, ViewIDs: []string{v.ID, c.ID, a.ID}}
		encoded, _ := json.Marshal(lostInput)
		r := httptest.NewRequest(http.MethodPut, route, bytes.NewReader(encoded))
		r.Header.Set("Authorization", "Bearer "+bootstrap.Token.Secret)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", lostKey)
		lost := &lostChoiceResponse{header: make(http.Header)}
		server.Handler().ServeHTTP(lost, r)
		if !lost.failed {
			t.Fatal("response was not lost")
		}
		call(t, http.MethodPut, route, lostKey, lostInput, 200)
		if list(t, table.Table.ID).Order.Revision != before.Order.Revision+1 {
			t.Fatal("lost outcome executed twice")
		}
		if _, err := db.ExecContext(ctx, `UPDATE idempotency_keys SET created_at=clock_timestamp()-interval '31 days' WHERE actor_id=$1 AND client_mutation_id=$2`, actor, lostKey); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.CleanupRetention(ctx, 0, 30*24*time.Hour, 10, 10000); err != nil {
			t.Fatal(err)
		}
		call(t, http.MethodPut, route, lostKey, lostInput, 410)
		call(t, http.MethodPut, route, lostKey, body, 409)
		// Empty and first View after clearing membership.
		for _, item := range list(t, table.Table.ID).Items {
			if err := cat.DeleteView(ctx, actor, item.ID, item.Revision); err != nil {
				t.Fatal(err)
			}
		}
		empty := list(t, table.Table.ID)
		if len(empty.Items) != 0 {
			t.Fatal("empty Table retained member")
		}
		result, err := reorder(table.Table.ID, newMutationID(t), empty.Order, []string{})
		if err != nil || result.Status != "unchanged" {
			t.Fatal(result, err)
		}
		first := create(t, table.Table.ID, newMutationID(t), a)
		if !slices.Equal(list(t, table.Table.ID).Order.ViewIDs, []string{first.ID}) {
			t.Fatal("first append failed")
		}
	})

	t.Run("two Reorders and same key", func(t *testing.T) {
		table := makeTable(t)
		a := getInitial(t, table)
		v := create(t, table.Table.ID, newMutationID(t), a)
		c := create(t, table.Table.ID, newMutationID(t), a)
		s := list(t, table.Table.ID)
		type out struct {
			r domain.ViewOrderResult
			e error
		}
		race := func(keys []string, ids [][]string) []out {
			runCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			start := make(chan struct{})
			done := make(chan out, 2)
			for i := 0; i < 2; i++ {
				go func(i int) {
					<-start
					r, e := cat.ReorderViews(runCtx, actor, keys[i], table.Table.ID, catalog.ViewOrderInput{ExpectedOrderRevision: s.Order.Revision, ViewIDs: ids[i]})
					done <- out{r, e}
				}(i)
			}
			close(start)
			return []out{<-done, <-done}
		}
		outcomes := race([]string{newMutationID(t), newMutationID(t)}, [][]string{{c.ID, v.ID, a.ID}, {v.ID, a.ID, c.ID}})
		success := 0
		conflict := 0
		for _, o := range outcomes {
			if o.e == nil {
				success++
			} else {
				var e *domain.ViewOrderConflictError
				if errors.As(o.e, &e) {
					conflict++
				} else {
					t.Fatal(o.e)
				}
			}
		}
		if success != 1 || conflict != 1 {
			t.Fatal("concurrent CAS silently overwrote")
		}
		s = list(t, table.Table.ID)
		ids := append([]string{}, s.Order.ViewIDs...)
		slices.Reverse(ids)
		key := newMutationID(t)
		outcomes = race([]string{key, key}, [][]string{ids, ids})
		if outcomes[0].e != nil || outcomes[1].e != nil || !slices.Equal(outcomes[0].r.Order.ViewIDs, outcomes[1].r.Order.ViewIDs) || outcomes[0].r.Order.Revision != outcomes[1].r.Order.Revision {
			t.Fatal("same identity raced")
		}
	})

	for _, action := range []string{"create", "duplicate", "delete", "restore", "default"} {
		t.Run("Reorder with "+action, func(t *testing.T) {
			table := makeTable(t)
			a := getInitial(t, table)
			v := create(t, table.Table.ID, newMutationID(t), a)
			target := v
			if action == "restore" {
				if err := cat.DeleteView(ctx, actor, v.ID, v.Revision); err != nil {
					t.Fatal(err)
				}
				target, err = cat.GetView(ctx, actor, v.ID)
				if err != nil {
					t.Fatal(err)
				}
				create(t, table.Table.ID, newMutationID(t), a)
			}
			s := list(t, table.Table.ID)
			wanted := append([]string{}, s.Order.ViewIDs...)
			slices.Reverse(wanted)
			runCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			start := make(chan struct{})
			ordered := make(chan error, 1)
			life := make(chan error, 1)
			orderKey := newMutationID(t)
			go func() {
				<-start
				_, e := cat.ReorderViews(runCtx, actor, orderKey, table.Table.ID, catalog.ViewOrderInput{ExpectedOrderRevision: s.Order.Revision, ViewIDs: wanted})
				ordered <- e
			}()
			key := newMutationID(t)
			go func() {
				<-start
				var e error
				switch action {
				case "create", "duplicate":
					_, e = cat.CreateView(runCtx, actor, key, table.Table.ID, catalog.ViewInput{Name: "Race", Type: target.Type, Config: target.Config})
				case "delete":
					e = cat.DeleteView(runCtx, actor, target.ID, target.Revision)
				case "restore":
					_, e = cat.RestoreView(runCtx, actor, target.ID, target.Revision)
				case "default":
					_, e = cat.SetDefaultView(runCtx, actor, target.ID, target.Revision)
				}
				life <- e
			}()
			close(start)
			orderErr, lifeErr := <-ordered, <-life
			if lifeErr != nil {
				t.Fatal(lifeErr)
			}
			if orderErr != nil {
				var conflict *domain.ViewOrderConflictError
				if !errors.As(orderErr, &conflict) || action == "default" {
					t.Fatal(orderErr)
				}
			}
			final := list(t, table.Table.ID)
			expected := s.Order.Revision
			if action != "default" {
				expected++
			}
			if orderErr == nil {
				expected++
			}
			if final.Order.Revision != expected {
				t.Fatal("lost or duplicate Order revision")
			}
			if action == "create" || action == "duplicate" || action == "restore" {
				tail := final.Items[len(final.Items)-1]
				if action == "restore" && tail.ID != target.ID {
					t.Fatal("Restore lost tail")
				}
				if action != "restore" && (tail.ID == a.ID || tail.ID == v.ID) {
					t.Fatal("Create lost tail")
				}
			}
			if action == "delete" && slices.Contains(final.Order.ViewIDs, target.ID) {
				t.Fatal("Delete remained in Order")
			}
		})
	}

	t.Run("List shares one snapshot", func(t *testing.T) {
		table := makeTable(t)
		a := getInitial(t, table)
		v := create(t, table.Table.ID, newMutationID(t), a)
		before := list(t, table.Table.ID)
		block, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := block.ExecContext(ctx, `LOCK TABLE views IN ACCESS EXCLUSIVE MODE`); err != nil {
			block.Rollback()
			t.Fatal(err)
		}
		runCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		type readOutcome struct {
			s domain.ViewListSnapshot
			e error
		}
		done := make(chan readOutcome, 1)
		joined := false
		t.Cleanup(func() {
			block.Rollback()
			cancel()
			if !joined {
				<-done
			}
		})
		go func() {
			s, e := cat.ListViewsSnapshot(runCtx, actor, table.Table.ID, "active")
			done <- readOutcome{s, e}
		}()
		waitForChoiceLock(t, runCtx, db, "%FROM views v WHERE%")
		if _, err := reorder(table.Table.ID, newMutationID(t), before.Order, []string{v.ID, a.ID}); err != nil {
			t.Fatal(err)
		}
		if err := block.Rollback(); err != nil {
			t.Fatal(err)
		}
		read := <-done
		joined = true
		if read.e != nil || read.s.Order.Revision != before.Order.Revision || !slices.Equal(read.s.Order.ViewIDs, before.Order.ViewIDs) {
			t.Fatal("List mixed snapshots", read.e)
		}
		if !slices.Equal([]string{read.s.Items[0].ID, read.s.Items[1].ID}, before.Order.ViewIDs) {
			t.Fatal("List items were newer than revision")
		}
		if list(t, table.Table.ID).Order.Revision != before.Order.Revision+1 {
			t.Fatal("writer did not commit")
		}
	})

	t.Run("Receipt SQL failure rolls back Order Change and guard", func(t *testing.T) {
		table := makeTable(t)
		a := getInitial(t, table)
		v := create(t, table.Table.ID, newMutationID(t), a)
		s := list(t, table.Table.ID)
		if _, err := db.ExecContext(ctx, `CREATE FUNCTION reject_g3_receipt() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic G3 failure'; END; $$; CREATE TRIGGER reject_g3 BEFORE INSERT ON idempotency_keys FOR EACH ROW EXECUTE FUNCTION reject_g3_receipt()`); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			db.ExecContext(context.Background(), `DROP TRIGGER IF EXISTS reject_g3 ON idempotency_keys; DROP FUNCTION IF EXISTS reject_g3_receipt()`)
		})
		var changesBefore, changesAfter int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM changes WHERE table_id=$1`, table.Table.ID).Scan(&changesBefore); err != nil {
			t.Fatal(err)
		}
		key := newMutationID(t)
		if _, err := reorder(table.Table.ID, key, s.Order, []string{v.ID, a.ID}); err == nil {
			t.Fatal("SQL failure did not abort")
		}
		noReceipt(t, key)
		if got := list(t, table.Table.ID); got.Order.Revision != s.Order.Revision || !slices.Equal(got.Order.ViewIDs, s.Order.ViewIDs) {
			t.Fatal("partial Order")
		}
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM changes WHERE table_id=$1`, table.Table.ID).Scan(&changesAfter); err != nil || changesAfter != changesBefore {
			t.Fatal("partial Change")
		}
		if _, err := db.ExecContext(ctx, `DROP TRIGGER reject_g3 ON idempotency_keys; DROP FUNCTION reject_g3_receipt()`); err != nil {
			t.Fatal(err)
		}
		if _, err := reorder(table.Table.ID, key, s.Order, []string{v.ID, a.ID}); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("008 deterministic backfill and retry", func(t *testing.T) {
		table := makeTable(t)
		a := getInitial(t, table)
		v := create(t, table.Table.ID, newMutationID(t), a)
		deleted := create(t, table.Table.ID, newMutationID(t), a)
		if err := cat.DeleteView(ctx, actor, deleted.ID, deleted.Revision); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `UPDATE views SET created_at='2020-01-01' WHERE table_id=$1`, table.Table.ID); err != nil {
			t.Fatal(err)
		}
		// Only the disposable test fixture reconstructs the prior-seven schema.
		if _, err := db.ExecContext(ctx, `DROP TRIGGER views_maintain_order ON views; DROP FUNCTION maintain_view_order(); ALTER TABLE tables DROP COLUMN view_order,DROP COLUMN view_order_revision; DELETE FROM schema_migrations WHERE version='008_view_order'`); err != nil {
			t.Fatal(err)
		}
		if err := postgres.ReadyChecker(db)(ctx); !errors.Is(err, appstatus.ErrMigrationRequired) {
			t.Fatal("008 readiness missing", err)
		}
		for i := 0; i < 2; i++ {
			if err := postgres.ApplyMigrations(ctx, db, filepath.Join("..", "..", "..", "migrations")); err != nil {
				t.Fatal(err)
			}
		}
		if err := postgres.ReadyChecker(db)(ctx); err != nil {
			t.Fatal(err)
		}
		want := []string{a.ID, v.ID}
		slices.Sort(want)
		s := list(t, table.Table.ID)
		if s.Order.Revision != 1 || !slices.Equal(s.Order.ViewIDs, want) {
			t.Fatal("unstable backfill", s.Order)
		}
		old, err := cat.GetView(ctx, actor, v.ID)
		if err != nil || old.Revision != 1 {
			t.Fatal("backfill rewrote View revision")
		}
		newView := create(t, table.Table.ID, newMutationID(t), a)
		if list(t, table.Table.ID).Order.ViewIDs[2] != newView.ID {
			t.Fatal("backfill trigger not installed")
		}
	})
}

// The private QA runner starts a fixed old binary on its restored disposable
// database. This probe never resets data and never accepts a production URL.
func TestViewOrderLegacyBinaryWriter(t *testing.T) {
	url := os.Getenv("LOOMTABLE_TEST_LEGACY_SERVER_URL")
	if url == "" {
		t.Skip("isolated old-binary runner is not configured")
	}
	if os.Getenv("LOOMTABLE_TEST_DATABASE_DISPOSABLE") != "true" || os.Getenv("LOOMTABLE_TEST_LEGACY_SERVER_DISPOSABLE") != "true" {
		t.Fatal("legacy probe requires separately provisioned disposable server and DB")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	db, err := postgres.Open(os.Getenv("LOOMTABLE_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var database string
	if err := db.QueryRowContext(ctx, `SELECT current_database()`).Scan(&database); err != nil || database != "loomtable_test_restore" {
		t.Fatal("legacy probe only uses the runner's restored database")
	}
	repo := postgres.NewRepository(db)
	issued, err := loomauth.NewAdmin(repo).Create(ctx, "Legacy probe "+newMutationID(t))
	if err != nil {
		t.Fatal(err)
	}
	cat := catalog.New(repo)
	w, err := cat.CreateWorkspace(ctx, issued.ActorID, newMutationID(t), "Legacy probe")
	if err != nil {
		t.Fatal(err)
	}
	b, err := cat.CreateBase(ctx, issued.ActorID, newMutationID(t), w.ID, "Legacy")
	if err != nil {
		t.Fatal(err)
	}
	table, err := cat.CreateTable(ctx, issued.ActorID, newMutationID(t), b.ID, "Legacy", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	call := func(method, route, key string, body any, status int) []byte {
		t.Helper()
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		r, err := http.NewRequestWithContext(ctx, method, url+route, bytes.NewReader(encoded))
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Authorization", "Bearer "+issued.Secret)
		r.Header.Set("Content-Type", "application/json")
		if key != "" {
			r.Header.Set("Idempotency-Key", key)
		}
		response, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		result, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != status {
			t.Fatalf("legacy HTTP status=%d want%d", response.StatusCode, status)
		}
		return result
	}
	meta := call(http.MethodGet, "/v1/meta", "", nil, 200)
	if bytes.Contains(meta, []byte("viewOrderCAS")) {
		t.Fatal("old binary declared G3")
	}
	source, err := cat.GetView(ctx, issued.ActorID, table.InitialView.ID)
	if err != nil {
		t.Fatal(err)
	}
	key := newMutationID(t)
	body := map[string]any{"name": "Old Client", "type": source.Type, "config": source.Config}
	created := call(http.MethodPost, "/v1/tables/"+table.Table.ID+"/views", key, body, 201)
	if !bytes.Equal(created, call(http.MethodPost, "/v1/tables/"+table.Table.ID+"/views", key, body, 201)) {
		t.Fatal("old Create replay changed")
	}
	var view domain.View
	if err := json.Unmarshal(created, &view); err != nil {
		t.Fatal(err)
	}
	s, err := cat.ListViewsSnapshot(ctx, issued.ActorID, table.Table.ID, "active")
	if err != nil || s.Order.Revision != 3 || !slices.Equal(s.Order.ViewIDs, []string{source.ID, view.ID}) {
		t.Fatal("old writer did not append once", err)
	}
	call(http.MethodDelete, "/v1/views/"+view.ID+"?expectedRevision=1", "", nil, 204)
	call(http.MethodPost, "/v1/views/"+view.ID+"/restore", "", map[string]any{"expectedRevision": 2}, 200)
	call(http.MethodPost, "/v1/views/"+view.ID+"/restore", "", map[string]any{"expectedRevision": 2}, 409)
	s, err = cat.ListViewsSnapshot(ctx, issued.ActorID, table.Table.ID, "active")
	if err != nil || s.Order.Revision != 5 || !slices.Equal(s.Order.ViewIDs, []string{source.ID, view.ID}) {
		t.Fatal("old restore changed membership twice", err)
	}
}
