package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

var sessionIDPattern = regexp.MustCompile(`^ses_[0-9a-f]{12}[0-9a-zA-Z]{14}$`)

func TestNewSessionIDShape(t *testing.T) {
	id := newSessionID()
	if !sessionIDPattern.MatchString(id) {
		t.Fatalf("session id %q does not match ses_<12 hex><14 base62>", id)
	}
	if other := newSessionID(); other == id {
		t.Fatalf("consecutive session ids collided: %q", id)
	}
}

func TestChatSessionIDIsStablePerThread(t *testing.T) {
	first := chatSessionID("thread-a")
	if again := chatSessionID("thread-a"); again != first {
		t.Fatalf("session id changed for the same thread: %q then %q", first, again)
	}
	if other := chatSessionID("thread-b"); other == first {
		t.Fatalf("different threads share a session id: %q", other)
	}
	if !sessionIDPattern.MatchString(first) {
		t.Fatalf("thread session id %q does not match expected shape", first)
	}
}

func TestChatSessionIDFallsBackToProcessID(t *testing.T) {
	if got := chatSessionID(""); got != processSessionID {
		t.Fatalf("threadless turn used %q, want the per-process id %q", got, processSessionID)
	}
}

// TestChatUserAgentValue pins the exact User-Agent string. The header test below
// compares against the constant, so it would happily pass on any value; the
// upstream cares about this specific client string, so assert the literal.
func TestChatUserAgentValue(t *testing.T) {
	const want = "opencode/1.18.11 ai-sdk/openai-compatible/3.0.20 ai-sdk/provider-utils/5.0.18 runtime/bun/1.3.14"
	if chatUserAgent != want {
		t.Fatalf("chatUserAgent = %q, want %q", chatUserAgent, want)
	}
}

// TestChatRequestNeverSendsGoDefaultUserAgent guards the failure this whole
// change exists to prevent: net/http silently reinstating its own UA if the
// header is ever dropped from executeChatRequestImpl.
func TestChatRequestNeverSendsGoDefaultUserAgent(t *testing.T) {
	var got string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	t.Cleanup(server.Close)

	client := NewClient(Config{BaseURL: server.URL + "/v1"}, server.Client())
	if _, err := client.StreamChat(context.Background(), []Message{{Role: "user", Content: "Hi"}}, func(string) error { return nil }); err != nil {
		t.Fatalf("StreamChat() error: %v", err)
	}
	if strings.HasPrefix(got, "Go-http-client") || got == "" {
		t.Fatalf("User-Agent = %q, want the configured client string", got)
	}
}

func TestChatRequestSendsSessionHeaders(t *testing.T) {
	var got http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	t.Cleanup(server.Close)

	client := NewClient(Config{BaseURL: server.URL + "/v1", APIKey: "secret"}, server.Client())
	ctx := WithInferenceMetadata(context.Background(), InferenceMetadata{ThreadID: "thread-headers"})
	if _, err := client.StreamChat(ctx, []Message{{Role: "user", Content: "Hi"}}, func(string) error { return nil }); err != nil {
		t.Fatalf("StreamChat() error: %v", err)
	}

	if ua := got.Get("User-Agent"); ua != chatUserAgent {
		t.Fatalf("User-Agent = %q, want %q", ua, chatUserAgent)
	}
	if accept := got.Get("Accept"); accept != "*/*" {
		t.Fatalf("Accept = %q, want */*", accept)
	}
	want := chatSessionID("thread-headers")
	if id := got.Get("X-Session-Id"); id != want {
		t.Fatalf("X-Session-Id = %q, want %q", id, want)
	}
	if affinity := got.Get("x-session-affinity"); affinity != want {
		t.Fatalf("x-session-affinity = %q, want %q", affinity, want)
	}
}

func TestChatSessionIDCacheIsBounded(t *testing.T) {
	sessionCache.Lock()
	sessionCache.byThread = map[string]string{}
	sessionCache.order = nil
	sessionCache.Unlock()

	for i := 0; i < sessionCacheLimit+10; i++ {
		chatSessionID(string(rune('a'+i%26)) + string(rune(i)))
	}

	sessionCache.Lock()
	defer sessionCache.Unlock()
	if len(sessionCache.byThread) > sessionCacheLimit {
		t.Fatalf("cache grew to %d entries, limit is %d", len(sessionCache.byThread), sessionCacheLimit)
	}
	if len(sessionCache.order) > sessionCacheLimit {
		t.Fatalf("order slice grew to %d entries, limit is %d", len(sessionCache.order), sessionCacheLimit)
	}
}
