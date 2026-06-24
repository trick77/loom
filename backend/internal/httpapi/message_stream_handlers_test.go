package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/trick77/loom/internal/artifact"
	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/classifier"
	"github.com/trick77/loom/internal/docgen"
	"github.com/trick77/loom/internal/imagegen"
	"github.com/trick77/loom/internal/llm"
	"github.com/trick77/loom/internal/mcp"
	"github.com/trick77/loom/internal/store"
)

func TestStreamMessageEmitsDeltasAndPersistsAssistant(t *testing.T) {
	store := &fakeThreadStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: chat.DefaultThreadTitle},
	}
	srv := newAuthenticatedServer(t, Deps{
		Thread: store,
		LLM:    fakeChatClient{title: "# Albert Einstein 🧠⚛️ The legendary physicist"},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Hi"}`)

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"event: user_message",
		"event: assistant_delta",
		`data: {"content":"Hel"}`,
		"event: assistant_message",
		"event: thread",
		`"title":"Albert Einstein The legendary physicist"`,
		"event: done",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("SSE body missing %q:\n%s", want, body)
		}
	}
	threadEvent := strings.Index(body, "event: thread")
	assistantDelta := strings.Index(body, "event: assistant_delta")
	if threadEvent < 0 || assistantDelta < 0 || threadEvent > assistantDelta {
		t.Fatalf("thread title event index = %d, assistant delta index = %d, want title before assistant response:\n%s", threadEvent, assistantDelta, body)
	}
	if store.assistantContent != "Hello" {
		t.Fatalf("assistantContent = %q, want Hello", store.assistantContent)
	}
	if len(store.messages) != 2 {
		t.Fatalf("persisted messages = %d, want 2", len(store.messages))
	}
	if store.messages[0].Role != chat.RoleUser || store.messages[0].Content != "Hi" {
		t.Fatalf("first persisted message = %#v, want user Hi", store.messages[0])
	}
	if store.messages[1].Role != chat.RoleAssistant || store.messages[1].Content != "Hello" {
		t.Fatalf("second persisted message = %#v, want assistant Hello", store.messages[1])
	}
}

func TestStreamMessageGeneratesTitleWhenThreadTitleIsFirstPrompt(t *testing.T) {
	store := &fakeThreadStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Explain this document"},
	}
	srv := newAuthenticatedServer(t, Deps{
		Thread: store,
		LLM:    fakeChatClient{title: "Document summary"},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Explain this document"}`)

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "event: thread") || !strings.Contains(body, `"title":"Document summary"`) {
		t.Fatalf("SSE body missing generated replacement title:\n%s", body)
	}
}

func TestStreamMessageSendsAndPersistsReasoningContent(t *testing.T) {
	store := &fakeThreadStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: chat.DefaultThreadTitle},
	}
	streamText := "Answer."
	srv := newAuthenticatedServer(t, Deps{
		Thread: store,
		LLM: fakeChatClient{
			streamText:    &streamText,
			reasoningText: "I should reason first.",
		},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Hi"}`)

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "event: assistant_reasoning_delta") {
		t.Fatalf("body missing assistant_reasoning_delta:\n%s", body)
	}
	if !strings.Contains(body, `"content":"I should reason first."`) {
		t.Fatalf("body missing reasoning content:\n%s", body)
	}
	if len(store.messages) == 0 {
		t.Fatal("no messages persisted")
	}
	last := store.messages[len(store.messages)-1]
	if last.Role != chat.RoleAssistant || last.ReasoningContent != "I should reason first." {
		t.Fatalf("persisted assistant = %#v", last)
	}
}

func TestStreamMessageEmitsAndPersistsReasoningTitle(t *testing.T) {
	store := &fakeThreadStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing"},
	}
	streamText := "Answer."
	srv := newAuthenticatedServer(t, Deps{
		Thread: store,
		LLM: fakeChatClient{
			streamText:     &streamText,
			reasoningText:  "The user wants the latest sources, so I will search.",
			reasoningTitle: "Searching current sources",
		},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Hi"}`)

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "event: assistant_reasoning_title") {
		t.Fatalf("body missing assistant_reasoning_title:\n%s", body)
	}
	if !strings.Contains(body, `"id":"reasoning-1"`) || !strings.Contains(body, `"title":"Searching current sources"`) {
		t.Fatalf("title event payload wrong:\n%s", body)
	}
	// The title event must precede assistant_message so the live label settles in order.
	if strings.Index(body, "event: assistant_reasoning_title") > strings.Index(body, "event: assistant_message") {
		t.Fatalf("title event came after assistant_message:\n%s", body)
	}
	if len(store.messages) == 0 {
		t.Fatal("no messages persisted")
	}
	last := store.messages[len(store.messages)-1]
	if !strings.Contains(string(last.ActivityTrace), `"title":"Searching current sources"`) {
		t.Fatalf("persisted activity trace missing title: %s", last.ActivityTrace)
	}
}

func TestStreamMessageAlignsReasoningTitlesAcrossRounds(t *testing.T) {
	store := &fakeThreadStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing"},
	}
	llmClient := &fakeToolChatClient{
		results: []llm.StreamResult{
			{
				ReasoningContent: "alpha reasoning",
				ToolCalls: []llm.ToolCall{{
					ID:       "call_1",
					Type:     "function",
					Function: llm.ToolCallFunction{Name: "search__web", Arguments: `{"q":"lume"}`},
				}},
			},
			{ReasoningContent: "beta reasoning", Content: "Final answer."},
		},
		titleFor: func(reasoning string) string {
			switch {
			case strings.Contains(reasoning, "alpha"):
				return "Alpha abstract"
			case strings.Contains(reasoning, "beta"):
				return "Beta abstract"
			default:
				return ""
			}
		},
	}
	srv := newAuthenticatedServer(t, Deps{
		Thread: store,
		LLM:    llmClient,
		MCP: fakeMCPService{
			tools: []llm.Tool{{
				Type:     "function",
				Function: llm.ToolFunction{Name: "search__web", Description: "Search", Parameters: map[string]any{"type": "object"}},
			}},
			result: "search result",
		},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Search"}`)

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if len(store.messages) == 0 {
		t.Fatal("no messages persisted")
	}
	last := store.messages[len(store.messages)-1]
	var trace []activityTraceEvent
	if err := json.Unmarshal(last.ActivityTrace, &trace); err != nil {
		t.Fatalf("unmarshal activity trace: %v\n%s", err, last.ActivityTrace)
	}
	// Each title must land on the reasoning block whose content it summarizes,
	// even though the frontend and backend assign reasoning ids independently.
	titleByContent := map[string]string{}
	for _, event := range trace {
		if event.Type == "reasoning" {
			titleByContent[event.Content] = event.Title
		}
	}
	if titleByContent["alpha reasoning"] != "Alpha abstract" {
		t.Fatalf("alpha block title = %q, want Alpha abstract\ntrace=%s", titleByContent["alpha reasoning"], last.ActivityTrace)
	}
	if titleByContent["beta reasoning"] != "Beta abstract" {
		t.Fatalf("beta block title = %q, want Beta abstract\ntrace=%s", titleByContent["beta reasoning"], last.ActivityTrace)
	}
}

func TestStreamMessageUsesFallbackWhenForcedFinalAnswerIsEmpty(t *testing.T) {
	// After running a tool the model stops without producing text, so the loop
	// forces a tool-free final answer. MiMo answers that with another inline tool
	// call, which is stripped — leaving the content empty. The turn must not persist
	// an empty (or raw-XML) message: a fallback answer is substituted instead.
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(context.Background(), `
INSERT INTO users (id, oidc_subject, username, role)
VALUES ('user_1', 'subject-user_1', 'user_1', 'user')`); err != nil {
		t.Fatal(err)
	}
	threadStore := chat.NewStore(db)
	artifactStore := artifact.NewStore(db)
	user := testUser
	thread, err := threadStore.CreateThread(context.Background(), user.ID, chat.CreateThreadInput{Title: "Fallback"})
	if err != nil {
		t.Fatal(err)
	}

	llmClient := &fakeToolChatClient{
		results: []llm.StreamResult{
			{
				Content: "",
				ToolCalls: []llm.ToolCall{{
					ID:       "call_1",
					Type:     "function",
					Function: llm.ToolCallFunction{Name: "create_text_file", Arguments: `{"filename":"notes.md","extension":"md","content":"# Notes"}`},
				}},
			},
			{Content: ""}, // round 2: no text, no tool calls -> forces tool-free final answer
		},
		plain: "", // every tool-free call (final + retry) returns empty, as if the inline XML was stripped
	}
	server := newAuthenticatedServer(t, Deps{
		Thread:    threadStore,
		Artifacts: artifactStore,
		DocTools:  []docgen.Generator{docgen.TextGenerator{}},
		UsersDir:  t.TempDir(),
		LLM:       llmClient,
	})

	req := authenticatedRequest(http.MethodPost, "/api/threads/"+thread.ID+"/messages:stream", `{"content":"make a markdown file"}`)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	messages, found, err := threadStore.ListMessages(context.Background(), user.ID, thread.ID)
	if err != nil || !found {
		t.Fatalf("ListMessages() found=%v err=%v", found, err)
	}
	var assistant chat.Message
	for _, message := range messages {
		if message.Role == chat.RoleAssistant {
			assistant = message
			break
		}
	}
	if strings.TrimSpace(assistant.Content) == "" {
		t.Fatalf("assistant content is empty; want a fallback answer instead of an empty turn")
	}
	if strings.Contains(assistant.Content, "<tool_call>") {
		t.Fatalf("assistant content leaked raw tool XML: %q", assistant.Content)
	}
}

func TestStreamMessageExecutesBuiltInArtifactTool(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(context.Background(), `
INSERT INTO users (id, oidc_subject, username, role)
VALUES ('user_1', 'subject-user_1', 'user_1', 'user')`); err != nil {
		t.Fatal(err)
	}
	threadStore := chat.NewStore(db)
	artifactStore := artifact.NewStore(db)
	user := testUser
	thread, err := threadStore.CreateThread(context.Background(), user.ID, chat.CreateThreadInput{Title: "Artifacts"})
	if err != nil {
		t.Fatal(err)
	}

	llmClient := &fakeToolChatClient{
		results: []llm.StreamResult{
			{
				Content: "",
				ToolCalls: []llm.ToolCall{{
					ID:   "call_1",
					Type: "function",
					Function: llm.ToolCallFunction{
						Name:      "create_text_file",
						Arguments: `{"filename":"notes.md","extension":"md","content":"# Notes"}`,
					},
				}},
			},
			{Content: "Created notes.md."},
		},
	}
	server := newAuthenticatedServer(t, Deps{
		Thread:    threadStore,
		Artifacts: artifactStore,
		DocTools:  []docgen.Generator{docgen.TextGenerator{}},
		UsersDir:  t.TempDir(),
		LLM:       llmClient,
	})

	req := authenticatedRequest(http.MethodPost, "/api/threads/"+thread.ID+"/messages:stream", `{"content":"make a markdown file"}`)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "event: artifact") {
		t.Fatalf("stream missing artifact event:\n%s", rec.Body.String())
	}

	messages, found, err := threadStore.ListMessages(context.Background(), user.ID, thread.ID)
	if err != nil || !found {
		t.Fatalf("ListMessages() found=%v err=%v", found, err)
	}
	var assistant chat.Message
	for _, message := range messages {
		if message.Role == chat.RoleAssistant {
			assistant = message
			break
		}
	}
	if !strings.Contains(string(assistant.Artifacts), "notes.md") {
		t.Fatalf("assistant artifacts = %s", assistant.Artifacts)
	}
}

