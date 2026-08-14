package attachment

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/Mahjong404/LoomTable-Server/internal/domain"
)

type FileStore struct {
	root string
}

func NewFileStore(root string) *FileStore {
	return &FileStore{root: root}
}

func (s *FileStore) Put(ctx context.Context, storageKey string, maxBytes int64, declaredMime string, source io.Reader) (Content, error) {
	if s == nil || source == nil {
		return Content{}, domain.ErrDependencyMissing
	}
	if err := ctx.Err(); err != nil {
		return Content{}, err
	}
	target, err := s.resolve(storageKey)
	if err != nil {
		return Content{}, err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return Content{}, fmt.Errorf("create attachment directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(target), ".upload-*")
	if err != nil {
		return Content{}, fmt.Errorf("create attachment temporary file: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return Content{}, fmt.Errorf("restrict attachment temporary file: %w", err)
	}
	digest := sha256.New()
	prefix := &prefixWriter{}
	written, err := io.Copy(io.MultiWriter(temporary, digest, prefix), io.LimitReader(source, maxBytes+1))
	if err != nil {
		_ = temporary.Close()
		return Content{}, fmt.Errorf("write attachment content: %w", err)
	}
	if written > maxBytes {
		_ = temporary.Close()
		return Content{}, domain.NewValidationError(domain.ValidationIssue{Path: "/content", Code: "limit", Message: "attachment content exceeds the configured size limit"})
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return Content{}, fmt.Errorf("sync attachment content: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return Content{}, fmt.Errorf("close attachment content: %w", err)
	}
	if err := os.Rename(temporaryName, target); err != nil {
		return Content{}, fmt.Errorf("commit attachment content: %w", err)
	}

	// The bounded prefix is collected while streaming, so MIME detection does
	// not require buffering the entire upload in memory.
	detected := normalizeMediaType(http.DetectContentType(prefix.data))
	declaredMime = normalizeMediaType(declaredMime)
	if declaredMime != "" && declaredMime != "application/octet-stream" && detected != "application/octet-stream" && declaredMime != detected {
		_ = os.Remove(target)
		return Content{}, domain.NewValidationError(domain.ValidationIssue{Path: "/mimeType", Code: "format", Message: "declared MIME type does not match content"})
	}
	content := Content{Size: written, Hash: hex.EncodeToString(digest.Sum(nil)), MimeType: detected}
	if strings.HasPrefix(detected, "image/") {
		imageFile, openErr := os.Open(target)
		if openErr == nil {
			config, _, decodeErr := image.DecodeConfig(imageFile)
			_ = imageFile.Close()
			if decodeErr == nil && config.Width > 0 && config.Height > 0 {
				content.Width = intPointer(config.Width)
				content.Height = intPointer(config.Height)
			}
		}
	}
	return content, nil
}

func (s *FileStore) Open(ctx context.Context, storageKey string) (io.ReadCloser, error) {
	if s == nil {
		return nil, domain.ErrDependencyMissing
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	target, err := s.resolve(storageKey)
	if err != nil {
		return nil, err
	}
	return os.Open(target)
}

func (s *FileStore) Remove(ctx context.Context, storageKey string) error {
	if s == nil {
		return domain.ErrDependencyMissing
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	target, err := s.resolve(storageKey)
	if err != nil {
		return err
	}
	if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove attachment content: %w", err)
	}
	return nil
}

func (s *FileStore) resolve(storageKey string) (string, error) {
	if strings.TrimSpace(storageKey) == "" || filepath.IsAbs(storageKey) {
		return "", domain.NewValidationError(domain.ValidationIssue{Path: "/storageKey", Code: "format", Message: "storageKey must be a relative path"})
	}
	root, err := filepath.Abs(s.root)
	if err != nil {
		return "", fmt.Errorf("resolve attachment root: %w", err)
	}
	target := filepath.Join(root, filepath.FromSlash(storageKey))
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return "", domain.NewValidationError(domain.ValidationIssue{Path: "/storageKey", Code: "format", Message: "storageKey escapes the attachment root"})
	}
	return target, nil
}

type prefixWriter struct {
	data []byte
}

func (w *prefixWriter) Write(value []byte) (int, error) {
	originalLength := len(value)
	if len(w.data) < 512 {
		remaining := 512 - len(w.data)
		if len(value) > remaining {
			value = value[:remaining]
		}
		w.data = append(w.data, value...)
	}
	return originalLength, nil
}

func intPointer(value int) *int { return &value }

func normalizeMediaType(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if mediaType, _, err := mime.ParseMediaType(value); err == nil {
		return strings.ToLower(mediaType)
	}
	return strings.ToLower(value)
}

