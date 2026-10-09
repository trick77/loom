package turn

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/llm"
	"github.com/trick77/loom/internal/sse"
	"github.com/trick77/loom/internal/turn/turntest"
)

// Local names for the shared turn-port fakes in turntest.
type (
	fakeArtifactStore  = turntest.ArtifactStore
	fakeThreadStore    = turntest.ThreadStore
	fakeChatClient     = turntest.ChatClient
	fakeToolChatClient = turntest.ToolChatClient
	fakeMCPService     = turntest.ToolService
	stubDocs           = turntest.RetrievingDocuments
	listDocuments      = turntest.ListingDocuments
	fakeSandbox        = turntest.Sandbox
	stubUsageStore     = turntest.UsageStore
	fakeImageProvider  = turntest.ImageProvider
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

// noMemory is a Memory with no user or project context, as for a user with
// none stored.
type noMemory struct{}

func (noMemory) UserContext(context.Context, string) string                   { return "" }
func (noMemory) ProjectContext(context.Context, string, chat.Thread) string   { return "" }
func (noMemory) RefreshProjectDescription(context.Context, auth.User, string) {}

// turnOutcome is what a test reads back from a driven turn: the SSE body its
// events produced and the assistant loop's result and error.
type turnOutcome struct {
	body   string
	result LoopResult
	err    error
}

// runStoredTurn drives testUser's turn on store's thread the way the stream
// handler does: it stores the user message, prepares the turn, runs the
// assistant loop and persists an answer the handler would persist. The
// turn's events are recorded as SSE. cfg.Thread is set to store, and a nil
// cfg.Memory to noMemory.
func runStoredTurn(t *testing.T, cfg Config, store *fakeThreadStore, content string) turnOutcome {
	t.Helper()
	cfg.Thread = store
	if cfg.Memory == nil {
		cfg.Memory = noMemory{}
	}
	e := New(cfg)
	rec := httptest.NewRecorder()
	w, err := sse.NewWriter(rec)
	if err != nil {
		t.Fatal(err)
	}
	stream := sseEmitter{w: w}
	usage := llm.NewUsageAccumulator()
	inference := llm.InferenceMetadata{UserID: testUser.ID, Username: testUser.Username, ThreadID: store.Thread.ID}
	ctx := llm.WithInferenceMetadata(llm.WithUsageAccumulator(context.Background(), usage), inference)
	prior, _, err := store.ListMessages(ctx, testUser.ID, store.Thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	userMessage, err := store.AddMessageWithAttachments(ctx, testUser.ID, store.Thread.ID, chat.RoleUser, content, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	titles := NewReasoningTitleTracker(ctx, cfg.LLM, stream, inference, UserResponseLanguage(testUser))
	defer titles.Wait()
	run := e.Prepare(RunConfig{
		Stream:      stream,
		Titles:      titles,
		Inference:   inference,
		User:        testUser,
		Thread:      store.Thread,
		UserMessage: userMessage,
		Usage:       usage,
		Start:       time.Now(),
	}, PrepareInput{
		StreamCtx:     ctx,
		TurnCtx:       ctx,
		ReqCtx:        context.Background(),
		Content:       content,
		PriorMessages: prior,
	})
	result, err := run.RunAssistantLoop(ctx)
	imageFailed := run.ImageRequired() && len(result.Artifacts) == 0
	if err == nil && !imageFailed && strings.TrimSpace(result.Content) != "" {
		if _, persistErr := run.PersistAssistantTurn(context.Background(), &result); persistErr != nil {
			t.Fatalf("persist assistant turn: %v", persistErr)
		}
	}
	titles.Wait()
	return turnOutcome{body: rec.Body.String(), result: result, err: err}
}
