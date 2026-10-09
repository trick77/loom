package httpapi

import (
	"encoding/json"

	"github.com/trick77/loom/internal/sse"
)

// sseEmitter adapts an *sse.Writer to turn.Emitter by JSON-encoding the
// payload. The stream handlers send their own events through it too, so a
// turn's events all take one path to the client.
type sseEmitter struct {
	w *sse.Writer
}

func (e sseEmitter) Send(event string, data any) error {
	payload, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return e.w.Send(event, string(payload))
}
