package rag

import (
	"database/sql"
	"encoding/binary"
	"math"
)

// Store persists documents, their chunks, and chunk embeddings, and retrieves
// the most relevant chunks for a query. All operations are user-scoped. Its
// methods are grouped by concern across documents.go, chunks.go, and retrieve.go.
type Store struct {
	db *sql.DB
}

// NewStore creates a Store for persisting and retrieving documents and embeddings.
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// scopeValue maps a nullable project id to the vec_chunks metadata encoding
// (” for user-global scope, since vec0 metadata columns are not nullable).
func scopeValue(projectID *string) string {
	if projectID == nil {
		return ""
	}
	return *projectID
}

// threadScopePrefix namespaces a thread-private scope key in the shared
// vec_chunks.project_id metadata slot. Document/thread IDs are base64url
// (see chat.NewIDForInternalUse), never contain ':', so the namespaces can't
// collide with a real project id or the ” global scope.
const threadScopePrefix = "thread:"

// scopeKey derives the vec_chunks.project_id metadata value for a document.
// A thread-private document (no project, a thread) is keyed 'thread:<threadID>'
// so it is retrievable only within that thread; everything else keeps the
// project/global encoding from scopeValue.
func scopeKey(projectID, threadID *string) string {
	if projectID == nil && threadID != nil && *threadID != "" {
		return threadScopePrefix + *threadID
	}
	return scopeValue(projectID)
}

// vecBlob encodes a float32 vector as the little-endian blob sqlite-vec reads
// directly. The JSON-array text form it also accepts costs a float format here
// and a parse there for every element.
func vecBlob(v []float32) []byte {
	b := make([]byte, 0, 4*len(v))
	for _, f := range v {
		b = binary.LittleEndian.AppendUint32(b, math.Float32bits(f))
	}
	return b
}
