package rag

import (
	"database/sql"
	"strconv"
	"strings"
)

// Store persists documents, their chunks, and chunk embeddings, and retrieves
// the most relevant chunks for a query. All operations are user-scoped. Its
// methods are grouped by concern across documents.go, chunks.go, and retrieve.go.
type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// scopeValue maps a nullable project id to the vec_chunks metadata encoding
// ('' for user-global scope, since vec0 metadata columns are not nullable).
func scopeValue(projectID *string) string {
	if projectID == nil {
		return ""
	}
	return *projectID
}

// vecLiteral encodes a float32 vector as the JSON-array text sqlite-vec accepts.
func vecLiteral(v []float32) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, f := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(f), 'g', -1, 32))
	}
	b.WriteByte(']')
	return b.String()
}
