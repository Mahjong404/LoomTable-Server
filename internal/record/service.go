package record

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"github.com/Mahjong404/LoomTable-Server/internal/cursor"
	"github.com/Mahjong404/LoomTable-Server/internal/domain"
	"github.com/Mahjong404/LoomTable-Server/internal/id"
)

type Store interface {
	GetRecord(context.Context, string, string) (Record, error)
	MutateRecords(context.Context, string, string, string, [32]byte, []Command) (StoredMutationResult, error)
	CursorKey(context.Context) ([]byte, error)
}

type Service struct {
	store Store
}

func New(store Store) *Service {
	return &Service{store: store}
}

func (s *Service) Get(ctx context.Context, actorID, recordID string) (Record, error) {
	if !id.Valid(id.RecordPrefix, recordID) {
		return Record{}, &domain.BadRequestError{Message: "/recordId has an invalid typed ID"}
	}
	if s == nil || s.store == nil {
		return Record{}, domain.ErrDependencyMissing
	}
	return s.store.GetRecord(ctx, actorID, recordID)
}

func (s *Service) Mutate(ctx context.Context, actorID, tableID, clientMutationID string, commands []Command) (MutationResult, error) {
	if !id.Valid(id.TablePrefix, tableID) {
		return MutationResult{}, &domain.BadRequestError{Message: "/tableId has an invalid typed ID"}
	}
	if !id.Valid(id.MutationPrefix, clientMutationID) {
		return MutationResult{}, &domain.BadRequestError{Message: "/clientMutationId has an invalid typed ID"}
	}
	if s == nil || s.store == nil {
		return MutationResult{}, domain.ErrDependencyMissing
	}
	if err := validateCommands(commands); err != nil {
		return MutationResult{}, err
	}
	fingerprint, err := mutationFingerprint(tableID, clientMutationID, commands)
	if err != nil {
		return MutationResult{}, err
	}
	key, err := s.store.CursorKey(ctx)
	if err != nil {
		return MutationResult{}, err
	}
	stored, err := s.store.MutateRecords(ctx, actorID, tableID, clientMutationID, fingerprint, commands)
	if err != nil {
		return MutationResult{}, err
	}
	signer, err := cursor.NewSigner(key)
	if err != nil {
		return MutationResult{}, fmt.Errorf("create change cursor signer: %w", err)
	}
	changeCursor, err := signer.Encode("change", struct {
		ActorID  string `json:"actorId"`
		TableID  string `json:"tableId"`
		Sequence int64  `json:"sequence"`
	}{ActorID: actorID, TableID: tableID, Sequence: stored.ChangeSequence})
	if err != nil {
		return MutationResult{}, fmt.Errorf("encode change cursor: %w", err)
	}
	return MutationResult{
		ClientMutationID: stored.ClientMutationID,
		Results:          stored.Results,
		ChangeCursor:     changeCursor,
	}, nil
}

func validateCommands(commands []Command) error {
	if len(commands) == 0 {
		return domain.NewValidationError(domain.ValidationIssue{Path: "/commands", Code: "required", Message: "at least one command is required"})
	}
	if len(commands) > 500 {
		return domain.NewValidationError(domain.ValidationIssue{Path: "/commands", Code: "limit", Message: "at most 500 commands are allowed"})
	}
	issues := make([]domain.ValidationIssue, 0)
	targets := make(map[string]int)
	for index, command := range commands {
		path := fmt.Sprintf("/commands/%d", index)
		switch command.Kind {
		case "createRecord":
			if !command.ValuesPresent || command.Values == nil {
				issues = append(issues, domain.ValidationIssue{Path: path + "/values", Code: "required", Message: "values must be an object"})
			}
		case "updateRecord":
			issues = append(issues, validateTargetCommand(path, command)...)
			if !command.SetPresent && !command.UnsetFieldsPresent {
				issues = append(issues, domain.ValidationIssue{Path: path, Code: "required", Message: "set or unsetFieldIds is required"})
			}
			if command.SetPresent && len(command.Set) == 0 {
				issues = append(issues, domain.ValidationIssue{Path: path + "/set", Code: "required", Message: "set cannot be empty when supplied"})
			}
			if command.UnsetFieldsPresent && len(command.UnsetFieldIDs) == 0 {
				issues = append(issues, domain.ValidationIssue{Path: path + "/unsetFieldIds", Code: "required", Message: "unsetFieldIds cannot be empty when supplied"})
			}
		case "deleteRecord", "restoreRecord":
			issues = append(issues, validateTargetCommand(path, command)...)
		default:
			issues = append(issues, domain.ValidationIssue{Path: path + "/kind", Code: "format", Message: "unsupported mutation command"})
		}
		if command.RecordID != "" {
			if first, duplicate := targets[command.RecordID]; duplicate {
				issues = append(issues, domain.ValidationIssue{Path: path + "/recordId", Code: "duplicate", Message: fmt.Sprintf("Record is already targeted by command %d", first)})
			} else {
				targets[command.RecordID] = index
			}
		}
	}
	if len(issues) > 0 {
		return domain.NewValidationError(issues...)
	}
	return nil
}

func validateTargetCommand(path string, command Command) []domain.ValidationIssue {
	issues := make([]domain.ValidationIssue, 0, 2)
	if !id.Valid(id.RecordPrefix, command.RecordID) {
		issues = append(issues, domain.ValidationIssue{Path: path + "/recordId", Code: "format", Message: "recordId must be a typed Record ID"})
	}
	if command.ExpectedRevision < 1 {
		issues = append(issues, domain.ValidationIssue{Path: path + "/expectedRevision", Code: "required", Message: "expectedRevision must be at least 1"})
	}
	return issues
}

func mutationFingerprint(tableID, clientMutationID string, commands []Command) ([32]byte, error) {
	encoded, err := json.Marshal(struct {
		Method           string    `json:"method"`
		Path             string    `json:"path"`
		ClientMutationID string    `json:"clientMutationId"`
		Commands         []Command `json:"commands"`
	}{
		Method:           "POST",
		Path:             "/v1/tables/" + tableID + "/records/mutate",
		ClientMutationID: clientMutationID,
		Commands:         commands,
	})
	if err != nil {
		return [32]byte{}, fmt.Errorf("canonicalize mutation: %w", err)
	}
	return sha256.Sum256(encoded), nil
}
