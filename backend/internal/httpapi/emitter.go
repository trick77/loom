package httpapi

import "github.com/trick77/loom/internal/sse"

// Emitter sends one named event with a JSON-encoded payload to the client.
// The turn engine emits through it instead of an *sse.Writer, so it does not
// depend on the transport. Implementations must be safe for concurrent use:
// title goroutines emit while the assistant loop runs.
type Emitter interface {
	Send(event string, data any) error
}

// sseEmitter adapts an *sse.Writer to Emitter by JSON-encoding the payload.
type sseEmitter struct {
	w *sse.Writer
}

func (e sseEmitter) Send(event string, data any) error {
	return sendSSEJSON(e.w, event, data)
}
