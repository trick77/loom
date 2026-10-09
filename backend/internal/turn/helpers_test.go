package turn

import (
	"encoding/json"

	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/sse"
	"github.com/trick77/loom/internal/turn/turntest"
)

// Local names for the shared turn-port fakes in turntest.
type (
	fakeArtifactStore = turntest.ArtifactStore
	fakeThreadStore   = turntest.ThreadStore
	fakeChatClient    = turntest.ChatClient
	fakeMCPService    = turntest.ToolService
	stubDocs          = turntest.RetrievingDocuments
	listDocuments     = turntest.ListingDocuments
	fakeSandbox       = turntest.Sandbox
	stubUsageStore    = turntest.UsageStore
	fakeImageProvider = turntest.ImageProvider
)

var testUser = auth.User{ID: "user_1", Username: "jan", Role: auth.RoleUser, ResponseLanguage: "en"}

var errFakeTool = turntest.ErrTool

// sseEmitter writes a turn's events to an *sse.Writer, as the HTTP layer
// does, so a test can read them back from the recorded body.
type sseEmitter struct {
	w *sse.Writer
}

func (e sseEmitter) Send(event string, data any) error {
	payload, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return e.w.Send(event, string(payload))
}
