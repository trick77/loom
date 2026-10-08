package chat

import (
	"context"
	"encoding/json"
)

// Shorthand inserts for tests; production inserts go through
// AddMessageWithCitations and AddMessageWithAttachments.

func (s *Store) AddMessage(ctx context.Context, userID, threadID string, role Role, content string) (Message, error) {
	return s.insertMessage(ctx, messageInsert{userID: userID, threadID: threadID, role: role, content: content})
}

func (s *Store) AddMessageWithUsage(ctx context.Context, userID, threadID string, role Role, content string, usage MessageTokenUsage) (Message, error) {
	return s.insertMessage(ctx, messageInsert{userID: userID, threadID: threadID, role: role, content: content, usage: usage})
}

func (s *Store) AddMessageWithArtifacts(ctx context.Context, userID, threadID string, role Role, content string, usage MessageTokenUsage, artifacts json.RawMessage) (Message, error) {
	return s.insertMessage(ctx, messageInsert{userID: userID, threadID: threadID, role: role, content: content, usage: usage, artifacts: artifacts})
}

func (s *Store) AddMessageWithActivityTrace(ctx context.Context, userID, threadID string, role Role, content string, usage MessageTokenUsage, artifacts json.RawMessage, activityTrace json.RawMessage) (Message, error) {
	return s.insertMessage(ctx, messageInsert{userID: userID, threadID: threadID, role: role, content: content, usage: usage, artifacts: artifacts, activityTrace: activityTrace})
}
