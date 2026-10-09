package httpapi

import (
	"github.com/trick77/loom/internal/sse"
	"github.com/trick77/loom/internal/turn"
)

// Emitter is turn.Emitter: the turn engine emits through it instead of an
// *sse.Writer.
type Emitter = turn.Emitter

// sseEmitter adapts an *sse.Writer to Emitter by JSON-encoding the payload.
type sseEmitter struct {
	w *sse.Writer
}

func (e sseEmitter) Send(event string, data any) error {
	return sendSSEJSON(e.w, event, data)
}
