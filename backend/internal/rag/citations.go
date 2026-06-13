package rag

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

const scrubCitationsMarker = "scrub_out_of_scope_message_citations_v1"

type persistedCitation struct {
	DocumentID string `json:"documentId"`
}

// ScrubOutOfScopeMessageCitations removes stale persisted RAG citation metadata
// from old assistant messages. Current retrieval is already scoped; this one-time
// pass fixes historical messages that still display sources from documents now
// known to belong to a different thread/project.
func (s *Store) ScrubOutOfScopeMessageCitations(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var done int
	switch err := tx.QueryRowContext(ctx,
		`SELECT 1 FROM schema_migrations WHERE version = ?`, scrubCitationsMarker).Scan(&done); err {
	case nil:
		return nil
	case sql.ErrNoRows:
	default:
		return fmt.Errorf("check citation scrub marker: %w", err)
	}

	messages, err := collectMessagesWithCitations(ctx, tx)
	if err != nil {
		return err
	}
	for _, message := range messages {
		filtered, changed, err := filterScopedCitations(ctx, tx, message)
		if err != nil {
			return err
		}
		if !changed {
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE messages SET citations = ? WHERE user_id = ? AND id = ?`,
			string(filtered), message.userID, message.id); err != nil {
			return fmt.Errorf("update message %s citations: %w", message.id, err)
		}
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (version) VALUES (?)`, scrubCitationsMarker); err != nil {
		return fmt.Errorf("record citation scrub marker: %w", err)
	}
	return tx.Commit()
}

type citationMessage struct {
	id        string
	userID    string
	threadID  string
	projectID *string
	citations string
}

func collectMessagesWithCitations(ctx context.Context, tx *sql.Tx) ([]citationMessage, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT m.id, m.user_id, m.thread_id, t.project_id, m.citations
		FROM messages m
		JOIN threads t ON t.user_id = m.user_id AND t.id = m.thread_id
		WHERE m.citations != '[]'`)
	if err != nil {
		return nil, fmt.Errorf("collect message citations: %w", err)
	}
	defer rows.Close()

	var out []citationMessage
	for rows.Next() {
		var msg citationMessage
		if err := rows.Scan(&msg.id, &msg.userID, &msg.threadID, &msg.projectID, &msg.citations); err != nil {
			return nil, fmt.Errorf("scan message citation row: %w", err)
		}
		out = append(out, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate message citations: %w", err)
	}
	return out, nil
}

func filterScopedCitations(ctx context.Context, tx *sql.Tx, message citationMessage) ([]byte, bool, error) {
	var raw []json.RawMessage
	if err := json.Unmarshal([]byte(message.citations), &raw); err != nil {
		return nil, false, fmt.Errorf("parse message %s citations: %w", message.id, err)
	}

	kept := make([]json.RawMessage, 0, len(raw))
	changed := false
	for _, entry := range raw {
		var citation persistedCitation
		if err := json.Unmarshal(entry, &citation); err != nil {
			return nil, false, fmt.Errorf("parse message %s citation entry: %w", message.id, err)
		}
		keep, err := citationInMessageScope(ctx, tx, message, citation.DocumentID)
		if err != nil {
			return nil, false, err
		}
		if keep {
			kept = append(kept, entry)
		} else {
			changed = true
		}
	}
	if !changed {
		return nil, false, nil
	}
	filtered, err := json.Marshal(kept)
	if err != nil {
		return nil, false, fmt.Errorf("marshal scrubbed message %s citations: %w", message.id, err)
	}
	return filtered, true, nil
}

func citationInMessageScope(ctx context.Context, tx *sql.Tx, message citationMessage, documentID string) (bool, error) {
	var projectID, threadID sql.NullString
	err := tx.QueryRowContext(ctx,
		`SELECT project_id, thread_id FROM documents WHERE user_id = ? AND id = ?`,
		message.userID, documentID).Scan(&projectID, &threadID)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("load cited document %s: %w", documentID, err)
	}

	if projectID.Valid {
		return message.projectID != nil && *message.projectID == projectID.String, nil
	}
	if threadID.Valid && threadID.String != "" {
		return threadID.String == message.threadID, nil
	}
	return true, nil
}
