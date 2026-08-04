package httpapi

import (
	"context"

	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/inference"
)

// withUserAttribution fills in the user/thread attribution a model call logs
// under, without disturbing anything already on the context. The chat turn
// attaches full metadata around its own LLM calls (see the stream handler), but
// the calls that hang off a turn's edges — the RAG query embedding, a live
// vision description for an attached image — and the detached ingest goroutine
// run on a context that carries none, which used to leave their inference lines
// anonymous. Existing values win, so a caller that already set a purpose or a
// round keeps it.
func withUserAttribution(ctx context.Context, user auth.User, threadID string) context.Context {
	metadata := inference.MetadataFromContext(ctx)
	if metadata.UserID == "" {
		metadata.UserID = user.ID
	}
	if metadata.Username == "" {
		metadata.Username = user.Username
	}
	if metadata.ThreadID == "" {
		metadata.ThreadID = threadID
	}
	return inference.WithMetadata(ctx, metadata)
}

// withUserIDAttribution is withUserAttribution for the paths that only have the
// user's ID on hand (the knowledge/document helpers take a userID string, not
// the resolved auth.User).
func withUserIDAttribution(ctx context.Context, userID, threadID string) context.Context {
	return withUserAttribution(ctx, auth.User{ID: userID}, threadID)
}