func TestStreamMessageExecutesBuiltInImageTool(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(context.Background(), `
INSERT INTO users (id, oidc_subject, username, role)
VALUES ('user_1', 'subject-user_1', 'user_1', 'user')`); err != nil {
		t.Fatal(err)
	}
	threadStore := chat.NewStore(db)
	artifactStore := artifact.NewStore(db)
	user := testUser
	thread, err := threadStore.CreateThread(context.Background(), user.ID, chat.CreateThreadInput{Title: "Images"})
	if err != nil {
		t.Fatal(err)
	}

	llmClient := &fakeToolChatClient{
		results: []llm.StreamResult{
			{
				Content: "",
				ToolCalls: []llm.ToolCall{{
					ID:   "call_1",
					Type: "function",
					Function: llm.ToolCallFunction{
						Name:      "generate_image",
						Arguments: `{"prompt":"a small robot","filename":"robot","width":512,"height":512,"output_format":"png"}`,
					},
				}},
			},
		},
		plain: "Created robot.png.",
	}
	server := newAuthenticatedServer(t, Deps{
		Thread:     threadStore,
		Artifacts:  artifactStore,
		ImageTools: []imagegen.Tool{imagegen.NewTool(fakeImageProvider{})},
		UsersDir:   t.TempDir(),
		LLM:        llmClient,
	})

	req := authenticatedRequest(http.MethodPost, "/api/threads/"+thread.ID+"/messages:stream", `{"content":"make an image"}`)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "event: artifact") {
		t.Fatalf("stream missing artifact event:\n%s", body)
	}
	if !strings.Contains(body, "image/png") {
		t.Fatalf("stream missing image mime type:\n%s", body)
	}

	messages, found, err := threadStore.ListMessages(context.Background(), user.ID, thread.ID)
	if err != nil || !found {
		t.Fatalf("ListMessages() found=%v err=%v", found, err)
	}
	var assistant chat.Message
	for _, message := range messages {
		if message.Role == chat.RoleAssistant {
			assistant = message
			break
		}
	}
	if !bytes.Contains(assistant.Artifacts, []byte("image/png")) {
		t.Fatalf("assistant artifacts = %s", assistant.Artifacts)
	}
}

func TestStreamMessageUsesFallbackTextWhenImageFinalResponseIsEmpty(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(context.Background(), `
INSERT INTO users (id, oidc_subject, username, role)
VALUES ('user_1', 'subject-user_1', 'user_1', 'user')`); err != nil {
		t.Fatal(err)
	}
	threadStore := chat.NewStore(db)
	artifactStore := artifact.NewStore(db)
	user := testUser
	thread, err := threadStore.CreateThread(context.Background(), user.ID, chat.CreateThreadInput{Title: "Images"})
	if err != nil {
		t.Fatal(err)
	}
	llmClient := &fakeToolChatClient{
		results: []llm.StreamResult{{
			ToolCalls: []llm.ToolCall{{
				ID:   "call_1",
				Type: "function",
				Function: llm.ToolCallFunction{
					Name:      "generate_image",
					Arguments: `{"prompt":"a small robot","filename":"robot","width":512,"height":512,"output_format":"png"}`,
				},
			}},
		}},
	}
	server := newAuthenticatedServer(t, Deps{
		Thread:     threadStore,
		Artifacts:  artifactStore,
		ImageTools: []imagegen.Tool{imagegen.NewTool(fakeImageProvider{})},
		UsersDir:   t.TempDir(),
		LLM:        llmClient,
	})

	req := authenticatedRequest(http.MethodPost, "/api/threads/"+thread.ID+"/messages:stream", `{"content":"make an image"}`)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	body := rec.Body.String()
	if strings.Contains(body, "event: error") {
		t.Fatalf("SSE body contains error despite image artifact:\n%s", body)
	}
	messages, found, err := threadStore.ListMessages(context.Background(), user.ID, thread.ID)
	if err != nil || !found {
		t.Fatalf("ListMessages() found=%v err=%v", found, err)
	}
	var assistant chat.Message
	for _, message := range messages {
		if message.Role == chat.RoleAssistant {
			assistant = message
			break
		}
	}
	if assistant.Content != "Created robot.png." {
		t.Fatalf("assistant content = %q, want fallback artifact response", assistant.Content)
	}
	if !bytes.Contains(assistant.Artifacts, []byte("image/png")) {
		t.Fatalf("assistant artifacts = %s", assistant.Artifacts)
	}
}

