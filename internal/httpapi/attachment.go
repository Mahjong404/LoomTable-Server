package httpapi

import (
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	loomattachment "github.com/Mahjong404/LoomTable-Server/internal/attachment"
	"github.com/Mahjong404/LoomTable-Server/internal/domain"
)

type initializeAttachmentRequest struct {
	Source    *string `json:"source"`
	Filename  *string `json:"filename"`
	MimeType  *string `json:"mimeType"`
	Size      *int64  `json:"size"`
	VaultPath *string `json:"vaultPath"`
}

func (s *Server) attachment(w http.ResponseWriter, r *http.Request) {
	if s.attachments == nil {
		s.attachmentDisabled(w, r)
		return
	}
	trimmed := strings.TrimPrefix(r.URL.Path, "/v1/attachments/")
	if r.URL.Path == "/v1/attachments" || trimmed == "init" {
		s.initializeAttachment(w, r)
		return
	}
	if strings.HasSuffix(trimmed, "/content") {
		attachmentID := strings.TrimSuffix(trimmed, "/content")
		if attachmentID == "" || strings.Contains(attachmentID, "/") {
			writeAPIError(w, r, http.StatusNotFound, "NOT_FOUND", "resource not found")
			return
		}
		s.attachmentContent(w, r, attachmentID)
		return
	}
	if strings.Contains(trimmed, "/") || trimmed == "" {
		writeAPIError(w, r, http.StatusNotFound, "NOT_FOUND", "resource not found")
		return
	}
	s.attachmentMetadata(w, r, trimmed)
}

func (s *Server) initializeAttachment(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/v1/attachments/init" || r.Method != http.MethodPost {
		writeAPIError(w, r, http.StatusNotFound, "NOT_FOUND", "resource not found")
		return
	}
	var request initializeAttachmentRequest
	if err := decodeJSONRequest(r, &request); err != nil {
		writeDecodeError(w, r, err)
		return
	}
	issues := make([]domain.ValidationIssue, 0, 2)
	if request.Source == nil {
		issues = append(issues, requiredIssue("/source", "source is required"))
	}
	if request.Filename == nil {
		issues = append(issues, requiredIssue("/filename", "filename is required"))
	}
	if len(issues) > 0 {
		writeDomainError(w, r, domain.NewValidationError(issues...))
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		writeAPIError(w, r, http.StatusBadRequest, "BAD_REQUEST", "Idempotency-Key is required")
		return
	}
	requestValue := loomattachment.InitRequest{Source: *request.Source, Filename: *request.Filename}
	if request.MimeType != nil {
		requestValue.MimeType = *request.MimeType
	}
	if request.Size != nil {
		requestValue.Size = request.Size
	}
	if request.VaultPath != nil {
		requestValue.VaultPath = *request.VaultPath
	}
	item, err := s.attachments.Initialize(r.Context(), actorIDFrom(r), key, requestValue)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) attachmentMetadata(w http.ResponseWriter, r *http.Request, attachmentID string) {
	switch r.Method {
	case http.MethodGet:
		if len(r.URL.Query()) != 0 {
			writeAPIError(w, r, http.StatusBadRequest, "BAD_REQUEST", "getAttachment does not accept query parameters")
			return
		}
		item, err := s.attachments.Get(r.Context(), actorIDFrom(r), attachmentID)
		if err != nil {
			writeDomainError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, item)
	case http.MethodDelete:
		expectedRevision, ok := requiredPositiveRevisionQuery(r, "expectedRevision")
		if !ok || len(r.URL.Query()) != 1 {
			writeAPIError(w, r, http.StatusBadRequest, "BAD_REQUEST", "one positive expectedRevision query parameter is required")
			return
		}
		if err := s.attachments.Delete(r.Context(), actorIDFrom(r), attachmentID, expectedRevision); err != nil {
			writeDomainError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeAPIError(w, r, http.StatusNotFound, "NOT_FOUND", "resource not found")
	}
}

func (s *Server) attachmentContent(w http.ResponseWriter, r *http.Request, attachmentID string) {
	switch r.Method {
	case http.MethodPut:
		if r.Header.Get("Content-Encoding") != "" && r.Header.Get("Content-Encoding") != "identity" {
			writeAPIError(w, r, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", "attachment content must not be compressed")
			return
		}
		item, err := s.attachments.Upload(r.Context(), actorIDFrom(r), attachmentID, strings.TrimSpace(r.Header.Get("Content-Type")), r.Body)
		if err != nil {
			writeDomainError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, item)
	case http.MethodGet:
		item, content, err := s.attachments.Open(r.Context(), actorIDFrom(r), attachmentID)
		if err != nil {
			writeDomainError(w, r, err)
			return
		}
		defer content.Close()
		contentType := item.MimeType
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		w.Header().Set("Content-Type", contentType)
		if item.Size != nil {
			w.Header().Set("Content-Length", strconv.FormatInt(*item.Size, 10))
		}
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": item.Filename}))
		w.WriteHeader(http.StatusOK)
		_, _ = io.Copy(w, content)
	default:
		writeAPIError(w, r, http.StatusNotFound, "NOT_FOUND", "resource not found")
	}
}

