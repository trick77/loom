package rag

import "time"

// Document status values (mirror the CHECK constraint in migration 0005).
const (
	StatusPending    = "pending"
	StatusExtracting = "extracting"
	StatusEmbedding  = "embedding"
	StatusEmbedded   = "embedded"
	StatusStale      = "stale"
	StatusError      = "error"
)

// Document is an uploaded file tracked for RAG. ProjectID/ThreadID/ArtifactID are
// nil when absent. A nil ProjectID means user-global scope, unless ThreadID is set
// (thread-private: retrievable only within that one thread).
type Document struct {
	ID            string
	UserID        string
	ProjectID     *string
	ThreadID      *string
	ArtifactID    *string
	VolumeRelpath string
	Filename      string
	MIME          string
	SizeBytes     int64
	Status        string
	Error         string
	CreatedAt     time.Time
	EmbeddedAt    *time.Time
}

// RetrievedChunk is a chunk returned from a similarity search, with its source
// document and the KNN distance (smaller = closer).
type RetrievedChunk struct {
	DocumentID string
	Filename   string
	Ordinal    int
	Text       string
	Distance   float64
}
