package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

func TestChoicePaletteDatabaseAndHTTP(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	repository := postgres.NewRepository(integrationDatabase(t, ctx))
	bootstrap, err := loomauth.NewAdmin(repository).Bootstrap(ctx, "Palette QA")
	if err != nil {
		t.Fatal(err)
	}
	actorID := bootstrap.ActorID
	catalogService := catalog.New(repository)
	recordService := loomrecord.New(repository)
	workspace, err := catalogService.CreateWorkspace(ctx, actorID, newMutationID(t), "Palette QA")
	if err != nil {
		t.Fatal(err)
	}
	base, err := catalogService.CreateBase(ctx, actorID, newMutationID(t), workspace.ID, "Base")
	if err != nil {
		t.Fatal(err)
	}
	table, err := catalogService.CreateTable(ctx, actorID, newMutationID(t), base.ID, "Palette", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	server := httpapi.New(config.Load(), func(context.Context) error { return nil }, httpapi.Dependencies{
		Authenticator: repository, Catalog: catalogService, Records: recordService,
	})
	request := func(method, path, key, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+bootstrap.Token.Secret)
		r.Header.Set("Content-Type", "application/json")
		if key != "" {
			r.Header.Set("Idempotency-Key", key)
		}
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, r)
		return w
	}
	for _, fieldType := range []string{"select", "multiSelect"} {
		t.Run(fieldType, func(t *testing.T) {
			path := "/v1/tables/" + table.Table.ID + "/fields"
			options := make([]map[string]any, 0, 12)
			for _, color := range catalog.SelectOptionColors {
				options = append(options, map[string]any{"name": color, "color": color})
			}
			body, err := json.Marshal(map[string]any{"name": fieldType, "type": fieldType, "config": map[string]any{"options": options}})
			if err != nil {
				t.Fatal(err)
			}
			key := newMutationID(t)
			created := request(http.MethodPost, path, key, string(body))
			if created.Code != http.StatusCreated {
				t.Fatalf("create: %d %s", created.Code, created.Body.String())
			}
			var field domain.Field
			if err := json.Unmarshal(created.Body.Bytes(), &field); err != nil {
				t.Fatal(err)
			}
			stored, err := repository.GetField(ctx, actorID, field.ID)
			if err != nil {
				t.Fatal(err)
			}
			cfg := stored.Config.(domain.SelectFieldConfig)
			for i, option := range cfg.Options {
				if option.Color != catalog.SelectOptionColors[i] {
					t.Fatalf("persisted option = %#v", option)
				}
			}
			replayed := request(http.MethodPost, path, key, string(body))
			if replayed.Code != http.StatusCreated || replayed.Body.String() != created.Body.String() {
				t.Fatal("create replay changed response")
			}
			changed := strings.Replace(string(body), `"color":"lime"`, `"color":"brown"`, 1)
			if w := request(http.MethodPost, path, key, changed); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "IDEMPOTENCY_KEY_REUSED") {
				t.Fatalf("key reuse: %d %s", w.Code, w.Body.String())
			}
			for _, invalid := range []struct{ color, code string }{{"", "required"}, {`,"color":"#ff0000"`, "format"}, {`,"color":"Lime"`, "format"}, {`,"color":"teal"`, "format"}} {
				payload := fmt.Sprintf(`{"name":"Invalid","type":%q,"config":{"options":[{"name":"Option"%s}]}}`, fieldType, invalid.color)
				w := request(http.MethodPost, path, newMutationID(t), payload)
				if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), `"code":"`+invalid.code+`"`) || !strings.Contains(w.Body.String(), `/config/options/0/color`) {
					t.Fatalf("color validation: %d %s", w.Code, w.Body.String())
				}
			}
			// Existing Record references remain IDs through option delete/restore.
			var value any = cfg.Options[4].ID
			if fieldType == "multiSelect" {
				value = []any{cfg.Options[4].ID, cfg.Options[8].ID, cfg.Options[11].ID}
			}
			mutation, err := recordService.Mutate(ctx, actorID, table.Table.ID, newMutationID(t), []loomrecord.Command{{Kind: "createRecord", ValuesPresent: true, Values: map[string]any{stored.ID: value}}})
			if err != nil {
				t.Fatal(err)
			}
			patchOptions := make([]catalog.SelectOptionInput, 0, len(cfg.Options))
			for _, option := range cfg.Options {
				if option.Color != "lime" {
					patchOptions = append(patchOptions, catalog.SelectOptionInput{ID: &option.ID, Name: option.Name, Color: option.Color})
				}
			}
			updated, err := catalogService.UpdateField(ctx, actorID, stored.ID, catalog.FieldUpdate{Type: fieldType, ExpectedRevision: stored.Revision, Config: catalog.SelectFieldConfigInput{Options: patchOptions}})
			if err != nil {
				t.Fatal(err)
			}
			deletedCfg := updated.Config.(domain.SelectFieldConfig)
			if len(deletedCfg.DeletedOptions) != 1 || deletedCfg.DeletedOptions[0].Color != "lime" {
				t.Fatalf("tombstone = %#v", deletedCfg)
			}
			values, err := recordService.DistinctValues(ctx, actorID, table.Table.ID, stored.ID, loomrecord.DistinctValuesRequest{})
			if err != nil || len(values.Items) == 0 || values.Items[0].Display == "" {
				t.Fatalf("distinct after delete = %#v, %v", values, err)
			}
			history, err := recordService.History(ctx, actorID, table.Table.ID, loomrecord.HistoryRequest{RecordID: mutation.Results[0].Record.ID})
			if err != nil || len(history.Items) == 0 {
				t.Fatalf("history = %#v, %v", history, err)
			}
			patchOptions = append(patchOptions, catalog.SelectOptionInput{ID: &cfg.Options[4].ID, Name: "Restored", Color: "indigo"})
			updated, err = catalogService.UpdateField(ctx, actorID, stored.ID, catalog.FieldUpdate{Type: fieldType, ExpectedRevision: updated.Revision, Config: catalog.SelectFieldConfigInput{Options: patchOptions}})
			if err != nil {
				t.Fatal(err)
			}
			restoredCfg := updated.Config.(domain.SelectFieldConfig)
			if len(restoredCfg.DeletedOptions) != 0 || restoredCfg.Options[11].Color != "indigo" || restoredCfg.Options[11].ID != cfg.Options[4].ID {
				t.Fatalf("restored option = %#v", restoredCfg)
			}
			if err := catalogService.DeleteField(ctx, actorID, stored.ID, updated.Revision); err != nil {
				t.Fatal(err)
			}
			updated, err = catalogService.RestoreField(ctx, actorID, stored.ID, updated.Revision+1)
			if err != nil || !reflect.DeepEqual(updated.Config, restoredCfg) {
				t.Fatalf("Field restore changed config: %v", err)
			}
			// Two clients race with the same revision and different old/new colors.
			start := make(chan struct{})
			results := make(chan error, 2)
			for _, color := range []string{"blue", "brown"} {
				go func(color string) {
					<-start
					input := append([]catalog.SelectOptionInput(nil), patchOptions...)
					input[0].Color = color
					_, err := catalogService.UpdateField(ctx, actorID, stored.ID, catalog.FieldUpdate{Type: fieldType, ExpectedRevision: updated.Revision, Config: catalog.SelectFieldConfigInput{Options: input}})
					results <- err
				}(color)
			}
			close(start)
			wins, conflicts := 0, 0
			for i := 0; i < 2; i++ {
				err := <-results
				var conflict *domain.RevisionConflictError
				if err == nil {
					wins++
				} else if errors.As(err, &conflict) {
					conflicts++
				} else {
					t.Fatal(err)
				}
			}
			if wins != 1 || conflicts != 1 {
				t.Fatalf("CAS wins=%d conflicts=%d", wins, conflicts)
			}
		})
	}
}
