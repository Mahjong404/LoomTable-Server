package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sort"

	"github.com/Mahjong404/LoomTable-Server/internal/domain"
	"github.com/Mahjong404/LoomTable-Server/internal/id"
	loomrecord "github.com/Mahjong404/LoomTable-Server/internal/record"
)

func prepareChoiceMutation(ctx context.Context, tx *sql.Tx, actorID, tableID, mutationID string, input []loomrecord.Command) ([]loomrecord.Command, map[string]loomrecord.FieldDefinition, []loomrecord.ChoiceFieldResult, error) {
	fieldIDs := make([]string, 0)
	expected := make(map[string]int64)
	firstCommand := make(map[string]int)
	for index, command := range input {
		for _, choice := range command.Choices {
			if _, seen := expected[choice.FieldID]; !seen {
				fieldIDs = append(fieldIDs, choice.FieldID)
				expected[choice.FieldID] = choice.ExpectedFieldRevision
				firstCommand[choice.FieldID] = index
			}
		}
	}
	sort.Strings(fieldIDs)
	locked := make(map[string]domain.Field)
	configs := make(map[string]domain.SelectFieldConfig)
	for _, fieldID := range fieldIDs {
		field, err := scanField(tx.QueryRowContext(ctx, `
			SELECT id, table_id, name, position_index, schema_version, revision, type, config, deleted_at, description
			FROM fields WHERE id = $1 AND table_id = $2 FOR UPDATE
		`, fieldID, tableID))
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, nil, domain.ErrNotFound
		}
		if err != nil {
			return nil, nil, nil, fmt.Errorf("lock Choice Field: %w", err)
		}
		if field.Revision != expected[fieldID] {
			return nil, nil, nil, &loomrecord.ConflictError{ClientMutationID: mutationID, FailedCommandIndex: firstCommand[fieldID], Conflicts: []loomrecord.Conflict{}, FieldConflicts: []loomrecord.FieldConflict{{FieldID: fieldID, ExpectedRevision: expected[fieldID], CurrentRevision: field.Revision}}}
		}
		if field.DeletedAt != nil {
			return nil, nil, nil, &domain.InvalidStateTransitionError{Resource: "field", ID: fieldID, Action: "assign choice", Current: "deleted"}
		}
		config, ok := field.Config.(domain.SelectFieldConfig)
		if !ok {
			return nil, nil, nil, domain.NewValidationError(domain.ValidationIssue{Path: fmt.Sprintf("/commands/%d/choices", firstCommand[fieldID]), Code: "type", Message: "choices requires Select or MultiSelect Fields"})
		}
		locked[fieldID], configs[fieldID] = field, config
	}
	fields, err := loadFieldDefinitions(ctx, tx, tableID)
	if err != nil {
		return nil, nil, nil, err
	}
	// Lock targets before resolving any Option, and preserve normal Record CAS.
	targetIDs := make([]string, 0)
	for _, command := range input {
		if command.RecordID != "" {
			targetIDs = append(targetIDs, command.RecordID)
		}
	}
	sort.Strings(targetIDs)
	targets := make(map[string]loomrecord.Record)
	for _, recordID := range targetIDs {
		current, err := lockRecord(ctx, tx, tableID, recordID)
		if err != nil {
			return nil, nil, nil, err
		}
		targets[recordID] = current
	}
	for index, command := range input {
		if command.RecordID == "" {
			continue
		}
		current := targets[command.RecordID]
		if current.Revision != command.ExpectedRevision {
			return nil, nil, nil, recordConflict(mutationID, index, command, current)
		}
		if command.Kind == "updateRecord" && current.DeletedAt != nil {
			return nil, nil, nil, &domain.InvalidStateTransitionError{Resource: "record", ID: current.ID, Action: "update", Current: "deleted"}
		}
	}
	deltas := make(map[string]*loomrecord.ChoiceFieldResult)
	for _, fieldID := range fieldIDs {
		field := locked[fieldID]
		deltas[fieldID] = &loomrecord.ChoiceFieldResult{FieldID: fieldID, BeforeRevision: field.Revision, Revision: field.Revision, ResolvedOptions: []domain.SelectOption{}, CreatedOptions: []domain.SelectOption{}}
	}
	commands := append([]loomrecord.Command(nil), input...)
	for index, command := range input {
		for j, choice := range command.Choices {
			p := fmt.Sprintf("/commands/%d/choices/%d", index, j)
			field := locked[choice.FieldID]
			if field.Type == "select" && len(choice.Options) != 1 {
				return nil, nil, nil, domain.NewValidationError(domain.ValidationIssue{Path: p + "/options", Code: "limit", Message: "Select requires exactly one Option"})
			}
			config := configs[choice.FieldID]
			selected := make([]any, 0, len(choice.Options))
			seen := make(map[string]bool)
			for k, option := range choice.Options {
				resolved, created, err := resolveChoiceOption(&config, option, targets[command.RecordID].Values[choice.FieldID], fmt.Sprintf("%s/options/%d", p, k))
				if err != nil {
					return nil, nil, nil, err
				}
				if seen[resolved.ID] {
					return nil, nil, nil, domain.NewValidationError(domain.ValidationIssue{Path: fmt.Sprintf("%s/options/%d", p, k), Code: "duplicate", Message: "Option resolves to a repeated identity"})
				}
				seen[resolved.ID] = true
				selected = append(selected, resolved.ID)
				delta := deltas[choice.FieldID]
				if !slices.ContainsFunc(delta.ResolvedOptions, func(o domain.SelectOption) bool { return o.ID == resolved.ID }) {
					delta.ResolvedOptions = append(delta.ResolvedOptions, resolved)
				}
				if created {
					delta.CreatedOptions = append(delta.CreatedOptions, resolved)
				}
			}
			configs[choice.FieldID] = config
			var value any = selected
			if field.Type == "select" {
				value = selected[0]
			}
			if command.Kind == "createRecord" {
				// Never rewrite the caller's immutable request maps.
				if j == 0 {
					commands[index].Values = maps.Clone(command.Values)
				}
				commands[index].Values[choice.FieldID] = value
			} else {
				if j == 0 {
					commands[index].Set = maps.Clone(command.Set)
					if commands[index].Set == nil {
						commands[index].Set = make(map[string]any)
					}
				}
				commands[index].Set[choice.FieldID] = value
			}
		}
	}
	result := make([]loomrecord.ChoiceFieldResult, 0, len(fieldIDs))
	for _, fieldID := range fieldIDs {
		delta := deltas[fieldID]
		encoded, err := json.Marshal(configs[fieldID])
		if err != nil {
			return nil, nil, nil, fmt.Errorf("encode Choice config: %w", err)
		}
		if len(delta.CreatedOptions) > 0 {
			if _, err := tx.ExecContext(ctx, `UPDATE fields SET config=$1::jsonb, revision=revision+1, updated_at=clock_timestamp() WHERE id=$2`, string(encoded), fieldID); err != nil {
				return nil, nil, nil, fmt.Errorf("save Choice Options: %w", err)
			}
			delta.Revision++
			if err := insertMetadataChange(ctx, tx, actorID, "schemaChanged", tableID, fieldID, delta.Revision); err != nil {
				return nil, nil, nil, err
			}
		}
		definition := fields[fieldID]
		definition.Config = encoded
		definition.Revision = delta.Revision
		fields[fieldID] = definition
		result = append(result, *delta)
	}
	return commands, fields, result, nil
}

