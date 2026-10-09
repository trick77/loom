package httpapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/trick77/loom/internal/artifact"
	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/classifier"
	"github.com/trick77/loom/internal/docgen"
	"github.com/trick77/loom/internal/imagegen"
	"github.com/trick77/loom/internal/llm"
	"github.com/trick77/loom/internal/rag"
	"github.com/trick77/loom/internal/turn"
)

func TestStreamMessageEmitsDeltasAndPersistsAssistant(t *testing.T) {
	store := &fakeThreadStore{
		Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: chat.DefaultThreadTitle},
	}
	srv := newAuthenticatedServer(t, Deps{
		Thread: store,
		LLM:    fakeChatClient{Title: "# Albert Einstein 🧠⚛️ The legendary physicist"},
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
	// The title is generated from the answer, so its thread event necessarily
	// lands after the answer has streamed. Until then the sidebar shows the title
	// the thread was created with.
	threadEvent := strings.Index(body, "event: thread")
	assistantDelta := strings.Index(body, "event: assistant_delta")
	if threadEvent < 0 || assistantDelta < 0 || threadEvent < assistantDelta {
		t.Fatalf("thread title event index = %d, assistant delta index = %d, want title after assistant response:\n%s", threadEvent, assistantDelta, body)
	}
	if store.AssistantContent != "Hello" {
		t.Fatalf("assistantContent = %q, want Hello", store.AssistantContent)
	}
	if len(store.Messages) != 2 {
		t.Fatalf("persisted messages = %d, want 2", len(store.Messages))
	}
	if store.Messages[0].Role != chat.RoleUser || store.Messages[0].Content != "Hi" {
		t.Fatalf("first persisted message = %#v, want user Hi", store.Messages[0])
	}
	if store.Messages[1].Role != chat.RoleAssistant || store.Messages[1].Content != "Hello" {
		t.Fatalf("second persisted message = %#v, want assistant Hello", store.Messages[1])
	}
}

func TestStreamMessageTitlesFromTheAnswer(t *testing.T) {
	var seen string
	store := &fakeThreadStore{
		Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: chat.DefaultThreadTitle},
	}
	srv := newAuthenticatedServer(t, Deps{
		Thread: store,
		LLM:    fakeChatClient{Title: "Greeting", TitleAssistantSeen: &seen},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Hi"}`)

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	// The whole point of titling after the answer: the model sees the reply, not
	// just the question. Production passed "" here for as long as titling ran up
	// front, which is how an English thread ended up with a Chinese title.
	if seen != "Hello" {
		t.Fatalf("title gate saw assistant message %q, want %q", seen, "Hello")
	}
}

func TestStreamMessageKeepsExistingTitleWhenGenerationYieldsNothing(t *testing.T) {
	store := &fakeThreadStore{
		Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: chat.DefaultThreadTitle},
	}
	srv := newAuthenticatedServer(t, Deps{
		Thread: store,
		// An empty title is what the drift guard returns when the model answers in
		// a script the turn never used.
		LLM: fakeChatClient{Title: "", Category: "coding"},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Hi"}`)

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	// No title write at all — the thread keeps whatever it was created with
	// rather than being blanked to a placeholder.
	if store.UpdateThreadInput.Title != nil {
		t.Fatalf("UpdateThread title = %q, want no title write", *store.UpdateThreadInput.Title)
	}
	// The category is persisted by the pre-answer classifier and is unaffected.
	if store.UpdateThreadInput.Category == nil || *store.UpdateThreadInput.Category != "coding" {
		t.Fatalf("UpdateThread category = %#v, want coding", store.UpdateThreadInput.Category)
	}
}

func TestStreamMessageGeneratesTitleWhenThreadTitleIsFirstPrompt(t *testing.T) {
	store := &fakeThreadStore{
		Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Explain this document"},
	}
	srv := newAuthenticatedServer(t, Deps{
		Thread: store,
		LLM:    fakeChatClient{Title: "Document summary"},
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
		Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: chat.DefaultThreadTitle},
	}
	streamText := "Answer."
	srv := newAuthenticatedServer(t, Deps{
		Thread: store,
		LLM: fakeChatClient{
			StreamText:    &streamText,
			ReasoningText: "I should reason first.",
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
	if len(store.Messages) == 0 {
		t.Fatal("no messages persisted")
	}
	last := store.Messages[len(store.Messages)-1]
	if last.Role != chat.RoleAssistant || last.ReasoningContent != "I should reason first." {
		t.Fatalf("persisted assistant = %#v", last)
	}
}

// The title fills the wait for the answer, so it goes out before the first
// answer word even when the title call returns after the reasoning has ended.
func TestStreamMessageSendsReasoningTitleBeforeFirstAnswerDelta(t *testing.T) {
	gate := make(chan struct{})
	time.AfterFunc(100*time.Millisecond, func() { close(gate) })
	store := &fakeThreadStore{
		Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing"},
	}
	streamText := "Answer."
	srv := newAuthenticatedServer(t, Deps{
		Thread: store,
		LLM: fakeChatClient{
			StreamText:         &streamText,
			ReasoningText:      "Short thought.",
			ReasoningTitle:     "Thinking briefly",
			ReasoningTitleGate: gate,
		},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Hi"}`)

	srv.ServeHTTP(rec, req)

	body := rec.Body.String()
	titleAt := strings.Index(body, "event: assistant_reasoning_title")
	deltaAt := strings.Index(body, "event: assistant_delta")
	if titleAt == -1 || deltaAt == -1 || titleAt > deltaAt {
		t.Fatalf("title at %d, first answer delta at %d, want title first:\n%s", titleAt, deltaAt, body)
	}
}

// A title call that hangs holds the answer for Deps.ReasoningTitleHold, then the
// answer goes out without it.
func TestStreamMessageAnswerNotHeldPastReasoningTitleHold(t *testing.T) {
	gate := make(chan struct{})
	store := &fakeThreadStore{
		Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing"},
	}
	streamText := "Answer."
	srv := httptest.NewServer(newAuthenticatedServer(t, Deps{
		ReasoningTitleHold: 50 * time.Millisecond,
		Thread:             store,
		LLM: fakeChatClient{
			StreamText:         &streamText,
			ReasoningText:      "Short thought.",
			ReasoningTitle:     "Never arrives",
			ReasoningTitleGate: gate,
		},
	}))
	t.Cleanup(srv.Close)
	// After srv.Close in registration order, so it runs first: the handler
	// waits on the title before it returns, and Close waits on the handler.
	t.Cleanup(func() { close(gate) })
	req := authenticatedRequest(http.MethodPost, srv.URL+"/api/threads/thr_1/messages:stream", `{"content":"Hi"}`)
	req.RequestURI = ""
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	gotDelta := make(chan bool, 1)
	go func() {
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			if scanner.Text() == "event: assistant_delta" {
				gotDelta <- true
				return
			}
		}
		gotDelta <- false
	}()
	select {
	case ok := <-gotDelta:
		if !ok {
			t.Fatal("stream ended without an answer delta")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("answer held past reasoningTitleHold by a hung title call")
	}
}

// The first sweep line comes from the question, not the reasoning, so it can
// go out while the pre-answer gates are still running: the classifier is held
// until the working title has been read off the stream.
func TestStreamMessageSendsWorkingTitleWhileGatesRun(t *testing.T) {
	classifyGate := make(chan struct{})
	var release sync.Once
	store := &fakeThreadStore{Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Is 1001 prime?"}}
	srv := httptest.NewServer(newAuthenticatedServer(t, Deps{
		Thread: store,
		LLM:    fakeChatClient{WorkingTitle: "Checking whether 1001 is prime", ClassifyGate: classifyGate},
	}))
	t.Cleanup(srv.Close)
	// After srv.Close in registration order, so it runs first: Close waits on
	// the handler, which waits on the held classifier.
	t.Cleanup(func() { release.Do(func() { close(classifyGate) }) })
	req := authenticatedRequest(http.MethodPost, srv.URL+"/api/threads/thr_1/messages:stream", `{"content":"Is 1001 prime?"}`)
	req.RequestURI = ""
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	got := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			if scanner.Text() == "event: assistant_working_title" && scanner.Scan() {
				got <- scanner.Text()
				for scanner.Scan() {
				}
				return
			}
		}
		got <- ""
	}()
	select {
	case data := <-got:
		if data != `data: {"title":"Checking whether 1001 is prime"}` {
			t.Fatalf("working title event data = %q", data)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("working title held back behind the pre-answer gates")
	}
}

// The working title is worthless once the answer exists, so a slow working
// title call must not hold the finished answer unpersisted: assistant_message
// goes out while the call is still pending.
func TestStreamMessageAnswerNotHeldByWorkingTitle(t *testing.T) {
	gate := make(chan struct{})
	var release sync.Once
	store := &fakeThreadStore{Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing"}}
	srv := httptest.NewServer(newAuthenticatedServer(t, Deps{
		Thread: store,
		LLM:    fakeChatClient{WorkingTitle: "Greeting the user", WorkingTitleGate: gate},
	}))
	t.Cleanup(srv.Close)
	// After srv.Close in registration order, so it runs first: Close waits on
	// the handler, whose teardown waits on the held call.
	t.Cleanup(func() { release.Do(func() { close(gate) }) })
	req := authenticatedRequest(http.MethodPost, srv.URL+"/api/threads/thr_1/messages:stream", `{"content":"Hi"}`)
	req.RequestURI = ""
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	gotAnswer := make(chan bool, 1)
	go func() {
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			if scanner.Text() == "event: assistant_message" {
				gotAnswer <- true
				for scanner.Scan() {
				}
				return
			}
		}
		gotAnswer <- false
	}()
	select {
	case ok := <-gotAnswer:
		if !ok {
			t.Fatal("stream ended without an assistant message")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("finished answer held back by a pending working title call")
	}
}

// The working title is a call of the turn like the reasoning title: its cost
// lands on the answer.
func TestStreamMessageBooksWorkingTitleCost(t *testing.T) {
	store := &fakeThreadStore{Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing"}}
	srv := newAuthenticatedServer(t, Deps{
		Thread: store,
		LLM:    fakeChatClient{WorkingTitle: "Greeting the user", WorkingTitleCost: 7, Cost: 1000},
	})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Hi"}`))

	if len(store.Messages) != 2 {
		t.Fatalf("persisted messages = %d, want 2", len(store.Messages))
	}
	if cost := store.Messages[1].CostNanoUSD; cost == nil || *cost != 1007 {
		t.Fatalf("assistant CostNanoUSD = %v, want 1007 (answer and working title)", cost)
	}
}

// The reasoning title is the first thing the reader sees of a turn, so it must
// not wait for the model to stop thinking: once enough reasoning has streamed
// to name the subject, the title generates from it while the model thinks on.
func TestStreamMessageSendsReasoningTitleWhileStillReasoning(t *testing.T) {
	hold := make(chan struct{})
	var release sync.Once
	seen := make(chan string, 4)
	store := &fakeThreadStore{
		Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing"},
	}
	streamText := "Answer."
	srv := httptest.NewServer(newAuthenticatedServer(t, Deps{
		ReasoningTitleStartBytes: 20,
		Thread:                   store,
		LLM: fakeChatClient{
			StreamText:         &streamText,
			ReasoningDeltas:    []string{"The user asks why the sky is blue.", " Rayleigh scattering explains it."},
			ReasoningHold:      hold,
			ReasoningTitle:     "Explaining the blue sky",
			ReasoningTitleSeen: seen,
		},
	}))
	t.Cleanup(srv.Close)
	// After srv.Close in registration order, so it runs first: Close waits on
	// the handler, which waits on the held reasoning.
	t.Cleanup(func() { release.Do(func() { close(hold) }) })
	req := authenticatedRequest(http.MethodPost, srv.URL+"/api/threads/thr_1/messages:stream", `{"content":"Why is the sky blue?"}`)
	req.RequestURI = ""
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	gotTitle := make(chan bool, 1)
	go func() {
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			if scanner.Text() == "event: assistant_reasoning_title" {
				gotTitle <- true
				// Drain so the handler can finish once the reasoning resumes.
				for scanner.Scan() {
				}
				return
			}
		}
		gotTitle <- false
	}()
	select {
	case ok := <-gotTitle:
		if !ok {
			t.Fatal("stream ended without a reasoning title")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("reasoning title held back until the model stopped reasoning")
	}
	release.Do(func() { close(hold) })
	if got := <-seen; got != "The user asks why the sky is blue." {
		t.Fatalf("title generated from %q, want the reasoning streamed so far", got)
	}
	// The round is titled once: the boundary at the first answer word finds it
	// already spawned.
	select {
	case got := <-seen:
		t.Fatalf("second reasoning title call with %q, want one per round", got)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestStreamMessageEmitsAndPersistsReasoningTitle(t *testing.T) {
	store := &fakeThreadStore{
		Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing"},
	}
	streamText := "Answer."
	srv := newAuthenticatedServer(t, Deps{
		Thread: store,
		LLM: fakeChatClient{
			StreamText:     &streamText,
			ReasoningText:  "The user wants the latest sources, so I will search.",
			ReasoningTitle: "Searching current sources",
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
	if len(store.Messages) == 0 {
		t.Fatal("no messages persisted")
	}
	last := store.Messages[len(store.Messages)-1]
	if !strings.Contains(string(last.ActivityTrace), `"title":"Searching current sources"`) {
		t.Fatalf("persisted activity trace missing title: %s", last.ActivityTrace)
	}
}

func TestStreamMessageAlignsReasoningTitlesAcrossRounds(t *testing.T) {
	store := &fakeThreadStore{
		Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing"},
	}
	llmClient := &fakeToolChatClient{
		Results: []llm.StreamResult{
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
		TitleFor: func(reasoning string) string {
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
			ToolList: []llm.Tool{{
				Type:     "function",
				Function: llm.ToolFunction{Name: "search__web", Description: "Search", Parameters: map[string]any{"type": "object"}},
			}},
			Result: "search result",
		},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Search"}`)

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if len(store.Messages) == 0 {
		t.Fatal("no messages persisted")
	}
	last := store.Messages[len(store.Messages)-1]
	var trace []turn.ActivityTraceEvent
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
	// forces a tool-free final answer. A tool-eager model answers that with another
	// inline tool call, which is stripped — leaving the content empty. The turn must not persist
	// an empty (or raw-XML) message: a fallback answer is substituted instead.
	db := newUserDB(t)
	threadStore := chat.NewStore(db)
	artifactStore := artifact.NewStore(db)
	user := testUser
	thread, err := threadStore.CreateThread(context.Background(), user.ID, chat.CreateThreadInput{Title: "Fallback"})
	if err != nil {
		t.Fatal(err)
	}

	llmClient := &fakeToolChatClient{
		Results: []llm.StreamResult{
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
		Plain: "", // every tool-free call (final + retry) returns empty, as if the inline XML was stripped
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
	db := newUserDB(t)
	threadStore := chat.NewStore(db)
	artifactStore := artifact.NewStore(db)
	user := testUser
	thread, err := threadStore.CreateThread(context.Background(), user.ID, chat.CreateThreadInput{Title: "Artifacts"})
	if err != nil {
		t.Fatal(err)
	}

	llmClient := &fakeToolChatClient{
		Results: []llm.StreamResult{
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
	db := newUserDB(t)
	threadStore := chat.NewStore(db)
	artifactStore := artifact.NewStore(db)
	user := testUser
	thread, err := threadStore.CreateThread(context.Background(), user.ID, chat.CreateThreadInput{Title: "Images"})
	if err != nil {
		t.Fatal(err)
	}

	llmClient := &fakeToolChatClient{
		ImageIntent: llm.ImageIntent{Action: llm.ImageIntentCreate},
		Results: []llm.StreamResult{
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
		Plain: "Created robot.png.",
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
	db := newUserDB(t)
	threadStore := chat.NewStore(db)
	artifactStore := artifact.NewStore(db)
	user := testUser
	thread, err := threadStore.CreateThread(context.Background(), user.ID, chat.CreateThreadInput{Title: "Images"})
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

// A stop after the image exists but before the final prose starts must still
// persist the turn with its artifact, not drop the paid-for image from the
// transcript.
func TestStreamMessagePersistsImageWhenFinalResponseIsCanceled(t *testing.T) {
	db := newUserDB(t)
	threadStore := chat.NewStore(db)
	user := testUser
	thread, err := threadStore.CreateThread(context.Background(), user.ID, chat.CreateThreadInput{Title: "Images"})
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
					Arguments: `{"prompt":"a small robot","filename":"robot","width":512,"height":512,"output_format":"png"}`,
				},
			}},
		}},
		PlainErr: context.Canceled,
	}
	server := newAuthenticatedServer(t, Deps{
		Thread:     threadStore,
		Artifacts:  artifact.NewStore(db),
		ImageTools: []imagegen.Tool{imagegen.NewTool(fakeImageProvider{})},
		UsersDir:   t.TempDir(),
		LLM:        llmClient,
	})

	req := authenticatedRequest(http.MethodPost, "/api/threads/"+thread.ID+"/messages:stream", `{"content":"make an image"}`)
	server.ServeHTTP(httptest.NewRecorder(), req)

	messages, _, err := threadStore.ListMessages(context.Background(), user.ID, thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	var assistant chat.Message
	for _, message := range messages {
		if message.Role == chat.RoleAssistant {
			assistant = message
		}
	}
	if !bytes.Contains(assistant.Artifacts, []byte("image/png")) {
		t.Fatalf("assistant not persisted with its image after cancel: messages=%d artifacts=%s", len(messages), assistant.Artifacts)
	}
}

func TestStreamMessageGeneratesAtMostOneImagePerTurn(t *testing.T) {
	db := newUserDB(t)
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
		Results: []llm.StreamResult{
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

func TestStreamMessageGeneratesFromUserTextWhenCompilerRefuses(t *testing.T) {
	llmClient := &fakeToolChatClient{
		ImageIntent: llm.ImageIntent{Action: llm.ImageIntentCreate},
		Results:     []llm.StreamResult{{Content: "I am a text-based AI assistant and cannot generate images."}},
		Plain:       "Created the image.",
		TitleResult: "Glass City At Sunrise",
	}
	store := &fakeThreadStore{
		Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: chat.DefaultThreadTitle},
	}
	provider := &recordingImageProvider{}
	server := newAuthenticatedServer(t, Deps{
		Thread:     store,
		Artifacts:  fakeArtifactStore{},
		ImageTools: []imagegen.Tool{imagegen.NewTool(provider)},
		UsersDir:   t.TempDir(),
		LLM:        llmClient,
		MCP: fakeMCPService{ToolList: []llm.Tool{{
			Type:     "function",
			Function: llm.ToolFunction{Name: "search__web", Description: "Search the web"},
		}}},
	})

	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"generate an image of a glass city at sunrise"}`)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	body := rec.Body.String()
	// The compiler refused, so the image is generated from the user's own words
	// rather than the turn ending with nothing to show.
	if !strings.Contains(body, "event: artifact") {
		t.Fatalf("SSE body missing fallback image artifact:\n%s", body)
	}
	if got := provider.request.Prompt; got != "generate an image of a glass city at sunrise" {
		t.Fatalf("fallback prompt = %q, want the user's own message", got)
	}
	// No filename is sent, leaving the provider to derive one from the prompt with
	// a character set the artifact store can keep.
	if got := provider.request.Filename; got != "" {
		t.Fatalf("fallback filename = %q, want none", got)
	}
	// The fallback leaves through the normal answered-turn path, so the thread is
	// still named — a refusal must not cost the turn its title.
	if store.Thread.Title != "Glass City At Sunrise" {
		t.Fatalf("thread title = %q, want the generated title", store.Thread.Title)
	}
	if len(llmClient.Tools) == 0 {
		t.Fatal("no tool round was run")
	}
	offeredTools := llmClient.Tools[0]
	if len(offeredTools) != 1 || offeredTools[0].Function.Name != "generate_image" {
		t.Fatalf("offered tools = %#v, want only generate_image", offeredTools)
	}
	if len(llmClient.Histories) == 0 {
		t.Fatal("LLM history was not captured")
	}
	foundDirective := false
	for _, message := range llmClient.Histories[0] {
		if message.Role == "system" &&
			strings.Contains(message.Content, "Your only job is to call `generate_image` exactly once") &&
			strings.Contains(message.Content, "Do not refuse based on being text-based") {
			foundDirective = true
		}
	}
	if !foundDirective {
		t.Fatalf("history missing forced image compiler directive: %#v", llmClient.Histories[0])
	}
}

func TestStreamMessageReturnsImageToolFailureAsStreamError(t *testing.T) {
	llmClient := &fakeToolChatClient{
		ImageIntent: llm.ImageIntent{Action: llm.ImageIntentCreate},
		Results: []llm.StreamResult{{
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
		Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Images"},
	}
	server := newAuthenticatedServer(t, Deps{
		Thread:     store,
		Artifacts:  fakeArtifactStore{},
		ImageTools: []imagegen.Tool{imagegen.NewTool(errorImageProvider{err: errors.New("fal generation timed out: context deadline exceeded")})},
		UsersDir:   t.TempDir(),
		LLM:        llmClient,
	})

	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"make an image"}`)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, `"error":"tool failed: fal generation timed out: context deadline exceeded"`) {
		t.Fatalf("SSE body missing provider failure error:\n%s", body)
	}
	if store.AssistantContent != "" {
		t.Fatalf("assistantContent = %q, want no persisted assistant after image failure", store.AssistantContent)
	}
}

func TestStreamMessageDoesNotStreamTextBeforeRequiredImageToolCall(t *testing.T) {
	llmClient := &fakeToolChatClient{
		ImageIntent: llm.ImageIntent{Action: llm.ImageIntentCreate},
		Results: []llm.StreamResult{
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
		Plain: "Created robot.png.",
	}
	store := &fakeThreadStore{
		Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Images"},
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

func TestStreamMessageGeneratesFromUserTextWhenImageFollowUpIsTextOnly(t *testing.T) {
	textOnlyImageClaim := "Here's your trick77 logo in full cyberpunk style."
	var history []llm.Message
	store := &fakeThreadStore{
		Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Images"},
		Messages: []chat.Message{{
			ID:        "old_1",
			ThreadID:  "thr_1",
			Role:      chat.RoleAssistant,
			Content:   "Created a logo.",
			Artifacts: json.RawMessage(`[{"id":"art_1","displayFilename":"generated-image.png","mimeType":"image/png","downloadUrl":"/api/artifacts/art_1/download"}]`),
		}},
	}
	capturingLLM := &fakeToolChatClient{
		ImageIntent: llm.ImageIntent{Action: llm.ImageIntentEdit},
		Results:     []llm.StreamResult{{Content: textOnlyImageClaim}},
		Plain:       "Created the image.",
	}
	provider := &recordingImageProvider{}
	server := newAuthenticatedServer(t, Deps{
		Thread:     store,
		Artifacts:  fakeArtifactStore{},
		ImageTools: []imagegen.Tool{imagegen.NewTool(provider)},
		UsersDir:   t.TempDir(),
		LLM:        capturingLLM,
	})

	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"make it cyberpunk"}`)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	body := rec.Body.String()
	// A text-only claim is never accepted as an image; the fallback generates one
	// from the user's own words instead of persisting the claim.
	if !strings.Contains(body, "event: artifact") {
		t.Fatalf("SSE body missing fallback image artifact:\n%s", body)
	}
	if strings.Contains(store.AssistantContent, textOnlyImageClaim) {
		t.Fatalf("assistantContent = %q, want no persisted text-only image claim", store.AssistantContent)
	}
	if got := provider.request.Prompt; got != "make it cyberpunk" {
		t.Fatalf("fallback prompt = %q, want the user's own message", got)
	}
	if len(capturingLLM.Histories) == 0 {
		t.Fatal("LLM history was not captured")
	}
	history = capturingLLM.Histories[0]
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
		Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"},
	}
	srv := newAuthenticatedServer(t, Deps{
		Thread: store,
		LLM:    fakeChatClient{},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Hi"}`)

	srv.ServeHTTP(rec, req)

	if len(store.Messages) != 2 {
		t.Fatalf("persisted messages = %d, want 2", len(store.Messages))
	}
	if string(store.Messages[1].Artifacts) != "[]" {
		t.Fatalf("assistant artifacts = %s, want []", store.Messages[1].Artifacts)
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
		Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Images"},
	}
	srv := newAuthenticatedServer(t, Deps{
		Thread: store,
		Artifacts: fakeArtifactStore{Artifacts: []artifact.Artifact{{
			ID:              "art_image",
			UserID:          testUser.ID,
			ThreadID:        "thr_1",
			DisplayFilename: "screenshot.png",
			VolumeRelPath:   imageRel,
			MIMEType:        "image/png",
			SizeBytes:       int64(len(imageBytes)),
		}}},
		UsersDir: usersDir,
		LLM:      fakeChatClient{History: &history},
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

// recordingImageProvider is fakeImageProvider that keeps the request it was
// given, so a test can assert what the fallback actually asked the provider for.
type recordingImageProvider struct {
	request imagegen.GenerateRequest
}

func (p *recordingImageProvider) Generate(ctx context.Context, req imagegen.GenerateRequest) (imagegen.GenerateResult, error) {
	p.request = req
	return fakeImageProvider{}.Generate(ctx, req)
}

type errorImageProvider struct {
	err error
}

func (f errorImageProvider) Generate(context.Context, imagegen.GenerateRequest) (imagegen.GenerateResult, error) {
	return imagegen.GenerateResult{}, f.err
}

func TestStreamMessagePersistsAssistantTokenUsage(t *testing.T) {
	store := &fakeThreadStore{
		Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"},
	}
	srv := newAuthenticatedServer(t, Deps{
		Thread: store,
		LLM: fakeChatClient{Usage: llm.TokenUsage{
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

	if len(store.Messages) != 2 {
		t.Fatalf("persisted messages = %d, want 2", len(store.Messages))
	}
	assistant := store.Messages[1]
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
		Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: chat.DefaultThreadTitle},
	}
	recorder := &recordingUsageStore{}
	srv := newAuthenticatedServer(t, Deps{
		Thread: store,
		Usage:  recorder,
		LLM: fakeChatClient{
			Title:          "Fresh title",
			ReasoningTitle: "Explaining things",
			ReasoningText:  "Let me think about this.",
			Usage: llm.TokenUsage{
				PromptTokens: 7, CompletionTokens: 3, TotalTokens: 10,
				PromptTokensDetails:    llm.PromptTokenDetails{CachedTokens: 5},
				CompletionTokenDetails: llm.CompletionTokenDetails{ReasoningTokens: 2},
			},
			// Reasoning-title call runs with thinking disabled: cost is almost all
			// prompt (it re-sends the whole reasoning), no reasoning tokens.
			ReasoningTitleUsage: llm.TokenUsage{PromptTokens: 100, CompletionTokens: 1, TotalTokens: 101},
			TitleUsage:          llm.TokenUsage{PromptTokens: 20, CompletionTokens: 4, TotalTokens: 24},
			Cost:                1000,
			ReasoningTitleCost:  100,
			TitleCost:           10,
		},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Hi"}`)

	srv.ServeHTTP(rec, req)

	if len(store.Messages) != 2 {
		t.Fatalf("persisted messages = %d, want 2", len(store.Messages))
	}
	assistant := store.Messages[1]
	// Every call of the turn is on the message's cost, the thread title too:
	// it runs after the message is written and is added onto it afterwards, so
	// the thread's Σ counts it.
	if assistant.CostNanoUSD == nil || *assistant.CostNanoUSD != 1110 {
		t.Fatalf("message CostNanoUSD = %v, want 1110 (answer, reasoning title and thread title)", assistant.CostNanoUSD)
	}
	// 7+100 prompt, 3+1 completion, 10+101 total: the answer turn plus the
	// reasoning-title helper, which runs during the stream. The thread-title
	// helper is NOT here — it runs after the assistant message is persisted, so
	// that a slow title endpoint can never hold a streamed answer unpersisted.
	// Its tokens are asserted against the lifetime rollup below instead.
	//
	// The accumulator sums cached/reasoning detail fields too; here only the
	// answer turn sets them in the fakes (the helpers leave them 0), so the totals
	// stay 5 and 2 — this is the test data, not a code limitation.
	for _, c := range []struct {
		name string
		got  int
		want int
	}{
		{"PromptTokens", derefInt(assistant.PromptTokens), 107},
		{"CompletionTokens", derefInt(assistant.CompletionTokens), 4},
		{"TotalTokens", derefInt(assistant.TotalTokens), 111},
		{"CachedTokens", derefInt(assistant.CachedTokens), 5},
		{"ReasoningTokens", derefInt(assistant.ReasoningTokens), 2},
	} {
		if c.got != c.want {
			t.Fatalf("%s = %d, want %d", c.name, c.got, c.want)
		}
	}

	// The lifetime rollup is read after titling, so it carries every helper:
	// 7+100+20 prompt, 3+1+4 completion, 10+101+24 total.
	if len(recorder.Deltas) != 1 {
		t.Fatalf("AddTokens calls = %d, want 1", len(recorder.Deltas))
	}
	delta := recorder.Deltas[0]
	for _, c := range []struct {
		name string
		got  int
		want int
	}{
		{"PromptTokens", delta.PromptTokens, 127},
		{"CompletionTokens", delta.CompletionTokens, 8},
		{"TotalTokens", delta.TotalTokens, 135},
	} {
		if c.got != c.want {
			t.Fatalf("lifetime %s = %d, want %d", c.name, c.got, c.want)
		}
	}
	if delta.CostNanoUSD != 1110 {
		t.Fatalf("lifetime CostNanoUSD = %d, want 1110 (answer, reasoning title and thread title)", delta.CostNanoUSD)
	}
	// The browser learns the settled figure live, before the stream ends: the
	// title ran after assistant_message went out.
	body := rec.Body.String()
	costEvent := "event: message_cost\ndata: {\"id\":\"msg_2\",\"costNanoUsd\":1110}"
	costAt, doneAt := strings.Index(body, costEvent), strings.Index(body, "event: done")
	if costAt == -1 || doneAt == -1 || costAt > doneAt {
		t.Fatalf("want %q before done, body:\n%s", costEvent, body)
	}
}

// A turn that fails before it has an answer still spent money: the rounds
// before the failure and the thread title. With no assistant message to carry
// it, the cost lands on the user message (the thread's Σ sums every message)
// and in the lifetime rollup.
func TestStreamMessageFailedTurnCostLandsOnTheUserMessage(t *testing.T) {
	store := &fakeThreadStore{
		Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: chat.DefaultThreadTitle},
	}
	recorder := &recordingUsageStore{}
	srv := newAuthenticatedServer(t, Deps{
		Thread: store,
		Usage:  recorder,
		LLM: fakeChatClient{
			Title:         "Fresh title",
			TitleCost:     10,
			StreamErr:     errors.New("upstream exploded"),
			StreamErrCost: 5,
		},
	})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Hi"}`))

	if len(store.Messages) != 1 {
		t.Fatalf("persisted messages = %d, want only the user message", len(store.Messages))
	}
	user := store.Messages[0]
	if user.CostNanoUSD == nil || *user.CostNanoUSD != 15 {
		t.Fatalf("user message CostNanoUSD = %v, want 15 (failed round and thread title)", user.CostNanoUSD)
	}
	var lifetime int64
	for _, d := range recorder.Deltas {
		lifetime += d.CostNanoUSD
	}
	if lifetime != 15 {
		t.Fatalf("lifetime cost = %d over %+v, want 15", lifetime, recorder.Deltas)
	}
	// The error goes out at once, not after the title call: when the upstream
	// is down the title fails too and would use up its whole timeout first.
	// The spend so far is reported just ahead of it (the client stops reading
	// at "error"); the title's cost reaches the message afterwards.
	body := rec.Body.String()
	costEvent := "event: message_cost\ndata: {\"id\":\"msg_1\",\"costNanoUsd\":5}"
	costAt, errorAt := strings.Index(body, costEvent), strings.Index(body, "event: error")
	titleAt := strings.Index(body, "Fresh title")
	if costAt == -1 || errorAt == -1 || costAt > errorAt {
		t.Fatalf("want %q before error, body:\n%s", costEvent, body)
	}
	if titleAt != -1 && titleAt < errorAt {
		t.Fatalf("thread title was sent before the error event, body:\n%s", body)
	}
}

// A reasoning-title call still in flight when the turn fails (it goes to the
// same dead upstream) must not hold the error back; its cost is booked later.
// The first answer word waits for the title, but only for Deps.ReasoningTitleHold.
func TestStreamMessageFailedTurnErrorDoesNotWaitForReasoningTitles(t *testing.T) {
	gate := make(chan struct{})
	store := &fakeThreadStore{
		Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"},
	}
	srv := httptest.NewServer(newAuthenticatedServer(t, Deps{
		ReasoningTitleHold: 50 * time.Millisecond,
		Thread:             store,
		LLM: fakeChatClient{
			ReasoningText:      "Let me think about this.",
			ReasoningTitle:     "Thinking",
			ReasoningTitleGate: gate,
			StreamErr:          errors.New("upstream exploded"),
			StreamErrDelta:     "Partial",
		},
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(gate) })

	req := authenticatedRequest(http.MethodPost, srv.URL+"/api/threads/thr_1/messages:stream", `{"content":"Hi"}`)
	req.RequestURI = ""
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	gotError := make(chan bool, 1)
	go func() {
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			if scanner.Text() == "event: error" {
				gotError <- true
				return
			}
		}
		gotError <- false
	}()
	select {
	case ok := <-gotError:
		if !ok {
			t.Fatal("stream ended without an error event")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("error event held back by an in-flight reasoning-title call")
	}
}

// The persisted stats must sum every tool round, not just the final answer turn.
func TestStreamMessageAggregatesTokenUsageAcrossToolRounds(t *testing.T) {
	store := &fakeThreadStore{
		Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"},
	}
	llmClient := &fakeToolChatClient{
		Results: []llm.StreamResult{
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
			ToolList: []llm.Tool{{
				Type: "function",
				Function: llm.ToolFunction{
					Name:        "search__web",
					Description: "Search the web",
					Parameters:  map[string]any{"type": "object"},
				},
			}},
			Result: "search result",
		},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Search Lume"}`)

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if len(store.Messages) < 2 {
		t.Fatalf("persisted messages = %#v, want assistant message", store.Messages)
	}
	assistant := store.Messages[len(store.Messages)-1]
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
		Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"},
	}
	var cancel context.CancelFunc
	srv := newAuthenticatedServer(t, Deps{
		Thread: store,
		LLM: fakeChatClient{AfterStream: func() {
			cancel()
		}},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Hi"}`)
	ctx, cancelRequest := context.WithCancel(req.Context())
	cancel = cancelRequest
	req = req.WithContext(ctx)

	srv.ServeHTTP(rec, req)

	if store.AssistantContent != "Hello" {
		t.Fatalf("assistantContent = %q, want Hello", store.AssistantContent)
	}
	if store.AssistantContextErr != nil {
		t.Fatalf("assistant AddMessage context error = %v, want nil", store.AssistantContextErr)
	}
}

func TestStopStreamMessageCancelsActiveAssistantTurn(t *testing.T) {
	store := &fakeThreadStore{
		Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"},
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

	t.Run("stop endpoint keeps source in reason", func(t *testing.T) {
		ctx, cancel := context.WithCancelCause(context.Background())
		cancel(stopCause("escape"))
		source, reason := streamCancelDetails(ctx)
		if source != "stop_endpoint" || reason != "stream stop requested (escape)" {
			t.Fatalf("details = %q %q, want stop endpoint with escape source", source, reason)
		}
	})
}

func TestStopCauseSanitizesAndWrapsSource(t *testing.T) {
	t.Run("wraps sanitized source but stays a stop cause", func(t *testing.T) {
		cause := stopCause("Escape KEY!\n")
		if !errors.Is(cause, errStreamStopRequested) {
			t.Fatalf("cause = %v, want errors.Is errStreamStopRequested", cause)
		}
		if cause.Error() != "stream stop requested (escapekey)" {
			t.Fatalf("cause = %q, want sanitized escapekey", cause.Error())
		}
	})

	t.Run("empty source yields the bare stop cause", func(t *testing.T) {
		if cause := stopCause("   "); !errors.Is(cause, errStreamStopRequested) {
			t.Fatalf("cause = %v, want bare errStreamStopRequested", cause)
		}
	})
}

func TestStopStreamMessagePersistsPartialAssistantContent(t *testing.T) {
	store := &fakeThreadStore{
		Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"},
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
	if store.AssistantContent != "Partial answer" {
		t.Fatalf("assistantContent = %q, want partial answer", store.AssistantContent)
	}
}

func TestStreamMessageRejectsEmptyAssistantResponse(t *testing.T) {
	empty := ""
	store := &fakeThreadStore{
		Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"},
	}
	srv := newAuthenticatedServer(t, Deps{
		Thread: store,
		LLM:    fakeChatClient{StreamText: &empty},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Hi"}`)

	srv.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, `"error":"empty assistant response"`) {
		t.Fatalf("SSE body missing empty response error:\n%s", body)
	}
	if len(store.Messages) != 1 || store.Messages[0].Role != chat.RoleUser {
		t.Fatalf("persisted messages = %#v, want only user message", store.Messages)
	}
}

func TestStreamMessageBuildsResponseLanguageHistory(t *testing.T) {
	var history []llm.Message
	store := &fakeThreadStore{
		Thread:   chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"},
		Messages: []chat.Message{{ID: "old_1", ThreadID: "thr_1", Role: chat.RoleAssistant, Content: "Earlier answer"}},
	}
	user := testUser
	user.ResponseLanguage = "de"
	srv := newAuthenticatedServerForUser(t, user, Deps{
		Thread: store,
		LLM:    fakeChatClient{History: &history},
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
	if !strings.Contains(history[0].Content, "Answer in German. If the user asks for a different language") {
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
		Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"},
	}
	srv := newAuthenticatedServer(t, Deps{
		Thread: store,
		LLM:    fakeChatClient{History: &history},
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
	if !strings.Contains(history[0].Content, "Use the browser navigation tool only when fetch cannot access useful content") {
		t.Fatalf("system prompt = %q, want browser fallback directive", history[0].Content)
	}
}

func TestStreamMessageSystemPromptDirectsToolsAtKnowledgeLimit(t *testing.T) {
	var history []llm.Message
	store := &fakeThreadStore{
		Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"},
	}
	srv := newAuthenticatedServer(t, Deps{
		Thread: store,
		LLM:    fakeChatClient{History: &history},
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
		Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title", Category: string(classifier.Brainstorming)},
	}
	srv := newAuthenticatedServer(t, Deps{
		Thread: store,
		LLM:    fakeChatClient{History: &history},
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
		Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: chat.DefaultThreadTitle},
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
		Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: chat.DefaultThreadTitle},
	}
	srv := newAuthenticatedServer(t, Deps{
		Thread: store,
		LLM:    fakeChatClient{TitleErr: errors.New("title model unavailable")},
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

func TestStreamMessageExecutesToolCallAndResumesAssistantStream(t *testing.T) {
	store := &fakeThreadStore{
		Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"},
	}
	llmClient := &fakeToolChatClient{
		Results: []llm.StreamResult{
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
			ToolList: []llm.Tool{{
				Type: "function",
				Function: llm.ToolFunction{
					Name:        "search__web",
					Description: "Search the web",
					Parameters:  map[string]any{"type": "object"},
				},
			}},
			Result: "search result",
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
	if store.AssistantContent != "I found Lume." {
		t.Fatalf("assistantContent = %q, want final answer", store.AssistantContent)
	}
	if len(llmClient.Histories) != 2 {
		t.Fatalf("stream calls = %d, want 2", len(llmClient.Histories))
	}
	lastHistory := llmClient.Histories[1]
	if len(lastHistory) < 3 {
		t.Fatalf("last history too short: %#v", lastHistory)
	}
	if got := lastHistory[len(lastHistory)-1]; got.Role != "tool" || got.ToolCallID != "call_1" || got.Content != "search result" {
		t.Fatalf("last history message = %#v, want tool result", got)
	}
	if got := lastHistory[len(lastHistory)-2]; got.Role != "assistant" || got.ReasoningContent != "" {
		t.Fatalf("assistant tool-call history = %#v, want reasoning omitted from provider history", got)
	}
	if len(store.Messages) < 2 {
		t.Fatalf("persisted messages = %#v, want assistant message", store.Messages)
	}
	trace := string(store.Messages[len(store.Messages)-1].ActivityTrace)
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

func TestStreamMessageRecoversFromToolError(t *testing.T) {
	store := &fakeThreadStore{
		Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"},
	}
	llmClient := &fakeToolChatClient{
		Results: []llm.StreamResult{
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
			ToolList: []llm.Tool{{Type: "function", Function: llm.ToolFunction{Name: "search__web"}}},
			Err:      errFakeTool,
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
	if store.AssistantContent != "The search tool failed, but I can continue." {
		t.Fatalf("assistantContent = %q, want recovered answer", store.AssistantContent)
	}
}

// When the model batches more calls of one tool than its per-round cap allows,
// loom must not abort the turn. It runs up to the cap and defers the rest with a
// tool result, then completes with a normal final answer.
func TestStreamMessageDefersDefaultToolCallsBeyondCap(t *testing.T) {
	store := &fakeThreadStore{
		Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"},
	}
	// Round 1 batches one search past the default per-round cap; round 2 concludes.
	round1 := make([]llm.ToolCall, maxToolCallsPerRound+1)
	for i := range round1 {
		round1[i] = llm.ToolCall{ID: "call_search", Function: llm.ToolCallFunction{Name: "search__web", Arguments: `{}`}}
	}
	llmClient := &fakeToolChatClient{Results: []llm.StreamResult{
		{ToolCalls: round1},
		{Content: "Final answer."},
	}}
	calls := map[string]int{}
	srv := newAuthenticatedServer(t, Deps{
		Thread: store,
		LLM:    llmClient,
		MCP: fakeMCPService{
			ToolList: []llm.Tool{{Type: "function", Function: llm.ToolFunction{Name: "search__web"}}},
			CallFunc: func(_ context.Context, name string, _ map[string]any) (string, error) {
				calls[name]++
				return "search result", nil
			},
		},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Search Lume"}`)

	srv.ServeHTTP(rec, req)

	body := rec.Body.String()
	if strings.Contains(body, "event: error") || strings.Contains(body, "too many tool calls") {
		t.Fatalf("SSE body must not abort on tool-call overflow:\n%s", body)
	}
	if calls["search__web"] != maxToolCallsPerRound {
		t.Fatalf("search__web executed %d times, want %d (one deferred)", calls["search__web"], maxToolCallsPerRound)
	}
	if !strings.Contains(body, "Deferred:") {
		t.Fatalf("SSE body missing deferred tool result:\n%s", body)
	}
	if store.AssistantContent != "Final answer." {
		t.Fatalf("assistantContent = %q, want final answer", store.AssistantContent)
	}
}

// The cheap fetch/obscura tools get a higher per-round cap than the default, so a
// paste with a dozen links runs them all in one round.
func TestStreamMessageDefersCheapToolCallsBeyondHigherCap(t *testing.T) {
	store := &fakeThreadStore{
		Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"},
	}
	round1 := make([]llm.ToolCall, cheapToolCallsPerRound+1)
	for i := range round1 {
		round1[i] = llm.ToolCall{ID: "call_fetch", Function: llm.ToolCallFunction{Name: fetchToolName, Arguments: `{"url":"https://example.com"}`}}
	}
	llmClient := &fakeToolChatClient{Results: []llm.StreamResult{
		{ToolCalls: round1},
		{Content: "Fetched the links."},
	}}
	calls := map[string]int{}
	var callsMu sync.Mutex // a round's fetches run concurrently
	srv := newAuthenticatedServer(t, Deps{
		Thread: store,
		LLM:    llmClient,
		MCP: fakeMCPService{
			ToolList: []llm.Tool{{Type: "function", Function: llm.ToolFunction{Name: fetchToolName}}},
			CallFunc: func(_ context.Context, name string, _ map[string]any) (string, error) {
				callsMu.Lock()
				defer callsMu.Unlock()
				calls[name]++
				return "page contents", nil
			},
		},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Fetch these"}`)

	srv.ServeHTTP(rec, req)

	body := rec.Body.String()
	if strings.Contains(body, "event: error") || strings.Contains(body, "too many tool calls") {
		t.Fatalf("SSE body must not abort on fetch overflow:\n%s", body)
	}
	if calls[fetchToolName] != cheapToolCallsPerRound {
		t.Fatalf("fetch executed %d times, want %d (higher cheap-tool cap, one deferred)", calls[fetchToolName], cheapToolCallsPerRound)
	}
	if !strings.Contains(body, "Deferred:") {
		t.Fatalf("SSE body missing deferred tool result:\n%s", body)
	}
	if store.AssistantContent != "Fetched the links." {
		t.Fatalf("assistantContent = %q, want final answer", store.AssistantContent)
	}
}

// A round's web fetches are independent network reads: they run concurrently,
// but their results reach the browser, the history and the [n] source numbering
// in the order the model issued them.
func TestStreamMessageRunsARoundsFetchesConcurrentlyInCallOrder(t *testing.T) {
	store := &fakeThreadStore{
		Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"},
	}
	urls := []string{"https://a.example/1", "https://b.example/2", "https://c.example/3"}
	round1 := make([]llm.ToolCall, len(urls))
	for i, u := range urls {
		round1[i] = llm.ToolCall{ID: fmt.Sprintf("call_%d", i+1), Function: llm.ToolCallFunction{Name: fetchToolName, Arguments: fmt.Sprintf(`{"url":%q}`, u)}}
	}
	llmClient := &fakeToolChatClient{Results: []llm.StreamResult{
		{ToolCalls: round1},
		{Content: "Fetched the links."},
	}}
	// Every fetch blocks until all of them are in flight, so a serial loop
	// would only get past the first one by timing out.
	var inFlight sync.WaitGroup
	inFlight.Add(len(urls))
	allStarted := make(chan struct{})
	go func() { inFlight.Wait(); close(allStarted) }()
	var serial atomic.Bool
	srv := newAuthenticatedServer(t, Deps{
		Thread: store,
		LLM:    llmClient,
		MCP: fakeMCPService{
			ToolList: []llm.Tool{{Type: "function", Function: llm.ToolFunction{Name: fetchToolName}}},
			CallFunc: func(_ context.Context, _ string, args map[string]any) (string, error) {
				inFlight.Done()
				select {
				case <-allStarted:
				case <-time.After(2 * time.Second):
					serial.Store(true)
				}
				url, _ := args["url"].(string)
				// The first call finishes last: completion order is the reverse of call order.
				time.Sleep(time.Duration(len(urls)-slices.Index(urls, url)) * 5 * time.Millisecond)
				return "page " + url, nil
			},
		},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Fetch these"}`)

	srv.ServeHTTP(rec, req)

	if serial.Load() {
		t.Fatal("fetches ran one after another, want them in flight together")
	}
	body := rec.Body.String()
	last := -1
	for i, u := range urls {
		at := strings.Index(body, fmt.Sprintf(`Web source [%d]: %s`, i+1, u))
		if at < 0 {
			t.Fatalf("missing tool result numbering %s as source [%d]:\n%s", u, i+1, body)
		}
		if at < last {
			t.Fatalf("tool results out of call order at %s:\n%s", u, body)
		}
		last = at
	}
	if store.AssistantContent != "Fetched the links." {
		t.Fatalf("assistantContent = %q, want final answer", store.AssistantContent)
	}
}

func TestStreamMessageUsesFinalNoToolCallAfterRoundExhaustion(t *testing.T) {
	store := &fakeThreadStore{
		Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"},
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
	llmClient := &fakeToolChatClient{Results: results, Plain: "Final answer without more tools."}
	srv := newAuthenticatedServer(t, Deps{
		Thread: store,
		LLM:    llmClient,
		MCP: fakeMCPService{
			ToolList: []llm.Tool{{Type: "function", Function: llm.ToolFunction{Name: "search__web"}}},
			Result:   "search result",
		},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Search Lume"}`)

	srv.ServeHTTP(rec, req)

	body := rec.Body.String()
	if strings.Contains(body, "empty assistant response") {
		t.Fatalf("SSE body returned empty response after round exhaustion:\n%s", body)
	}
	if store.AssistantContent != "Final answer without more tools." {
		t.Fatalf("assistantContent = %q, want final no-tool answer", store.AssistantContent)
	}
}

func TestStreamMessageForcesFinalAnswerWhenModelStopsEmptyAfterTools(t *testing.T) {
	store := &fakeThreadStore{
		Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"},
	}
	// Round 1 runs a tool; round 2 returns nothing (no tool call, no content) —
	// the model gave up without answering. The loop must force a final answer.
	llmClient := &fakeToolChatClient{
		Results: []llm.StreamResult{
			{ToolCalls: []llm.ToolCall{{ID: "call_1", Function: llm.ToolCallFunction{Name: "search__web", Arguments: `{}`}}}},
			{},
		},
		Plain: "Final answer after the tool ran.",
	}
	srv := newAuthenticatedServer(t, Deps{
		Thread: store,
		LLM:    llmClient,
		MCP: fakeMCPService{
			ToolList: []llm.Tool{{Type: "function", Function: llm.ToolFunction{Name: "search__web"}}},
			Result:   "search result",
		},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Search Lume"}`)

	srv.ServeHTTP(rec, req)

	body := rec.Body.String()
	if strings.Contains(body, "empty assistant response") {
		t.Fatalf("SSE body returned empty response after the model stopped empty:\n%s", body)
	}
	if store.AssistantContent != "Final answer after the tool ran." {
		t.Fatalf("assistantContent = %q, want forced final answer", store.AssistantContent)
	}
	// The forced final turn is a clean, tool-free synthesis: it nudges the model to
	// write its answer from the gathered notes, and offers no tools.
	lastHistory := llmClient.Histories[len(llmClient.Histories)-1]
	nudged := false
	for _, msg := range lastHistory {
		if strings.Contains(msg.Content, "write your complete final answer") {
			nudged = true
		}
	}
	if !nudged {
		t.Fatalf("final turn history missing the synthesis directive: %#v", lastHistory)
	}
	lastTools := llmClient.Tools[len(llmClient.Tools)-1]
	if len(lastTools) != 0 {
		t.Fatalf("final turn tools = %#v, want none (tool-free synthesis)", lastTools)
	}
}

// The forced final-answer turn is rebuilt as a clean synthesis: the
// tool-call/tool-result rounds are dropped and the gathered notes are inlined into
// the final user message, so the model answers in prose (like the utility calls)
// instead of reflexively emitting another tool call over a tool-saturated history.
func TestStreamMessageForcedFinalUsesCleanSynthesisHistory(t *testing.T) {
	store := &fakeThreadStore{
		Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"},
	}
	// Round 1 runs a tool; round 2 is empty (model gives up) → forced final turn.
	llmClient := &fakeToolChatClient{
		Results: []llm.StreamResult{
			{ToolCalls: []llm.ToolCall{{ID: "call_1", Function: llm.ToolCallFunction{Name: "search__web", Arguments: `{}`}}}},
			{},
		},
		Plain: "Synthesized answer from notes.",
	}
	srv := newAuthenticatedServer(t, Deps{
		Thread: store,
		LLM:    llmClient,
		MCP: fakeMCPService{
			ToolList: []llm.Tool{{Type: "function", Function: llm.ToolFunction{Name: "search__web"}}},
			Result:   "SEARCH_RESULT_NOTE",
		},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Compare things"}`)

	srv.ServeHTTP(rec, req)

	if store.AssistantContent != "Synthesized answer from notes." {
		t.Fatalf("assistantContent = %q, want the synthesized prose", store.AssistantContent)
	}
	final := llmClient.Histories[len(llmClient.Histories)-1]
	// No tool-call/tool-result turns survive into the synthesis history.
	for _, msg := range final {
		if msg.Role == "tool" || len(msg.ToolCalls) > 0 {
			t.Fatalf("synthesis history still carries tool turns: %#v", final)
		}
	}
	// The gathered notes are inlined into the final message.
	joined := ""
	for _, msg := range final {
		joined += msg.Content
	}
	if !strings.Contains(joined, "SEARCH_RESULT_NOTE") {
		t.Fatalf("synthesis history missing inlined notes: %#v", final)
	}
}

// On the first message of an image-generation thread, the category is stamped
// deterministically as image_generation, overriding (and skipping) the text
// classifier — even when the classifier would have returned something else.
func TestStreamMessageStampsImageGenerationCategory(t *testing.T) {
	db := newUserDB(t)
	threadStore := chat.NewStore(db)
	artifactStore := artifact.NewStore(db)
	user := testUser
	// DefaultThreadTitle so the first-message title+classify path runs.
	thread, err := threadStore.CreateThread(context.Background(), user.ID, chat.CreateThreadInput{Title: chat.DefaultThreadTitle})
	if err != nil {
		t.Fatal(err)
	}

	llmClient := &fakeToolChatClient{
		TitleResult: "Cat astronaut",
		// The image-intent gate routes this as a creation, which stamps the category.
		ImageIntent: llm.ImageIntent{Action: llm.ImageIntentCreate},
		// The classifier would say creative_writing; the override must win.
		ClassifyResult: string(classifier.CreativeWriting),
		Results: []llm.StreamResult{{
			ToolCalls: []llm.ToolCall{{
				ID:   "call_1",
				Type: "function",
				Function: llm.ToolCallFunction{
					Name:      "generate_image",
					Arguments: `{"prompt":"a cat astronaut","filename":"cat","width":512,"height":512,"output_format":"png"}`,
				},
			}},
		}},
		Plain: "Created cat.png.",
	}
	server := newAuthenticatedServer(t, Deps{
		Thread:     threadStore,
		Artifacts:  artifactStore,
		ImageTools: []imagegen.Tool{imagegen.NewTool(fakeImageProvider{})},
		UsersDir:   t.TempDir(),
		LLM:        llmClient,
	})

	req := authenticatedRequest(http.MethodPost, "/api/threads/"+thread.ID+"/messages:stream", `{"content":"create an image of a cat astronaut"}`)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	got, found, err := threadStore.GetThread(context.Background(), user.ID, thread.ID)
	if err != nil || !found {
		t.Fatalf("GetThread() found=%v err=%v", found, err)
	}
	if got.Category != string(classifier.ImageGeneration) {
		t.Fatalf("thread category = %q, want %q", got.Category, classifier.ImageGeneration)
	}
}

// Without image generation configured (no imageTools), imageArtifactRequired
// short-circuits, so an image-phrased prompt is classified normally and is never
// stamped image_generation.
func TestStreamMessageDoesNotStampImageGenerationWhenImageToolsAbsent(t *testing.T) {
	db := newUserDB(t)
	threadStore := chat.NewStore(db)
	user := testUser
	thread, err := threadStore.CreateThread(context.Background(), user.ID, chat.CreateThreadInput{Title: chat.DefaultThreadTitle})
	if err != nil {
		t.Fatal(err)
	}

	llmClient := &fakeToolChatClient{
		TitleResult:    "Cat astronaut",
		ClassifyResult: string(classifier.CreativeWriting),
		Results:        []llm.StreamResult{{Content: "A cat astronaut would look like..."}},
		Plain:          "A cat astronaut would look like...",
	}
	// No ImageTools dependency: image generation is unconfigured.
	server := newAuthenticatedServer(t, Deps{
		Thread: threadStore,
		LLM:    llmClient,
	})

	req := authenticatedRequest(http.MethodPost, "/api/threads/"+thread.ID+"/messages:stream", `{"content":"create an image of a cat astronaut"}`)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	got, found, err := threadStore.GetThread(context.Background(), user.ID, thread.ID)
	if err != nil || !found {
		t.Fatalf("GetThread() found=%v err=%v", found, err)
	}
	if got.Category != string(classifier.CreativeWriting) {
		t.Fatalf("thread category = %q, want %q (normal classification)", got.Category, classifier.CreativeWriting)
	}
}

// TestStreamMessageGatesToolsByCategory drives a full HTTP turn through
// classification, the tool gate, and availableTools, then inspects the exact
// tool array handed to the LLM — the end-to-end proof that a plain turn ships a
// trimmed set while a coding turn gets the coding-relevant tools. It exercises
// the real path a manual run would, but deterministically.
func TestStreamMessageGatesToolsByCategory(t *testing.T) {
	toolNames := func(tools []llm.Tool) map[string]bool {
		names := map[string]bool{}
		for _, tool := range tools {
			names[tool.Function.Name] = true
		}
		return names
	}
	newServer := func(_ string, llmClient *fakeToolChatClient) http.Handler {
		return newAuthenticatedServer(t, Deps{
			Thread:    &fakeThreadStore{Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: chat.DefaultThreadTitle}},
			Artifacts: fakeArtifactStore{},
			UsersDir:  t.TempDir(),
			DocTools:  []docgen.Generator{docgen.TextGenerator{}},
			LLM:       llmClient,
			MCP: fakeMCPService{
				ToolList: []llm.Tool{
					{Type: "function", Function: llm.ToolFunction{Name: "search__web", Description: "Search the web"}},
					{Type: "function", Function: llm.ToolFunction{Name: "context7__query-docs", Description: "Library docs"}},
				},
				ToolCategories: map[string][]string{"context7__query-docs": {string(classifier.Coding)}},
			},
		})
	}
	run := func(t *testing.T, category, content string) map[string]bool {
		t.Helper()
		llmClient := &fakeToolChatClient{
			ClassifyResult: category,
			TitleResult:    "T",
			Results:        []llm.StreamResult{{Content: "ok"}},
		}
		req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"`+content+`"}`)
		rec := httptest.NewRecorder()
		newServer(category, llmClient).ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
		}
		if len(llmClient.Tools) != 1 {
			t.Fatalf("tool rounds = %d, want 1; body=%s", len(llmClient.Tools), rec.Body.String())
		}
		return toolNames(llmClient.Tools[0])
	}

	t.Run("general turn ships a trimmed set", func(t *testing.T) {
		names := run(t, string(classifier.General), "please tell me a short friendly greeting")
		// Always-on core + category-neutral MCP remain.
		for _, want := range []string{conversationSearchToolName, "search__web"} {
			if !names[want] {
				t.Fatalf("general turn missing always-on tool %q; got %v", want, names)
			}
		}
		// Gated groups are withheld.
		if names["create_text_file"] {
			t.Fatalf("general turn should not offer docgen tools; got %v", names)
		}
		if names["context7__query-docs"] {
			t.Fatalf("general turn should not offer coding-tagged context7; got %v", names)
		}
	})

	t.Run("coding turn gets docgen and context7", func(t *testing.T) {
		names := run(t, string(classifier.Coding), "please help me understand this program")
		if !names["create_text_file"] {
			t.Fatalf("coding turn should offer docgen tools; got %v", names)
		}
		if !names["context7__query-docs"] {
			t.Fatalf("coding turn should offer coding-tagged context7; got %v", names)
		}
	})
}

// TestStreamMessageDriftReclassifiesContinuedTurn verifies the semantic drift
// gate: a continued turn on a thread whose sticky category is general (already
// titled, so no first-message classification runs) re-classifies THIS message,
// and when that classification is coding the coding-tagged context7 tools are
// injected — without any keyword lexicon.
func TestStreamMessageDriftReclassifiesContinuedTurn(t *testing.T) {
	llmClient := &fakeToolChatClient{
		ClassifyResult: string(classifier.Coding), // fresh per-turn classification
		Results:        []llm.StreamResult{{Content: "ok"}},
	}
	server := newAuthenticatedServer(t, Deps{
		// Non-default title + stored general category => no re-title, drift runs.
		Thread:    &fakeThreadStore{Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Weekend plans", Category: string(classifier.General)}},
		Artifacts: fakeArtifactStore{},
		UsersDir:  t.TempDir(),
		DocTools:  []docgen.Generator{docgen.TextGenerator{}},
		LLM:       llmClient,
		MCP: fakeMCPService{
			ToolList:       []llm.Tool{{Type: "function", Function: llm.ToolFunction{Name: "context7__query-docs"}}},
			ToolCategories: map[string][]string{"context7__query-docs": {string(classifier.Coding)}},
		},
	})

	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"wie behebe ich diese Fehlermeldung in meinem Code"}`)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if len(llmClient.Tools) != 1 {
		t.Fatalf("tool rounds = %d, want 1; body=%s", len(llmClient.Tools), rec.Body.String())
	}
	var hasContext7, hasDocgen bool
	for _, tool := range llmClient.Tools[0] {
		switch tool.Function.Name {
		case "context7__query-docs":
			hasContext7 = true
		case "create_text_file":
			hasDocgen = true
		}
	}
	if !hasContext7 {
		t.Fatal("drift-to-coding turn should offer context7 (via fresh classification, not keywords)")
	}
	if !hasDocgen {
		t.Fatal("drift-to-coding turn should offer docgen (coding is a docgen category)")
	}
}

// Attachment validation must run before the user message is persisted and
// before the SSE stream opens: a rejected send is a plain 400 JSON response,
// never a JSON blob inside a committed event stream with an orphaned user turn.
func TestStreamMessageRejectsBadImageAttachmentsBeforePersisting(t *testing.T) {
	deleted := artifact.Artifact{ID: "art_gone", UserID: testUser.ID, MIMEType: "image/png", VolumeRelPath: "files/uploads/gone.png", Deleted: true}
	tests := []struct {
		name      string
		body      string
		artifacts []artifact.Artifact
	}{
		{"too many", `{"content":"Hi","imageAttachmentIds":["a","b","c","d","e","f"]}`, nil},
		{"unknown id", `{"content":"Hi","imageAttachmentIds":["art_missing"]}`, nil},
		// The batch lookup returns soft-deleted rows for the transcript overlay;
		// they are not attachable and the 400 must not name the volume path.
		{"deleted", `{"content":"Hi","imageAttachmentIds":["art_gone"]}`, []artifact.Artifact{deleted}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &fakeThreadStore{Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Images"}}
			usersDir := t.TempDir()
			srv := newAuthenticatedServer(t, Deps{
				Thread:    store,
				Artifacts: fakeArtifactStore{Artifacts: tt.artifacts},
				UsersDir:  usersDir,
				LLM:       fakeChatClient{},
			})
			req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", tt.body)
			rec := httptest.NewRecorder()

			srv.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body:\n%s", rec.Code, rec.Body.String())
			}
			if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
				t.Fatalf("Content-Type = %q, want application/json", ct)
			}
			if strings.Contains(rec.Body.String(), "event:") {
				t.Fatalf("body carries SSE events:\n%s", rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), usersDir) {
				t.Fatalf("body leaks the volume path:\n%s", rec.Body.String())
			}
			if len(store.Messages) != 0 {
				t.Fatalf("persisted messages = %d, want 0", len(store.Messages))
			}
		})
	}
}

// A panic inside the reasoning-title goroutine must not kill the process or
// hang the turn: the title is skipped and the answer still lands.
func TestStreamMessageSurvivesReasoningTitlePanic(t *testing.T) {
	store := &fakeThreadStore{Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing"}}
	streamText := "Answer."
	srv := newAuthenticatedServer(t, Deps{
		Thread: store,
		LLM: fakeChatClient{
			StreamText:          &streamText,
			ReasoningText:       "Thinking about it.",
			ReasoningTitlePanic: true,
		},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Hi"}`)

	srv.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "event: assistant_message") || !strings.Contains(body, "event: done") {
		t.Fatalf("stream did not complete:\n%s", body)
	}
	if strings.Contains(body, "assistant_reasoning_title") {
		t.Fatalf("a title was emitted despite the panic:\n%s", body)
	}
}

// A panic after the stream has opened cannot become a 500 (the 200 and the
// first events are already on the wire); the client must still get a
// terminal error event instead of a silently truncated stream.
func TestStreamMessageEmitsErrorEventOnPanic(t *testing.T) {
	store := &fakeThreadStore{Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "T"}}
	srv := newAuthenticatedServer(t, Deps{Thread: store, LLM: fakeChatClient{StreamPanic: true}})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Hi"}`)

	srv.ServeHTTP(rec, req)

	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, "event: user_message") {
		t.Fatalf("stream did not open: status %d body:\n%s", rec.Code, body)
	}
	if !strings.HasSuffix(strings.TrimSpace(body), "event: error\ndata: {\"error\":\"internal server error\"}") {
		t.Fatalf("stream did not end with an error event:\n%s", body)
	}
}

// Titling runs after the answer; a rename the user made while the answer was
// streaming is newer than the snapshot the turn started from and must win.
func TestStreamMessageKeepsRenameMadeDuringStream(t *testing.T) {
	store := &fakeThreadStore{Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: chat.DefaultThreadTitle}}
	srv := newAuthenticatedServer(t, Deps{
		Thread: store,
		LLM: fakeChatClient{
			Title:       "Generated",
			AfterStream: func() { store.Thread.Title = "Mine" },
		},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Hi"}`)

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if store.Thread.Title != "Mine" {
		t.Fatalf("thread title = %q, want the rename kept", store.Thread.Title)
	}
	if strings.Contains(rec.Body.String(), `"title":"Generated"`) {
		t.Fatalf("stream announced the generated title over the rename:\n%s", rec.Body.String())
	}
}

// The image-intent gate and the first-turn classifier are both model round
// trips the answer waits on; they must overlap rather than add up. The image
// gate is held until the classifier has started, which the old order (image
// gate first, classifier after it) never reached.
func TestPrepareTurnRunsImageGateAndClassifierConcurrently(t *testing.T) {
	imageGate := make(chan struct{})
	classifyEntered := make(chan struct{}, 1)
	store := &fakeThreadStore{Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Hi"}}
	srv := newAuthenticatedServer(t, Deps{
		Thread:     store,
		Artifacts:  fakeArtifactStore{},
		ImageTools: []imagegen.Tool{imagegen.NewTool(fakeImageProvider{})},
		UsersDir:   t.TempDir(),
		LLM:        fakeChatClient{Category: "coding", ImageIntentGate: imageGate, ClassifyEntered: classifyEntered},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Hi"}`)

	done := make(chan struct{})
	go func() {
		srv.ServeHTTP(rec, req)
		close(done)
	}()
	select {
	case <-classifyEntered:
	case <-time.After(3 * time.Second):
		t.Fatal("the classifier did not start while the image-intent gate was pending")
	}
	close(imageGate)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("turn did not finish")
	}
	if got := store.UpdateThreadInput.Category; got == nil || *got != "coding" {
		t.Fatalf("persisted category = %v, want the classifier's", got)
	}
}

// The classifier runs alongside the image gate, so on an image turn its guess
// is already in; the image category must still win, as it did when the
// classifier was skipped for image turns.
func TestPrepareTurnImageTurnKeepsImageCategoryOverClassifier(t *testing.T) {
	store := &fakeThreadStore{Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Draw"}}
	srv := newAuthenticatedServer(t, Deps{
		Thread:     store,
		Artifacts:  fakeArtifactStore{},
		ImageTools: []imagegen.Tool{imagegen.NewTool(fakeImageProvider{})},
		UsersDir:   t.TempDir(),
		LLM: fakeChatClient{
			Category:    "coding",
			ImageIntent: llm.ImageIntent{Action: llm.ImageIntentCreate},
		},
	})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Draw a robot"}`))

	if got := store.UpdateThreadInput.Category; got == nil || *got != string(classifier.ImageGeneration) {
		t.Fatalf("persisted category = %v, want %q", got, classifier.ImageGeneration)
	}
}

// The pre-answer loads (drift classification, user and project context, the
// document chain) are independent and each may be a slow round trip; they must
// overlap. The classifier is held open until the attached document's text has
// been requested, which the old sequential order never reached.
func TestPrepareTurnLoadsContextsConcurrently(t *testing.T) {
	classifyGate := make(chan struct{})
	fullTextEntered := make(chan struct{}, 1)
	store := &fakeThreadStore{
		Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing"},
		// A prior turn: the thread is not freshly classified, so the drift
		// classifier runs for this message.
		Messages: []chat.Message{{ID: "m0", ThreadID: "thr_1", Role: chat.RoleUser, Content: "earlier"}},
	}
	docs := &fakeDocumentService{
		Doc:             rag.Document{ID: "d1", ThreadID: strPtr("thr_1"), Filename: "notes.txt", Status: rag.StatusEmbedded},
		Text:            "notes",
		FullTextEntered: fullTextEntered,
	}
	srv := newAuthenticatedServer(t, Deps{Thread: store, Documents: docs, LLM: fakeChatClient{ClassifyGate: classifyGate}})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Summarize","documentAttachmentIds":["d1"]}`)

	done := make(chan struct{})
	go func() {
		srv.ServeHTTP(rec, req)
		close(done)
	}()
	select {
	case <-fullTextEntered:
	case <-time.After(3 * time.Second):
		t.Fatal("the attached document was not loaded while the drift classifier was still pending")
	}
	close(classifyGate)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("turn did not finish")
	}
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "event: assistant_message") {
		t.Fatalf("status = %d body:\n%s", rec.Code, rec.Body.String())
	}
}
