package attachment

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Mahjong404/LoomTable-Server/internal/domain"
	"github.com/Mahjong404/LoomTable-Server/internal/id"
	"golang.org/x/text/unicode/norm"
)

const (
	DefaultMaxBytes int64 = 50 * 1024 * 1024
	DefaultMaxCount       = 10
	MinMaxCount           = 1
	MaxMaxCount           = 100
)

type InitRequest struct {
	Source    string
	Filename  string
	MimeType  string
	Size      *int64
	VaultPath string
}

type Content struct {
	Size     int64
	Hash     string
	MimeType string
	Width    *int
	Height   *int
}

type Store interface {
	CreateAttachment(context.Context, string, string, [32]byte, domain.Attachment) (domain.Attachment, error)
	GetAttachment(context.Context, string, string) (domain.Attachment, error)
	MarkReady(context.Context, string, string, Content) (domain.Attachment, error)
	DeleteAttachment(context.Context, string, string, int64) error
}

type ContentStore interface {
	Put(context.Context, string, int64, string, io.Reader) (Content, error)
	Open(context.Context, string) (io.ReadCloser, error)
	Remove(context.Context, string) error
}

type IDGenerator func(string) (string, error)

type Service struct {
	store    Store
	content  ContentStore
	maxBytes int64
	newID    IDGenerator
	now      func() time.Time
}

func New(store Store, content ContentStore, maxBytes int64) *Service {
	return NewWithIDGeneratorAndClock(store, content, maxBytes, id.New, time.Now)
}

func NewWithIDGeneratorAndClock(store Store, content ContentStore, maxBytes int64, newID IDGenerator, now func() time.Time) *Service {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	return &Service{store: store, content: content, maxBytes: maxBytes, newID: newID, now: now}
}

func (s *Service) Initialize(ctx context.Context, actorID, idempotencyKey string, request InitRequest) (domain.Attachment, error) {
	if !id.Valid(id.MutationPrefix, idempotencyKey) {
		return domain.Attachment{}, &domain.BadRequestError{Message: "Idempotency-Key must be a typed Mutation ID"}
	}
	if s == nil || s.store == nil || s.content == nil {
		return domain.Attachment{}, domain.ErrDependencyMissing
	}
	normalized, err := normalizeInitRequest(request, s.maxBytes)
	if err != nil {
		return domain.Attachment{}, err
	}
	attachmentID, err := s.newID(id.AttachmentPrefix)
	if err != nil {
		return domain.Attachment{}, fmt.Errorf("generate attachment ID: %w", err)
	}
	status := "pending"
	storageKey := actorID + "/" + attachmentID
	if normalized.Source == "vault" {
		status = "ready"
		storageKey = ""
	}
	proposed := domain.Attachment{
		ID:         attachmentID,
		Source:     normalized.Source,
		Status:     status,
		Filename:   normalized.Filename,
		MimeType:   normalized.MimeType,
		Size:       normalized.Size,
		StorageKey: storageKey,
		VaultPath:  normalized.VaultPath,
		Revision:   1,
		CreatedAt:  s.now().UTC(),
		UpdatedAt:  s.now().UTC(),
	}
	fingerprint, err := initFingerprint(normalized)
	if err != nil {
		return domain.Attachment{}, err
	}
	return s.store.CreateAttachment(ctx, actorID, idempotencyKey, fingerprint, proposed)
}

func (s *Service) Get(ctx context.Context, actorID, attachmentID string) (domain.Attachment, error) {
	if !id.Valid(id.AttachmentPrefix, attachmentID) {
		return domain.Attachment{}, &domain.BadRequestError{Message: "attachmentId has an invalid typed ID"}
	}
	if s == nil || s.store == nil {
		return domain.Attachment{}, domain.ErrDependencyMissing
	}
	return s.store.GetAttachment(ctx, actorID, attachmentID)
}

func (s *Service) Delete(ctx context.Context, actorID, attachmentID string, expectedRevision int64) error {
	if !id.Valid(id.AttachmentPrefix, attachmentID) {
		return &domain.BadRequestError{Message: "attachmentId has an invalid typed ID"}
	}
	if expectedRevision < 1 {
		return &domain.BadRequestError{Message: "expectedRevision must be positive"}
	}
	if s == nil || s.store == nil {
		return domain.ErrDependencyMissing
	}
	return s.store.DeleteAttachment(ctx, actorID, attachmentID, expectedRevision)
}

