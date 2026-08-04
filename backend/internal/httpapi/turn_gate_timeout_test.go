package httpapi

import (
	"context"
	"testing"
	"time"

	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/imagegen"
	"github.com/trick77/loom/internal/llm"
)

// hangingIntentChatClient answers every helper call except the image-intent gate,
// which blocks until its context is done — the degraded-endpoint case that used to
// stall a whole turn (an observed 78s image_intent call on an unbounded context).
type hangingIntentChatClient struct {
	fakeChatClient
	entered chan struct{}
}

func (f *hangingIntentChatClient) ClassifyImageIntent(ctx context.Context, _ string, _, _ bool) (llm.ImageIntent, error) {
	close(f.entered)
	<-ctx.Done()
	return llm.ImageIntent{Action: llm.ImageIntentNone}, ctx.Err()
}

// serverWithHangingIntentGate wires the minimum classifyImageTurn needs to get
// past its short-circuits: an image tool, an artifact store and a users dir.
func serverWithHangingIntentGate(t *testing.T) (*server, *hangingIntentChatClient) {
	t.Helper()
	chat := &hangingIntentChatClient{entered: make(chan struct{})}
	return &server{
		llm:        chat,
		imageTools: []imagegen.Tool{imagegen.NewTool(fakeImageProvider{})},
		artifacts:  fakeArtifactStore{},
		usersDir:   t.TempDir(),
	}, chat
}

func TestClassifyImageTurnGivesUpOnAStalledGate(t *testing.T) {
	previous := turnGateTimeout
	turnGateTimeout = 50 * time.Millisecond
	t.Cleanup(func() { turnGateTimeout = previous })

	s, chat := serverWithHangingIntentGate(t)

	done := make(chan imageRouting, 1)
	go func() {
		done <- s.classifyImageTurn(context.Background(), auth.User{ID: "user-1", Username: "jan"}, "thread-1", "draw a red fox", false, nil)
	}()

	select {
	case <-chat.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("the gate was never called")
	}

	select {
	case route := <-done:
		if route.generate || route.reuseSource || route.typography {
			t.Fatalf("a timed-out gate must route as non-image, got %+v", route)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("classifyImageTurn did not give up on the stalled gate; it inherits the turn's unbounded context")
	}
}

// TestClassifyImageTurnDoesNotOutliveTheTurn keeps the bound from becoming a
// floor: a caller whose own context is already done must not wait for it.
func TestClassifyImageTurnDoesNotOutliveTheTurn(t *testing.T) {
	previous := turnGateTimeout
	turnGateTimeout = time.Minute
	t.Cleanup(func() { turnGateTimeout = previous })

	s, chat := serverWithHangingIntentGate(t)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan imageRouting, 1)
	go func() {
		done <- s.classifyImageTurn(ctx, auth.User{ID: "user-1", Username: "jan"}, "thread-1", "draw a red fox", false, nil)
	}()

	select {
	case <-chat.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("the gate was never called")
	}
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("classifyImageTurn kept waiting after the turn was canceled")
	}
}
