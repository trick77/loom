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
//
// Table: documents
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
//
// Table: chunks
type RetrievedChunk struct {
	DocumentID string
	Filename   string
	Ordinal    int
	Text       string
	Distance   float64
}

// IndexedDoc identifies an embedded document and the sum of its chunks' token
// counts, used to decide cheaply — before any text extraction — which knowledge
// documents are small enough to inject in full.
//
// Table: -
type IndexedDoc struct {
	ID         string
	Filename   string
	TokenCount int
}

// VecCompaction is what one CompactVectors call found and did. Chunks are
// vec0's storage chunks (vec_chunks_chunks rows), not loom's chunks.
//
// Table: -
type VecCompaction struct {
	Compacted    bool
	Rows         int64
	ChunksBefore int64
	ChunksAfter  int64
}

// MissingVector is a stored chunk without an embedding.
//
// Table: chunks
type MissingVector struct {
	ChunkID int64
	UserID  string
	Text    string
	scope   string
}