func (s *Service) Upload(ctx context.Context, actorID, attachmentID, declaredMime string, source io.Reader) (domain.Attachment, error) {
	if !id.Valid(id.AttachmentPrefix, attachmentID) {
		return domain.Attachment{}, &domain.BadRequestError{Message: "attachmentId has an invalid typed ID"}
	}
	if s == nil || s.store == nil || s.content == nil {
		return domain.Attachment{}, domain.ErrDependencyMissing
	}
	current, err := s.store.GetAttachment(ctx, actorID, attachmentID)
	if err != nil {
		return domain.Attachment{}, err
	}
	if current.Source != "managed" {
		return domain.Attachment{}, &domain.InvalidStateTransitionError{Resource: "attachment", ID: attachmentID, Action: "upload", Current: current.Source}
	}
	if current.Status != "pending" {
		return domain.Attachment{}, &domain.InvalidStateTransitionError{Resource: "attachment", ID: attachmentID, Action: "upload", Current: current.Status}
	}
	content, err := s.content.Put(ctx, current.StorageKey, s.maxBytes, declaredMime, source)
	if err != nil {
		return domain.Attachment{}, err
	}
	if current.Size != nil && *current.Size != content.Size {
		_ = s.content.Remove(ctx, current.StorageKey)
		return domain.Attachment{}, domain.NewValidationError(domain.ValidationIssue{Path: "/size", Code: "format", Message: "uploaded content size does not match initialized size"})
	}
	if current.MimeType != "" && declaredMime != "" && declaredMime != "application/octet-stream" && content.MimeType != "application/octet-stream" && current.MimeType != content.MimeType {
		_ = s.content.Remove(ctx, current.StorageKey)
		return domain.Attachment{}, domain.NewValidationError(domain.ValidationIssue{Path: "/mimeType", Code: "format", Message: "uploaded content MIME type does not match initialized MIME type"})
	}
	ready, err := s.store.MarkReady(ctx, actorID, attachmentID, content)
	if err != nil {
		_ = s.content.Remove(ctx, current.StorageKey)
		return domain.Attachment{}, err
	}
	return ready, nil
}

func (s *Service) Open(ctx context.Context, actorID, attachmentID string) (domain.Attachment, io.ReadCloser, error) {
	if !id.Valid(id.AttachmentPrefix, attachmentID) {
		return domain.Attachment{}, nil, &domain.BadRequestError{Message: "attachmentId has an invalid typed ID"}
	}
	if s == nil || s.store == nil || s.content == nil {
		return domain.Attachment{}, nil, domain.ErrDependencyMissing
	}
	current, err := s.store.GetAttachment(ctx, actorID, attachmentID)
	if err != nil {
		return domain.Attachment{}, nil, err
	}
	if current.Source != "managed" || current.Status != "ready" {
		return domain.Attachment{}, nil, &domain.InvalidStateTransitionError{Resource: "attachment", ID: attachmentID, Action: "download", Current: current.Status}
	}
	reader, err := s.content.Open(ctx, current.StorageKey)
	if err != nil {
		return domain.Attachment{}, nil, fmt.Errorf("open attachment content: %w", err)
	}
	return current, reader, nil
}

func normalizeInitRequest(request InitRequest, maxBytes int64) (InitRequest, error) {
	if request.Source != "managed" && request.Source != "vault" {
		return InitRequest{}, domain.NewValidationError(domain.ValidationIssue{Path: "/source", Code: "format", Message: "source must be managed or vault"})
	}
	filename, err := normalizeFilename(request.Filename)
	if err != nil {
		return InitRequest{}, err
	}
	request.Filename = filename
	request.MimeType = normalizeMediaType(request.MimeType)
	if request.Size != nil && (*request.Size < 0 || *request.Size > maxBytes) {
		return InitRequest{}, domain.NewValidationError(domain.ValidationIssue{Path: "/size", Code: "limit", Message: "size must be from 0 to the configured attachment limit"})
	}
	if request.Source == "managed" {
		if request.VaultPath != "" {
			return InitRequest{}, domain.NewValidationError(domain.ValidationIssue{Path: "/vaultPath", Code: "format", Message: "vaultPath is only valid for vault attachments"})
		}
		return request, nil
	}
	if request.VaultPath, err = normalizeVaultPath(request.VaultPath); err != nil {
		return InitRequest{}, err
	}
	return request, nil
}

func normalizeFilename(value string) (string, error) {
	value = norm.NFC.String(strings.TrimFunc(value, unicode.IsSpace))
	if value == "" || value == "." || value == ".." || strings.ContainsAny(value, "/\\") || strings.IndexFunc(value, unicode.IsControl) >= 0 || !utf8.ValidString(value) {
		return "", domain.NewValidationError(domain.ValidationIssue{Path: "/filename", Code: "format", Message: "filename must be a safe non-empty file name"})
	}
	if utf8.RuneCountInString(value) > 255 {
		return "", domain.NewValidationError(domain.ValidationIssue{Path: "/filename", Code: "limit", Message: "filename cannot exceed 255 Unicode code points"})
	}
	return value, nil
}

func normalizeVaultPath(value string) (string, error) {
	value = norm.NFC.String(strings.TrimSpace(strings.ReplaceAll(value, "\\", "/")))
	parts := strings.Split(value, "/")
	if value == "" || strings.HasPrefix(value, "/") || strings.Contains(value, ":") {
		return "", domain.NewValidationError(domain.ValidationIssue{Path: "/vaultPath", Code: "format", Message: "vaultPath must be a relative Vault path"})
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", domain.NewValidationError(domain.ValidationIssue{Path: "/vaultPath", Code: "format", Message: "vaultPath cannot contain empty, dot, or parent path members"})
		}
		if strings.IndexFunc(part, unicode.IsControl) >= 0 {
			return "", domain.NewValidationError(domain.ValidationIssue{Path: "/vaultPath", Code: "format", Message: "vaultPath cannot contain control characters"})
		}
	}
	return value, nil
}

func initFingerprint(request InitRequest) ([32]byte, error) {
	encoded, err := json.Marshal(request)
	if err != nil {
		return [32]byte{}, fmt.Errorf("canonicalize attachment initialization: %w", err)
	}
	return sha256.Sum256(encoded), nil
}

