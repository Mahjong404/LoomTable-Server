package attachment

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/Mahjong404/LoomTable-Server/internal/domain"
)

type serviceStore struct {
	created domain.Attachment
	current domain.Attachment
	content Content
	deleted bool
	marked  bool
}

func (s *serviceStore) CreateAttachment(_ context.Context, _, _ string, _ [32]byte, proposed domain.Attachment) (domain.Attachment, error) {
	s.created = proposed
	return proposed, nil
}

func (s *serviceStore) GetAttachment(context.Context, string, string) (domain.Attachment, error) {
	return s.current, nil
}

func (s *serviceStore) MarkReady(_ context.Context, _, _ string, content Content) (domain.Attachment, error) {
	s.marked = true
	s.content = content
	s.current.Status = "ready"
	s.current.Size = &content.Size
	s.current.Hash = content.Hash
	s.current.MimeType = content.MimeType
	return s.current, nil
}

func (s *serviceStore) DeleteAttachment(context.Context, string, string, int64) error {
	s.deleted = true
	return nil
}

type serviceContentStore struct {
	put     Content
	removed string
}

func (s *serviceContentStore) Put(context.Context, string, int64, string, io.Reader) (Content, error) {
	return s.put, nil
}

func (s *serviceContentStore) Open(context.Context, string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("content")), nil
}

func (s *serviceContentStore) Remove(_ context.Context, key string) error {
	s.removed = key
	return nil
}

func testAttachmentID(prefix string) (string, error) {
	return prefix + "00000000000000000000000000", nil
}

func TestInitializeManagedAttachmentStartsPending(t *testing.T) {
	store := &serviceStore{}
	service := NewWithIDGeneratorAndClock(store, &serviceContentStore{}, 100, testAttachmentID, func() time.Time {
		return time.Unix(10, 0)
	})

	size := int64(12)
	item, err := service.Initialize(context.Background(), "act_test", "mut_00000000000000000000000000", InitRequest{
		Source: "managed", Filename: " photo.png ", MimeType: "image/png", Size: &size,
	})
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if item.Status != "pending" || item.StorageKey != "act_test/att_00000000000000000000000000" {
		t.Fatalf("unexpected managed attachment: %+v", item)
	}
	if store.created.Filename != "photo.png" || store.created.Size == nil || *store.created.Size != size {
		t.Fatalf("unexpected normalized attachment: %+v", store.created)
	}
}

func TestInitializeVaultAttachmentIsReadyAndRequiresRelativePath(t *testing.T) {
	store := &serviceStore{}
	service := NewWithIDGeneratorAndClock(store, &serviceContentStore{}, 100, testAttachmentID, time.Now)
	item, err := service.Initialize(context.Background(), "act_test", "mut_00000000000000000000000000", InitRequest{
		Source: "vault", Filename: "note.md", VaultPath: "Files/note.md",
	})
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if item.Status != "ready" || item.StorageKey != "" || item.VaultPath != "Files/note.md" {
		t.Fatalf("unexpected vault attachment: %+v", item)
	}
	_, err = service.Initialize(context.Background(), "act_test", "mut_00000000000000000000000001", InitRequest{
		Source: "vault", Filename: "note.md", VaultPath: "../note.md",
	})
	if err == nil {
		t.Fatal("Initialize() accepted a parent Vault path")
	}
}

func TestUploadMarksManagedAttachmentReady(t *testing.T) {
	store := &serviceStore{current: domain.Attachment{
		ID: "att_00000000000000000000000000", Source: "managed", Status: "pending",
		StorageKey: "act_test/att_00000000000000000000000000",
	}}
	contentStore := &serviceContentStore{put: Content{Size: 4, Hash: strings.Repeat("a", 64), MimeType: "text/plain"}}
	service := NewWithIDGeneratorAndClock(store, contentStore, 100, testAttachmentID, time.Now)
	item, err := service.Upload(context.Background(), "act_test", store.current.ID, "text/plain", strings.NewReader("data"))
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	if !store.marked || item.Status != "ready" || item.Hash != strings.Repeat("a", 64) {
		t.Fatalf("unexpected uploaded attachment: %+v", item)
	}
}

func TestUploadRejectsVaultAttachment(t *testing.T) {
	store := &serviceStore{current: domain.Attachment{
		ID: "att_00000000000000000000000000", Source: "vault", Status: "ready",
	}}
	service := NewWithIDGeneratorAndClock(store, &serviceContentStore{}, 100, testAttachmentID, time.Now)
	_, err := service.Upload(context.Background(), "act_test", store.current.ID, "", strings.NewReader("data"))
	if err == nil {
		t.Fatal("Upload() accepted a Vault attachment")
	}
	var stateError *domain.InvalidStateTransitionError
	if !errors.As(err, &stateError) || stateError.Action != "upload" {
		t.Fatalf("Upload() error = %v, want invalid state transition", err)
	}
}

