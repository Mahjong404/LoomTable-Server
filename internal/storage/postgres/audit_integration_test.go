package postgres_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Mahjong404/LoomTable-Server/internal/auth"
	"github.com/Mahjong404/LoomTable-Server/internal/catalog"
	"github.com/Mahjong404/LoomTable-Server/internal/config"
	"github.com/Mahjong404/LoomTable-Server/internal/domain"
	"github.com/Mahjong404/LoomTable-Server/internal/httpapi"
	"github.com/Mahjong404/LoomTable-Server/internal/id"
	record "github.com/Mahjong404/LoomTable-Server/internal/record"
	"github.com/Mahjong404/LoomTable-Server/internal/storage/postgres"
)

func TestTableAuditTransactions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	db := integrationDatabase(t, ctx)
	repo := postgres.NewRepository(db)
	boot, err := auth.NewAdmin(repo).Bootstrap(ctx, "G5 synthetic")
	if err != nil {
		t.Fatal(err)
	}
	actor := boot.ActorID
	cat := catalog.New(repo)
	records := record.New(repo)
	ws, err := cat.CreateWorkspace(ctx, actor, newMutationID(t), "Audit")
	if err != nil {
		t.Fatal(err)
	}
	base, err := cat.CreateBase(ctx, actor, newMutationID(t), ws.ID, "Audit")
	if err != nil {
		t.Fatal(err)
	}
	newTable := func(t *testing.T) domain.CreateTableResult {
		t.Helper()
		v, err := cat.CreateTable(ctx, actor, newMutationID(t), base.ID, "Audit", nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	server := httpapi.New(config.Load(), postgres.ReadyChecker(db), httpapi.Dependencies{Authenticator: repo, Catalog: cat, Records: records})
	httpCall := func(t *testing.T, method, path string, body any) *httptest.ResponseRecorder {
		t.Helper()
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest(method, path, bytes.NewReader(raw))
		r.Header.Set("Authorization", "Bearer "+boot.Token.Secret)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, r)
		return w
	}
	audit := func(t *testing.T, table string, request record.AuditRequest) record.AuditPage {
		t.Helper()
		p, err := records.Audit(ctx, actor, table, request)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	payload := func(t *testing.T, e record.AuditEvent) map[string]any {
		t.Helper()
		var p map[string]any
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	t.Run("event-time Choice metadata exact association and response loss replay", func(t *testing.T) {
		table := newTable(t)
		field, err := cat.CreateField(ctx, actor, newMutationID(t), table.Table.ID, catalog.FieldInput{Name: "State then", Type: "select", Config: catalog.SelectFieldConfigInput{Options: []catalog.SelectOptionInput{}}})
		if err != nil {
			t.Fatal(err)
		}
		key := newMutationID(t)
		body := map[string]any{"clientMutationId": key, "commands": []any{map[string]any{"kind": "createRecord", "values": map[string]any{}, "choices": []any{map[string]any{"fieldId": field.ID, "expectedFieldRevision": field.Revision, "options": []any{map[string]any{"name": "Original", "color": "indigo"}}}}}}}
		path := "/v1/tables/" + table.Table.ID + "/records/mutate"
		w := httpCall(t, "POST", path, body)
		if w.Code != 200 {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
		validateMutationHTTPResponse(t, w)
		op := w.Header().Get("X-LoomTable-Operation-Id")
		if op == "" {
			t.Fatal("missing committed operation header")
		}
		p := audit(t, table.Table.ID, record.AuditRequest{AuditFilter: record.AuditFilter{OperationID: op}})
		if p.Count != 2 {
			t.Fatalf("G2 effects=%d", p.Count)
		}
		for _, e := range p.Items {
			if e.OperationID == nil || *e.OperationID != op || e.ClientMutationID != key || len(e.ChangeIDs) != 1 {
				t.Fatalf("association=%#v", e)
			}
		}
		replayed := httpCall(t, "POST", path, body)
		if replayed.Code != 200 || replayed.Header().Get("X-LoomTable-Operation-Id") != op || !bytes.Equal(w.Body.Bytes(), replayed.Body.Bytes()) {
			t.Fatal("response loss retry changed receipt/identity")
		}
		if audit(t, table.Table.ID, record.AuditRequest{AuditFilter: record.AuditFilter{ClientMutationID: key}}).Count != 2 {
			t.Fatal("replay duplicated audit")
		}
		fields, err := cat.ListFields(ctx, actor, table.Table.ID, "active")
		if err != nil {
			t.Fatal(err)
		}
		var current domain.Field
		for _, f := range fields {
			if f.ID == field.ID {
				current = f
			}
		}
		cfg := current.Config.(domain.SelectFieldConfig)
		cfg.Options[0].Name = "Now"
		cfg.Options[0].Color = "brown"
		_, err = cat.UpdateField(ctx, actor, field.ID, catalog.FieldUpdate{Name: func() *string { value := "State now"; return &value }(), Type: "select", ExpectedRevision: current.Revision, Config: catalog.SelectFieldConfigInput{Options: []catalog.SelectOptionInput{{ID: &cfg.Options[0].ID, Name: "Now", Color: "brown"}}}})
		if err != nil {
			t.Fatal(err)
		}
		p = audit(t, table.Table.ID, record.AuditRequest{AuditFilter: record.AuditFilter{OperationID: op, Category: "record"}})
		if p.Count != 1 {
			t.Fatal("record projection lost G2")
		}
		cell := payload(t, p.Items[0])["fields"].([]any)[0].(map[string]any)["after"].(map[string]any)
		if cell["field"].(map[string]any)["name"] != "State then" || cell["options"].([]any)[0].(map[string]any)["name"] != "Original" || cell["options"].([]any)[0].(map[string]any)["color"] != "indigo" {
			t.Fatalf("event-time drift: %#v", cell)
		}
		w = httpCall(t, "GET", "/v1/tables/"+table.Table.ID+"/audit?category=record&operationId="+url.QueryEscape(op), nil)
		if w.Code != 200 {
			t.Fatalf("audit HTTP=%d %s", w.Code, w.Body.String())
		}
		var doc any
		if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
			t.Fatal(err)
		}
		if err := compileOpenAPIResponseSchema(t, "#/components/schemas/AuditPage").Validate(doc); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("mixed category pagination count and anchored concurrent append", func(t *testing.T) {
		table := newTable(t)
		for i := 0; i < 12; i++ {
			_, err := records.Mutate(ctx, actor, table.Table.ID, newMutationID(t), []record.Command{{Kind: "createRecord", ValuesPresent: true, Values: map[string]any{table.PrimaryField.ID: "Row"}}})
			if err != nil {
				t.Fatal(err)
			}
			_, err = cat.UpdateTable(ctx, actor, table.Table.ID, int64(i+1), string(rune('A'+i)))
			if err != nil {
				t.Fatal(err)
			}
		}
		request := record.AuditRequest{AuditFilter: record.AuditFilter{Category: "record"}, Limit: 3}
		first := audit(t, table.Table.ID, request)
		if first.Count != 12 || len(first.Items) != 3 || !first.HasMore {
			t.Fatalf("first=%#v", first)
		}
		_, err := records.Mutate(ctx, actor, table.Table.ID, newMutationID(t), []record.Command{{Kind: "createRecord", ValuesPresent: true, Values: map[string]any{}}})
		if err != nil {
			t.Fatal(err)
		}
		seen := map[string]bool{}
		page := first
		for {
			for _, e := range page.Items {
				if seen[e.ID] || e.Category != "record" {
					t.Fatal("duplicate/category leak")
				}
				seen[e.ID] = true
			}
			if page.Count != 12 {
				t.Fatal("snapshot count changed")
			}
			if !page.HasMore {
				break
			}
			request.Cursor = page.NextCursor
			page = audit(t, table.Table.ID, request)
		}
		if len(seen) != 12 {
			t.Fatalf("pages lost rows: %d", len(seen))
		}
		request.Cursor = first.NextCursor
		request.Category = "all"
		_, err = records.Audit(ctx, actor, table.Table.ID, request)
		var invalid *domain.InvalidCursorError
		if !errors.As(err, &invalid) {
			t.Fatalf("filter cursor=%v", err)
		}
		other := newTable(t)
		request.Category = "record"
		_, err = records.Audit(ctx, actor, other.Table.ID, request)
		if !errors.As(err, &invalid) {
			t.Fatalf("cross table=%v", err)
		}
		foreign, _ := id.New(id.ActorPrefix)
		_, err = records.Audit(ctx, foreign, table.Table.ID, record.AuditRequest{})
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("foreign actor=%v", err)
		}
	})
	t.Run("no-op and two client CAS", func(t *testing.T) {
		table := newTable(t)
		created, err := records.Mutate(ctx, actor, table.Table.ID, newMutationID(t), []record.Command{{Kind: "createRecord", ValuesPresent: true, Values: map[string]any{table.PrimaryField.ID: "Same"}}})
		if err != nil {
			t.Fatal(err)
		}
		r := created.Results[0].Record
		request := record.Command{Kind: "updateRecord", RecordID: r.ID, ExpectedRevision: r.Revision, SetPresent: true, Set: map[string]any{table.PrimaryField.ID: "Same"}}
		before := audit(t, table.Table.ID, record.AuditRequest{}).Count
		noOp, err := records.Mutate(ctx, actor, table.Table.ID, newMutationID(t), []record.Command{request})
		if err != nil || noOp.Results[0].Status != "unchanged" || audit(t, table.Table.ID, record.AuditRequest{}).Count != before {
			t.Fatalf("no-op=%v %v", noOp, err)
		}
		var wg sync.WaitGroup
		out := make(chan error, 2)
		for i := 0; i < 2; i++ {
			key := newMutationID(t)
			wg.Add(1)
			go func(key string) {
				defer wg.Done()
				command := request
				command.Set = map[string]any{table.PrimaryField.ID: key}
				_, err := records.Mutate(ctx, actor, table.Table.ID, key, []record.Command{command})
				out <- err
			}(key)
		}
		wg.Wait()
		close(out)
		success, conflict := 0, 0
		for err := range out {
			var c *record.ConflictError
			if err == nil {
				success++
			} else if errors.As(err, &c) {
				conflict++
			} else {
				t.Fatal(err)
			}
		}
		if success != 1 || conflict != 1 || audit(t, table.Table.ID, record.AuditRequest{}).Count != before+1 {
			t.Fatal("CAS audit mismatch")
		}
	})
	t.Run("audit failure rolls back Choice record receipt and guard", func(t *testing.T) {
		table := newTable(t)
		field, err := cat.CreateField(ctx, actor, newMutationID(t), table.Table.ID, catalog.FieldInput{Name: "Choice", Type: "select", Config: catalog.SelectFieldConfigInput{Options: []catalog.SelectOptionInput{}}})
		if err != nil {
			t.Fatal(err)
		}
		_, err = db.ExecContext(ctx, `CREATE FUNCTION g5_fail_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.kind='recordCreated' THEN RAISE EXCEPTION 'synthetic audit failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER g5_fail_audit BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION g5_fail_audit()`)
		if err != nil {
			t.Fatal(err)
		}
		key := newMutationID(t)
		before := audit(t, table.Table.ID, record.AuditRequest{}).Count
		_, err = records.Mutate(ctx, actor, table.Table.ID, key, []record.Command{{Kind: "createRecord", ValuesPresent: true, Values: map[string]any{}, Choices: []record.ChoiceAssignment{{FieldID: field.ID, ExpectedFieldRevision: 1, Options: []record.ChoiceOption{{Name: "Must rollback", Color: "lime"}}}}}})
		if err == nil {
			t.Fatal("expected injected audit failure")
		}
		if _, err := db.ExecContext(ctx, `DROP TRIGGER g5_fail_audit ON audit_events; DROP FUNCTION g5_fail_audit()`); err != nil {
			t.Fatal(err)
		}
		if audit(t, table.Table.ID, record.AuditRequest{}).Count != before {
			t.Fatal("partial audit persisted")
		}
		var count int
		if err := db.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM records WHERE table_id=$1)+(SELECT count(*) FROM idempotency_keys WHERE client_mutation_id=$2)+(SELECT count(*) FROM mutation_identity_guards WHERE client_mutation_id=$2)`, table.Table.ID, key).Scan(&count); err != nil || count != 0 {
			t.Fatalf("partial rows=%d %v", count, err)
		}
		fields, err := cat.ListFields(ctx, actor, table.Table.ID, "active")
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range fields {
			if f.ID == field.ID && (f.Revision != 1 || len(f.Config.(domain.SelectFieldConfig).Options) != 0) {
				t.Fatal("partial option persisted")
			}
		}
	})
	t.Run("conversion before and after types and grouped effects", func(t *testing.T) {
		table := newTable(t)
		f, err := cat.CreateField(ctx, actor, newMutationID(t), table.Table.ID, catalog.FieldInput{Name: "Convert", Type: "text", Config: domain.EmptyFieldConfig{}})
		if err != nil {
			t.Fatal(err)
		}
		_, err = records.Mutate(ctx, actor, table.Table.ID, newMutationID(t), []record.Command{{Kind: "createRecord", ValuesPresent: true, Values: map[string]any{f.ID: "123"}}})
		if err != nil {
			t.Fatal(err)
		}
		preview, err := cat.PreviewFieldConversion(ctx, actor, f.ID, "number")
		if err != nil {
			t.Fatal(err)
		}
		c, op := domain.WithAuditOperation(ctx)
		_, err = cat.ConvertField(c, actor, f.ID, catalog.ConversionRequest{TargetType: "number", Mode: "parse", ExpectedRevision: f.Revision, PreviewToken: preview.PreviewToken})
		if err != nil {
			t.Fatal(err)
		}
		p := audit(t, table.Table.ID, record.AuditRequest{AuditFilter: record.AuditFilter{OperationID: op.ID}})
		if p.Count != 2 {
			t.Fatalf("convert effects=%d", p.Count)
		}
		for _, e := range p.Items {
			if e.Category == "record" {
				cell := payload(t, e)["fields"].([]any)[0].(map[string]any)
				if cell["before"].(map[string]any)["field"].(map[string]any)["type"] != "text" || cell["after"].(map[string]any)["field"].(map[string]any)["type"] != "number" {
					t.Fatal("conversion metadata type drift")
				}
			}
		}
	})
	t.Run("View Default and Order are distinct with exact replay", func(t *testing.T) {
		table := newTable(t)
		view, err := cat.CreateView(ctx, actor, newMutationID(t), table.Table.ID, catalog.ViewInput{Name: "Second", Type: "grid", Config: table.InitialView.Config})
		if err != nil {
			t.Fatal(err)
		}
		c, op := domain.WithAuditOperation(ctx)
		_, err = cat.SetDefaultView(c, actor, view.ID, view.Revision)
		if err != nil {
			t.Fatal(err)
		}
		p := audit(t, table.Table.ID, record.AuditRequest{AuditFilter: record.AuditFilter{OperationID: op.ID}})
		if p.Count != 2 {
			t.Fatalf("default effects=%d", p.Count)
		}
		for _, e := range p.Items {
			if e.Kind != "viewUpdated" || e.Category != "metadata" {
				t.Fatalf("default misclassified=%#v", e)
			}
		}
		list, err := cat.ListViewsSnapshot(ctx, actor, table.Table.ID, "active")
		if err != nil {
			t.Fatal(err)
		}
		key := newMutationID(t)
		input := catalog.ViewOrderInput{ExpectedOrderRevision: list.Order.Revision, ViewIDs: []string{view.ID, table.InitialView.ID}}
		c, op = domain.WithAuditOperation(ctx)
		result, err := cat.ReorderViews(c, actor, key, table.Table.ID, input)
		if err != nil || result.Status != "applied" {
			t.Fatalf("reorder=%v %v", result, err)
		}
		original := op.ID
		p = audit(t, table.Table.ID, record.AuditRequest{AuditFilter: record.AuditFilter{OperationID: original}})
		if p.Count != 1 || p.Items[0].Kind != "viewOrderChanged" || p.Items[0].Category != "order" {
			t.Fatalf("order=%#v", p)
		}
		c, op = domain.WithAuditOperation(ctx)
		_, err = cat.ReorderViews(c, actor, key, table.Table.ID, input)
		if err != nil || op.ID != original {
			t.Fatalf("order replay=%v %q", err, op.ID)
		}
		before := audit(t, table.Table.ID, record.AuditRequest{}).Count
		input.ExpectedOrderRevision = result.Order.Revision
		_, err = cat.ReorderViews(ctx, actor, newMutationID(t), table.Table.ID, input)
		if err != nil || audit(t, table.Table.ID, record.AuditRequest{}).Count != before {
			t.Fatal("order no-op emitted audit")
		}
	})
	t.Run("typed presence MultiSelect deleted option and missing metadata", func(t *testing.T) {
		table := newTable(t)
		f, err := cat.CreateField(ctx, actor, newMutationID(t), table.Table.ID, catalog.FieldInput{Name: "Many", Type: "multiSelect", Config: catalog.SelectFieldConfigInput{Options: []catalog.SelectOptionInput{{Name: "Past", Color: "lime"}}}})
		if err != nil {
			t.Fatal(err)
		}
		option := f.Config.(domain.SelectFieldConfig).Options[0]
		created, err := records.Mutate(ctx, actor, table.Table.ID, newMutationID(t), []record.Command{{Kind: "createRecord", ValuesPresent: true, Values: map[string]any{table.PrimaryField.ID: "", f.ID: []any{option.ID}}}})
		if err != nil {
			t.Fatal(err)
		}
		r := created.Results[0].Record
		_, err = cat.UpdateField(ctx, actor, f.ID, catalog.FieldUpdate{Type: "multiSelect", ExpectedRevision: f.Revision, Config: catalog.SelectFieldConfigInput{Options: []catalog.SelectOptionInput{}}})
		if err != nil {
			t.Fatal(err)
		}
		c, op := domain.WithAuditOperation(ctx)
		deleted, err := records.Mutate(c, actor, table.Table.ID, newMutationID(t), []record.Command{{Kind: "deleteRecord", RecordID: r.ID, ExpectedRevision: r.Revision}})
		if err != nil {
			t.Fatal(err)
		}
		p := audit(t, table.Table.ID, record.AuditRequest{AuditFilter: record.AuditFilter{OperationID: op.ID}})
		cells := payload(t, p.Items[0])["fields"].([]any)
		for _, raw := range cells {
			cell := raw.(map[string]any)
			before := cell["before"].(map[string]any)
			if cell["fieldId"] == f.ID {
				opt := before["options"].([]any)[0].(map[string]any)
				if opt["deletedAt"] == nil || opt["name"] != "Past" || opt["color"] != "lime" {
					t.Fatal("deleted option metadata lost")
				}
			} else if before["presence"] != "value" || before["value"] != "" {
				t.Fatal("empty string mistaken for missing")
			}
		}
		_, err = records.Mutate(ctx, actor, table.Table.ID, newMutationID(t), []record.Command{{Kind: "restoreRecord", RecordID: r.ID, ExpectedRevision: deleted.Results[0].Record.Revision}})
		if err != nil {
			t.Fatal(err)
		}
		// Synthetic legacy payload stores null and an orphan Field; no current metadata may be invented.
		orphan, _ := id.New(id.FieldPrefix)
		raw, _ := json.Marshal(map[string]any{table.PrimaryField.ID: nil, orphan: []any{}})
		if _, err := db.ExecContext(ctx, `UPDATE records SET "values"=$1::jsonb WHERE id=$2`, string(raw), r.ID); err != nil {
			t.Fatal(err)
		}
		p = audit(t, table.Table.ID, record.AuditRequest{AuditFilter: record.AuditFilter{RecordID: r.ID}, Limit: 1})
		cells = payload(t, p.Items[0])["fields"].([]any)
		foundNull, foundOrphan := false, false
		for _, raw := range cells {
			cell := raw.(map[string]any)
			after := cell["after"].(map[string]any)
			if cell["fieldId"] == table.PrimaryField.ID {
				foundNull = after["presence"] == "null"
			}
			if cell["fieldId"] == orphan {
				foundOrphan = after["metadataRecorded"] == false && after["field"] == nil && after["presence"] == "value"
			}
		}
		if !foundNull || !foundOrphan || !p.Availability.UninstrumentedWriter {
			t.Fatal("unknown/presence facts invented")
		}
	})
	t.Run("10000 Record events paginated before projection limit", func(t *testing.T) {
		table := newTable(t)
		started := time.Now()
		for batch := 0; batch < 20; batch++ {
			commands := make([]record.Command, 500)
			for i := range commands {
				commands[i] = record.Command{Kind: "createRecord", ValuesPresent: true, Values: map[string]any{table.PrimaryField.ID: "Synthetic"}}
			}
			if _, err := records.Mutate(ctx, actor, table.Table.ID, newMutationID(t), commands); err != nil {
				t.Fatal(err)
			}
		}
		writeElapsed := time.Since(started)
		started = time.Now()
		request := record.AuditRequest{AuditFilter: record.AuditFilter{Category: "record"}, Limit: 100}
		seen := map[string]bool{}
		pages := 0
		for {
			p := audit(t, table.Table.ID, request)
			pages++
			if p.Count != 10000 {
				t.Fatalf("count=%d", p.Count)
			}
			for _, e := range p.Items {
				if seen[e.ID] {
					t.Fatal("duplicate large-query event")
				}
				seen[e.ID] = true
			}
			if !p.HasMore {
				break
			}
			request.Cursor = p.NextCursor
		}
		if len(seen) != 10000 || pages != 100 {
			t.Fatalf("events/pages=%d/%d", len(seen), pages)
		}
		t.Logf("synthetic audit 10000 events: writer=%s read100pages=%s (cloud QA, not production latency)", writeElapsed, time.Since(started))
	})
	t.Run("retention invalidates cursor and reports actual remaining coverage", func(t *testing.T) {
		table := newTable(t)
		p := audit(t, table.Table.ID, record.AuditRequest{Limit: 1})
		if !p.HasMore {
			t.Fatal("need multi-page timeline")
		}
		if _, err := db.ExecContext(ctx, `UPDATE audit_events SET occurred_at=clock_timestamp()-interval '31 days' WHERE table_id=$1`, table.Table.ID); err != nil {
			t.Fatal(err)
		}
		stats, err := repo.CleanupRetention(ctx, 30*24*time.Hour, 30*24*time.Hour, 10, 10000)
		if err != nil || stats.AuditDeleted == 0 {
			t.Fatalf("cleanup=%#v %v", stats, err)
		}
		_, err = records.Audit(ctx, actor, table.Table.ID, record.AuditRequest{Cursor: p.NextCursor, Limit: 1})
		var expired *domain.CursorExpiredError
		if !errors.As(err, &expired) {
			t.Fatalf("retention cursor=%v", err)
		}
		fresh := audit(t, table.Table.ID, record.AuditRequest{})
		if fresh.Count != 0 || fresh.Availability.EarliestReadableAt != nil || fresh.Availability.ExpiredThrough == 0 || fresh.Availability.FullTableRollbackAvailable {
			t.Fatalf("availability=%#v", fresh)
		}
	})
}

func TestTableAuditMigrationLegacyImport(t *testing.T) {
	ctx := context.Background()
	db := integrationDatabase(t, ctx)
	repo := postgres.NewRepository(db)
	boot, err := auth.NewAdmin(repo).Bootstrap(ctx, "legacy audit")
	if err != nil {
		t.Fatal(err)
	}
	cat := catalog.New(repo)
	ws, err := cat.CreateWorkspace(ctx, boot.ActorID, newMutationID(t), "Legacy")
	if err != nil {
		t.Fatal(err)
	}
	base, err := cat.CreateBase(ctx, boot.ActorID, newMutationID(t), ws.ID, "Legacy")
	if err != nil {
		t.Fatal(err)
	}
	table, err := cat.CreateTable(ctx, boot.ActorID, newMutationID(t), base.ID, "Legacy", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = record.New(repo).Mutate(ctx, boot.ActorID, table.Table.ID, newMutationID(t), []record.Command{{Kind: "createRecord", ValuesPresent: true, Values: map[string]any{table.PrimaryField.ID: "Legacy"}}})
	if err != nil {
		t.Fatal(err)
	}
	// Owned disposable database only: recreate the actual pre-010 source state.
	_, err = db.ExecContext(ctx, `DROP TRIGGER capture_audit ON records; DROP TRIGGER capture_audit ON fields; DROP TRIGGER capture_audit ON views; DROP TRIGGER capture_audit ON tables; DROP TRIGGER capture_ancestor_audit ON bases; DROP TRIGGER capture_ancestor_audit ON workspaces; DROP TRIGGER associate_audit_change ON changes; DROP TABLE audit_events,audit_coverage,audit_retention_watermarks,audit_instrumentation_gaps; ALTER TABLE idempotency_keys DROP COLUMN operation_id; DELETE FROM schema_migrations WHERE version='010_table_audit'`)
	if err != nil {
		t.Fatal(err)
	}
	if err := postgres.ApplyMigrations(ctx, db, filepath.Join("..", "..", "..", "migrations")); err != nil {
		t.Fatal(err)
	}
	if err := postgres.ApplyMigrations(ctx, db, filepath.Join("..", "..", "..", "migrations")); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", "010_table_audit.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, string(raw)); err != nil {
		t.Fatal(err)
	}
	p, err := record.New(repo).Audit(ctx, boot.ActorID, table.Table.ID, record.AuditRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if p.Count != 1 || !p.Availability.HasLegacyEvents || p.Items[0].OperationID != nil || p.Items[0].PayloadVersion != 0 {
		t.Fatalf("legacy facts=%#v", p)
	}
	var doc any
	bytes, _ := json.Marshal(p)
	json.Unmarshal(bytes, &doc)
	if err := compileOpenAPIResponseSchema(t, "#/components/schemas/AuditPage").Validate(doc); err != nil {
		t.Fatal(err)
	}
	// Pre-010 receipt replays without inventing a new identity or Audit event.
	var key string
	if err := db.QueryRowContext(ctx, `SELECT client_mutation_id FROM idempotency_keys WHERE response ? 'results' LIMIT 1`).Scan(&key); err != nil {
		t.Fatal(err)
	}
	c, op := domain.WithAuditOperation(ctx)
	_, err = record.New(repo).Mutate(c, boot.ActorID, table.Table.ID, key, []record.Command{{Kind: "createRecord", ValuesPresent: true, Values: map[string]any{table.PrimaryField.ID: "Legacy"}}})
	if err != nil || op.ID != "" {
		t.Fatalf("legacy replay=%v %q", err, op.ID)
	}
}
