package httpapi

import (
	"errors"
	"testing"

	"github.com/trick77/loom/internal/llm"
)

func TestStreamFailureMessageMapsUserStalledAndGenericErrors(t *testing.T) {
	result := assistantLoopResult{}
	if got := streamFailureMessage(streamUserError{message: "image generation refused"}, result, "message", "t1"); got != "image generation refused" {
		t.Fatalf("user error -> %q", got)
	}
	if got := streamFailureMessage(llm.ErrStreamStalled, result, "message", "t1"); got != llm.ErrStreamStalled.Error() {
		t.Fatalf("stalled -> %q", got)
	}
	if got := streamFailureMessage(errors.New("boom"), result, "message", "t1"); got != "stream failed" {
		t.Fatalf("generic -> %q", got)
	}
}

func TestMarshalTurnJSONFallsBackToEmptyArrays(t *testing.T) {
	trace, blocks := marshalTurnJSON("t1", nil, nil)
	if string(trace) != "[]" || string(blocks) != "[]" {
		t.Fatalf("empty turn -> %s / %s, want [] / []", trace, blocks)
	}
	trace, blocks = marshalTurnJSON("t1", []activityTraceEvent{{}}, []contentBlock{{Type: "text", Content: "x"}})
	if string(trace) == "[]" || string(blocks) == "[]" {
		t.Fatalf("populated turn -> %s / %s, want encoded arrays", trace, blocks)
	}
}
