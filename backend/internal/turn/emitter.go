package turn

// Emitter sends one named event with a JSON-encoded payload to the client.
// The turn engine emits through it instead of an *sse.Writer, so it does not
// depend on the transport. Implementations must be safe for concurrent use:
// title goroutines emit while the assistant loop runs.
type Emitter interface {
	Send(event string, data any) error
}
