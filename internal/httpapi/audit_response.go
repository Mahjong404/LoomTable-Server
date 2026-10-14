package httpapi

import (
	"github.com/Mahjong404/LoomTable-Server/internal/domain"
	"net/http"
)

type auditResponseWriter struct {
	http.ResponseWriter
	operation *domain.AuditOperation
}

func (w *auditResponseWriter) WriteHeader(status int) {
	if status >= 200 && status < 300 && w.operation.ID != "" {
		w.Header().Set("X-LoomTable-Operation-Id", w.operation.ID)
	}
	w.ResponseWriter.WriteHeader(status)
}
