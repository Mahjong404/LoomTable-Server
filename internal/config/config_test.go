package config

import "testing"

func TestLoadUsesLoopbackDefaultAddress(t *testing.T) {
	t.Setenv("LOOMTABLE_HTTP_ADDR", "")

	cfg := Load()

	if cfg.HTTPAddr != "127.0.0.1:31201" {
		t.Fatalf("HTTPAddr = %q, want %q", cfg.HTTPAddr, "127.0.0.1:31201")
	}
}

func TestLoadAllowsExplicitAddress(t *testing.T) {
	t.Setenv("LOOMTABLE_HTTP_ADDR", "0.0.0.0:41201")

	cfg := Load()

	if cfg.HTTPAddr != "0.0.0.0:41201" {
		t.Fatalf("HTTPAddr = %q, want %q", cfg.HTTPAddr, "0.0.0.0:41201")
	}
}
