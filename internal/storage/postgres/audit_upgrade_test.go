package postgres_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"path/filepath"
	"testing"

	attachment "github.com/Mahjong404/LoomTable-Server/internal/attachment"
	"github.com/Mahjong404/LoomTable-Server/internal/auth"
	"github.com/Mahjong404/LoomTable-Server/internal/catalog"
	"github.com/Mahjong404/LoomTable-Server/internal/config"
	"github.com/Mahjong404/LoomTable-Server/internal/domain"
	"github.com/Mahjong404/LoomTable-Server/internal/httpapi"
	record "github.com/Mahjong404/LoomTable-Server/internal/record"
	status "github.com/Mahjong404/LoomTable-Server/internal/status"
	"github.com/Mahjong404/LoomTable-Server/internal/storage/postgres"
)

func TestAuditApplied010Upgrade(t *testing.T) {
	ctx := context.Background()
	db := integrationDatabase(t, ctx)
	repo := postgres.NewRepository(db)
	boot, err := auth.NewAdmin(repo).Bootstrap(ctx, "G5 010 upgrade synthetic")
	if err != nil {
		t.Fatal(err)
	}
	cat := catalog.New(repo)
	ws, err := cat.CreateWorkspace(ctx, boot.ActorID, newMutationID(t), "Upgrade")
	if err != nil {
		t.Fatal(err)
	}
	base, err := cat.CreateBase(ctx, boot.ActorID, newMutationID(t), ws.ID, "Upgrade")
	if err != nil {
		t.Fatal(err)
	}
	table, err := cat.CreateTable(ctx, boot.ActorID, newMutationID(t), base.ID, "Upgrade", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	field, err := cat.CreateField(ctx, boot.ActorID, newMutationID(t), table.Table.ID, catalog.FieldInput{Name: "File", Type: "attachment", Config: domain.AttachmentFieldConfig{MaxCount: 1}})
	if err != nil {
		t.Fatal(err)
	}
	file, err := attachment.New(repo, attachment.NewFileStore(t.TempDir()), attachment.DefaultMaxBytes).Initialize(ctx, boot.ActorID, newMutationID(t), attachment.InitRequest{Source: "vault", Filename: "synthetic.txt", VaultPath: "Synthetic-010/synthetic.txt"})
	if err != nil {
		t.Fatal(err)
	}
	// Only owned disposable DB: reproduce an already-applied original 010, not a fresh install.
	if _, err := db.ExecContext(ctx, `DROP TRIGGER sanitize_audit_payload ON audit_events; DROP FUNCTION loomtable_sanitize_audit_payload(); DROP FUNCTION loomtable_audit_safe_payload(jsonb); DROP FUNCTION loomtable_audit_safe_value(jsonb); DELETE FROM schema_migrations WHERE version='011_audit_attachment_redaction'`); err != nil {
		t.Fatal(err)
	}
	var applied bool
	if err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version='010_table_audit')`).Scan(&applied); err != nil || !applied {
		t.Fatal("010 must remain registered", err)
	}
	if err := postgres.ReadyChecker(db)(ctx); !errors.Is(err, status.ErrMigrationRequired) {
		t.Fatalf("missing upgrade readiness=%v", err)
	}
	server := httpapi.New(config.Load(), postgres.ReadyChecker(db), httpapi.Dependencies{Authenticator: repo, Catalog: cat, Records: record.New(repo)})
	req := httptest.NewRequest("GET", "/v1/tables/"+table.Table.ID+"/audit", nil)
	req.Header.Set("Authorization", "Bearer "+boot.Token.Secret)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, req)
	if response.Code != 503 {
		t.Fatalf("unavailable upgrade=%d", response.Code)
	}
	var body any
	json.Unmarshal(response.Body.Bytes(), &body)
	if err := compileOpenAPIResponseSchema(t, "#/components/responses/ErrorResponse/content/application~1json/schema").Validate(body); err != nil {
		t.Fatal(err)
	}
	result, err := record.New(repo).Mutate(ctx, boot.ActorID, table.Table.ID, newMutationID(t), []record.Command{{Kind: "createRecord", ValuesPresent: true, Values: map[string]any{field.ID: []any{map[string]any{"id": file.ID, "source": "vault", "filename": file.Filename, "vaultPath": file.VaultPath}}}}})
	if err != nil {
		t.Fatal(err)
	}
	var eventID, operationID string
	var unsafe []byte
	if err := db.QueryRowContext(ctx, `SELECT id,operation_id,payload FROM audit_events WHERE record_id=$1`, result.Results[0].Record.ID).Scan(&eventID, &operationID, &unsafe); err != nil || !bytes.Contains(unsafe, []byte(file.VaultPath)) {
		t.Fatal("fixture must contain original 010 coordinate", err)
	}
	if err := postgres.ApplyMigrations(ctx, db, filepath.Join("..", "..", "..", "migrations")); err != nil {
		t.Fatal(err)
	}
	if err := postgres.ReadyChecker(db)(ctx); err != nil {
		t.Fatal(err)
	}
	page, err := record.New(repo).Audit(ctx, boot.ActorID, table.Table.ID, record.AuditRequest{AuditFilter: record.AuditFilter{OperationID: operationID}})
	if err != nil || page.Count != 1 || page.Items[0].ID != eventID || bytes.Contains(page.Items[0].Payload, []byte(file.VaultPath)) {
		t.Fatalf("upgrade identity/privacy=%#v %v", page, err)
	}
	var stored []byte
	if err := db.QueryRowContext(ctx, `SELECT payload FROM audit_events WHERE id=$1`, eventID).Scan(&stored); err != nil || bytes.Contains(stored, []byte(file.VaultPath)) {
		t.Fatal("011 did not scrub retained row", err)
	}
	if err := postgres.ApplyMigrations(ctx, db, filepath.Join("..", "..", "..", "migrations")); err != nil {
		t.Fatal(err)
	}
	page, err = record.New(repo).Audit(ctx, boot.ActorID, table.Table.ID, record.AuditRequest{AuditFilter: record.AuditFilter{OperationID: operationID}})
	if err != nil || page.Count != 1 {
		t.Fatal("upgrade retry changed effects", err)
	}
	encoded, _ := json.Marshal(page)
	json.Unmarshal(encoded, &body)
	if err := compileOpenAPIResponseSchema(t, "#/components/schemas/AuditPage").Validate(body); err != nil {
		t.Fatal(err)
	}
}
