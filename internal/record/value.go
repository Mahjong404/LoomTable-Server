package record

import (
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Mahjong404/LoomTable-Server/internal/domain"
	"github.com/Mahjong404/LoomTable-Server/internal/id"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

type normalizedValues struct {
	Canonical  map[string]any
	Query      map[string]any
	SearchText string
}

func NormalizeCreateValues(values map[string]any, fields map[string]FieldDefinition) (map[string]any, map[string]any, string, error) {
	return normalizeCompleteValues(values, nil, fields)
}

func NormalizeUpdatedValues(current, set map[string]any, unset []string, fields map[string]FieldDefinition) (map[string]any, map[string]any, string, error) {
	target := cloneValues(current)
	issues := make([]domain.ValidationIssue, 0)
	seenUnset := make(map[string]struct{}, len(unset))
	for index, fieldID := range unset {
		path := fmt.Sprintf("/unsetFieldIds/%d", index)
		if _, exists := seenUnset[fieldID]; exists {
			issues = append(issues, domain.ValidationIssue{Path: path, Code: "duplicate", Message: "Field ID appears more than once"})
			continue
		}
		seenUnset[fieldID] = struct{}{}
		if _, overlaps := set[fieldID]; overlaps {
			issues = append(issues, domain.ValidationIssue{Path: path, Code: "duplicate", Message: "Field ID also appears in set"})
			continue
		}
		if err := validateWritableField(path, fieldID, fields); err != nil {
			issues = append(issues, err...)
			continue
		}
		delete(target, fieldID)
	}
	if len(issues) > 0 {
		return nil, nil, "", domain.NewValidationError(issues...)
	}
	setKeys := make([]string, 0, len(set))
	for fieldID := range set {
		setKeys = append(setKeys, fieldID)
	}
	sort.Strings(setKeys)
	for _, fieldID := range setKeys {
		path := "/set/" + escapePointer(fieldID)
		definition, ok := fields[fieldID]
		if !ok || definition.DeletedAt != nil || !id.Valid(id.FieldPrefix, fieldID) {
			issues = append(issues, domain.ValidationIssue{Path: path, Code: "invalidReference", Message: "Field is unknown, belongs to another Table, or is deleted"})
			continue
		}
		normalized, fieldIssues := normalizeFieldValue(path, set[fieldID], current[fieldID], definition)
		if len(fieldIssues) > 0 {
			issues = append(issues, fieldIssues...)
			continue
		}
		target[fieldID] = normalized
	}
	if len(issues) > 0 {
		return nil, nil, "", domain.NewValidationError(issues...)
	}
	query, search := buildQueryProjection(target, fields)
	return target, query, search, nil
}

func normalizeCompleteValues(values, previous map[string]any, fields map[string]FieldDefinition) (map[string]any, map[string]any, string, error) {
	canonical := make(map[string]any, len(values))
	issues := make([]domain.ValidationIssue, 0)
	keys := make([]string, 0, len(values))
	for fieldID := range values {
		keys = append(keys, fieldID)
	}
	sort.Strings(keys)
	for _, fieldID := range keys {
		path := "/values/" + escapePointer(fieldID)
		definition, ok := fields[fieldID]
		if !ok || definition.DeletedAt != nil || !id.Valid(id.FieldPrefix, fieldID) {
			issues = append(issues, domain.ValidationIssue{Path: path, Code: "invalidReference", Message: "Field is unknown, belongs to another Table, or is deleted"})
			continue
		}
		var old any
		if previous != nil {
			old = previous[fieldID]
		}
		normalized, fieldIssues := normalizeFieldValue(path, values[fieldID], old, definition)
		if len(fieldIssues) > 0 {
			issues = append(issues, fieldIssues...)
			continue
		}
		canonical[fieldID] = normalized
	}
	if len(issues) > 0 {
		return nil, nil, "", domain.NewValidationError(issues...)
	}
	query, search := buildQueryProjection(canonical, fields)
	return canonical, query, search, nil
}

func validateWritableField(path, fieldID string, fields map[string]FieldDefinition) []domain.ValidationIssue {
	definition, ok := fields[fieldID]
	if !id.Valid(id.FieldPrefix, fieldID) || !ok || definition.DeletedAt != nil {
		return []domain.ValidationIssue{{Path: path, Code: "invalidReference", Message: "Field is unknown, belongs to another Table, or is deleted"}}
	}
	return nil
}

func normalizeFieldValue(path string, value, previous any, field FieldDefinition) (any, []domain.ValidationIssue) {
	if value == nil {
		return nil, nil
	}
	switch field.Type {
	case "text", "longText":
		text, ok := value.(string)
		if !ok {
			return nil, typeIssue(path, "value must be a string or null")
		}
		limit := 10000
		if field.Type == "longText" {
			limit = 100000
		}
		if utf8.RuneCountInString(text) > limit {
			return nil, limitIssue(path, fmt.Sprintf("value exceeds %d Unicode code points", limit))
		}
		return text, nil
	case "number":
		number, ok := value.(float64)
		if !ok || math.IsNaN(number) || math.IsInf(number, 0) {
			return nil, typeIssue(path, "value must be a finite number or null")
		}
		return number, nil
	case "checkbox":
		checked, ok := value.(bool)
		if !ok {
			return nil, typeIssue(path, "value must be a boolean or null")
		}
		return checked, nil
	case "date":
		date, ok := value.(string)
		if !ok || !validDate(date) {
			return nil, formatIssue(path, "value must be a valid Gregorian YYYY-MM-DD date")
		}
		return date, nil
	case "url":
		address, ok := value.(string)
		if !ok {
			return nil, typeIssue(path, "value must be an absolute HTTP/HTTPS URL or null")
		}
		if address == "" || utf8.RuneCountInString(address) > 2048 {
			return nil, formatIssue(path, "value must be a non-empty URL no longer than 2,048 characters")
		}
		parsed, err := url.Parse(address)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return nil, formatIssue(path, "value must be an absolute HTTP/HTTPS URL")
		}
		return address, nil
	case "select":
		optionID, ok := value.(string)
		if !ok || !id.Valid(id.OptionPrefix, optionID) {
			return nil, typeIssue(path, "value must be an Option ID or null")
		}
		active, deleted, err := selectOptions(field.Config)
		if err != nil {
			return nil, formatIssue(path, "Field configuration is invalid")
		}
		if _, ok := active[optionID]; ok {
			return optionID, nil
		}
		if previousID, ok := previous.(string); ok && previousID == optionID {
			if _, retained := deleted[optionID]; retained {
				return optionID, nil
			}
		}
		return nil, invalidReferenceIssue(path, "Option is unknown, foreign, or deleted")
	case "multiSelect":
		entries, ok := value.([]any)
		if !ok {
			return nil, typeIssue(path, "value must be an array of Option IDs or null")
		}
		if len(entries) > 100 {
			return nil, limitIssue(path, "value cannot contain more than 100 Option IDs")
		}
		active, deleted, err := selectOptions(field.Config)
		if err != nil {
			return nil, formatIssue(path, "Field configuration is invalid")
		}
		previousIDs := stringSet(previous)
		seen := make(map[string]struct{}, len(entries))
		result := make([]any, 0, len(entries))
		issues := make([]domain.ValidationIssue, 0)
		for index, entry := range entries {
			entryPath := fmt.Sprintf("%s/%d", path, index)
			optionID, ok := entry.(string)
			if !ok || !id.Valid(id.OptionPrefix, optionID) {
				issues = append(issues, typeIssue(entryPath, "item must be an Option ID")...)
				continue
			}
			if _, duplicate := seen[optionID]; duplicate {
				issues = append(issues, domain.ValidationIssue{Path: entryPath, Code: "duplicate", Message: "Option ID appears more than once"})
				continue
			}
			seen[optionID] = struct{}{}
			_, isActive := active[optionID]
			_, isDeleted := deleted[optionID]
			_, wasPresent := previousIDs[optionID]
			if !isActive && !(isDeleted && wasPresent) {
				issues = append(issues, invalidReferenceIssue(entryPath, "Option is unknown, foreign, or deleted")...)
				continue
			}
			result = append(result, optionID)
		}
		if len(issues) > 0 {
			return nil, issues
		}
		return result, nil
	case "attachment":
		return normalizeAttachment(path, value, field.Config)
	case "location":
		return normalizeLocation(path, value)
	default:
		return nil, invalidReferenceIssue(path, "Field Type is unsupported")
	}
}

