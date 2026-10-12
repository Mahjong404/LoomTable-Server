package record

import (
	"errors"
	"fmt"
	"slices"

	"github.com/Mahjong404/LoomTable-Server/internal/domain"
	"github.com/Mahjong404/LoomTable-Server/internal/id"
)

func validateChoices(commands []Command) []domain.ValidationIssue {
	issues := make([]domain.ValidationIssue, 0)
	expectedFields := make(map[string]int64)
	optionCount := 0
	for index, command := range commands {
		prefix := fmt.Sprintf("/commands/%d/choices", index)
		if command.ChoicesPresent && len(command.Choices) == 0 {
			issues = append(issues, domain.ValidationIssue{Path: prefix, Code: "required", Message: "choices cannot be empty when supplied"})
		}
		if len(command.Choices) > 0 && command.Kind != "createRecord" && command.Kind != "updateRecord" {
			issues = append(issues, domain.ValidationIssue{Path: prefix, Code: "format", Message: "choices is only valid for createRecord/updateRecord"})
		}
		if len(command.Choices) > 50 {
			issues = append(issues, domain.ValidationIssue{Path: prefix, Code: "limit", Message: "at most 50 Choice Fields per command"})
		}
		seen := make(map[string]bool)
		for i, choice := range command.Choices {
			path := fmt.Sprintf("%s/%d", prefix, i)
			if !id.Valid(id.FieldPrefix, choice.FieldID) {
				issues = append(issues, domain.ValidationIssue{Path: path + "/fieldId", Code: "format", Message: "fieldId must be a typed Field ID"})
			}
			if choice.ExpectedFieldRevision < 1 {
				issues = append(issues, domain.ValidationIssue{Path: path + "/expectedFieldRevision", Code: "required", Message: "expectedFieldRevision must be at least 1"})
			}
			if previous, ok := expectedFields[choice.FieldID]; ok && previous != choice.ExpectedFieldRevision {
				issues = append(issues, domain.ValidationIssue{Path: path + "/expectedFieldRevision", Code: "format", Message: "one Field must use the same initial revision throughout the request"})
			}
			expectedFields[choice.FieldID] = choice.ExpectedFieldRevision
			_, inValues := command.Values[choice.FieldID]
			_, inSet := command.Set[choice.FieldID]
			if seen[choice.FieldID] || inValues || inSet || slices.Contains(command.UnsetFieldIDs, choice.FieldID) {
				issues = append(issues, domain.ValidationIssue{Path: path + "/fieldId", Code: "duplicate", Message: "Choice Field is duplicated or overlaps values/set/unsetFieldIds"})
			}
			seen[choice.FieldID] = true
			if choice.Options == nil {
				issues = append(issues, domain.ValidationIssue{Path: path + "/options", Code: "type", Message: "options must be an array"})
			}
			if len(choice.Options) > 100 {
				issues = append(issues, domain.ValidationIssue{Path: path + "/options", Code: "limit", Message: "at most 100 Option inputs per Field"})
			}
			optionCount += len(choice.Options)
			for j, option := range choice.Options {
				p := fmt.Sprintf("%s/options/%d", path, j)
				if option.OptionID != "" {
					if !id.Valid(id.OptionPrefix, option.OptionID) {
						issues = append(issues, domain.ValidationIssue{Path: p + "/optionId", Code: "format", Message: "optionId must be a typed Option ID"})
					}
					if option.Name != "" || option.Color != "" {
						issues = append(issues, domain.ValidationIssue{Path: p, Code: "format", Message: "use optionId or name/color, not both"})
					}
					continue
				}
				if _, err := domain.NormalizeOptionName(p+"/name", option.Name); err != nil {
					var validation *domain.ValidationError
					if errors.As(err, &validation) {
						issues = append(issues, validation.Issues...)
					}
				}
				if !domain.ValidSelectOptionColor(option.Color) {
					issues = append(issues, domain.ValidationIssue{Path: p + "/color", Code: "format", Message: "unsupported option color"})
				}
			}
		}
	}
	if len(expectedFields) > 50 {
		issues = append(issues, domain.ValidationIssue{Path: "/commands", Code: "limit", Message: "at most 50 distinct Choice Fields per request"})
	}
	if optionCount > 500 {
		issues = append(issues, domain.ValidationIssue{Path: "/commands", Code: "limit", Message: "at most 500 Option inputs per request"})
	}
	return issues
}