func TestStreamMessageGeneratesAtMostOneImagePerTurn(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(context.Background(), `
INSERT INTO users (id, oidc_subject, username, role)
VALUES ('user_1', 'subject-user_1', 'user_1', 'user')`); err != nil {
		t.Fatal(err)
	}
	threadStore := chat.NewStore(db)
	artifactStore := artifact.NewStore(db)
	user := testUser
	thread, err := threadStore.CreateThread(context.Background(), user.ID, chat.CreateThreadInput{Title: "Work"})
	if err != nil {
		t.Fatal(err)
	}

	// One round emits two generate_image calls; a second round ends the loop with
	// plain text. The cap must run only the first call regardless of format.
	llmClient := &fakeToolChatClient{
		results: []llm.StreamResult{
			{ToolCalls: []llm.ToolCall{
				{ID: "call_1", Type: "function", Function: llm.ToolCallFunction{
					Name:      "generate_image",
					Arguments: `{"prompt":"a robot","filename":"robot","width":512,"height":512,"output_format":"png"}`,
				}},
				{ID: "call_2", Type: "function", Function: llm.ToolCallFunction{
					Name:      "generate_image",
					Arguments: `{"prompt":"a cat","filename":"cat","width":512,"height":512,"output_format":"jpeg"}`,
				}},
			}},
			{Content: "Here is your image."},
		},
	}
	server := newAuthenticatedServer(t, Deps{
		Thread:     threadStore,
		Artifacts:  artifactStore,
		ImageTools: []imagegen.Tool{imagegen.NewTool(fakeImageProvider{})},
		UsersDir:   t.TempDir(),
		LLM:        llmClient,
	})

	// The prompt avoids image-creation keywords so the request takes the default
	// tool loop (where the per-turn cap lives), not the required-image path that
	// already forces exactly one call.
	req := authenticatedRequest(http.MethodPost, "/api/threads/"+thread.ID+"/messages:stream", `{"content":"please help me finish this"}`)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if got := strings.Count(body, "event: artifact"); got != 1 {
		t.Fatalf("artifact events = %d, want exactly 1:\n%s", got, body)
	}
	if !strings.Contains(body, "Only one image can be generated per turn") {
		t.Fatalf("stream missing per-turn skip notice:\n%s", body)
	}

	messages, found, err := threadStore.ListMessages(context.Background(), user.ID, thread.ID)
	if err != nil || !found {
		t.Fatalf("ListMessages() found=%v err=%v", found, err)
	}
	var assistant chat.Message
	for _, message := range messages {
		if message.Role == chat.RoleAssistant {
			assistant = message
		}
	}
	var persisted []map[string]any
	if err := json.Unmarshal(assistant.Artifacts, &persisted); err != nil {
		t.Fatalf("unmarshal artifacts: %v", err)
	}
	if len(persisted) != 1 {
		t.Fatalf("persisted artifacts = %d, want 1: %s", len(persisted), assistant.Artifacts)
	}
}

func TestStreamMessageRequiresGenerateImageForObviousImageRequest(t *testing.T) {
	llmClient := &fakeToolChatClient{results: []llm.StreamResult{{Content: "I am a text-based AI assistant and cannot generate images."}}}
	store := &fakeThreadStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Images"},
	}
	server := newAuthenticatedServer(t, Deps{
		Thread:     store,
		Artifacts:  fakeArtifactStore{},
		ImageTools: []imagegen.Tool{imagegen.NewTool(fakeImageProvider{})},
		UsersDir:   t.TempDir(),
		LLM:        llmClient,
		MCP: fakeMCPService{tools: []llm.Tool{{
			Type:     "function",
			Function: llm.ToolFunction{Name: "search__web", Description: "Search the web"},
		}}},
	})

	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"generate an image of a glass city at sunrise"}`)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, `"error":"image generation was not completed"`) {
		t.Fatalf("SSE body missing image-generation error:\n%s", body)
	}
	if store.assistantContent != "" {
		t.Fatalf("assistantContent = %q, want no persisted text-only response", store.assistantContent)
	}
	if len(store.messages) != 1 || store.messages[0].Role != chat.RoleUser {
		t.Fatalf("persisted messages = %#v, want only user message", store.messages)
	}
	if len(llmClient.tools) != 1 {
		t.Fatalf("tool rounds = %d, want 1", len(llmClient.tools))
	}
	offeredTools := llmClient.tools[0]
	if len(offeredTools) != 1 || offeredTools[0].Function.Name != "generate_image" {
		t.Fatalf("offered tools = %#v, want only generate_image", offeredTools)
	}
	if len(llmClient.histories) != 1 {
		t.Fatalf("histories = %d, want 1", len(llmClient.histories))
	}
	foundDirective := false
	for _, message := range llmClient.histories[0] {
		if message.Role == "system" &&
			strings.Contains(message.Content, "Your only job is to call `generate_image` exactly once") &&
			strings.Contains(message.Content, "Do not refuse based on being text-based") {
			foundDirective = true
		}
	}
	if !foundDirective {
		t.Fatalf("history missing forced image compiler directive: %#v", llmClient.histories[0])
	}
}

func TestStreamMessageReturnsImageToolFailureAsStreamError(t *testing.T) {
	llmClient := &fakeToolChatClient{
		results: []llm.StreamResult{{
			ToolCalls: []llm.ToolCall{{
				ID:   "call_1",
				Type: "function",
				Function: llm.ToolCallFunction{
					Name:      "generate_image",
					Arguments: `{"prompt":"a small robot","filename":"robot"}`,
				},
			}},
		}},
	}
	store := &fakeThreadStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Images"},
	}
	server := newAuthenticatedServer(t, Deps{
		Thread:     store,
		Artifacts:  fakeArtifactStore{},
		ImageTools: []imagegen.Tool{imagegen.NewTool(errorImageProvider{err: errors.New("BFL generation timed out: context deadline exceeded")})},
		UsersDir:   t.TempDir(),
		LLM:        llmClient,
	})

	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"make an image"}`)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, `"error":"tool failed: BFL generation timed out: context deadline exceeded"`) {
		t.Fatalf("SSE body missing provider failure error:\n%s", body)
	}
	if store.assistantContent != "" {
		t.Fatalf("assistantContent = %q, want no persisted assistant after image failure", store.assistantContent)
	}
}

func TestStreamMessageDoesNotStreamTextBeforeRequiredImageToolCall(t *testing.T) {
	llmClient := &fakeToolChatClient{
		results: []llm.StreamResult{
			{
				Content: "Sure, I will create that now.",
				ToolCalls: []llm.ToolCall{{
					ID:   "call_1",
					Type: "function",
					Function: llm.ToolCallFunction{
						Name:      "generate_image",
						Arguments: `{"prompt":"a small robot","filename":"robot","width":512,"height":512,"output_format":"png"}`,
					},
				}},
			},
		},
		plain: "Created robot.png.",
	}
	store := &fakeThreadStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Images"},
	}
	server := newAuthenticatedServer(t, Deps{
		Thread:     store,
		Artifacts:  fakeArtifactStore{},
		ImageTools: []imagegen.Tool{imagegen.NewTool(fakeImageProvider{})},
		UsersDir:   t.TempDir(),
		LLM:        llmClient,
	})

	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"make an image"}`)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	body := rec.Body.String()
	toolCallIndex := strings.Index(body, "event: tool_call")
	leakedTextIndex := strings.Index(body, "Sure, I will create that now.")
	if toolCallIndex < 0 {
		t.Fatalf("SSE body missing tool call:\n%s", body)
	}
	if leakedTextIndex >= 0 && leakedTextIndex < toolCallIndex {
		t.Fatalf("SSE body streamed conversational text before tool call:\n%s", body)
	}
}

func TestStreamMessageRejectsImageFollowUpWithoutArtifact(t *testing.T) {
	textOnlyImageClaim := "Here's your trick77 logo in full cyberpunk style."
	var history []llm.Message
	store := &fakeThreadStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Images"},
		messages: []chat.Message{{
			ID:        "old_1",
			ThreadID:  "thr_1",
			Role:      chat.RoleAssistant,
			Content:   "Created a logo.",
			Artifacts: json.RawMessage(`[{"id":"art_1","displayFilename":"generated-image.png","mimeType":"image/png","downloadUrl":"/api/artifacts/art_1/download"}]`),
		}},
	}
	capturingLLM := &fakeToolChatClient{results: []llm.StreamResult{{Content: textOnlyImageClaim}}}
	server := newAuthenticatedServer(t, Deps{
		Thread:     store,
		Artifacts:  fakeArtifactStore{},
		ImageTools: []imagegen.Tool{imagegen.NewTool(fakeImageProvider{})},
		UsersDir:   t.TempDir(),
		LLM:        capturingLLM,
	})

	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"make it cyberpunk"}`)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, `"error":"image generation was not completed"`) {
		t.Fatalf("SSE body missing image-generation error:\n%s", body)
	}
	if store.assistantContent != "" {
		t.Fatalf("assistantContent = %q, want no persisted text-only image claim", store.assistantContent)
	}
	if len(store.messages) != 2 {
		t.Fatalf("persisted messages = %d, want prior assistant plus new user only: %#v", len(store.messages), store.messages)
	}
	if store.messages[1].Role != chat.RoleUser || store.messages[1].Content != "make it cyberpunk" {
		t.Fatalf("last persisted message = %#v, want cyberpunk user message", store.messages[1])
	}
	if len(capturingLLM.histories) == 0 {
		t.Fatal("LLM history was not captured")
	}
	history = capturingLLM.histories[0]
	foundDirective := false
	for _, message := range history {
		if message.Role == "system" && strings.Contains(message.Content, "Your only job is to call `generate_image` exactly once") {
			foundDirective = true
		}
	}
	if !foundDirective {
		t.Fatalf("history missing image artifact directive: %#v", history)
	}
}

