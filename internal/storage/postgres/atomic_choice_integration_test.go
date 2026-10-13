package postgres_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	loomauth "github.com/Mahjong404/LoomTable-Server/internal/auth"
	"github.com/Mahjong404/LoomTable-Server/internal/catalog"
	"github.com/Mahjong404/LoomTable-Server/internal/config"
	"github.com/Mahjong404/LoomTable-Server/internal/domain"
	"github.com/Mahjong404/LoomTable-Server/internal/httpapi"
	"github.com/Mahjong404/LoomTable-Server/internal/id"
	loomrecord "github.com/Mahjong404/LoomTable-Server/internal/record"
	appstatus "github.com/Mahjong404/LoomTable-Server/internal/status"
	"github.com/Mahjong404/LoomTable-Server/internal/storage/postgres"
)

func TestAtomicChoiceTransactions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	db := integrationDatabase(t, ctx)
	repository := postgres.NewRepository(db)
	bootstrap, err := loomauth.NewAdmin(repository).Bootstrap(ctx, "G2 synthetic")
	if err != nil {
		t.Fatal(err)
	}
	actor := bootstrap.ActorID
	cat := catalog.New(repository)
	records := loomrecord.New(repository)
	workspace, err := cat.CreateWorkspace(ctx, actor, newMutationID(t), "G2")
	if err != nil {
		t.Fatal(err)
	}
	base, err := cat.CreateBase(ctx, actor, newMutationID(t), workspace.ID, "G2")
	if err != nil {
		t.Fatal(err)
	}
	table, err := cat.CreateTable(ctx, actor, newMutationID(t), base.ID, "G2", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	field, err := cat.CreateField(ctx, actor, newMutationID(t), table.Table.ID, catalog.FieldInput{Name: "State", Type: "select", Config: catalog.SelectFieldConfigInput{Options: []catalog.SelectOptionInput{{Name: "Open", Color: "blue"}}}})
	if err != nil {
		t.Fatal(err)
	}
	initial, err := records.Mutate(ctx, actor, table.Table.ID, newMutationID(t), []loomrecord.Command{{Kind: "createRecord", ValuesPresent: true, Values: map[string]any{}}})
	if err != nil {
		t.Fatal(err)
	}
	server := httpapi.New(config.Load(), postgres.ReadyChecker(db), httpapi.Dependencies{Authenticator: repository, Catalog: cat, Records: records})
	mutationID := newMutationID(t)
	requestBody, err := json.Marshal(map[string]any{"clientMutationId": mutationID, "commands": []any{map[string]any{
		"kind": "updateRecord", "recordId": initial.Results[0].Record.ID, "expectedRevision": 1,
		"choices": []any{map[string]any{"fieldId": field.ID, "expectedFieldRevision": 1, "options": []any{map[string]any{"name": "  Straße  ", "color": "lime"}}}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/v1/tables/"+table.Table.ID+"/records/mutate", strings.NewReader(string(requestBody)))
	r.Header.Set("Authorization", "Bearer "+bootstrap.Token.Secret)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("atomic HTTP = %d %s", w.Code, w.Body.String())
	}
	validateMutationHTTPResponse(t, w)
	var result loomrecord.MutationResult
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.ChoiceFields) != 1 || len(result.ChoiceFields[0].CreatedOptions) != 1 {
		t.Fatalf("missing option delta: %#v", result)
	}
	created := result.ChoiceFields[0].CreatedOptions[0]
	if created.Name != "Straße" || created.Color != "lime" || result.Results[0].Record.Values[field.ID] != created.ID || result.Results[0].Record.Revision != 2 {
		t.Fatalf("atomic result = %#v", result)
	}
	actualField, err := repository.GetField(ctx, actor, field.ID)
	if err != nil {
		t.Fatal(err)
	}
	if actualField.Revision != 2 || len(actualField.Config.(domain.SelectFieldConfig).Options) != 2 {
		t.Fatalf("persisted field = %#v", actualField)
	}
	makeField := func(t *testing.T, kind string, options ...catalog.SelectOptionInput) domain.Field {
		t.Helper()
		f, err := cat.CreateField(ctx, actor, newMutationID(t), table.Table.ID, catalog.FieldInput{Name: t.Name(), Type: kind, Config: catalog.SelectFieldConfigInput{Options: options}})
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	freshRecord := func(t *testing.T, values map[string]any) loomrecord.Record {
		t.Helper()
		r, err := records.Mutate(ctx, actor, table.Table.ID, newMutationID(t), []loomrecord.Command{{Kind: "createRecord", Values: values, ValuesPresent: true}})
		if err != nil {
			t.Fatal(err)
		}
		wire, _ := json.Marshal(r)
		if len(r.ChoiceFields) != 0 || strings.Contains(string(wire), "choiceFields") {
			t.Fatal("legacy response changed")
		}
		return r.Results[0].Record
	}
	fieldNow := func(t *testing.T, fieldID string) domain.Field {
		t.Helper()
		f, err := repository.GetField(ctx, actor, fieldID)
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	countRecords := func(t *testing.T) int64 {
		t.Helper()
		q, err := records.Query(ctx, actor, table.Table.ID, loomrecord.QueryRequest{Limit: 1})
		if err != nil {
			t.Fatal(err)
		}
		if q.TotalCount == nil {
			t.Fatal("missing complete count")
		}
		return *q.TotalCount
	}
	choice := func(f domain.Field, options ...loomrecord.ChoiceOption) loomrecord.ChoiceAssignment {
		return loomrecord.ChoiceAssignment{FieldID: f.ID, ExpectedFieldRevision: f.Revision, Options: append([]loomrecord.ChoiceOption{}, options...)}
	}
	update := func(r loomrecord.Record, choices ...loomrecord.ChoiceAssignment) loomrecord.Command {
		return loomrecord.Command{Kind: "updateRecord", RecordID: r.ID, ExpectedRevision: r.Revision, Choices: choices, ChoicesPresent: true}
	}
	draft := func(choices ...loomrecord.ChoiceAssignment) loomrecord.Command {
		return loomrecord.Command{Kind: "createRecord", Values: map[string]any{}, ValuesPresent: true, Choices: choices, ChoicesPresent: true}
	}
	noReceipt := func(t *testing.T, key string) {
		t.Helper()
		var receipt, guard bool
		if err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM idempotency_keys WHERE actor_id=$1 AND client_mutation_id=$2),EXISTS(SELECT 1 FROM mutation_identity_guards WHERE actor_id=$1 AND client_mutation_id=$2)`, actor, key).Scan(&receipt, &guard); err != nil {
			t.Fatal(err)
		}
		if receipt || guard {
			t.Fatalf("failure persisted receipt=%t guard=%t", receipt, guard)
		}
	}
	type outcome struct {
		result loomrecord.MutationResult
		err    error
	}
	concurrent := func(t *testing.T, keys []string, commands [][]loomrecord.Command) []outcome {
		t.Helper()
		runCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		start := make(chan struct{})
		done := make(chan outcome, 2)
		for i := 0; i < 2; i++ {
			go func(i int) {
				<-start
				r, err := records.Mutate(runCtx, actor, table.Table.ID, keys[i], commands[i])
				done <- outcome{r, err}
			}(i)
		}
		close(start)
		return []outcome{<-done, <-done}
	}

	t.Run("reuse no-op and stale CAS replay", func(t *testing.T) {
		original := []loomrecord.Command{update(initial.Results[0].Record, choice(field, loomrecord.ChoiceOption{Name: "  Straße  ", Color: "lime"}))}
		replay, err := records.Mutate(ctx, actor, table.Table.ID, mutationID, original)
		if err != nil {
			t.Fatal(err)
		}
		a, _ := json.Marshal(result)
		b, _ := json.Marshal(replay)
		if !bytes.Equal(a, b) {
			t.Fatal("replay changed operation identity or delta")
		}
		noop, err := records.Mutate(ctx, actor, table.Table.ID, newMutationID(t), []loomrecord.Command{update(result.Results[0].Record, choice(actualField, loomrecord.ChoiceOption{Name: "STRASSE", Color: "brown"}))})
		if err != nil {
			t.Fatal(err)
		}
		if noop.Results[0].Status != "unchanged" || noop.Results[0].Record.Revision != 2 || noop.ChoiceFields[0].Revision != 2 || len(noop.ChoiceFields[0].CreatedOptions) != 0 || noop.ChoiceFields[0].ResolvedOptions[0] != created || noop.ChangeCursor != result.ChangeCursor {
			t.Fatalf("reuse/no-op=%#v", noop)
		}
		changed := append([]loomrecord.Command(nil), original...)
		changed[0].ExpectedRevision = 2
		_, err = records.Mutate(ctx, actor, table.Table.ID, mutationID, changed)
		var reused *domain.IdempotencyKeyReusedError
		if !errors.As(err, &reused) {
			t.Fatalf("changed immutable payload=%v", err)
		}
	})

	t.Run("complete HTTP response schemas", func(t *testing.T) {
		f := makeField(t, "select")
		r := freshRecord(t, map[string]any{})
		call := func(key string, command map[string]any, status int) map[string]any {
			t.Helper()
			body, err := json.Marshal(map[string]any{"clientMutationId": key, "commands": []any{command}})
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(http.MethodPost, "/v1/tables/"+table.Table.ID+"/records/mutate", bytes.NewReader(body))
			request.Header.Set("Authorization", "Bearer "+bootstrap.Token.Secret)
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			server.Handler().ServeHTTP(response, request)
			if response.Code != status {
				t.Fatalf("HTTP status=%d want=%d %s", response.Code, status, response.Body.String())
			}
			return validateMutationHTTPResponse(t, response)
		}
		command := func(fieldRevision, recordRevision int64) map[string]any {
			return map[string]any{"kind": "updateRecord", "recordId": r.ID, "expectedRevision": recordRevision,
				"choices": []any{map[string]any{"fieldId": f.ID, "expectedFieldRevision": fieldRevision, "options": []any{map[string]any{"name": "Schema", "color": "lime"}}}}}
		}
		if err := cat.DeleteField(ctx, actor, f.ID, f.Revision); err != nil {
			t.Fatal(err)
		}
		f = fieldNow(t, f.ID)
		for _, kind := range []string{"updateRecord", "createRecord"} {
			key := newMutationID(t)
			cmd := command(f.Revision, r.Revision)
			if kind == "createRecord" {
				delete(cmd, "recordId")
				delete(cmd, "expectedRevision")
				cmd["kind"], cmd["values"] = kind, map[string]any{}
			}
			body := call(key, cmd, 422)
			errBody := body["error"].(map[string]any)
			details := errBody["details"].(map[string]any)
			if errBody["code"] != "INVALID_STATE_TRANSITION" || details["action"] != "update" || details["current"] != "deleted" || details["resource"].(map[string]any)["id"] != f.ID {
				t.Fatalf("deleted Field details=%v", errBody)
			}
			noReceipt(t, key)
		}
		// Field CAS takes priority even when the Field is deleted.
		key := newMutationID(t)
		call(key, command(f.Revision-1, r.Revision), 409)
		noReceipt(t, key)
		f, err = cat.RestoreField(ctx, actor, f.ID, f.Revision)
		if err != nil {
			t.Fatal(err)
		}
		key = newMutationID(t)
		call(key, command(f.Revision, r.Revision+1), 409)
		noReceipt(t, key)
		// Select cardinality, bad color and foreign Option references retain schema-valid errors.
		for _, options := range []any{
			[]any{},
			[]any{map[string]any{"name": "A", "color": "red"}, map[string]any{"name": "B", "color": "blue"}},
			[]any{map[string]any{"name": "Bad", "color": "unknown"}},
			[]any{map[string]any{"optionId": "opt_00000000000000000000000000"}},
		} {
			cmd := command(f.Revision, r.Revision)
			cmd["choices"].([]any)[0].(map[string]any)["options"] = options
			key = newMutationID(t)
			call(key, cmd, 422)
			noReceipt(t, key)
		}
		plain, err := cat.CreateField(ctx, actor, newMutationID(t), table.Table.ID, catalog.FieldInput{Name: "Schema text", Type: "text", Config: domain.EmptyFieldConfig{}})
		if err != nil {
			t.Fatal(err)
		}
		multi := makeField(t, "multiSelect")
		for _, target := range []domain.Field{plain, multi} {
			cmd := command(target.Revision, r.Revision)
			entry := cmd["choices"].([]any)[0].(map[string]any)
			entry["fieldId"] = target.ID
			entry["options"] = []any{map[string]any{"name": "A", "color": "red"}, map[string]any{"name": "a", "color": "blue"}}
			key = newMutationID(t)
			call(key, cmd, 422)
			noReceipt(t, key)
		}
		foreign := command(f.Revision, r.Revision)
		foreign["choices"].([]any)[0].(map[string]any)["fieldId"] = "fld_00000000000000000000000000"
		key = newMutationID(t)
		call(key, foreign, 404)
		noReceipt(t, key)
		key = newMutationID(t)
		original := command(f.Revision, r.Revision)
		applied := call(key, original, 200)
		replay := call(key, original, 200)
		a, _ := json.Marshal(applied)
		b, _ := json.Marshal(replay)
		if !bytes.Equal(a, b) {
			t.Fatal("HTTP replay changed full response")
		}
		call(key, command(f.Revision, r.Revision+1), 409)
		if _, err := db.ExecContext(ctx, `UPDATE idempotency_keys SET created_at=clock_timestamp()-interval '31 days' WHERE actor_id=$1 AND client_mutation_id=$2`, actor, key); err != nil {
			t.Fatal(err)
		}
		if _, err := repository.CleanupRetention(ctx, 0, 30*24*time.Hour, 10, 10000); err != nil {
			t.Fatal(err)
		}
		call(key, original, 410)
		// Legacy HTTP response remains schema-valid and omits the G2 delta.
		legacy := call(newMutationID(t), map[string]any{"kind": "createRecord", "values": map[string]any{}}, 200)
		if _, found := legacy["choiceFields"]; found {
			t.Fatal("legacy HTTP gained choiceFields")
		}
		current, err := records.Get(ctx, actor, r.ID)
		if err != nil {
			t.Fatal(err)
		}
		deleted, err := records.Mutate(ctx, actor, table.Table.ID, newMutationID(t), []loomrecord.Command{{Kind: "deleteRecord", RecordID: r.ID, ExpectedRevision: current.Revision}})
		if err != nil {
			t.Fatal(err)
		}
		f = fieldNow(t, f.ID)
		key = newMutationID(t)
		call(key, command(f.Revision, deleted.Results[0].Record.Revision), 422)
		noReceipt(t, key)
	})

	t.Run("multi Field Draft and batch initial Field revision", func(t *testing.T) {
		s := makeField(t, "select")
		m := makeField(t, "multiSelect")
		commands := []loomrecord.Command{
			draft(choice(s, loomrecord.ChoiceOption{Name: "One", Color: "lime"}), choice(m, loomrecord.ChoiceOption{Name: " Cafe\u0301 ", Color: "indigo"}, loomrecord.ChoiceOption{Name: "Two", Color: "brown"})),
			draft(choice(m, loomrecord.ChoiceOption{Name: "CAFÉ", Color: "gray"})),
		}
		before := countRecords(t)
		key := newMutationID(t)
		r, err := records.Mutate(ctx, actor, table.Table.ID, key, commands)
		if err != nil {
			t.Fatal(err)
		}
		if len(r.Results) != 2 || len(r.ChoiceFields) != 2 || countRecords(t) != before+2 {
			t.Fatalf("draft results=%#v", r)
		}
		first := r.Results[0].Record.Values[m.ID].([]any)
		second := r.Results[1].Record.Values[m.ID].([]any)
		if len(first) != 2 || len(second) != 1 || first[0] != second[0] || r.Results[0].Record.Revision != 1 || r.Results[0].Record.ID == r.Results[1].Record.ID {
			t.Fatal("Draft identities/canonical reuse invalid")
		}
		if len(commands[0].Values) != 0 || commands[0].Choices[1].Options[0].Name != " Cafe\u0301 " {
			t.Fatal("caller Draft rewritten")
		}
		if fieldNow(t, m.ID).Revision != 2 || fieldNow(t, s.ID).Revision != 2 {
			t.Fatal("Field revision was not incremented once per batch")
		}
		replay, err := records.Mutate(ctx, actor, table.Table.ID, key, commands)
		if err != nil {
			t.Fatal(err)
		}
		a, _ := json.Marshal(r)
		b, _ := json.Marshal(replay)
		if !bytes.Equal(a, b) || countRecords(t) != before+2 {
			t.Fatal("Draft replay recreated data")
		}
	})

	t.Run("double CAS and rollback after earlier commands", func(t *testing.T) {
		f := makeField(t, "select")
		r := freshRecord(t, map[string]any{})
		for _, tc := range []struct {
			fieldRev, recordRev int64
			fieldConflict       bool
		}{{2, 1, true}, {1, 2, false}, {2, 2, true}} {
			cmd := update(r, choice(f, loomrecord.ChoiceOption{Name: "Never", Color: "lime"}))
			cmd.ExpectedRevision = tc.recordRev
			cmd.Choices[0].ExpectedFieldRevision = tc.fieldRev
			key := newMutationID(t)
			_, err := records.Mutate(ctx, actor, table.Table.ID, key, []loomrecord.Command{cmd})
			var conflict *loomrecord.ConflictError
			if !errors.As(err, &conflict) || (len(conflict.FieldConflicts) > 0) != tc.fieldConflict {
				t.Fatalf("double CAS=%v", err)
			}
			noReceipt(t, key)
		}
		before := countRecords(t)
		tail, err := records.Changes(ctx, actor, table.Table.ID, "", 100)
		if err != nil {
			t.Fatal(err)
		}
		key := newMutationID(t)
		_, err = records.Mutate(ctx, actor, table.Table.ID, key, []loomrecord.Command{draft(choice(f, loomrecord.ChoiceOption{Name: "Rolled back", Color: "indigo"})), {Kind: "createRecord", ValuesPresent: true, Values: map[string]any{table.Table.PrimaryFieldID: 42}}})
		var validation *domain.ValidationError
		if !errors.As(err, &validation) {
			t.Fatalf("later command failure=%v", err)
		}
		noReceipt(t, key)
		after := fieldNow(t, f.ID)
		changes, err := records.Changes(ctx, actor, table.Table.ID, tail.NextCursor, 100)
		if err != nil {
			t.Fatal(err)
		}
		if after.Revision != 1 || len(after.Config.(domain.SelectFieldConfig).Options) != 0 || countRecords(t) != before || len(changes.Items) != 0 {
			t.Fatal("failed transaction left partial effects")
		}
	})

	t.Run("deleted Option retain create and explicit restore", func(t *testing.T) {
		f := makeField(t, "multiSelect", catalog.SelectOptionInput{Name: "Ghost", Color: "blue"}, catalog.SelectOptionInput{Name: "Live", Color: "gray"})
		cfg := f.Config.(domain.SelectFieldConfig)
		ghost := cfg.Options[0]
		live := cfg.Options[1]
		r := freshRecord(t, map[string]any{f.ID: []any{ghost.ID}})
		f, err = cat.UpdateField(ctx, actor, f.ID, catalog.FieldUpdate{Type: "multiSelect", ExpectedRevision: 1, Config: catalog.SelectFieldConfigInput{Options: []catalog.SelectOptionInput{{ID: &live.ID, Name: live.Name, Color: live.Color}}}})
		if err != nil {
			t.Fatal(err)
		}
		assigned, err := records.Mutate(ctx, actor, table.Table.ID, newMutationID(t), []loomrecord.Command{update(r, choice(f, loomrecord.ChoiceOption{OptionID: ghost.ID}, loomrecord.ChoiceOption{Name: "Fresh", Color: "brown"}))})
		if err != nil {
			t.Fatal(err)
		}
		f = fieldNow(t, f.ID)
		cfg = f.Config.(domain.SelectFieldConfig)
		if len(cfg.DeletedOptions) != 1 || cfg.DeletedOptions[0].ID != ghost.ID || len(assigned.ChoiceFields[0].CreatedOptions) != 1 {
			t.Fatal("retention implicitly restored tombstone")
		}
		key := newMutationID(t)
		before := countRecords(t)
		_, err = records.Mutate(ctx, actor, table.Table.ID, key, []loomrecord.Command{draft(choice(f, loomrecord.ChoiceOption{Name: "Never", Color: "lime"}, loomrecord.ChoiceOption{OptionID: ghost.ID}))})
		var validation *domain.ValidationError
		if !errors.As(err, &validation) || countRecords(t) != before || fieldNow(t, f.ID).Revision != f.Revision {
			t.Fatalf("new deleted reference=%v", err)
		}
		noReceipt(t, key)
		newGhost, err := records.Mutate(ctx, actor, table.Table.ID, newMutationID(t), []loomrecord.Command{update(assigned.Results[0].Record, choice(f, loomrecord.ChoiceOption{Name: "GHOST", Color: "lime"}))})
		if err != nil {
			t.Fatal(err)
		}
		if newGhost.ChoiceFields[0].CreatedOptions[0].ID == ghost.ID {
			t.Fatal("name lookup resurrected deleted identity")
		}
		f = fieldNow(t, f.ID)
		cfg = f.Config.(domain.SelectFieldConfig)
		options := []catalog.SelectOptionInput{}
		for _, o := range cfg.Options {
			if o.ID != newGhost.ChoiceFields[0].CreatedOptions[0].ID {
				oid := o.ID
				options = append(options, catalog.SelectOptionInput{ID: &oid, Name: o.Name, Color: o.Color})
			}
		}
		options = append(options, catalog.SelectOptionInput{ID: &ghost.ID, Name: ghost.Name, Color: ghost.Color})
		f, err = cat.UpdateField(ctx, actor, f.ID, catalog.FieldUpdate{Type: "multiSelect", ExpectedRevision: f.Revision, Config: catalog.SelectFieldConfigInput{Options: options}})
		if err != nil {
			t.Fatal(err)
		}
		_, err = records.Mutate(ctx, actor, table.Table.ID, newMutationID(t), []loomrecord.Command{update(newGhost.Results[0].Record, choice(f, loomrecord.ChoiceOption{OptionID: ghost.ID}))})
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("Field and Record lifecycle", func(t *testing.T) {
		f := makeField(t, "select")
		r := freshRecord(t, map[string]any{})
		if err := cat.DeleteField(ctx, actor, f.ID, 1); err != nil {
			t.Fatal(err)
		}
		f = fieldNow(t, f.ID)
		key := newMutationID(t)
		_, err := records.Mutate(ctx, actor, table.Table.ID, key, []loomrecord.Command{update(r, choice(f, loomrecord.ChoiceOption{Name: "Never", Color: "lime"}))})
		var state *domain.InvalidStateTransitionError
		if !errors.As(err, &state) {
			t.Fatalf("deleted Field=%v", err)
		}
		noReceipt(t, key)
		f, err = cat.RestoreField(ctx, actor, f.ID, f.Revision)
		if err != nil {
			t.Fatal(err)
		}
		deleted, err := records.Mutate(ctx, actor, table.Table.ID, newMutationID(t), []loomrecord.Command{{Kind: "deleteRecord", RecordID: r.ID, ExpectedRevision: 1}})
		if err != nil {
			t.Fatal(err)
		}
		key = newMutationID(t)
		_, err = records.Mutate(ctx, actor, table.Table.ID, key, []loomrecord.Command{update(deleted.Results[0].Record, choice(f, loomrecord.ChoiceOption{Name: "Never", Color: "lime"}))})
		if !errors.As(err, &state) {
			t.Fatalf("deleted Record=%v", err)
		}
		noReceipt(t, key)
		restored, err := records.Mutate(ctx, actor, table.Table.ID, newMutationID(t), []loomrecord.Command{{Kind: "restoreRecord", RecordID: r.ID, ExpectedRevision: 2}})
		if err != nil {
			t.Fatal(err)
		}
		_, err = records.Mutate(ctx, actor, table.Table.ID, newMutationID(t), []loomrecord.Command{update(restored.Results[0].Record, choice(f, loomrecord.ChoiceOption{Name: "Restored", Color: "indigo"}))})
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("SQL failure and retry original operation", func(t *testing.T) {
		f := makeField(t, "select")
		cmds := []loomrecord.Command{draft(choice(f, loomrecord.ChoiceOption{Name: "Atomic", Color: "brown"}))}
		key := newMutationID(t)
		before := countRecords(t)
		_, err := db.ExecContext(ctx, `CREATE FUNCTION g2_fail_receipt() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic receipt failure'; END $$; CREATE TRIGGER g2_fail_receipt BEFORE INSERT ON idempotency_keys FOR EACH ROW EXECUTE FUNCTION g2_fail_receipt()`)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			db.ExecContext(context.Background(), `DROP TRIGGER IF EXISTS g2_fail_receipt ON idempotency_keys; DROP FUNCTION IF EXISTS g2_fail_receipt()`)
		})
		_, err = records.Mutate(ctx, actor, table.Table.ID, key, cmds)
		if err == nil || countRecords(t) != before || fieldNow(t, f.ID).Revision != 1 {
			t.Fatalf("SQL failure not atomic: %v", err)
		}
		noReceipt(t, key)
		if _, err := db.ExecContext(ctx, `DROP TRIGGER g2_fail_receipt ON idempotency_keys; DROP FUNCTION g2_fail_receipt()`); err != nil {
			t.Fatal(err)
		}
		r, err := records.Mutate(ctx, actor, table.Table.ID, key, cmds)
		if err != nil {
			t.Fatal(err)
		}
		replay, err := records.Mutate(ctx, actor, table.Table.ID, key, cmds)
		if err != nil {
			t.Fatal(err)
		}
		if countRecords(t) != before+1 || r.Results[0].Record.ID != replay.Results[0].Record.ID {
			t.Fatal("unknown retry duplicated Draft")
		}
	})

	t.Run("response write lost and receipt expiry guard", func(t *testing.T) {
		f := makeField(t, "select")
		key := newMutationID(t)
		commands := []loomrecord.Command{draft(choice(f, loomrecord.ChoiceOption{Name: "Committed", Color: "lime"}))}
		before := countRecords(t)
		body, _ := json.Marshal(map[string]any{"clientMutationId": key, "commands": []any{map[string]any{"kind": "createRecord", "values": map[string]any{}, "choices": commands[0].Choices}}})
		r := httptest.NewRequest(http.MethodPost, "/v1/tables/"+table.Table.ID+"/records/mutate", strings.NewReader(string(body)))
		r.Header.Set("Authorization", "Bearer "+bootstrap.Token.Secret)
		r.Header.Set("Content-Type", "application/json")
		lost := &lostChoiceResponse{header: make(http.Header)}
		server.Handler().ServeHTTP(lost, r)
		if !lost.failed || countRecords(t) != before+1 {
			t.Fatal("loss was not after commit")
		}
		recovered, err := records.Mutate(ctx, actor, table.Table.ID, key, commands)
		if err != nil {
			t.Fatal(err)
		}
		if recovered.ClientMutationID != key || countRecords(t) != before+1 || len(recovered.ChoiceFields[0].CreatedOptions) != 1 {
			t.Fatal("lost response retry did not replay")
		}
		if _, err := db.ExecContext(ctx, `UPDATE idempotency_keys SET created_at=clock_timestamp()-interval '31 days' WHERE actor_id=$1 AND client_mutation_id=$2`, actor, key); err != nil {
			t.Fatal(err)
		}
		stats, err := repository.CleanupRetention(ctx, 0, 30*24*time.Hour, 10, 10000)
		if err != nil || stats.IdempotencyDeleted != 1 {
			t.Fatalf("retention=%#v %v", stats, err)
		}
		_, err = records.Mutate(ctx, actor, table.Table.ID, key, commands)
		var expired *domain.MutationReceiptExpiredError
		if !errors.As(err, &expired) || countRecords(t) != before+1 {
			t.Fatalf("expired operation reapplied: %v", err)
		}
		changed := []loomrecord.Command{draft(choice(f, loomrecord.ChoiceOption{Name: "Different", Color: "brown"}))}
		_, err = records.Mutate(ctx, actor, table.Table.ID, key, changed)
		var reused *domain.IdempotencyKeyReusedError
		if !errors.As(err, &reused) {
			t.Fatalf("guard allowed changed payload=%v", err)
		}
		_, err = cat.CreateWorkspace(ctx, actor, key, "Other endpoint")
		if !errors.As(err, &reused) {
			t.Fatalf("guard lost shared key namespace=%v", err)
		}
	})

	t.Run("G1 full palette through atomic MultiSelect", func(t *testing.T) {
		f := makeField(t, "multiSelect")
		opts := []loomrecord.ChoiceOption{}
		for i, color := range catalog.SelectOptionColors {
			opts = append(opts, loomrecord.ChoiceOption{Name: fmt.Sprintf("Color %d", i), Color: color})
		}
		r, err := records.Mutate(ctx, actor, table.Table.ID, newMutationID(t), []loomrecord.Command{draft(choice(f, opts...))})
		if err != nil {
			t.Fatal(err)
		}
		if len(r.ChoiceFields[0].CreatedOptions) != 12 {
			t.Fatal("palette reduced")
		}
		for i, o := range r.ChoiceFields[0].CreatedOptions {
			if o.Color != catalog.SelectOptionColors[i] {
				t.Fatal("palette color changed")
			}
		}
		_, err = records.Mutate(ctx, actor, table.Table.ID, newMutationID(t), []loomrecord.Command{update(r.Results[0].Record, choice(fieldNow(t, f.ID)))})
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("rights foreign IDs duplicates and active budget", func(t *testing.T) {
		f := makeField(t, "multiSelect")
		r := freshRecord(t, map[string]any{})
		for _, opts := range [][]loomrecord.ChoiceOption{{{Name: "DUP", Color: "gray"}, {Name: "dup", Color: "lime"}}, {{OptionID: created.ID}}} {
			key := newMutationID(t)
			_, err := records.Mutate(ctx, actor, table.Table.ID, key, []loomrecord.Command{update(r, choice(f, opts...))})
			var v *domain.ValidationError
			if !errors.As(err, &v) || fieldNow(t, f.ID).Revision != 1 {
				t.Fatalf("invalid selection=%v", err)
			}
			noReceipt(t, key)
		}
		foreign, err := id.New(id.ActorPrefix)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO actors(id) VALUES($1)`, foreign); err != nil {
			t.Fatal(err)
		}
		_, err = records.Mutate(ctx, foreign, table.Table.ID, newMutationID(t), []loomrecord.Command{update(r, choice(f, loomrecord.ChoiceOption{Name: "Denied", Color: "gray"}))})
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("foreign Actor=%v", err)
		}
		options := []catalog.SelectOptionInput{}
		for i := 0; i < 500; i++ {
			options = append(options, catalog.SelectOptionInput{Name: fmt.Sprintf("Existing %d", i), Color: "gray"})
		}
		full := makeField(t, "select", options...)
		key := newMutationID(t)
		_, err = records.Mutate(ctx, actor, table.Table.ID, key, []loomrecord.Command{draft(choice(full, loomrecord.ChoiceOption{Name: "Overflow", Color: "brown"}))})
		var v *domain.ValidationError
		if !errors.As(err, &v) || fieldNow(t, full.ID).Revision != 1 {
			t.Fatalf("active budget=%v", err)
		}
		noReceipt(t, key)
		_, err = records.Mutate(ctx, actor, table.Table.ID, newMutationID(t), []loomrecord.Command{draft(choice(full, loomrecord.ChoiceOption{Name: "EXISTING 0", Color: "lime"}))})
		if err != nil {
			t.Fatal("full Field should still reuse:", err)
		}
	})

	t.Run("concurrent canonical name and explicit rebase", func(t *testing.T) {
		f := makeField(t, "select")
		r1 := freshRecord(t, map[string]any{})
		r2 := freshRecord(t, map[string]any{})
		cmds := [][]loomrecord.Command{{update(r1, choice(f, loomrecord.ChoiceOption{Name: " Cafe\u0301 ", Color: "lime"}))}, {update(r2, choice(f, loomrecord.ChoiceOption{Name: "CAFÉ", Color: "indigo"}))}}
		outs := concurrent(t, []string{newMutationID(t), newMutationID(t)}, cmds)
		success, conflicts := 0, 0
		for _, o := range outs {
			if o.err == nil {
				success++
			} else {
				var c *loomrecord.ConflictError
				if !errors.As(o.err, &c) || len(c.FieldConflicts) != 1 {
					t.Fatalf("same name race=%v", o.err)
				}
				conflicts++
			}
		}
		f = fieldNow(t, f.ID)
		cfg := f.Config.(domain.SelectFieldConfig)
		if success != 1 || conflicts != 1 || f.Revision != 2 || len(cfg.Options) != 1 {
			t.Fatal("same name race bypassed CAS")
		}
		for _, r := range []loomrecord.Record{r1, r2} {
			current, err := records.Get(ctx, actor, r.ID)
			if err != nil {
				t.Fatal(err)
			}
			_, err = records.Mutate(ctx, actor, table.Table.ID, newMutationID(t), []loomrecord.Command{update(current, choice(f, loomrecord.ChoiceOption{Name: "café", Color: "brown"}))})
			if err != nil {
				t.Fatal(err)
			}
		}
		if fieldNow(t, f.ID).Revision != 2 {
			t.Fatal("explicit rebase created a second identity")
		}
	})

	t.Run("concurrent Record CAS with unchanged Field", func(t *testing.T) {
		f := makeField(t, "select", catalog.SelectOptionInput{Name: "A", Color: "gray"}, catalog.SelectOptionInput{Name: "B", Color: "lime"})
		r := freshRecord(t, map[string]any{})
		cfg := f.Config.(domain.SelectFieldConfig)
		outs := concurrent(t, []string{newMutationID(t), newMutationID(t)}, [][]loomrecord.Command{{update(r, choice(f, loomrecord.ChoiceOption{OptionID: cfg.Options[0].ID}))}, {update(r, choice(f, loomrecord.ChoiceOption{OptionID: cfg.Options[1].ID}))}})
		success, conflicts := 0, 0
		for _, o := range outs {
			if o.err == nil {
				success++
			} else {
				var c *loomrecord.ConflictError
				if !errors.As(o.err, &c) || len(c.Conflicts) != 1 || len(c.FieldConflicts) != 0 {
					t.Fatalf("Record race=%v", o.err)
				}
				conflicts++
			}
		}
		if success != 1 || conflicts != 1 || fieldNow(t, f.ID).Revision != 1 {
			t.Fatal("Record CAS race violated revisions")
		}
	})

	t.Run("concurrent original key replays one Draft", func(t *testing.T) {
		f := makeField(t, "select")
		key := newMutationID(t)
		cmds := []loomrecord.Command{draft(choice(f, loomrecord.ChoiceOption{Name: "Once", Color: "brown"}))}
		before := countRecords(t)
		outs := concurrent(t, []string{key, key}, [][]loomrecord.Command{cmds, cmds})
		for _, o := range outs {
			if o.err != nil {
				t.Fatal(o.err)
			}
		}
		a, _ := json.Marshal(outs[0].result)
		b, _ := json.Marshal(outs[1].result)
		if !bytes.Equal(a, b) || countRecords(t) != before+1 || fieldNow(t, f.ID).Revision != 2 {
			t.Fatal("same key race duplicated data/identity")
		}
	})

	t.Run("Convert Table lock then Field lock with Actor FK", func(t *testing.T) {
		f := makeField(t, "select", catalog.SelectOptionInput{Name: "Old", Color: "gray"})
		other := makeField(t, "multiSelect")
		oldID := f.Config.(domain.SelectFieldConfig).Options[0].ID
		r := freshRecord(t, map[string]any{f.ID: oldID})
		preview, err := cat.PreviewFieldConversion(ctx, actor, f.ID, "text")
		if err != nil {
			t.Fatal(err)
		}
		blocker, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := blocker.ExecContext(ctx, `SELECT id FROM fields WHERE id=$1 FOR UPDATE`, other.ID); err != nil {
			blocker.Rollback()
			t.Fatal(err)
		}
		runCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		convertDone := make(chan struct{})
		choiceDone := make(chan struct{})
		choiceStarted := false
		var convertErr, choiceErr error
		t.Cleanup(func() {
			blocker.Rollback()
			cancel()
			<-convertDone
			if choiceStarted {
				<-choiceDone
			}
		})
		go func() {
			defer close(convertDone)
			_, convertErr = cat.ConvertField(runCtx, actor, f.ID, catalog.ConversionRequest{TargetType: "text", Mode: "optionName", ExpectedRevision: 1, PreviewToken: preview.PreviewToken})
		}()
		waitForChoiceLock(t, runCtx, db, "%FROM fields%")
		key := newMutationID(t)
		cmds := []loomrecord.Command{update(r, choice(f, loomrecord.ChoiceOption{Name: "Never", Color: "lime"}))}
		choiceStarted = true
		go func() {
			defer close(choiceDone)
			_, choiceErr = records.Mutate(runCtx, actor, table.Table.ID, key, cmds)
		}()
		waitForChoiceLock(t, runCtx, db, "%FOR UPDATE OF t%")
		if err := blocker.Rollback(); err != nil {
			t.Fatal(err)
		}
		<-convertDone
		<-choiceDone
		var conflict *loomrecord.ConflictError
		if convertErr != nil || !errors.As(choiceErr, &conflict) || len(conflict.FieldConflicts) != 1 {
			t.Fatalf("Convert/G2 lock order: convert=%v Choice=%v", convertErr, choiceErr)
		}
		current, err := records.Get(ctx, actor, r.ID)
		if err != nil {
			t.Fatal(err)
		}
		if fieldNow(t, f.ID).Type != "text" || current.Values[f.ID] != "Old" || current.Revision != 1 {
			t.Fatal("Convert projection or Record CAS changed")
		}
		noReceipt(t, key)
	})

	t.Run("total tombstone budget", func(t *testing.T) {
		f := makeField(t, "select")
		cfg := domain.SelectFieldConfig{Options: []domain.SelectOption{}, DeletedOptions: []domain.DeletedSelectOption{}}
		for i := 0; i < 5000; i++ {
			oid, err := id.New(id.OptionPrefix)
			if err != nil {
				t.Fatal(err)
			}
			cfg.DeletedOptions = append(cfg.DeletedOptions, domain.DeletedSelectOption{ID: oid, Name: fmt.Sprintf("Retired %d", i), Color: "gray", DeletedAt: time.Now().UTC()})
		}
		encoded, _ := json.Marshal(cfg)
		// Seed only this disposable fixture's valid historical tombstones.
		if _, err := db.ExecContext(ctx, `UPDATE fields SET config=$1::jsonb WHERE id=$2`, string(encoded), f.ID); err != nil {
			t.Fatal(err)
		}
		key := newMutationID(t)
		before := countRecords(t)
		_, err := records.Mutate(ctx, actor, table.Table.ID, key, []loomrecord.Command{draft(choice(f, loomrecord.ChoiceOption{Name: "Overflow", Color: "brown"}))})
		var v *domain.ValidationError
		if !errors.As(err, &v) || countRecords(t) != before || fieldNow(t, f.ID).Revision != 1 {
			t.Fatalf("total budget=%v", err)
		}
		noReceipt(t, key)
	})
}

func waitForChoiceLock(t *testing.T, ctx context.Context, db *sql.DB, queryPattern string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var blocked bool
		err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE $1)`, queryPattern).Scan(&blocked)
		if err != nil {
			t.Fatal(err)
		}
		if blocked {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("writer did not wait at expected lock boundary %s", queryPattern)
}

type lostChoiceResponse struct {
	header http.Header
	failed bool
}

func (w *lostChoiceResponse) Header() http.Header       { return w.header }
func (w *lostChoiceResponse) WriteHeader(int)           {}
func (w *lostChoiceResponse) Write([]byte) (int, error) { w.failed = true; return 0, io.ErrClosedPipe }

func TestAtomicChoiceMigrationForwardPreservesLegacyData(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	db := integrationDatabase(t, ctx)
	repo := postgres.NewRepository(db)
	b, err := loomauth.NewAdmin(repo).Bootstrap(ctx, "Migration fixture")
	if err != nil {
		t.Fatal(err)
	}
	cat := catalog.New(repo)
	records := loomrecord.New(repo)
	w, err := cat.CreateWorkspace(ctx, b.ActorID, newMutationID(t), "Legacy")
	if err != nil {
		t.Fatal(err)
	}
	base, err := cat.CreateBase(ctx, b.ActorID, newMutationID(t), w.ID, "Legacy")
	if err != nil {
		t.Fatal(err)
	}
	table, err := cat.CreateTable(ctx, b.ActorID, newMutationID(t), base.ID, "Legacy", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	f, err := cat.CreateField(ctx, b.ActorID, newMutationID(t), table.Table.ID, catalog.FieldInput{Name: "Legacy color", Type: "select", Config: catalog.SelectFieldConfigInput{Options: []catalog.SelectOptionInput{{Name: "Kept", Color: "indigo"}}}})
	if err != nil {
		t.Fatal(err)
	}
	before, err := records.Mutate(ctx, b.ActorID, table.Table.ID, newMutationID(t), []loomrecord.Command{{Kind: "createRecord", ValuesPresent: true, Values: map[string]any{f.ID: f.Config.(domain.SelectFieldConfig).Options[0].ID}}})
	if err != nil {
		t.Fatal(err)
	}
	var guards int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM mutation_identity_guards`).Scan(&guards); err != nil || guards != 0 {
		t.Fatal("legacy fixture unexpectedly has G2 guards")
	}
	// Reconstruct the prior six-migration schema only while the disposable
	// identity table is empty. This is not a production down migration.
	if _, err := db.ExecContext(ctx, `DROP TABLE mutation_identity_guards; DELETE FROM schema_migrations WHERE version='007_mutation_identity_guards'`); err != nil {
		t.Fatal(err)
	}
	if err := postgres.ReadyChecker(db)(ctx); !errors.Is(err, appstatus.ErrMigrationRequired) {
		t.Fatalf("unmigrated readiness=%v", err)
	}
	server := httpapi.New(config.Load(), postgres.ReadyChecker(db))
	meta := httptest.NewRecorder()
	server.Handler().ServeHTTP(meta, httptest.NewRequest(http.MethodGet, "/v1/meta", nil))
	if !strings.Contains(meta.Body.String(), `"migrationRequired":true`) {
		t.Fatal("unmigrated Meta hid migration requirement")
	}
	for i := 0; i < 2; i++ {
		if err := postgres.ApplyMigrations(ctx, db, filepath.Join("..", "..", "..", "migrations")); err != nil {
			t.Fatal(err)
		}
	}
	if err := postgres.ReadyChecker(db)(ctx); err != nil {
		t.Fatal(err)
	}
	current, err := records.Get(ctx, b.ActorID, before.Results[0].Record.ID)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(before.Results[0].Record)
	c, _ := json.Marshal(current)
	if !bytes.Equal(a, c) {
		t.Fatal("007 rewrote legacy Record")
	}
	field, err := repo.GetField(ctx, b.ActorID, f.ID)
	if err != nil {
		t.Fatal(err)
	}
	a, _ = json.Marshal(f)
	c, _ = json.Marshal(field)
	if !bytes.Equal(a, c) {
		t.Fatal("007 rewrote legacy Field")
	}
	var migrations int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&migrations); err != nil || migrations != 7 {
		t.Fatalf("migration count=%d %v", migrations, err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM mutation_identity_guards`).Scan(&guards); err != nil || guards != 0 {
		t.Fatal("007 fabricated legacy operation identities")
	}
	_, err = records.Mutate(ctx, b.ActorID, table.Table.ID, newMutationID(t), []loomrecord.Command{{Kind: "updateRecord", RecordID: current.ID, ExpectedRevision: current.Revision, Choices: []loomrecord.ChoiceAssignment{{FieldID: f.ID, ExpectedFieldRevision: f.Revision, Options: []loomrecord.ChoiceOption{{Name: "After migration", Color: "brown"}}}}, ChoicesPresent: true}})
	if err != nil {
		t.Fatal(err)
	}
}