func normalizeAttachment(path string, value any, rawConfig json.RawMessage) (any, []domain.ValidationIssue) {
	entries, ok := value.([]any)
	if !ok {
		return nil, typeIssue(path, "value must be an array of AttachmentRef objects or null")
	}
	maxCount := 10
	var config domain.AttachmentFieldConfig
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, formatIssue(path, "Field configuration is invalid")
	}
	if config.MaxCount != 0 {
		maxCount = config.MaxCount
	}
	if maxCount < 1 || maxCount > 100 {
		return nil, formatIssue(path, "Field configuration is invalid")
	}
	if len(entries) > maxCount {
		return nil, limitIssue(path, fmt.Sprintf("value cannot contain more than %d Attachments", maxCount))
	}
	seen := make(map[string]struct{}, len(entries))
	result := make([]any, 0, len(entries))
	issues := make([]domain.ValidationIssue, 0)
	for index, entry := range entries {
		entryPath := fmt.Sprintf("%s/%d", path, index)
		ref, refIssues := normalizeAttachmentRef(entryPath, entry)
		issues = append(issues, refIssues...)
		if ref == nil {
			continue
		}
		attachmentID, hasID := ref["id"].(string)
		if !hasID {
			continue
		}
		if _, duplicate := seen[attachmentID]; duplicate {
			issues = append(issues, domain.ValidationIssue{Path: entryPath + "/id", Code: "duplicate", Message: "Attachment ID appears more than once"})
			continue
		}
		seen[attachmentID] = struct{}{}
		result = append(result, ref)
	}
	if len(issues) > 0 {
		return nil, issues
	}
	return result, nil
}

