package rag

import (
	"context"
	"fmt"
	"strings"
)

// Retrieve returns up to k chunks most similar to queryEmbedding, scoped to the
// user and the thread's knowledge scope: a project thread (projectID != nil)
// sees that project's chunks plus user-global ones; a project-less thread sees
// only user-global chunks.
func (s *Store) Retrieve(ctx context.Context, userID string, projectID *string, queryEmbedding []float32, k int) ([]RetrievedChunk, error) {
	if k <= 0 {
		k = 5
	}
	scopes := []string{""} // always include global
	if projectID != nil {
		scopes = append(scopes, *projectID)
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(scopes)), ",")

	// KNN over the partition-keyed vtab, joined back to chunks/documents. The
	// vec0 MATCH/k drive the search; user_id (partition key) and project_id
	// (metadata) constrain the scope.
	query := `
		SELECT c.document_id, d.filename, c.ordinal, c.text, v.distance
		FROM vec_chunks v
		JOIN chunks c ON c.id = v.rowid
		JOIN documents d ON d.id = c.document_id AND d.user_id = v.user_id
		WHERE v.embedding MATCH ? AND k = ?
		  AND v.user_id = ?
		  AND v.project_id IN (` + placeholders + `)
		ORDER BY v.distance`

	args := []any{vecLiteral(queryEmbedding), k, userID}
	for _, sc := range scopes {
		args = append(args, sc)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("retrieve: %w", err)
	}
	defer rows.Close()
	var out []RetrievedChunk
	for rows.Next() {
		var rc RetrievedChunk
		if err := rows.Scan(&rc.DocumentID, &rc.Filename, &rc.Ordinal, &rc.Text, &rc.Distance); err != nil {
			return nil, fmt.Errorf("scan retrieved chunk: %w", err)
		}
		out = append(out, rc)
	}
	return out, rows.Err()
}