func TestStreamMessagePersistsEmptyArtifactListForTextOnlyAssistant(t *testing.T) {
	store := &fakeThreadStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"},
	}
	srv := newAuthenticatedServer(t, Deps{
		Thread: store,
		LLM:    fakeChatClient{},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Hi"}`)

	srv.ServeHTTP(rec, req)

	if len(store.messages) != 2 {
		t.Fatalf("persisted messages = %d, want 2", len(store.messages))
	}
	if string(store.messages[1].Artifacts) != "[]" {
		t.Fatalf("assistant artifacts = %s, want []", store.messages[1].Artifacts)
	}
}

func TestStreamMessageAddsImageAttachmentsToLLMHistory(t *testing.T) {
	usersDir := t.TempDir()
	imageRel := filepath.ToSlash(filepath.Join("files", "screenshot.png"))
	imageAbs := filepath.Join(usersDir, testUser.ID, filepath.FromSlash(imageRel))
	if err := os.MkdirAll(filepath.Dir(imageAbs), 0o700); err != nil {
		t.Fatalf("mkdir image dir: %v", err)
	}
	imageBytes := []byte("fake-png")
	if err := os.WriteFile(imageAbs, imageBytes, 0o600); err != nil {
		t.Fatalf("write image: %v", err)
	}
	var history []llm.Message
	store := &fakeThreadStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Images"},
	}
	srv := newAuthenticatedServer(t, Deps{
		Thread: store,
		Artifacts: fakeArtifactStore{artifacts: []artifact.Artifact{{
			ID:              "art_image",
			UserID:          testUser.ID,
			ThreadID:        "thr_1",
			DisplayFilename: "screenshot.png",
			VolumeRelPath:   imageRel,
			MIMEType:        "image/png",
			SizeBytes:       int64(len(imageBytes)),
		}}},
		UsersDir: usersDir,
		LLM:      fakeChatClient{history: &history},
	})
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"What is this?","imageAttachmentIds":["art_image"]}`)
	rec := httptest.NewRecorder()

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body:\n%s", rec.Code, rec.Body.String())
	}
	if len(history) == 0 {
		t.Fatal("LLM history was not captured")
	}
	last := history[len(history)-1]
	wantURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(imageBytes)
	if last.Role != "user" || last.Content != "" || len(last.ContentParts) != 2 || last.ContentParts[0].ImageURL == nil || last.ContentParts[0].ImageURL.URL != wantURL || last.ContentParts[1].Text != "What is this?" {
		t.Fatalf("last history message = %#v, want image data URL and text content parts", last)
	}
}

func TestImageArtifactRequiredAvoidsSubstringFalsePositives(t *testing.T) {
	srv := &server{
		artifacts:  fakeArtifactStore{},
		usersDir:   t.TempDir(),
		imageTools: []imagegen.Tool{imagegen.NewTool(fakeImageProvider{})},
	}
	priorMessages := []chat.Message{{
		Role:      chat.RoleAssistant,
		Artifacts: json.RawMessage(`[{"mimeType":"image/png"}]`),
	}}

	for _, content := range []string{
		"I surrender",
		"what's the entry point",
		"explain that industry",
		"How do I render this template?",
		"describe the image under this URL",
		"explain how to draw a UML diagram",
		"lifestyle changes",
		"conversion tracking",
		// Bare generic verbs and pronouns no longer route an unrelated turn to the
		// image tool just because an image exists earlier in the thread.
		"change the subject and tell me about Rome",
		"make sure to cite that source",
		"try again, explain it differently",
	} {
		t.Run(content, func(t *testing.T) {
			if srv.imageArtifactRequired(content, priorMessages) {
				t.Fatalf("imageArtifactRequired(%q) = true, want false", content)
			}
		})
	}
}

func TestImageArtifactRequiredDetectsCreationAndGermanFollowUps(t *testing.T) {
	srv := &server{
		artifacts:  fakeArtifactStore{},
		usersDir:   t.TempDir(),
		imageTools: []imagegen.Tool{imagegen.NewTool(fakeImageProvider{})},
	}
	priorMessages := []chat.Message{{
		Role:      chat.RoleAssistant,
		Artifacts: json.RawMessage(`[{"mimeType":"image/png"}]`),
	}}

	for _, content := range []string{
		"generate an image of a robot",
		"create a logo for trick77",
		"erstelle ein Logo fuer trick77",
		"zeichne mir ein Bild",
		"mach es cyberpunk",
		"ändere den Stil",
	} {
		t.Run(content, func(t *testing.T) {
			if !srv.imageArtifactRequired(content, priorMessages) {
				t.Fatalf("imageArtifactRequired(%q) = false, want true", content)
			}
		})
	}
}

func TestLatestImageArtifactIDReturnsNewestWithID(t *testing.T) {
	messages := []chat.Message{
		{Role: chat.RoleAssistant, Artifacts: json.RawMessage(`[{"id":"img_old","mimeType":"image/png"}]`)},
		{Role: chat.RoleAssistant, Artifacts: json.RawMessage(`[{"id":"doc_1","mimeType":"application/pdf"}]`)},
		{Role: chat.RoleAssistant, Artifacts: json.RawMessage(`[{"id":"img_new","mimeType":"image/jpeg"}]`)},
	}
	if got := latestImageArtifactID(messages); got != "img_new" {
		t.Fatalf("latestImageArtifactID = %q, want img_new", got)
	}

	// No image artifacts, or image artifacts missing an id, yield "" — there is
	// nothing to silently re-attach as the model's vision input.
	none := []chat.Message{
		{Role: chat.RoleAssistant, Artifacts: json.RawMessage(`[{"id":"doc_1","mimeType":"text/plain"}]`)},
		{Role: chat.RoleAssistant, Artifacts: json.RawMessage(`[{"mimeType":"image/png"}]`)},
	}
	if got := latestImageArtifactID(none); got != "" {
		t.Fatalf("latestImageArtifactID = %q, want empty", got)
	}
}

func TestIsImageEditFollowUpGating(t *testing.T) {
	srv := &server{
		artifacts:  fakeArtifactStore{},
		usersDir:   t.TempDir(),
		imageTools: []imagegen.Tool{imagegen.NewTool(fakeImageProvider{})},
	}
	withImage := []chat.Message{{Role: chat.RoleAssistant, Artifacts: json.RawMessage(`[{"id":"img_1","mimeType":"image/png"}]`)}}

	// An explicit edit/restyle of the existing image — a pronoun pointing back at
	// it, or a transform verb/noun — reuses the prior image as the vision source.
	for _, content := range []string{
		"make it cyberpunk",
		"mach es cyberpunk",
		"create a variation",
		"turn it into a watercolor",
		"ändere den Stil",
		"give it a retro look",
		// Edits without a style word: an edit-target (size, colour, part, medium)
		// near an action verb, or a strong image-specific verb on its own.
		"make it bigger",
		"make it darker",
		"remove the background",
		"make the background blue",
		"crop it",
	} {
		if !srv.isImageEditFollowUp(content, withImage) {
			t.Fatalf("isImageEditFollowUp(%q) = false, want true", content)
		}
	}

	// A fresh creation must NOT pull in an unrelated prior image — even when it
	// carries a style word, and even when (like "draw a retro car") it lacks an
	// "image"/"logo" object token so isImageCreationRequest alone wouldn't catch it.
	// A bare style adjective is not an edit signal.
	for _, content := range []string{
		"generate an image of a robot",
		"make a logo with bold colors",
		"zeichne ein minimalistisches Bild",
		"draw a retro car",
		"create a neon sign",
		"a cyberpunk cityscape",
	} {
		if srv.isImageEditFollowUp(content, withImage) {
			t.Fatalf("isImageEditFollowUp(%q) = true, want false (fresh creation)", content)
		}
	}

	// Ordinary chat that merely contains a back-reference pronoun or a generic
	// verb — with no image-style descriptor nearby — must NOT silently re-feed the
	// prior image as vision input.
	for _, content := range []string{
		"what does this mean",
		"do you understand that",
		"how do I render this template",
		"change the subject and tell me about Rome",
		"make sure to cite that source",
		// An edit-target word ("red") near a bare pronoun is NOT enough — only an
		// action verb corroborates a target — so ordinary chat does not misfire.
		"what does this red error mean",
		"this is a red flag",
	} {
		if srv.isImageEditFollowUp(content, withImage) {
			t.Fatalf("isImageEditFollowUp(%q) = true, want false (not an edit)", content)
		}
	}

	// A follow-up phrasing without any prior image has nothing to reuse.
	if srv.isImageEditFollowUp("make it cyberpunk", nil) {
		t.Fatal("isImageEditFollowUp(no prior image) = true, want false")
	}
}

func TestAvailableToolsSkipsMCPDuplicateOfBuiltInTool(t *testing.T) {
	srv := &server{
		artifacts: fakeArtifactStore{},
		usersDir:  t.TempDir(),
		docTools:  []docgen.Generator{docgen.TextGenerator{}},
		mcp: fakeMCPService{tools: []llm.Tool{
			{Type: "function", Function: llm.ToolFunction{Name: "create_text_file"}},
			{Type: "function", Function: llm.ToolFunction{Name: "search__web"}},
		}},
	}

	tools := srv.availableTools()

	var builtInCount, searchCount int
	for _, tool := range tools {
		switch tool.Function.Name {
		case "create_text_file":
			builtInCount++
		case "search__web":
			searchCount++
		}
	}
	if builtInCount != 1 || searchCount != 1 {
		t.Fatalf("tool counts create_text_file=%d search__web=%d, want 1 and 1", builtInCount, searchCount)
	}
}

func TestExecuteToolCallFetchObscuraFallback(t *testing.T) {
	fetchCall := llm.ToolCall{
		Function: llm.ToolCallFunction{
			Name:      fetchToolName,
			Arguments: `{"url":"https://example.com"}`,
		},
	}

	t.Run("falls back to obscura when fetch fails", func(t *testing.T) {
		var navigated bool
		srv := &server{mcp: fakeMCPService{
			available: map[string]bool{
				obscuraNavigateToolName: true,
				obscuraSnapshotToolName: true,
			},
			callFunc: func(_ context.Context, name string, args map[string]any) (string, error) {
				switch name {
				case fetchToolName:
					return "", errFakeTool
				case obscuraNavigateToolName:
					if args["url"] != "https://example.com" {
						t.Fatalf("navigate url = %v, want https://example.com", args["url"])
					}
					navigated = true
					return "ok", nil
				case obscuraSnapshotToolName:
					if !navigated {
						t.Fatal("snapshot called before navigate")
					}
					return "rendered page text", nil
				}
				return "", errFakeTool
			},
		}}

		got := srv.executeToolCall(context.Background(), auth.User{ID: "u1", Username: "u1"}, fetchCall, 0)

		if got != "rendered page text" {
			t.Fatalf("output = %q, want obscura snapshot", got)
		}
	})

	t.Run("surfaces fetch failure when obscura is unavailable", func(t *testing.T) {
		srv := &server{mcp: fakeMCPService{err: errFakeTool}}

		got := srv.executeToolCall(context.Background(), auth.User{ID: "u1", Username: "u1"}, fetchCall, 0)

		if !strings.HasPrefix(got, "tool failed") {
			t.Fatalf("output = %q, want tool failed prefix", got)
		}
	})

	t.Run("does not fall back for non-fetch tools", func(t *testing.T) {
		var obscuraCalled bool
		srv := &server{mcp: fakeMCPService{
			available: map[string]bool{
				obscuraNavigateToolName: true,
				obscuraSnapshotToolName: true,
			},
			callFunc: func(_ context.Context, name string, _ map[string]any) (string, error) {
				if name == obscuraNavigateToolName || name == obscuraSnapshotToolName {
					obscuraCalled = true
				}
				return "", errFakeTool
			},
		}}
		otherCall := llm.ToolCall{Function: llm.ToolCallFunction{Name: "search__web", Arguments: `{"query":"x"}`}}

		got := srv.executeToolCall(context.Background(), auth.User{ID: "u1", Username: "u1"}, otherCall, 0)

		if obscuraCalled {
			t.Fatal("obscura must not be called for non-fetch tools")
		}
		if !strings.HasPrefix(got, "tool failed") {
			t.Fatalf("output = %q, want tool failed prefix", got)
		}
	})
}

type fakeImageProvider struct{}

func (fakeImageProvider) Generate(_ context.Context, req imagegen.GenerateRequest) (imagegen.GenerateResult, error) {
	return imagegen.GenerateResult{
		Filename:  req.Filename,
		Extension: "png",
		MIMEType:  "image/png",
		Bytes:     []byte("\x89PNG\r\n\x1a\nfake"),
		Provider:  "fake",
		Model:     "fake-model",
		RequestID: "request-1",
		Prompt:    req.Prompt,
		Width:     req.Width,
		Height:    req.Height,
	}, nil
}

type errorImageProvider struct {
	err error
}

func (f errorImageProvider) Generate(context.Context, imagegen.GenerateRequest) (imagegen.GenerateResult, error) {
	return imagegen.GenerateResult{}, f.err
}

func TestStreamMessagePersistsAssistantTokenUsage(t *testing.T) {
	store := &fakeThreadStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"},
	}
	srv := newAuthenticatedServer(t, Deps{
		Thread: store,
		LLM: fakeChatClient{usage: llm.TokenUsage{
			PromptTokens:     7,
			CompletionTokens: 3,
			TotalTokens:      10,
			PromptTokensDetails: llm.PromptTokenDetails{
				CachedTokens: 5,
			},
			CompletionTokenDetails: llm.CompletionTokenDetails{
				ReasoningTokens: 2,
			},
		}},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Hi"}`)

	srv.ServeHTTP(rec, req)

	if len(store.messages) != 2 {
		t.Fatalf("persisted messages = %d, want 2", len(store.messages))
	}
	assistant := store.messages[1]
	if got := derefInt(assistant.PromptTokens); got != 7 {
		t.Fatalf("PromptTokens = %d, want 7", got)
	}
	if got := derefInt(assistant.CompletionTokens); got != 3 {
		t.Fatalf("CompletionTokens = %d, want 3", got)
	}
	if got := derefInt(assistant.TotalTokens); got != 10 {
		t.Fatalf("TotalTokens = %d, want 10", got)
	}
	if got := derefInt(assistant.CachedTokens); got != 5 {
		t.Fatalf("CachedTokens = %d, want 5", got)
	}
	if got := derefInt(assistant.ReasoningTokens); got != 2 {
		t.Fatalf("ReasoningTokens = %d, want 2", got)
	}
}

