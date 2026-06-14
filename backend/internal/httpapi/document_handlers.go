package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/trick77/slopr/internal/artifact"
	"github.com/trick77/slopr/internal/documents"
	"github.com/trick77/slopr/internal/rag"
)

// DocumentService is the RAG document dependency used by document handlers. It is
// nil when embeddings are not configured, which disables the feature (404).
type DocumentService interface {
	Upload(context.Context, documents.UploadInput) (rag.Document, artifact.Artifact, error)
	List(context.Context, string, *string) ([]rag.Document, error)
	Get(context.Context, string, string) (rag.Document, bool, error)
	Index(context.Context, string, string) error
	Unindex(context.Context, string, string) error
	Delete(context.Context, string, string) error
	Retrieve(context.Context, string, *string, *string, string, int) ([]rag.RetrievedChunk, error)
}

type documentResponse struct {
	ID         string    `json:"id"`
	Filename   string    `json:"filename"`
	MIMEType   string    `json:"mimeType"`
	SizeBytes  int64     `json:"sizeBytes"`
	Status     string    `json:"status"`
	Error      string    `json:"error,omitempty"`
	ProjectID  *string   `json:"projectId,omitempty"`
	ArtifactID *string   `json:"artifactId,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
}

func toDocumentResponse(d rag.Document) documentResponse {
	return documentResponse{
		ID:         d.ID,
		Filename:   d.Filename,
		MIMEType:   d.MIME,
		SizeBytes:  d.SizeBytes,
		Status:     d.Status,
		Error:      d.Error,
		ProjectID:  d.ProjectID,
		ArtifactID: d.ArtifactID,
		CreatedAt:  d.CreatedAt,
	}
}

func optionalFormValue(r *http.Request, key string) *string {
	v := strings.TrimSpace(r.FormValue(key))
	if v == "" {
		return nil
	}
	return &v
}

func (s *server) handleUploadDocument(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}
	if s.documents == nil {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}

	// Cap the whole request body to the artifact size limit before parsing.
	r.Body = http.MaxBytesReader(w, r.Body, artifact.MaxArtifactSizeBytes)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		if isRequestBodyTooLarge(err) {
			writeJSONError(w, http.StatusRequestEntityTooLarge, "upload too large")
			return
		}
		writeJSONError(w, http.StatusRequestEntityTooLarge, "upload too large or malformed")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "missing file")
		return
	}
	defer file.Close()

	doc, _, err := s.documents.Upload(r.Context(), documents.UploadInput{
		UserID:    user.ID,
		ThreadID:  strings.TrimSpace(r.FormValue("threadId")),
		ProjectID: optionalFormValue(r, "projectId"),
		Filename:  header.Filename,
		Reader:    file,
	})
	if errors.Is(err, documents.ErrUnsupportedFormat) {
		writeJSONError(w, http.StatusUnsupportedMediaType, "unsupported document format")
		return
	}
	if errors.Is(err, documents.ErrChatDocumentLimit) {
		writeJSONError(w, http.StatusConflict, "too many documents in this chat")
		return
	}
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "upload failed")
		return
	}
	writeJSON(w, toDocumentResponse(doc))
}

func (s *server) handleListDocuments(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}
	if s.documents == nil {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	docs, err := s.documents.List(r.Context(), user.ID, optionalQueryValue(r, "projectId"))
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "list documents failed")
		return
	}
	out := make([]documentResponse, 0, len(docs))
	for _, d := range docs {
		out = append(out, toDocumentResponse(d))
	}
	writeJSON(w, map[string]any{"items": out})
}

func (s *server) handleIndexDocument(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}
	if s.documents == nil {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	docID := r.PathValue("documentID")
	doc, found, err := s.documents.Get(r.Context(), user.ID, docID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "load document failed")
		return
	}
	if !found {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	// Idempotency gate: if an ingestion is already in flight for this document,
	// don't spawn a second one (avoids duplicate Tika/embedding cost on rapid
	// re-clicks). The store serializes the actual writes regardless.
	if doc.Status == rag.StatusExtracting || doc.Status == rag.StatusEmbedding {
		writeJSON(w, toDocumentResponse(doc))
		return
	}
	// Run ingestion off the request path; the client polls status via list/get.
	detached := context.WithoutCancel(r.Context())
	go func() {
		_ = s.documents.Index(detached, user.ID, docID)
	}()
	doc.Status = rag.StatusPending
	writeJSON(w, toDocumentResponse(doc))
}

func (s *server) handleUnindexDocument(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}
	if s.documents == nil {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	if err := s.documents.Unindex(r.Context(), user.ID, r.PathValue("documentID")); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "unindex failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) handleDeleteDocument(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}
	if s.documents == nil {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	if err := s.documents.Delete(r.Context(), user.ID, r.PathValue("documentID")); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "delete failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func optionalQueryValue(r *http.Request, key string) *string {
	v := strings.TrimSpace(r.URL.Query().Get(key))
	if v == "" {
		return nil
	}
	return &v
}
