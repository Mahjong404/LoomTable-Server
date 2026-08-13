package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeResourceName(t *testing.T) {
	got, err := NormalizeResourceName("/name", " \tCafe\u0301　")
	if err != nil {
		t.Fatal(err)
	}
	if got != "Café" {
		t.Fatalf("NormalizeResourceName() = %q, want %q", got, "Café")
	}
}

func TestNormalizeResourceNameRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name  string
		value string
		code  string
	}{
		{name: "empty", value: "　 ", code: "required"},
		{name: "control", value: "bad\nname", code: "format"},
		{name: "too long", value: strings.Repeat("界", ResourceNameMaxCodePoints+1), code: "limit"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NormalizeResourceName("/name", test.value)
			var validation *ValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("error = %v, want ValidationError", err)
			}
			if len(validation.Issues) != 1 || validation.Issues[0].Code != test.code {
				t.Fatalf("issues = %#v, want code %q", validation.Issues, test.code)
			}
		})
	}
}