func derefInt(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

// The persisted stats must include the background helper calls — the reasoning
// abstract and the thread title — not just the answer turn.
func TestStreamMessageAggregatesHelperTokenUsage(t *testing.T) {
	store := &fakeThreadStore{
		// Default title so the thread-title helper call fires this turn.
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: chat.DefaultThreadTitle},
	}
	srv := newAuthenticatedServer(t, Deps{
		Thread: store,
		LLM: fakeChatClient{
			title:          "Fresh title",
			reasoningTitle: "Explaining things",
			reasoningText:  "Let me think about this.",
			usage: llm.TokenUsage{
				PromptTokens: 7, CompletionTokens: 3, TotalTokens: 10,
				PromptTokensDetails:    llm.PromptTokenDetails{CachedTokens: 5},
				CompletionTokenDetails: llm.CompletionTokenDetails{ReasoningTokens: 2},
			},
			// Reasoning-title call runs with thinking disabled: cost is almost all
			// prompt (it re-sends the whole reasoning), no reasoning tokens.
			reasoningTitleUsage: llm.TokenUsage{PromptTokens: 100, CompletionTokens: 1, TotalTokens: 101},
			titleUsage:          llm.TokenUsage{PromptTokens: 20, CompletionTokens: 4, TotalTokens: 24},
		},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Hi"}`)

	srv.ServeHTTP(rec, req)

	if len(store.messages) != 2 {
		t.Fatalf("persisted messages = %d, want 2", len(store.messages))
	}
	assistant := store.messages[1]
	// 7+100+20 prompt, 3+1+4 completion, 10+101+24 total. The accumulator sums
	// cached/reasoning detail fields too; here only the answer turn sets them in
	// the fakes (the helpers leave them 0), so the totals stay 5 and 2 — this is
	// the test data, not a code limitation.
	for _, c := range []struct {
		name string
		got  int
		want int
	}{
		{"PromptTokens", derefInt(assistant.PromptTokens), 127},
		{"CompletionTokens", derefInt(assistant.CompletionTokens), 8},
		{"TotalTokens", derefInt(assistant.TotalTokens), 135},
		{"CachedTokens", derefInt(assistant.CachedTokens), 5},
		{"ReasoningTokens", derefInt(assistant.ReasoningTokens), 2},
	} {
		if c.got != c.want {
			t.Fatalf("%s = %d, want %d", c.name, c.got, c.want)
		}
	}
}

// The persisted stats must sum every tool round, not just the final answer turn.
func TestStreamMessageAggregatesTokenUsageAcrossToolRounds(t *testing.T) {
	store := &fakeThreadStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"},
	}
	llmClient := &fakeToolChatClient{
		results: []llm.StreamResult{
			{
				ReasoningContent: "I should search first.",
				ToolCalls: []llm.ToolCall{{
					ID:       "call_1",
					Type:     "function",
					Function: llm.ToolCallFunction{Name: "search__web", Arguments: `{"q":"lume"}`},
				}},
				Usage: llm.TokenUsage{PromptTokens: 11, CompletionTokens: 5, TotalTokens: 16},
			},
			{
				Content: "I found Lume.",
				Usage:   llm.TokenUsage{PromptTokens: 30, CompletionTokens: 7, TotalTokens: 37},
			},
		},
	}
	srv := newAuthenticatedServer(t, Deps{
		Thread: store,
		LLM:    llmClient,
		MCP: fakeMCPService{
			tools: []llm.Tool{{
				Type: "function",
				Function: llm.ToolFunction{
					Name:        "search__web",
					Description: "Search the web",
					Parameters:  map[string]any{"type": "object"},
				},
			}},
			result: "search result",
		},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Search Lume"}`)

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if len(store.messages) < 2 {
		t.Fatalf("persisted messages = %#v, want assistant message", store.messages)
	}
	assistant := store.messages[len(store.messages)-1]
	if got := derefInt(assistant.PromptTokens); got != 41 {
		t.Fatalf("PromptTokens = %d, want 41 (11+30)", got)
	}
	if got := derefInt(assistant.CompletionTokens); got != 12 {
		t.Fatalf("CompletionTokens = %d, want 12 (5+7)", got)
	}
	if got := derefInt(assistant.TotalTokens); got != 53 {
		t.Fatalf("TotalTokens = %d, want 53 (16+37)", got)
	}
}

func TestStreamMessagePersistsAssistantAfterClientContextCancel(t *testing.T) {
	store := &fakeThreadStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"},
	}
	var cancel context.CancelFunc
	srv := newAuthenticatedServer(t, Deps{
		Thread: store,
		LLM: fakeChatClient{afterStream: func() {
			cancel()
		}},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Hi"}`)
	ctx, cancelRequest := context.WithCancel(req.Context())
	cancel = cancelRequest
	req = req.WithContext(ctx)

	srv.ServeHTTP(rec, req)

	if store.assistantContent != "Hello" {
		t.Fatalf("assistantContent = %q, want Hello", store.assistantContent)
	}
	if store.assistantContextErr != nil {
		t.Fatalf("assistant AddMessage context error = %v, want nil", store.assistantContextErr)
	}
}

func TestStopStreamMessageCancelsActiveAssistantTurn(t *testing.T) {
	store := &fakeThreadStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"},
	}
	llmClient := &blockingChatClient{
		started: make(chan struct{}),
		done:    make(chan struct{}),
	}
	srv := newAuthenticatedServer(t, Deps{
		Thread: store,
		LLM:    llmClient,
	})

	streamCtx, cancelStream := context.WithCancel(context.Background())
	defer cancelStream()
	streamDone := make(chan struct{})
	go func() {
		defer close(streamDone)
		rec := httptest.NewRecorder()
		req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Hi"}`).WithContext(streamCtx)
		srv.ServeHTTP(rec, req)
	}()

	select {
	case <-llmClient.started:
	case <-time.After(time.Second):
		t.Fatal("stream did not reach llm client")
	}

	stopRec := httptest.NewRecorder()
	stopReq := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stop", "")
	srv.ServeHTTP(stopRec, stopReq)
	if stopRec.Code != http.StatusNoContent {
		cancelStream()
		t.Fatalf("status = %d, want 204: %s", stopRec.Code, stopRec.Body.String())
	}

	select {
	case <-llmClient.done:
	case <-time.After(time.Second):
		cancelStream()
		t.Fatal("stop did not cancel llm context")
	}
	if !errors.Is(llmClient.cancelCause, errStreamStopRequested) {
		t.Fatalf("cancel cause = %v, want %v", llmClient.cancelCause, errStreamStopRequested)
	}
	select {
	case <-streamDone:
	case <-time.After(time.Second):
		cancelStream()
		t.Fatal("stream handler did not return after stop")
	}
}

func TestActiveStreamRegistryCancelsPreviousStreamWithSupersededCause(t *testing.T) {
	var registry activeStreamRegistry
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)

	unregisterFirst := registry.register("user_1", "thr_1", cancel)
	defer unregisterFirst()
	unregisterSecond := registry.register("user_1", "thr_1", func(error) {})
	defer unregisterSecond()

	if !errors.Is(context.Cause(ctx), errStreamSuperseded) {
		t.Fatalf("cancel cause = %v, want %v", context.Cause(ctx), errStreamSuperseded)
	}
}

func TestStreamCancelDetailsClassifiesCancellationSource(t *testing.T) {
	t.Run("stop endpoint", func(t *testing.T) {
		ctx, cancel := context.WithCancelCause(context.Background())
		cancel(errStreamStopRequested)
		source, reason := streamCancelDetails(ctx)
		if source != "stop_endpoint" || reason != errStreamStopRequested.Error() {
			t.Fatalf("details = %q %q, want stop endpoint", source, reason)
		}
	})

	t.Run("superseded stream", func(t *testing.T) {
		ctx, cancel := context.WithCancelCause(context.Background())
		cancel(errStreamSuperseded)
		source, reason := streamCancelDetails(ctx)
		if source != "superseded_stream" || reason != errStreamSuperseded.Error() {
			t.Fatalf("details = %q %q, want superseded stream", source, reason)
		}
	})

	t.Run("request context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		source, reason := streamCancelDetails(ctx)
		if source != "request_context" || reason != context.Canceled.Error() {
			t.Fatalf("details = %q %q, want request context", source, reason)
		}
	})

	t.Run("deadline", func(t *testing.T) {
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		defer cancel()
		source, reason := streamCancelDetails(ctx)
		if source != "deadline" || reason != context.DeadlineExceeded.Error() {
			t.Fatalf("details = %q %q, want deadline", source, reason)
		}
	})
}

func TestStopStreamMessagePersistsPartialAssistantContent(t *testing.T) {
	store := &fakeThreadStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"},
	}
	llmClient := &blockingChatClient{
		started:        make(chan struct{}),
		done:           make(chan struct{}),
		partialContent: "Partial answer",
	}
	srv := newAuthenticatedServer(t, Deps{
		Thread: store,
		LLM:    llmClient,
	})

	streamDone := make(chan struct{})
	go func() {
		defer close(streamDone)
		rec := httptest.NewRecorder()
		req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Hi"}`)
		srv.ServeHTTP(rec, req)
	}()

	select {
	case <-llmClient.started:
	case <-time.After(time.Second):
		t.Fatal("stream did not reach llm client")
	}

	stopRec := httptest.NewRecorder()
	stopReq := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stop", "")
	srv.ServeHTTP(stopRec, stopReq)
	if stopRec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", stopRec.Code, stopRec.Body.String())
	}

	select {
	case <-streamDone:
	case <-time.After(time.Second):
		t.Fatal("stream handler did not return after stop")
	}
	if store.assistantContent != "Partial answer" {
		t.Fatalf("assistantContent = %q, want partial answer", store.assistantContent)
	}
}

