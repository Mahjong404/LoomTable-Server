package record

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Mahjong404/LoomTable-Server/internal/domain"
)

type choiceCaptureStore struct {
	serviceStore
	fingerprint [32]byte
	commands    []Command
	calls       int
}

func (s *choiceCaptureStore) MutateRecords(_ context.Context, _, _, _ string, fingerprint [32]byte, commands []Command) (StoredMutationResult, error) {
	s.calls++
	s.fingerprint = fingerprint
	s.commands = commands
	return s.stored, nil
}

func TestAtomicChoiceServiceAcceptsChoiceOnlyUpdateAndReturnsReceiptDelta(t *testing.T) {
	fieldID := "fld_00000000000000000000000000"
	option := domain.SelectOption{ID: "opt_00000000000000000000000000", Name: "Ready", Color: "lime"}
	store := &serviceStore{stored: StoredMutationResult{
		ClientMutationID: "mut_00000000000000000000000000",
		Results:          []CommandResult{},
		ChoiceFields:     []ChoiceFieldResult{{FieldID: fieldID, BeforeRevision: 1, Revision: 2, ResolvedOptions: []domain.SelectOption{option}, CreatedOptions: []domain.SelectOption{option}}},
	}}
	result, err := New(store).Mutate(context.Background(), "act_00000000000000000000000000", "tbl_00000000000000000000000000", store.stored.ClientMutationID, []Command{{
		Kind: "updateRecord", RecordID: "rec_00000000000000000000000000", ExpectedRevision: 4,
		Choices: []ChoiceAssignment{{FieldID: fieldID, ExpectedFieldRevision: 1, Options: []ChoiceOption{{Name: "  Ready  ", Color: "lime"}}}}, ChoicesPresent: true,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ChoiceFields) != 1 || result.ChoiceFields[0].CreatedOptions[0] != option {
		t.Fatalf("receipt delta = %#v", result.ChoiceFields)
	}
}

func TestAtomicChoiceValidationAndImmutableFingerprint(t *testing.T) {
	field := "fld_00000000000000000000000000"
	valid := Command{Kind: "updateRecord", RecordID: "rec_00000000000000000000000000", ExpectedRevision: 1, Choices: []ChoiceAssignment{{FieldID: field, ExpectedFieldRevision: 1, Options: []ChoiceOption{{Name: "  Café  ", Color: "brown"}}}}, ChoicesPresent: true}
	for _, tc := range []struct {
		name  string
		alter func(*Command)
	}{
		{"empty choices", func(c *Command) { c.Choices = nil }},
		{"wrong Field ID", func(c *Command) { c.Choices[0].FieldID = "bad" }},
		{"missing Field CAS", func(c *Command) { c.Choices[0].ExpectedFieldRevision = 0 }},
		{"missing array", func(c *Command) { c.Choices[0].Options = nil }},
		{"too many entries", func(c *Command) { c.Choices[0].Options = make([]ChoiceOption, 101) }},
		{"overlap set", func(c *Command) { c.Set = map[string]any{field: nil}; c.SetPresent = true }},
		{"overlap unset", func(c *Command) { c.UnsetFieldIDs = []string{field}; c.UnsetFieldsPresent = true }},
		{"duplicate Field", func(c *Command) { c.Choices = append(c.Choices, c.Choices[0]) }},
		{"invalid name", func(c *Command) { c.Choices[0].Options[0].Name = "\n" }},
		{"control", func(c *Command) { c.Choices[0].Options[0].Name = "a\nb" }},
		{"long name", func(c *Command) { c.Choices[0].Options[0].Name = strings.Repeat("界", 101) }},
		{"unknown color", func(c *Command) { c.Choices[0].Options[0].Color = "chartreuse" }},
		{"mixed option", func(c *Command) { c.Choices[0].Options[0].OptionID = "opt_00000000000000000000000000" }},
		{"bad option ID", func(c *Command) { c.Choices[0].Options[0] = ChoiceOption{OptionID: "bad"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := valid
			c.Choices = append([]ChoiceAssignment(nil), valid.Choices...)
			c.Choices[0].Options = append([]ChoiceOption(nil), valid.Choices[0].Options...)
			tc.alter(&c)
			store := &choiceCaptureStore{}
			_, err := New(store).Mutate(context.Background(), "act_00000000000000000000000000", "tbl_00000000000000000000000000", "mut_00000000000000000000000000", []Command{c})
			var validation *domain.ValidationError
			if !errors.As(err, &validation) || store.calls != 0 {
				t.Fatalf("invalid Choice = %v; calls=%d", err, store.calls)
			}
		})
	}
	store := &choiceCaptureStore{}
	commands := []Command{{Kind: "updateRecord", RecordID: valid.RecordID, ExpectedRevision: 1, Set: map[string]any{field: "Open"}, SetPresent: true}}
	_, err := New(store).Mutate(context.Background(), "act_00000000000000000000000000", "tbl_00000000000000000000000000", "mut_00000000000000000000000000", commands)
	if err != nil {
		t.Fatal(err)
	}
	oldWire := fmt.Sprintf(`{"method":"POST","path":"/v1/tables/tbl_00000000000000000000000000/records/mutate","clientMutationId":"mut_00000000000000000000000000","commands":[{"kind":"updateRecord","recordId":"%s","expectedRevision":1,"set":{"%s":"Open"}}]}`, valid.RecordID, field)
	if store.fingerprint != sha256.Sum256([]byte(oldWire)) {
		t.Fatal("legacy request fingerprint changed")
	}
	_, err = New(store).Mutate(context.Background(), "act_00000000000000000000000000", "tbl_00000000000000000000000000", "mut_00000000000000000000000000", []Command{valid})
	if err != nil {
		t.Fatal(err)
	}
	firstHash := store.fingerprint
	if store.commands[0].Choices[0].Options[0].Name != "  Café  " {
		t.Fatal("canonicalization rewrote caller payload")
	}
	valid.Choices[0].Options[0].Name = "Café"
	_, err = New(store).Mutate(context.Background(), "act_00000000000000000000000000", "tbl_00000000000000000000000000", "mut_00000000000000000000000000", []Command{valid})
	if err != nil {
		t.Fatal(err)
	}
	if store.fingerprint == firstHash {
		t.Fatal("distinct original payloads shared a fingerprint")
	}
}

func TestAtomicChoiceRequestBudgetsAndConsistentFieldCAS(t *testing.T) {
	for _, tc := range []struct {
		name    string
		choices []ChoiceAssignment
	}{
		{"inconsistent initial revision", []ChoiceAssignment{{FieldID: "fld_00000000000000000000000000", ExpectedFieldRevision: 1, Options: []ChoiceOption{}}, {FieldID: "fld_00000000000000000000000000", ExpectedFieldRevision: 2, Options: []ChoiceOption{}}}},
		{"51 distinct Fields", func() []ChoiceAssignment {
			out := []ChoiceAssignment{}
			for i := 0; i < 51; i++ {
				out = append(out, ChoiceAssignment{FieldID: fmt.Sprintf("fld_%026d", i), ExpectedFieldRevision: 1, Options: []ChoiceOption{}})
			}
			return out
		}()},
		{"600 option inputs", func() []ChoiceAssignment {
			out := []ChoiceAssignment{}
			for i := 0; i < 6; i++ {
				opts := []ChoiceOption{}
				for j := 0; j < 100; j++ {
					opts = append(opts, ChoiceOption{Name: fmt.Sprintf("N %d", j), Color: "gray"})
				}
				out = append(out, ChoiceAssignment{FieldID: fmt.Sprintf("fld_%026d", i), ExpectedFieldRevision: 1, Options: opts})
			}
			return out
		}()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &choiceCaptureStore{}
			commands := []Command{}
			for _, c := range tc.choices {
				commands = append(commands, Command{Kind: "createRecord", ValuesPresent: true, Values: map[string]any{}, Choices: []ChoiceAssignment{c}, ChoicesPresent: true})
			}
			_, err := New(store).Mutate(context.Background(), "act_00000000000000000000000000", "tbl_00000000000000000000000000", "mut_00000000000000000000000000", commands)
			var v *domain.ValidationError
			if !errors.As(err, &v) || store.calls != 0 {
				t.Fatalf("budget=%v calls=%d", err, store.calls)
			}
		})
	}
}
