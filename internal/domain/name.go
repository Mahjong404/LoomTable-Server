package domain

import (
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

const ResourceNameMaxCodePoints = 200

func NormalizeResourceName(path, value string) (string, error) {
	normalized := norm.NFC.String(trimUnicodeSpace(value))
	if normalized == "" {
		return "", NewValidationError(ValidationIssue{Path: path, Code: "required", Message: "name is required"})
	}
	for _, r := range normalized {
		if unicode.IsControl(r) {
			return "", NewValidationError(ValidationIssue{Path: path, Code: "format", Message: "name cannot contain control characters"})
		}
	}
	if utf8.RuneCountInString(normalized) > ResourceNameMaxCodePoints {
		return "", NewValidationError(ValidationIssue{Path: path, Code: "limit", Message: "name exceeds 200 Unicode code points"})
	}
	return normalized, nil
}

func trimUnicodeSpace(value string) string {
	start := 0
	for start < len(value) {
		r, size := utf8.DecodeRuneInString(value[start:])
		if !unicode.IsSpace(r) {
			break
		}
		start += size
	}

	end := len(value)
	for end > start {
		r, size := utf8.DecodeLastRuneInString(value[:end])
		if !unicode.IsSpace(r) {
			break
		}
		end -= size
	}
	return value[start:end]
}
