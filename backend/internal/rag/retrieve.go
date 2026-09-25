package rag

import (
	"context"
	"fmt"
	"strings"
)

// retrieveOverfetchFactor is how many more neighbours than k the vector search
// asks for, to leave room for the status filter applied afterwards.
const retrieveOverfetchFactor = 3

// Retrieve returns up to k chunks most similar to queryEmbedding, scoped to the
// user and the thread's knowledge scope: every thread sees user-global chunks; a
// project thread (projectID != nil) also sees that project's chunks; and a thread
// additionally sees its own thread-private chunks (composer uploads in a
// project-less thread).
func (s *Store) Retrieve(ctx context.Context, userID string, projectID, threadID *string, queryEmbedding []float32, k int) ([]RetrievedChunk, error) {
	if k <= 0 {
		k = 5
	}
	scopes := []string{""} // always include global
	if projectID != nil {
		scopes = append(scopes, *projectID)
	}
	if threadID != nil && *threadID != "" {
		scopes = append(scopes, threadScopePrefix+*threadID)
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(scopes)), ",")

	// KNN over the partition-keyed vtab, joined back to chunks/documents. The
	// vec0 MATCH/k drive the search; user_id (partition key) and project_id
	// (metadata) constrain the scope.
	const queryPrefix = `
		SELECT c.document_id, d.filename, c.ordinal, c.text, v.distance
		FROM vec_chunks v
		JOIN chunks c ON c.id = v.rowid
		JOIN documents d ON d.id = c.document_id AND d.user_id = v.user_id
		WHERE v.embedding MATCH ? AND k = ?
		  AND v.user_id = ?
		  AND v.project_id IN (`
	const querySuffix = `)
		  AND d.status = 'embedded'
		ORDER BY v.distance`
	query := queryPrefix + placeholders + querySuffix //nolint:gosec // only the ?-placeholder list is interpolated, sized from len(scopes); every value is bound

	// The status filter runs after the nearest-neighbour search: asking vec0 for
	// exactly k neighbours and then dropping the ones whose document is still
	// indexing returned fewer than k. Over-fetch, then trim to k below.
	args := []any{vecLiteral(queryEmbedding), k * retrieveOverfetchFactor, userID}
	for _, sc := range scopes {
		args = append(args, sc)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("retrieve: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []RetrievedChunk
	for rows.Next() {
		var rc RetrievedChunk
		if err := rows.Scan(&rc.DocumentID, &rc.Filename, &rc.Ordinal, &rc.Text, &rc.Distance); err != nil {
			return nil, fmt.Errorf("scan retrieved chunk: %w", err)
		}
		out = append(out, rc)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) > k {
		out = out[:k]
	}
	return out, nil
}
