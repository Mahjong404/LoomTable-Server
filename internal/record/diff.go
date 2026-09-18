package record

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
)

func DiffRecordValues(before, after map[string]any) ([]FieldChange, error) {
	if len(before) == 0 && len(after) == 0 {
		return nil, nil
	}
	keys := make(map[string]struct{}, len(before)+len(after))
	for key := range before {
		keys[key] = struct{}{}
	}
	for key := range after {
		keys[key] = struct{}{}
	}
	sorted := make([]string, 0, len(keys))
	for key := range keys {
		sorted = append(sorted, key)
	}
	sort.Strings(sorted)
	var changes []FieldChange
	for _, key := range sorted {
		beforeValue, hadBefore := before[key]
		afterValue, hasAfter := after[key]
		if hadBefore == hasAfter && reflect.DeepEqual(beforeValue, afterValue) {
			continue
		}
		change := FieldChange{FieldID: key}
		if hadBefore {
			raw, err := json.Marshal(beforeValue)
			if err != nil {
				return nil, fmt.Errorf("encode previous value for %s: %w", key, err)
			}
			change.Before = raw
		}
		if hasAfter {
			raw, err := json.Marshal(afterValue)
			if err != nil {
				return nil, fmt.Errorf("encode new value for %s: %w", key, err)
			}
			change.After = raw
		}
		changes = append(changes, change)
	}
	return changes, nil
}