func resolveChoiceOption(config *domain.SelectFieldConfig, input loomrecord.ChoiceOption, previous any, path string) (domain.SelectOption, bool, error) {
	if input.OptionID != "" {
		for _, option := range config.Options {
			if option.ID == input.OptionID {
				return option, false, nil
			}
		}
		retained := previous == input.OptionID
		if entries, ok := previous.([]any); ok {
			for _, entry := range entries {
				if entry == input.OptionID {
					retained = true
				}
			}
		}
		if retained {
			for _, option := range config.DeletedOptions {
				if option.ID == input.OptionID {
					return domain.SelectOption{ID: option.ID, Name: option.Name, Color: option.Color}, false, nil
				}
			}
		}
		return domain.SelectOption{}, false, domain.NewValidationError(domain.ValidationIssue{Path: path + "/optionId", Code: "invalidReference", Message: "Option is unknown, foreign, or a new deleted reference"})
	}
	name, err := domain.NormalizeOptionName(path+"/name", input.Name)
	if err != nil {
		return domain.SelectOption{}, false, err
	}
	key := domain.FoldKey(name)
	for _, option := range config.Options {
		if domain.FoldKey(option.Name) == key {
			return option, false, nil
		}
	}
	if len(config.Options) >= 500 || len(config.Options)+len(config.DeletedOptions) >= 5000 {
		return domain.SelectOption{}, false, domain.NewValidationError(domain.ValidationIssue{Path: path, Code: "limit", Message: "Field Option budget exceeded"})
	}
	optionID, err := id.New(id.OptionPrefix)
	if err != nil {
		return domain.SelectOption{}, false, fmt.Errorf("generate Choice Option ID: %w", err)
	}
	option := domain.SelectOption{ID: optionID, Name: name, Color: input.Color}
	config.Options = append(config.Options, option)
	return option, true, nil
}
