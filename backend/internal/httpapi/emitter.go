package httpapi

import (
	"github.com/trick77/loom/internal/sse"
)

// sseEmitter adapts an *sse.Writer to turn.Emitter by JSON-encoding the
// payload.
type sseEmitter struct {
	w *sse.Writer
}

func (e sseEmitter) Send(event string, data any) error {
	return sendSSEJSON(e.w, event, data)
}
