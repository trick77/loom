package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/trick77/loom/internal/artifact"
	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/imagegen"
	"github.com/trick77/loom/internal/llm"
)

// End-to-end check that an aspect_ratio the model puts in its tool call survives
// argument parsing, normalization and the typography clamp, and lands on the
// artifact the user sees. The unit tests cover each step; this covers the wiring
// between them, which is where a forgotten field assignment would hide.
func TestStreamMessageAppliesAspectRatioFromToolCall(t *testing.T) {
	db := newUserDB(t)
	threadStore := chat.NewStore(db)
	artifactStore := artifact.NewStore(db)
	thread, err := threadStore.CreateThread(context.Background(), testUser.ID, chat.CreateThreadInput{Title: "Images"})
	if err != nil {
		t.Fatal(err)
	}

	llmClient := &fakeToolChatClient{
		ImageIntent: llm.ImageIntent{Action: llm.ImageIntentCreate},
		Results: []llm.StreamResult{{
			ToolCalls: []llm.ToolCall{{
				ID:   "call_1",
				Type: "function",
				Function: llm.ToolCallFunction{
					Name:      "generate_image",
					Arguments: `{"prompt":"a wide desert canyon at dusk","filename":"desert-canyon","aspect_ratio":"16:9"}`,
				},
			}},
		}},
		Plain: "Created desert-canyon.png.",
	}
	server := newAuthenticatedServer(t, Deps{
		Thread:     threadStore,
		Artifacts:  artifactStore,
		ImageTools: []imagegen.Tool{imagegen.NewTool(normalizingImageProvider{})},
		UsersDir:   t.TempDir(),
		LLM:        llmClient,
	})

	req := authenticatedRequest(http.MethodPost, "/api/threads/"+thread.ID+"/messages:stream", `{"content":"a wide shot of a desert canyon"}`)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	// fakeImageProvider echoes the request dimensions back, and the artifact event
	// carries them, so 1024x576 here proves the ratio was resolved rather than
	// silently defaulting to the 1024x1024 square.
	if !strings.Contains(body, `"width":1024`) || !strings.Contains(body, `"height":576`) {
		t.Fatalf("artifact event does not report a 16:9 size (want 1024x576):\n%s", body)
	}
	if strings.Contains(body, `"height":1024`) {
		t.Fatalf("artifact event reports a square height, aspect_ratio was ignored:\n%s", body)
	}
}

// normalizingImageProvider mirrors what a real provider does with the request it
// is handed: run it through Normalized (which is where aspect_ratio becomes a
// concrete size) and report the size it actually generated. fakeImageProvider
// echoes the raw request instead, so it cannot show that resolution happening.
type normalizingImageProvider struct{}

func (normalizingImageProvider) Generate(_ context.Context, req imagegen.GenerateRequest) (imagegen.GenerateResult, error) {
	norm, err := req.Normalized()
	if err != nil {
		return imagegen.GenerateResult{}, err
	}
	return imagegen.GenerateResult{
		Filename:  norm.Filename,
		Extension: "png",
		MIMEType:  "image/png",
		Bytes:     []byte("\x89PNG\r\n\x1a\nfake"),
		Provider:  "fake",
		Model:     "fake-model",
		RequestID: "request-1",
		Prompt:    norm.Prompt,
		Width:     norm.Width,
		Height:    norm.Height,
	}, nil
}
