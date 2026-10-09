package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/llm"
	"github.com/trick77/loom/internal/sandbox"
	"github.com/trick77/loom/internal/turn"
)

func toolNames(tools []llm.Tool) map[string]bool {
	out := map[string]bool{}
	for _, tool := range tools {
		out[tool.Function.Name] = true
	}
	return out
}

func TestStreamMessageRunsPythonAndAnswers(t *testing.T) {
	store := &fakeThreadStore{Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"}}
	llmClient := &fakeToolChatClient{Results: []llm.StreamResult{
		{ToolCalls: []llm.ToolCall{{ID: "call_py", Type: "function", Function: llm.ToolCallFunction{
			Name: sandboxToolName, Arguments: `{"code":"print('strawberry'.count('r'))"}`,
		}}}},
		{Content: "There are 3."},
	}}
	box := &fakeSandbox{Enabled: true, Result: sandbox.Result{Stdout: "3\n"}}
	srv := newAuthenticatedServer(t, Deps{
		Thread:    store,
		LLM:       llmClient,
		Sandbox:   box,
		Artifacts: fakeArtifactStore{},
		UsersDir:  t.TempDir(),
	})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"How many r in strawberry?"}`))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"name":"run_python"`) || !strings.Contains(rec.Body.String(), `exit_code: 0\nstdout:\n3`) {
		t.Fatalf("SSE body lacks the run:\n%s", rec.Body.String())
	}
	if store.AssistantContent != "There are 3." {
		t.Fatalf("answer %q", store.AssistantContent)
	}
	if !toolNames(llmClient.Tools[0])[sandboxToolName] {
		t.Fatal("run_python not offered to the model")
	}
	if !strings.Contains(llmClient.Histories[0][0].Content, turn.SandboxGuidancePrompt) {
		t.Fatal("guidance missing from the system prompt")
	}
	if len(box.Got) != 1 || box.Got[0].Code != "print('strawberry'.count('r'))" {
		t.Fatalf("sandbox requests %+v", box.Got)
	}
}

func TestStreamMessageOmitsPythonWhenSandboxDown(t *testing.T) {
	store := &fakeThreadStore{Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"}}
	llmClient := &fakeToolChatClient{Results: []llm.StreamResult{{Content: "Hi."}}}
	srv := newAuthenticatedServer(t, Deps{
		Thread:    store,
		LLM:       llmClient,
		Sandbox:   &fakeSandbox{Enabled: false},
		Artifacts: fakeArtifactStore{},
		UsersDir:  t.TempDir(),
	})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Hello"}`))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if toolNames(llmClient.Tools[0])[sandboxToolName] || strings.Contains(llmClient.Histories[0][0].Content, "run_python") {
		t.Fatal("run_python offered or named while the sandbox is down")
	}
}
