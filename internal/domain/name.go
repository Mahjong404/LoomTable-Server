package domain

import (
	"fmt"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

const ResourceNameMaxCodePoints = 200

func NormalizeResourceName(path, value string) (string, error) {
	return normalizeName(path, value, ResourceNameMaxCodePoints, "name")
}

func NormalizeOptionName(path, value string) (string, error) {
	return normalizeName(path, value, 100, "option name")
}

func NormalizeTokenName(path, value string) (string, error) {
	return normalizeName(path, value, 100, "token name")
}

func FoldKey(value string) string {
	return cases.Fold().String(norm.NFC.String(value))
}

func normalizeName(path, value string, maxCodePoints int, label string) (string, error) {
	normalized := norm.NFC.String(trimUnicodeSpace(value))
	if normalized == "" {
		return "", NewValidationError(ValidationIssue{Path: path, Code: "required", Message: label + " is required"})
	}
	for _, r := range normalized {
		if unicode.IsControl(r) {
			return "", NewValidationError(ValidationIssue{Path: path, Code: "format", Message: label + " cannot contain control characters"})
		}
	}
	if utf8.RuneCountInString(normalized) > maxCodePoints {
		return "", NewValidationError(ValidationIssue{Path: path, Code: "limit", Message: fmt.Sprintf("%s exceeds %d Unicode code points", label, maxCodePoints)})
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
