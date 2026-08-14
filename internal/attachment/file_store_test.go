package attachment

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileStorePutDetectsContentAndUsesAtomicStorageKey(t *testing.T) {
	store := NewFileStore(t.TempDir())
	content, err := store.Put(context.Background(), "act_test/att_test", 1024, "text/plain", strings.NewReader("hello"))
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if content.Size != 5 || content.MimeType != "text/plain" {
		t.Fatalf("unexpected content metadata: %+v", content)
	}
	digest := sha256.Sum256([]byte("hello"))
	if content.Hash != hex.EncodeToString(digest[:]) {
		t.Fatalf("content hash = %q, want SHA-256 of stored content", content.Hash)
	}
	reader, err := store.Open(context.Background(), "act_test/att_test")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil || string(data) != "hello" {
		t.Fatalf("stored content = %q, error = %v", data, err)
	}
}

func TestFileStoreRejectsOversizeAndPathEscape(t *testing.T) {
	root := t.TempDir()
	store := NewFileStore(root)
	if _, err := store.Put(context.Background(), "act_test/att_test", 4, "text/plain", strings.NewReader("hello")); err == nil {
		t.Fatal("Put() accepted content over the limit")
	}
	if _, err := store.Put(context.Background(), "../outside", 100, "text/plain", strings.NewReader("hello")); err == nil {
		t.Fatal("Put() accepted a storage key escaping the root")
	}
	if _, err := os.Stat(filepath.Join(root, "outside")); !os.IsNotExist(err) {
		t.Fatalf("unexpected escaped file state: %v", err)
	}
}

func TestFileStoreRejectsDeclaredMimeMismatch(t *testing.T) {
	store := NewFileStore(t.TempDir())
	if _, err := store.Put(context.Background(), "act_test/att_test", 1024, "image/png", strings.NewReader("not an image")); err == nil {
		t.Fatal("Put() accepted a MIME mismatch")
	}
}