func TestStreamMessageRejectsEmptyAssistantResponse(t *testing.T) {
	empty := ""
	store := &fakeThreadStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"},
	}
	srv := newAuthenticatedServer(t, Deps{
		Thread: store,
		LLM:    fakeChatClient{streamText: &empty},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Hi"}`)

	srv.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, `"error":"empty assistant response"`) {
		t.Fatalf("SSE body missing empty response error:\n%s", body)
	}
	if len(store.messages) != 1 || store.messages[0].Role != chat.RoleUser {
		t.Fatalf("persisted messages = %#v, want only user message", store.messages)
	}
}

func TestStreamMessageBuildsResponseLanguageHistory(t *testing.T) {
	var history []llm.Message
	store := &fakeThreadStore{
		thread:   chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"},
		messages: []chat.Message{{ID: "old_1", ThreadID: "thr_1", Role: chat.RoleAssistant, Content: "Earlier answer"}},
	}
	user := testUser
	user.ResponseLanguage = "de"
	srv := newAuthenticatedServerForUser(t, user, Deps{
		Thread: store,
		LLM:    fakeChatClient{history: &history},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Neue Frage"}`)

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if len(history) != 3 {
		t.Fatalf("history len = %d, want 3: %#v", len(history), history)
	}
	if !strings.Contains(history[0].Content, "Always answer in this language: German.") {
		t.Fatalf("system prompt = %q, want response-language directive", history[0].Content)
	}
	if history[1].Role != "assistant" || history[1].Content != "Earlier answer" {
		t.Fatalf("prior message = %#v", history[1])
	}
	if history[2].Role != "user" || history[2].Content != "Neue Frage" {
		t.Fatalf("new user message = %#v", history[2])
	}
}

func TestStreamMessageSystemPromptRoutesURLTools(t *testing.T) {
	var history []llm.Message
	store := &fakeThreadStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"},
	}
	srv := newAuthenticatedServer(t, Deps{
		Thread: store,
		LLM:    fakeChatClient{history: &history},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Read https://example.com"}`)

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if len(history) == 0 {
		t.Fatal("history is empty")
	}
	if !strings.Contains(history[0].Content, "For URLs, use the lightweight fetch tool first") {
		t.Fatalf("system prompt = %q, want fetch-first URL routing directive", history[0].Content)
	}
	if !strings.Contains(history[0].Content, "Use browser tools only when fetch cannot access useful content") {
		t.Fatalf("system prompt = %q, want browser fallback directive", history[0].Content)
	}
}

func TestSystemPromptForUserIncludesCurrentDate(t *testing.T) {
	now := time.Date(2026, time.June, 19, 10, 30, 0, 0, time.UTC)

	prompt := systemPromptForUser(auth.User{}, now)

	if !strings.Contains(prompt, "The current date is 2026-06-19") {
		t.Fatalf("system prompt = %q, want current date line", prompt)
	}
	if !strings.Contains(prompt, "do not assume an earlier year") {
		t.Fatalf("system prompt = %q, want search-year guidance", prompt)
	}
}

func TestStreamMessageSystemPromptDirectsToolsAtKnowledgeLimit(t *testing.T) {
	var history []llm.Message
	store := &fakeThreadStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"},
	}
	srv := newAuthenticatedServer(t, Deps{
		Thread: store,
		LLM:    fakeChatClient{history: &history},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"What happened last week?"}`)

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if len(history) == 0 {
		t.Fatal("history is empty")
	}
	if !strings.Contains(history[0].Content, "past your training cutoff") {
		t.Fatalf("system prompt = %q, want knowledge-limit directive", history[0].Content)
	}
	if !strings.Contains(history[0].Content, "first use the available search and fetch tools to look it up") {
		t.Fatalf("system prompt = %q, want tool-use-before-giving-up directive", history[0].Content)
	}
}

// The bounded-brainstorming directive moved out of the static base prompt into the
// dynamic `brainstorming` category block. A thread classified `brainstorming` must
// therefore receive that directive via the injected block.
func TestStreamMessageSystemPromptBoundsOpenEndedBrainstorming(t *testing.T) {
	var history []llm.Message
	store := &fakeThreadStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title", Category: string(classifier.Brainstorming)},
	}
	srv := newAuthenticatedServer(t, Deps{
		Thread: store,
		LLM:    fakeChatClient{history: &history},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Name my chatbot"}`)

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if len(history) == 0 {
		t.Fatal("history is empty")
	}
	if !strings.Contains(history[0].Content, "at most 12 options") {
		t.Fatalf("system prompt = %q, want bounded brainstorming directive from the injected block", history[0].Content)
	}
	if !strings.Contains(history[0].Content, "non-obvious angle") {
		t.Fatalf("system prompt = %q, want brainstorming surface directive", history[0].Content)
	}
}

func TestStreamMessageReturns503WhenLLMDependencyMissing(t *testing.T) {
	store := &fakeThreadStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: chat.DefaultThreadTitle},
	}
	srv := newAuthenticatedServer(t, Deps{Thread: store})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Hi"}`)

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"error":"llm is not configured"`) {
		t.Fatalf("body = %q, want llm configuration error", rec.Body.String())
	}
}

func TestStreamMessageStillCompletesWhenTitleGenerationFails(t *testing.T) {
	store := &fakeThreadStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: chat.DefaultThreadTitle},
	}
	srv := newAuthenticatedServer(t, Deps{
		Thread: store,
		LLM:    fakeChatClient{titleErr: errors.New("title model unavailable")},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Hi"}`)

	srv.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "event: assistant_message") {
		t.Fatalf("SSE body missing assistant message:\n%s", body)
	}
	if !strings.Contains(body, "event: done") {
		t.Fatalf("SSE body missing done despite title failure:\n%s", body)
	}
	if strings.Contains(body, "event: error") {
		t.Fatalf("SSE body contains error for best-effort title failure:\n%s", body)
	}
}

