package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Mahjong404/LoomTable-Server/internal/domain"
)

const maxJSONBodyBytes = 8 * 1024 * 1024

type requestDecodeError struct {
	Status  int
	Code    string
	Message string
	Details any
}

func (e *requestDecodeError) Error() string {
	return e.Message
}

func decodeJSONRequest(r *http.Request, destination any) error {
	mediaType, parameters, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(mediaType, "application/json") {
		return unsupportedMediaType("Content-Type must be application/json")
	}
	for name, value := range parameters {
		if !strings.EqualFold(name, "charset") || !strings.EqualFold(value, "utf-8") {
			return unsupportedMediaType("application/json only accepts an optional UTF-8 charset")
		}
	}
	encoding := strings.TrimSpace(r.Header.Get("Content-Encoding"))
	if encoding != "" && !strings.EqualFold(encoding, "identity") {
		return unsupportedMediaType("Content-Encoding must be identity")
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxJSONBodyBytes+1))
	if err != nil {
		return &requestDecodeError{Status: http.StatusBadRequest, Code: "BAD_REQUEST", Message: "request body could not be read"}
	}
	if len(body) > maxJSONBodyBytes {
		return &requestDecodeError{
			Status:  http.StatusRequestEntityTooLarge,
			Code:    "PAYLOAD_TOO_LARGE",
			Message: "JSON request body exceeds the configured limit",
			Details: map[string]int{"limitBytes": maxJSONBodyBytes},
		}
	}
	if !utf8.Valid(body) {
		return unsupportedMediaType("JSON request body must be UTF-8")
	}
	if err := validateSingleJSONValue(body); err != nil {
		var validation *domain.ValidationError
		if errors.As(err, &validation) {
			return &requestDecodeError{
				Status:  http.StatusUnprocessableEntity,
				Code:    "VALIDATION_ERROR",
				Message: "request validation failed",
				Details: map[string]any{"issues": validation.Issues},
			}
		}
		return &requestDecodeError{Status: http.StatusBadRequest, Code: "BAD_REQUEST", Message: "request body must contain exactly one valid JSON value"}
	}

	return decodeStrictJSONBytes(body, destination)
}

func decodeStrictJSONBytes(body []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		if field, ok := unknownJSONField(err); ok {
			validation := domain.NewValidationError(domain.ValidationIssue{Path: "/" + escapeJSONPointer(field), Code: "unknownProperty", Message: "unknown property"})
			return &requestDecodeError{Status: http.StatusUnprocessableEntity, Code: "VALIDATION_ERROR", Message: validation.Error(), Details: map[string]any{"issues": validation.Issues}}
		}
		var typeError *json.UnmarshalTypeError
		if errors.As(err, &typeError) {
			path := "/" + strings.ReplaceAll(typeError.Field, ".", "/")
			validation := domain.NewValidationError(domain.ValidationIssue{Path: path, Code: "type", Message: "property has the wrong JSON type"})
			return &requestDecodeError{Status: http.StatusUnprocessableEntity, Code: "VALIDATION_ERROR", Message: validation.Error(), Details: map[string]any{"issues": validation.Issues}}
		}
		return &requestDecodeError{Status: http.StatusBadRequest, Code: "BAD_REQUEST", Message: "request body does not match the expected JSON structure"}
	}
	return nil
}

func unsupportedMediaType(message string) *requestDecodeError {
	return &requestDecodeError{Status: http.StatusUnsupportedMediaType, Code: "UNSUPPORTED_MEDIA_TYPE", Message: message}
}

func validateSingleJSONValue(body []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := consumeJSONValue(decoder, ""); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return errors.New("multiple top-level JSON values")
	}
	return nil
}

func consumeJSONValue(decoder *json.Decoder, path string) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}

	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("object key is not a string")
			}
			childPath := path + "/" + escapeJSONPointer(key)
			if _, duplicate := seen[key]; duplicate {
				return domain.NewValidationError(domain.ValidationIssue{Path: childPath, Code: "duplicate", Message: "duplicate object key"})
			}
			seen[key] = struct{}{}
			if err := consumeJSONValue(decoder, childPath); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim('}') {
			return errors.New("unterminated object")
		}
	case '[':
		index := 0
		for decoder.More() {
			if err := consumeJSONValue(decoder, path+"/"+strconv.Itoa(index)); err != nil {
				return err
			}
			index++
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim(']') {
			return errors.New("unterminated array")
		}
	default:
		return fmt.Errorf("unexpected JSON delimiter %q", delimiter)
	}
	return nil
}

func escapeJSONPointer(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "~", "~0"), "/", "~1")
}

func unknownJSONField(err error) (string, bool) {
	const prefix = "json: unknown field "
	message := err.Error()
	if !strings.HasPrefix(message, prefix) {
		return "", false
	}
	field, unquoteErr := strconv.Unquote(strings.TrimPrefix(message, prefix))
	return field, unquoteErr == nil
}

func writeDecodeError(w http.ResponseWriter, r *http.Request, err error) {
	var decodeError *requestDecodeError
	if !errors.As(err, &decodeError) {
		writeAPIError(w, r, http.StatusBadRequest, "BAD_REQUEST", "invalid request body")
		return
	}
	writeAPIErrorWithDetails(w, r, decodeError.Status, decodeError.Code, decodeError.Message, decodeError.Details)
}
