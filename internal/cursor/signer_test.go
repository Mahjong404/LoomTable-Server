package cursor

import (
	"errors"
	"strings"
	"testing"
)

func TestSignerProducesVersionedOpaqueToken(t *testing.T) {
	signer, err := NewSigner([]byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatal(err)
	}
	token, err := signer.Encode("change", map[string]any{"sequence": 12})
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(token, ".")
	if len(parts) != 4 || parts[0] != "v1" || parts[1] != "change" {
		t.Fatalf("token = %q", token)
	}
}

func TestSignerRequiresExactKeyLength(t *testing.T) {
	if _, err := NewSigner([]byte("short")); err == nil {
		t.Fatal("NewSigner accepted a short key")
	}
}

func TestSignerDecodesOnlyMatchingUntamperedPurpose(t *testing.T) {
	signer, err := NewSigner([]byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatal(err)
	}
	token, err := signer.Encode("query", map[string]any{"sequence": 12})
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Sequence int64 `json:"sequence"`
	}
	if err := signer.Decode("query", token, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Sequence != 12 {
		t.Fatalf("sequence = %d, want 12", payload.Sequence)
	}

	for _, invalid := range []struct {
		purpose string
		token   string
	}{
		{purpose: "change", token: token},
		{purpose: "query", token: token + "x"},
	} {
		if err := signer.Decode(invalid.purpose, invalid.token, &payload); !errors.Is(err, ErrInvalid) {
			t.Fatalf("Decode(%q) error = %v, want ErrInvalid", invalid.purpose, err)
		}
	}
}