func TestStreamMessageEmitsMcpStatus(t *testing.T) {
	store := &fakeThreadStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"},
	}
	srv := newAuthenticatedServer(t, Deps{
		Thread: store,
		LLM:    fakeChatClient{title: "Greeting"},
		MCP: fakeMCPService{statuses: []mcp.ServerStatus{
			{Name: "alpha", Active: true},
			{Name: "zeta", Active: false},
		}},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Hi"}`)

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "event: mcp_status") {
		t.Fatalf("SSE body missing mcp_status event:\n%s", body)
	}
	if !strings.Contains(body, `data: {"active":1,"configured":2,"servers":[{"name":"alpha","active":true},{"name":"zeta","active":false}]}`) {
		t.Fatalf("SSE body missing mcp_status payload:\n%s", body)
	}
}

func TestStreamMessageOmitsMcpStatusWhenNoneConfigured(t *testing.T) {
	store := &fakeThreadStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"},
	}
	srv := newAuthenticatedServer(t, Deps{
		Thread: store,
		LLM:    fakeChatClient{title: "Greeting"},
		MCP:    fakeMCPService{},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Hi"}`)

	srv.ServeHTTP(rec, req)

	if strings.Contains(rec.Body.String(), "event: mcp_status") {
		t.Fatalf("SSE body should omit mcp_status when none configured:\n%s", rec.Body.String())
	}
}

func TestStreamMessageExecutesToolCallAndResumesAssistantStream(t *testing.T) {
	store := &fakeThreadStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"},
	}
	llmClient := &fakeToolChatClient{
		results: []llm.StreamResult{
			{
				ReasoningContent: "I should search first.",
				ToolCalls: []llm.ToolCall{{
					ID:   "call_1",
					Type: "function",
					Function: llm.ToolCallFunction{
						Name:      "search__web",
						Arguments: `{"q":"lume"}`,
					},
				}},
			},
			{Content: "I found Lume."},
		},
	}
	srv := newAuthenticatedServer(t, Deps{
		Thread: store,
		LLM:    llmClient,
		MCP: fakeMCPService{
			tools: []llm.Tool{{
				Type: "function",
				Function: llm.ToolFunction{
					Name:        "search__web",
					Description: "Search the web",
					Parameters:  map[string]any{"type": "object"},
				},
			}},
			result: "search result",
		},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Search Lume"}`)

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"event: tool_pending",
		"event: tool_call",
		`"name":"search__web"`,
		"event: tool_result",
		`"content":"search result"`,
		"event: assistant_delta",
		`data: {"content":"I found Lume."}`,
		"event: assistant_message",
		"event: done",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("SSE body missing %q:\n%s", want, body)
		}
	}
	if pending, call := strings.Index(body, "event: tool_pending"), strings.Index(body, "event: tool_call"); pending == -1 || pending > call {
		t.Fatalf("tool_pending should precede tool_call: pending=%d call=%d\n%s", pending, call, body)
	}
	if store.assistantContent != "I found Lume." {
		t.Fatalf("assistantContent = %q, want final answer", store.assistantContent)
	}
	if len(llmClient.histories) != 2 {
		t.Fatalf("stream calls = %d, want 2", len(llmClient.histories))
	}
	lastHistory := llmClient.histories[1]
	if len(lastHistory) < 3 {
		t.Fatalf("last history too short: %#v", lastHistory)
	}
	if got := lastHistory[len(lastHistory)-1]; got.Role != "tool" || got.ToolCallID != "call_1" || got.Content != "search result" {
		t.Fatalf("last history message = %#v, want tool result", got)
	}
	if got := lastHistory[len(lastHistory)-2]; got.Role != "assistant" || got.ReasoningContent != "" {
		t.Fatalf("assistant tool-call history = %#v, want reasoning omitted from provider history", got)
	}
	if len(store.messages) < 2 {
		t.Fatalf("persisted messages = %#v, want assistant message", store.messages)
	}
	trace := string(store.messages[len(store.messages)-1].ActivityTrace)
	for _, want := range []string{
		`"type":"reasoning"`,
		`"content":"I should search first."`,
		`"type":"tool"`,
		`"id":"call_1"`,
		`"name":"search__web"`,
		`"rawArguments":"{\"q\":\"lume\"}"`,
		`"rawOutput":"search result"`,
	} {
		if !strings.Contains(trace, want) {
			t.Fatalf("activity trace missing %q:\n%s", want, trace)
		}
	}
	if strings.Contains(trace, `"summary"`) {
		t.Fatalf("activity trace persisted backend summary, want raw trace only:\n%s", trace)
	}
}

