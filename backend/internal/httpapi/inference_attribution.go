package httpapi

import (
	"context"

	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/inference"
)

// withUserAttribution fills in the user/thread attribution a model call logs
// under, without disturbing anything already on the context. A chat turn
// attributes itself up front (see handleStreamMessage), so this is for the model
// calls that reach a client from somewhere else: the detached ingest goroutine,
// which runs on a context carrying no metadata at all, and the image tool, whose
// dispatch context is not one of the attributed chat contexts. Existing values
// win, so a caller that already set a purpose or a round keeps it.
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