func normalizeAttachmentRef(path string, value any) (map[string]any, []domain.ValidationIssue) {
	input, ok := value.(map[string]any)
	if !ok {
		return nil, typeIssue(path, "AttachmentRef must be an object")
	}
	allowed := map[string]bool{
		"id": true, "source": true, "filename": true, "mimeType": true,
		"size": true, "storageKey": true, "vaultPath": true, "hash": true,
		"width": true, "height": true,
	}
	issues := make([]domain.ValidationIssue, 0)
	for key := range input {
		if !allowed[key] {
			issues = append(issues, domain.ValidationIssue{Path: path + "/" + escapePointer(key), Code: "unknownProperty", Message: "unknown AttachmentRef property"})
		}
	}
	result := make(map[string]any, len(input))
	attachmentID, ok := input["id"].(string)
	if !ok || !id.Valid(id.AttachmentPrefix, attachmentID) {
		issues = append(issues, typeIssue(path+"/id", "id must be an Attachment ID")...)
	} else {
		result["id"] = attachmentID
	}
	source, ok := input["source"].(string)
	if !ok || (source != "managed" && source != "vault") {
		issues = append(issues, formatIssue(path+"/source", "source must be managed or vault")...)
	} else {
		result["source"] = source
	}
	filename, ok := input["filename"].(string)
	if !ok {
		issues = append(issues, typeIssue(path+"/filename", "filename must be a string")...)
	} else {
		filename = norm.NFC.String(strings.TrimFunc(filename, unicode.IsSpace))
		if filename == "" || utf8.RuneCountInString(filename) > 255 || containsControl(filename) || strings.ContainsAny(filename, "/\\") {
			issues = append(issues, formatIssue(path+"/filename", "filename must be a safe non-empty file name")...)
		} else {
			result["filename"] = filename
		}
	}
	for _, key := range []string{"mimeType", "storageKey", "vaultPath", "hash"} {
		if current, present := input[key]; present {
			text, textOK := current.(string)
			if !textOK {
				issues = append(issues, typeIssue(path+"/"+key, "property must be a string")...)
				continue
			}
			result[key] = strings.TrimSpace(text)
		}
	}
	if hash, present := result["hash"]; present && hash.(string) != "" && !isSHA256(hash.(string)) {
		issues = append(issues, formatIssue(path+"/hash", "hash must be a lowercase SHA-256 hexadecimal digest")...)
	}
	for _, key := range []string{"size", "width", "height"} {
		if current, present := input[key]; present {
			number, numberOK := current.(float64)
			if !numberOK || math.IsNaN(number) || math.IsInf(number, 0) || number < 0 || number != math.Trunc(number) || number > math.MaxInt64 {
				issues = append(issues, formatIssue(path+"/"+key, "property must be a non-negative integer")...)
				continue
			}
			if (key == "width" || key == "height") && number == 0 {
				issues = append(issues, formatIssue(path+"/"+key, "dimension must be positive")...)
				continue
			}
			result[key] = number
		}
	}
	return result, issues
}

func isSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, current := range value {
		if !(current >= '0' && current <= '9') && !(current >= 'a' && current <= 'f') {
			return false
		}
	}
	return true
}

func normalizeLocation(path string, value any) (any, []domain.ValidationIssue) {
	input, ok := value.(map[string]any)
	if !ok {
		return nil, typeIssue(path, "value must be a Location object or null")
	}
	allowed := map[string]bool{"label": true, "address": true, "lat": true, "lng": true, "provider": true, "precision": true}
	result := make(map[string]any, len(input))
	issues := make([]domain.ValidationIssue, 0)
	for key := range input {
		if !allowed[key] {
			issues = append(issues, domain.ValidationIssue{Path: path + "/" + escapePointer(key), Code: "unknownProperty", Message: "unknown Location property"})
		}
	}
	limits := map[string]int{"label": 500, "address": 2000, "provider": 100}
	for _, key := range []string{"label", "address", "provider"} {
		value, present := input[key]
		if !present {
			continue
		}
		text, ok := value.(string)
		if !ok {
			issues = append(issues, typeIssue(path+"/"+key, "property must be a string")...)
			continue
		}
		normalized := norm.NFC.String(strings.TrimFunc(text, unicode.IsSpace))
		if normalized == "" {
			continue
		}
		if containsControl(normalized) {
			issues = append(issues, formatIssue(path+"/"+key, "property cannot contain control characters")...)
			continue
		}
		if utf8.RuneCountInString(normalized) > limits[key] {
			issues = append(issues, limitIssue(path+"/"+key, "property exceeds its Unicode code point limit")...)
			continue
		}
		result[key] = normalized
	}

	lat, latPresent := input["lat"]
	lng, lngPresent := input["lng"]
	if latPresent != lngPresent {
		missing := "lat"
		if latPresent {
			missing = "lng"
		}
		issues = append(issues, domain.ValidationIssue{Path: path + "/" + missing, Code: "required", Message: "lat and lng must appear together"})
	} else if latPresent {
		latitude, latOK := lat.(float64)
		longitude, lngOK := lng.(float64)
		if !latOK || math.IsNaN(latitude) || math.IsInf(latitude, 0) || latitude < -90 || latitude > 90 {
			issues = append(issues, formatIssue(path+"/lat", "lat must be a finite number from -90 to 90")...)
		} else {
			result["lat"] = latitude
		}
		if !lngOK || math.IsNaN(longitude) || math.IsInf(longitude, 0) || longitude < -180 || longitude > 180 {
			issues = append(issues, formatIssue(path+"/lng", "lng must be a finite number from -180 to 180")...)
		} else {
			result["lng"] = longitude
		}
	}

	if precision, present := input["precision"]; present {
		value, ok := precision.(string)
		if !ok || (value != "exact" && value != "rooftop" && value != "approximate") {
			issues = append(issues, formatIssue(path+"/precision", "precision must be exact, rooftop, or approximate")...)
		} else {
			result["precision"] = value
		}
	}
	if len(issues) == 0 {
		_, hasLabel := result["label"]
		_, hasAddress := result["address"]
		_, hasProvider := result["provider"]
		_, hasLatitude := result["lat"]
		if !hasLabel && !hasAddress && !hasProvider && !hasLatitude {
			issues = append(issues, domain.ValidationIssue{Path: path, Code: "required", Message: "Location must contain text or a complete coordinate pair"})
		}
	}
	if len(issues) > 0 {
		return nil, issues
	}
	return result, nil
}

