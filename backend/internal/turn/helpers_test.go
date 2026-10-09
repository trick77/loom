package turn

import (
	"context"
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
// turn's events are recorded as SSE. cfg.Thread is set to store.
func runStoredTurn(t *testing.T, cfg Config, store *fakeThreadStore, content string) turnOutcome {
	t.Helper()
	cfg.Thread = store
	e := New(cfg)
	rec := httptest.NewRecorder()
	stream, err := sse.NewWriter(rec)
	if err != nil {
		t.Fatal(err)
	}
	usage := llm.NewUsageAccumulator()
	inference := llm.InferenceMetadata{UserID: testUser.ID, Username: testUser.Username, ThreadID: store.Thread.ID}
	ctx := llm.WithInferenceMetadata(llm.WithUsageAccumulator(context.Background(), usage), inference)
	prior, _, err := store.ListMessages(ctx, testUser.ID, store.Thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	userMessage, err := store.AddMessageWithAttachments(ctx, testUser.ID, store.Thread.ID, chat.RoleUser, content, nil, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	titles := NewReasoningTitleTracker(ctx, cfg.LLM, stream, inference, testUser.ResponseLanguageName())
	defer titles.Wait()
	run := e.Prepare(RunConfig{
		Stream:      stream,
		Titles:      titles,
		User:        testUser,
		Thread:      store.Thread,
		UserMessage: userMessage,
		Usage:       usage,
		Start:       time.Now(),
	}, PrepareInput{
		StreamCtx:     ctx,
		TurnCtx:       ctx,
		ReqCtx:        context.Background(),
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
