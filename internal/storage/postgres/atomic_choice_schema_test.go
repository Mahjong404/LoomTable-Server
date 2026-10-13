package postgres_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v5"
	"gopkg.in/yaml.v3"
)

// Compile the actual operation response schema, including all local references.
// No copied error enums or network-loaded schemas can mask contract drift.
func mutationResponseSchema(t *testing.T, status int) *jsonschema.Schema {
	t.Helper()
	return compileOpenAPIResponseSchema(t, fmt.Sprintf("#/paths/~1v1~1tables~1{tableId}~1records~1mutate/post/responses/%d/content/application~1json/schema", status))
}

func compileOpenAPIResponseSchema(t *testing.T, pointer string) *jsonschema.Schema {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "api", "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := yaml.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	compiler := jsonschema.NewCompiler()
	compiler.Draft = jsonschema.Draft2020
	compiler.AssertFormat = true
	compiler.LoadURL = func(url string) (io.ReadCloser, error) {
		return nil, fmt.Errorf("unexpected external schema reference: %s", url)
	}
	const url = "https://loomtable.invalid/openapi.json"
	if err := compiler.AddResource(url, bytes.NewReader(encoded)); err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile(url + pointer)
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

func validateMutationHTTPResponse(t *testing.T, response *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if response.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("response content type = %q", response.Header().Get("Content-Type"))
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if err := mutationResponseSchema(t, response.Code).Validate(body); err != nil {
		t.Fatalf("HTTP %d response violates OpenAPI: %v\n%s", response.Code, err, response.Body.String())
	}
	// The published 409 operation also allows generic errors (key reuse).
	// A CAS error must satisfy its more specific, complete ConflictResponse.
	if e, ok := body["error"].(map[string]any); ok && e["code"] == "CONFLICT" {
		if err := compileOpenAPIResponseSchema(t, "#/components/schemas/ConflictResponse").Validate(body); err != nil {
			t.Fatalf("CAS response violates OpenAPI: %v", err)
		}
	}
	return body
}

func TestAtomicChoicePublishedLifecycleResponseSchema(t *testing.T) {
	schema := mutationResponseSchema(t, 422)
	for _, tc := range []struct {
		name string
		edit func(map[string]any)
	}{
		{"invalid action", func(e map[string]any) { e["details"].(map[string]any)["action"] = "assign choice" }},
		{"missing request identity", func(e map[string]any) { delete(e, "requestId") }},
		{"invalid resource type", func(e map[string]any) { e["details"].(map[string]any)["resource"].(map[string]any)["type"] = "choice" }},
		{"extra detail", func(e map[string]any) { e["details"].(map[string]any)["extra"] = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var body map[string]any
			if err := json.Unmarshal([]byte(`{"error":{"code":"INVALID_STATE_TRANSITION","message":"Field is deleted","requestId":"req_00000000000000000000000000","details":{"resource":{"type":"field","id":"fld_00000000000000000000000000"},"action":"update","current":"deleted"}}}`), &body); err != nil {
				t.Fatal(err)
			}
			if err := schema.Validate(body); err != nil {
				t.Fatalf("valid complete response rejected: %v", err)
			}
			tc.edit(body["error"].(map[string]any))
			if err := schema.Validate(body); err == nil {
				t.Fatal("invalid complete response accepted")
			}
		})
	}
}