func buildQueryProjection(values map[string]any, fields map[string]FieldDefinition) (map[string]any, string) {
	projection := make(map[string]any, len(values))
	searchParts := make([]string, 0)
	keys := make([]string, 0, len(values))
	for fieldID := range values {
		keys = append(keys, fieldID)
	}
	sort.Strings(keys)
	for _, fieldID := range keys {
		value := values[fieldID]
		field, exists := fields[fieldID]
		if !exists || field.DeletedAt != nil {
			continue
		}
		if value == nil {
			projection[fieldID] = nil
			continue
		}
		switch field.Type {
		case "text", "longText", "url":
			folded := foldText(value.(string))
			projection[fieldID] = folded
			searchParts = append(searchParts, folded)
		case "location":
			location := value.(map[string]any)
			queryLocation := make(map[string]any, len(location))
			for key, current := range location {
				if text, ok := current.(string); ok && key != "precision" {
					queryLocation[key] = foldText(text)
				} else {
					queryLocation[key] = current
				}
			}
			projection[fieldID] = queryLocation
		default:
			projection[fieldID] = value
		}
	}
	return projection, strings.Join(searchParts, "\n")
}

func selectOptions(raw json.RawMessage) (map[string]struct{}, map[string]struct{}, error) {
	var config struct {
		Options []struct {
			ID string `json:"id"`
		} `json:"options"`
		DeletedOptions []struct {
			ID string `json:"id"`
		} `json:"deletedOptions"`
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		return nil, nil, err
	}
	active := make(map[string]struct{}, len(config.Options))
	deleted := make(map[string]struct{}, len(config.DeletedOptions))
	for _, option := range config.Options {
		active[option.ID] = struct{}{}
	}
	for _, option := range config.DeletedOptions {
		deleted[option.ID] = struct{}{}
	}
	return active, deleted, nil
}

func stringSet(value any) map[string]struct{} {
	result := make(map[string]struct{})
	switch values := value.(type) {
	case []any:
		for _, current := range values {
			if text, ok := current.(string); ok {
				result[text] = struct{}{}
			}
		}
	case []string:
		for _, current := range values {
			result[current] = struct{}{}
		}
	}
	return result
}

func cloneValues(values map[string]any) map[string]any {
	result := make(map[string]any, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}

func validDate(value string) bool {
	parsed, err := time.Parse("2006-01-02", value)
	return err == nil && parsed.Format("2006-01-02") == value
}

func containsControl(value string) bool {
	for _, current := range value {
		if unicode.IsControl(current) {
			return true
		}
	}
	return false
}

func foldText(value string) string {
	return cases.Fold().String(norm.NFC.String(value))
}

func escapePointer(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "~", "~0"), "/", "~1")
}

func typeIssue(path, message string) []domain.ValidationIssue {
	return []domain.ValidationIssue{{Path: path, Code: "type", Message: message}}
}

func formatIssue(path, message string) []domain.ValidationIssue {
	return []domain.ValidationIssue{{Path: path, Code: "format", Message: message}}
}

func limitIssue(path, message string) []domain.ValidationIssue {
	return []domain.ValidationIssue{{Path: path, Code: "limit", Message: message}}
}

func invalidReferenceIssue(path, message string) []domain.ValidationIssue {
	return []domain.ValidationIssue{{Path: path, Code: "invalidReference", Message: message}}
}

