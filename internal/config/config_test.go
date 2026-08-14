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

func TestLoadUsesAttachmentDefaults(t *testing.T) {
	t.Setenv("LOOMTABLE_ATTACHMENT_ROOT", "")
	t.Setenv("LOOMTABLE_ATTACHMENT_MAX_BYTES", "")
	t.Setenv("LOOMTABLE_ATTACHMENTS_ENABLED", "")

	cfg := Load()

	if cfg.AttachmentRoot != "data/attachments" || cfg.AttachmentMaxBytes != 50*1024*1024 || !cfg.AttachmentsEnabled {
		t.Fatalf("attachment config = %#v", cfg)
	}
	if len(cfg.Capabilities) != 3 || cfg.Capabilities[2] != "attachments" {
		t.Fatalf("capabilities = %#v", cfg.Capabilities)
	}
}

func TestLoadRejectsInvalidAttachmentLimit(t *testing.T) {
	t.Setenv("LOOMTABLE_ATTACHMENT_MAX_BYTES", "-1")

	if got := Load().AttachmentMaxBytes; got != 50*1024*1024 {
		t.Fatalf("AttachmentMaxBytes = %d, want default", got)
	}
}