func TestActivityTraceFromResultPersistsGenericAndFileToolCalls(t *testing.T) {
	b := &blockBuilder{}
	b.addResult(nil, llm.StreamResult{
		ToolCalls: []llm.ToolCall{
			{
				ID:   "call_pdf",
				Type: "function",
				Function: llm.ToolCallFunction{
					Name:      "create_pdf_file",
					Arguments: `{"filename":"report.pdf"}`,
				},
			},
			{
				ID:   "call_future",
				Type: "function",
				Function: llm.ToolCallFunction{
					Name:      "acme__transmogrify_asset",
					Arguments: `{"asset":"draft.pdf"}`,
				},
			},
		},
	})
	b.setToolResult("call_pdf", "created report.pdf")
	b.setToolResult("call_future", "Created draft.pdf")
	trace := b.flatTrace()

	if len(trace) != 2 {
		t.Fatalf("len(trace) = %d, want 2: %#v", len(trace), trace)
	}
	if trace[0].Name != "create_pdf_file" || trace[0].RawArguments != `{"filename":"report.pdf"}` || trace[0].RawOutput != "created report.pdf" {
		t.Fatalf("pdf trace = %#v", trace[0])
	}
	if trace[1].Name != "acme__transmogrify_asset" || trace[1].RawArguments != `{"asset":"draft.pdf"}` || trace[1].RawOutput != "Created draft.pdf" {
		t.Fatalf("future tool trace = %#v", trace[1])
	}
}

func TestStreamMessageRecoversFromToolError(t *testing.T) {
	store := &fakeThreadStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"},
	}
	llmClient := &fakeToolChatClient{
		results: []llm.StreamResult{
			{ToolCalls: []llm.ToolCall{{
				ID: "call_1",
				Function: llm.ToolCallFunction{
					Name:      "search__web",
					Arguments: `{"q":"lume"}`,
				},
			}}},
			{Content: "The search tool failed, but I can continue."},
		},
	}
	srv := newAuthenticatedServer(t, Deps{
		Thread: store,
		LLM:    llmClient,
		MCP: fakeMCPService{
			tools: []llm.Tool{{Type: "function", Function: llm.ToolFunction{Name: "search__web"}}},
			err:   errFakeTool,
		},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Search Lume"}`)

	srv.ServeHTTP(rec, req)

	body := rec.Body.String()
	if strings.Contains(body, "event: error") {
		t.Fatalf("SSE body contains turn-level error for tool failure:\n%s", body)
	}
	if !strings.Contains(body, "event: tool_result") || !strings.Contains(body, "tool failed: fake tool failed") {
		t.Fatalf("SSE body missing tool failure result:\n%s", body)
	}
	if store.assistantContent != "The search tool failed, but I can continue." {
		t.Fatalf("assistantContent = %q, want recovered answer", store.assistantContent)
	}
}

func TestStreamMessageStopsAfterToolCallLimit(t *testing.T) {
	store := &fakeThreadStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"},
	}
	toolCalls := make([]llm.ToolCall, maxToolCallsPerRound+1)
	for i := range toolCalls {
		toolCalls[i] = llm.ToolCall{
			ID: "call_limit",
			Function: llm.ToolCallFunction{
				Name:      "search__web",
				Arguments: `{}`,
			},
		}
	}
	srv := newAuthenticatedServer(t, Deps{
		Thread: store,
		LLM:    &fakeToolChatClient{results: []llm.StreamResult{{ToolCalls: toolCalls}}},
		MCP:    fakeMCPService{tools: []llm.Tool{{Type: "function", Function: llm.ToolFunction{Name: "search__web"}}}},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Search Lume"}`)

	srv.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "too many tool calls") {
		t.Fatalf("SSE body missing tool-call-limit error:\n%s", body)
	}
	if len(store.messages) != 1 || store.messages[0].Role != chat.RoleUser {
		t.Fatalf("persisted messages = %#v, want only user message", store.messages)
	}
}

func TestStreamMessageUsesFinalNoToolCallAfterRoundExhaustion(t *testing.T) {
	store := &fakeThreadStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"},
	}
	results := make([]llm.StreamResult, maxToolRounds)
	for i := range results {
		results[i] = llm.StreamResult{ToolCalls: []llm.ToolCall{{
			ID: "call_round",
			Function: llm.ToolCallFunction{
				Name:      "search__web",
				Arguments: `{}`,
			},
		}}}
	}
	llmClient := &fakeToolChatClient{results: results, plain: "Final answer without more tools."}
	srv := newAuthenticatedServer(t, Deps{
		Thread: store,
		LLM:    llmClient,
		MCP: fakeMCPService{
			tools:  []llm.Tool{{Type: "function", Function: llm.ToolFunction{Name: "search__web"}}},
			result: "search result",
		},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Search Lume"}`)

	srv.ServeHTTP(rec, req)

	body := rec.Body.String()
	if strings.Contains(body, "empty assistant response") {
		t.Fatalf("SSE body returned empty response after round exhaustion:\n%s", body)
	}
	if store.assistantContent != "Final answer without more tools." {
		t.Fatalf("assistantContent = %q, want final no-tool answer", store.assistantContent)
	}
}

func TestStreamMessageForcesFinalAnswerWhenModelStopsEmptyAfterTools(t *testing.T) {
	store := &fakeThreadStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"},
	}
	// Round 1 runs a tool; round 2 returns nothing (no tool call, no content) —
	// the model gave up without answering. The loop must force a final answer.
	llmClient := &fakeToolChatClient{
		results: []llm.StreamResult{
			{ToolCalls: []llm.ToolCall{{ID: "call_1", Function: llm.ToolCallFunction{Name: "search__web", Arguments: `{}`}}}},
			{},
		},
		plain: "Final answer after the tool ran.",
	}
	srv := newAuthenticatedServer(t, Deps{
		Thread: store,
		LLM:    llmClient,
		MCP: fakeMCPService{
			tools:  []llm.Tool{{Type: "function", Function: llm.ToolFunction{Name: "search__web"}}},
			result: "search result",
		},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Search Lume"}`)

	srv.ServeHTTP(rec, req)

	body := rec.Body.String()
	if strings.Contains(body, "empty assistant response") {
		t.Fatalf("SSE body returned empty response after the model stopped empty:\n%s", body)
	}
	if store.assistantContent != "Final answer after the tool ran." {
		t.Fatalf("assistantContent = %q, want forced final answer", store.assistantContent)
	}
	// The forced final turn must nudge the model to stop using tools.
	lastHistory := llmClient.histories[len(llmClient.histories)-1]
	nudged := false
	for _, msg := range lastHistory {
		if msg.Role == "system" && strings.Contains(msg.Content, "Do not call any more tools") {
			nudged = true
		}
	}
	if !nudged {
		t.Fatalf("final turn history missing the no-more-tools nudge: %#v", lastHistory)
	}
}
