package postgres_test

import (
	"encoding/json"
	"testing"
)

func TestScopedQueryNullRequestSchema(t *testing.T) {
	schema := compileOpenAPIResponseSchema(t, "#/components/schemas/QueryRequest")
	for _, test := range []struct {
		body  string
		valid bool
	}{{`{"selectionScopeToken":"opaque","filter":null}`, true}, {`{"filter":null}`, false}, {`{}`, true}, {`{"selectionScopeToken":"opaque"}`, true}, {`{"selectionScopeToken":"","filter":null}`, false}, {`{"filter":{"kind":"rule","fieldId":"fld_00000000000000000000000000","operator":"isEmpty"}}`, true}} {
		var value any
		if err := json.Unmarshal([]byte(test.body), &value); err != nil {
			t.Fatal(err)
		}
		err := schema.Validate(value)
		if (err == nil) != test.valid {
			t.Fatalf("request schema validity %v, want %v for %s", err == nil, test.valid, test.body)
		}
	}
}
