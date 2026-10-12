package httpapi

import (
	"encoding/json"
	"fmt"

	"github.com/Mahjong404/LoomTable-Server/internal/domain"
	loomrecord "github.com/Mahjong404/LoomTable-Server/internal/record"
)

func decodeChoiceAssignments(raw json.RawMessage) ([]loomrecord.ChoiceAssignment, bool, error) {
	if raw == nil {
		return nil, false, nil
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil || entries == nil {
		return nil, true, validationDecodeError(domain.NewValidationError(domain.ValidationIssue{Path: "/choices", Code: "type", Message: "choices must be an array"}))
	}
	choices := make([]loomrecord.ChoiceAssignment, 0, len(entries))
	for i, entry := range entries {
		var wire struct {
			FieldID               string          `json:"fieldId"`
			ExpectedFieldRevision int64           `json:"expectedFieldRevision"`
			Options               json.RawMessage `json:"options"`
		}
		prefix := fmt.Sprintf("/choices/%d", i)
		if err := decodeStrictJSONBytes(entry, &wire); err != nil {
			return nil, true, prefixDecodeError(err, prefix)
		}
		var options []json.RawMessage
		if err := json.Unmarshal(wire.Options, &options); err != nil || options == nil {
			return nil, true, validationDecodeError(domain.NewValidationError(domain.ValidationIssue{Path: prefix + "/options", Code: "type", Message: "options must be an array"}))
		}
		choice := loomrecord.ChoiceAssignment{FieldID: wire.FieldID, ExpectedFieldRevision: wire.ExpectedFieldRevision, Options: make([]loomrecord.ChoiceOption, 0, len(options))}
		for j, rawOption := range options {
			option, err := decodeChoiceOption(rawOption)
			if err != nil {
				return nil, true, prefixDecodeError(err, fmt.Sprintf("%s/options/%d", prefix, j))
			}
			choice.Options = append(choice.Options, option)
		}
		choices = append(choices, choice)
	}
	return choices, true, nil
}

func decodeChoiceOption(raw json.RawMessage) (loomrecord.ChoiceOption, error) {
	var wire struct {
		OptionID json.RawMessage `json:"optionId"`
		Name     json.RawMessage `json:"name"`
		Color    json.RawMessage `json:"color"`
	}
	if err := decodeStrictJSONBytes(raw, &wire); err != nil {
		return loomrecord.ChoiceOption{}, err
	}
	if wire.OptionID != nil {
		if wire.Name != nil || wire.Color != nil {
			return loomrecord.ChoiceOption{}, validationDecodeError(domain.NewValidationError(domain.ValidationIssue{Path: "", Code: "format", Message: "use optionId or name/color, not both"}))
		}
		value, _, err := decodeOptionalJSONString(wire.OptionID, "/optionId")
		return loomrecord.ChoiceOption{OptionID: value}, err
	}
	if wire.Name == nil || wire.Color == nil {
		return loomrecord.ChoiceOption{}, validationDecodeError(domain.NewValidationError(domain.ValidationIssue{Path: "", Code: "required", Message: "name and color are required without optionId"}))
	}
	name, _, err := decodeOptionalJSONString(wire.Name, "/name")
	if err != nil {
		return loomrecord.ChoiceOption{}, err
	}
	color, _, err := decodeOptionalJSONString(wire.Color, "/color")
	return loomrecord.ChoiceOption{Name: name, Color: color}, err
}
