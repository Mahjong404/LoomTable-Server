package cursor

import (
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
