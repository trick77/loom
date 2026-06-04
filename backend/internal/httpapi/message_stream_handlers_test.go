package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/trick77/spark/internal/artifact"
	"github.com/trick77/spark/internal/chat"
	"github.com/trick77/spark/internal/docgen"
	"github.com/trick77/spark/internal/imagegen"
	"github.com/trick77/spark/internal/llm"
	"github.com/trick77/spark/internal/mcp"
	"github.com/trick77/spark/internal/store"
)

func TestStreamMessageEmitsDeltasAndPersistsAssistant(t *testing.T) {
	store := &fakeChatStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: chat.DefaultThreadTitle},
	}
	srv := newAuthenticatedChatServer(t, Deps{
		Chat: store,
		LLM:  fakeChatClient{title: "# Albert Einstein 🧠⚛️ The legendary physicist"},
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

func TestStreamMessageSendsAndPersistsReasoningContent(t *testing.T) {
	store := &fakeChatStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: chat.DefaultThreadTitle},
	}
	streamText := "Answer."
	srv := newAuthenticatedChatServer(t, Deps{
		Chat: store,
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
	chatStore := chat.NewStore(db)
	artifactStore := artifact.NewStore(db)
	user := testUser
	thread, err := chatStore.CreateThread(context.Background(), user.ID, chat.CreateThreadInput{Title: "Artifacts"})
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
	server := newAuthenticatedChatServer(t, Deps{
		Chat:      chatStore,
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

	messages, found, err := chatStore.ListMessages(context.Background(), user.ID, thread.ID)
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
	chatStore := chat.NewStore(db)
	artifactStore := artifact.NewStore(db)
	user := testUser
	thread, err := chatStore.CreateThread(context.Background(), user.ID, chat.CreateThreadInput{Title: "Images"})
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
	server := newAuthenticatedChatServer(t, Deps{
		Chat:       chatStore,
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

	messages, found, err := chatStore.ListMessages(context.Background(), user.ID, thread.ID)
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
	chatStore := chat.NewStore(db)
	artifactStore := artifact.NewStore(db)
	user := testUser
	thread, err := chatStore.CreateThread(context.Background(), user.ID, chat.CreateThreadInput{Title: "Images"})
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
	server := newAuthenticatedChatServer(t, Deps{
		Chat:       chatStore,
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
	messages, found, err := chatStore.ListMessages(context.Background(), user.ID, thread.ID)
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

func TestStreamMessageRequiresGenerateImageForObviousImageRequest(t *testing.T) {
	llmClient := &fakeToolChatClient{results: []llm.StreamResult{{Content: "I am a text-based AI assistant and cannot generate images."}}}
	store := &fakeChatStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Images"},
	}
	server := newAuthenticatedChatServer(t, Deps{
		Chat:       store,
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
	store := &fakeChatStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Images"},
	}
	server := newAuthenticatedChatServer(t, Deps{
		Chat:       store,
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
	store := &fakeChatStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Images"},
	}
	server := newAuthenticatedChatServer(t, Deps{
		Chat:       store,
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
	store := &fakeChatStore{
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
	server := newAuthenticatedChatServer(t, Deps{
		Chat:       store,
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
	store := &fakeChatStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"},
	}
	srv := newAuthenticatedChatServer(t, Deps{
		Chat: store,
		LLM:  fakeChatClient{},
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
	store := &fakeChatStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"},
	}
	srv := newAuthenticatedChatServer(t, Deps{
		Chat: store,
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

func TestStreamMessagePersistsAssistantAfterClientContextCancel(t *testing.T) {
	store := &fakeChatStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"},
	}
	var cancel context.CancelFunc
	srv := newAuthenticatedChatServer(t, Deps{
		Chat: store,
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
	store := &fakeChatStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"},
	}
	llmClient := &blockingChatClient{
		started: make(chan struct{}),
		done:    make(chan struct{}),
	}
	srv := newAuthenticatedChatServer(t, Deps{
		Chat: store,
		LLM:  llmClient,
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
	select {
	case <-streamDone:
	case <-time.After(time.Second):
		cancelStream()
		t.Fatal("stream handler did not return after stop")
	}
}

func TestStopStreamMessagePersistsPartialAssistantContent(t *testing.T) {
	store := &fakeChatStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"},
	}
	llmClient := &blockingChatClient{
		started:        make(chan struct{}),
		done:           make(chan struct{}),
		partialContent: "Partial answer",
	}
	srv := newAuthenticatedChatServer(t, Deps{
		Chat: store,
		LLM:  llmClient,
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
	store := &fakeChatStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"},
	}
	srv := newAuthenticatedChatServer(t, Deps{
		Chat: store,
		LLM:  fakeChatClient{streamText: &empty},
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
	store := &fakeChatStore{
		thread:   chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"},
		messages: []chat.Message{{ID: "old_1", ThreadID: "thr_1", Role: chat.RoleAssistant, Content: "Earlier answer"}},
	}
	user := testUser
	user.ResponseLanguage = "de"
	srv := newAuthenticatedChatServerForUser(t, user, Deps{
		Chat: store,
		LLM:  fakeChatClient{history: &history},
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
	store := &fakeChatStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"},
	}
	srv := newAuthenticatedChatServer(t, Deps{
		Chat: store,
		LLM:  fakeChatClient{history: &history},
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

func TestStreamMessageReturns503WhenLLMDependencyMissing(t *testing.T) {
	store := &fakeChatStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: chat.DefaultThreadTitle},
	}
	srv := newAuthenticatedChatServer(t, Deps{Chat: store})
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
	store := &fakeChatStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: chat.DefaultThreadTitle},
	}
	srv := newAuthenticatedChatServer(t, Deps{
		Chat: store,
		LLM:  fakeChatClient{titleErr: errors.New("title model unavailable")},
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
	store := &fakeChatStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"},
	}
	srv := newAuthenticatedChatServer(t, Deps{
		Chat: store,
		LLM:  fakeChatClient{title: "Greeting"},
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
	if !strings.Contains(body, `data: {"active":1,"configured":2}`) {
		t.Fatalf("SSE body missing mcp_status payload:\n%s", body)
	}
}

func TestStreamMessageOmitsMcpStatusWhenNoneConfigured(t *testing.T) {
	store := &fakeChatStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"},
	}
	srv := newAuthenticatedChatServer(t, Deps{
		Chat: store,
		LLM:  fakeChatClient{title: "Greeting"},
		MCP:  fakeMCPService{},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Hi"}`)

	srv.ServeHTTP(rec, req)

	if strings.Contains(rec.Body.String(), "event: mcp_status") {
		t.Fatalf("SSE body should omit mcp_status when none configured:\n%s", rec.Body.String())
	}
}

func TestStreamMessageExecutesToolCallAndResumesAssistantStream(t *testing.T) {
	store := &fakeChatStore{
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
						Arguments: `{"q":"spark"}`,
					},
				}},
			},
			{Content: "I found Spark."},
		},
	}
	srv := newAuthenticatedChatServer(t, Deps{
		Chat: store,
		LLM:  llmClient,
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
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Search Spark"}`)

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"event: tool_call",
		`"name":"search__web"`,
		"event: tool_result",
		`"content":"search result"`,
		"event: assistant_delta",
		`data: {"content":"I found Spark."}`,
		"event: assistant_message",
		"event: done",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("SSE body missing %q:\n%s", want, body)
		}
	}
	if store.assistantContent != "I found Spark." {
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
	if got := lastHistory[len(lastHistory)-2]; got.Role != "assistant" || got.ReasoningContent != "I should search first." {
		t.Fatalf("assistant tool-call history = %#v, want preserved reasoning content", got)
	}
}

func TestStreamMessageRecoversFromToolError(t *testing.T) {
	store := &fakeChatStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"},
	}
	llmClient := &fakeToolChatClient{
		results: []llm.StreamResult{
			{ToolCalls: []llm.ToolCall{{
				ID: "call_1",
				Function: llm.ToolCallFunction{
					Name:      "search__web",
					Arguments: `{"q":"spark"}`,
				},
			}}},
			{Content: "The search tool failed, but I can continue."},
		},
	}
	srv := newAuthenticatedChatServer(t, Deps{
		Chat: store,
		LLM:  llmClient,
		MCP: fakeMCPService{
			tools: []llm.Tool{{Type: "function", Function: llm.ToolFunction{Name: "search__web"}}},
			err:   errFakeTool,
		},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Search Spark"}`)

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
	store := &fakeChatStore{
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
	srv := newAuthenticatedChatServer(t, Deps{
		Chat: store,
		LLM:  &fakeToolChatClient{results: []llm.StreamResult{{ToolCalls: toolCalls}}},
		MCP:  fakeMCPService{tools: []llm.Tool{{Type: "function", Function: llm.ToolFunction{Name: "search__web"}}}},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Search Spark"}`)

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
	store := &fakeChatStore{
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
	srv := newAuthenticatedChatServer(t, Deps{
		Chat: store,
		LLM:  llmClient,
		MCP: fakeMCPService{
			tools:  []llm.Tool{{Type: "function", Function: llm.ToolFunction{Name: "search__web"}}},
			result: "search result",
		},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Search Spark"}`)

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
	store := &fakeChatStore{
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
	srv := newAuthenticatedChatServer(t, Deps{
		Chat: store,
		LLM:  llmClient,
		MCP: fakeMCPService{
			tools:  []llm.Tool{{Type: "function", Function: llm.ToolFunction{Name: "search__web"}}},
			result: "search result",
		},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Search Spark"}`)

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
