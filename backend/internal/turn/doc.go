// Package turn runs one chat turn: prepare, the assistant/tool loop,
// persistence and cost. It knows nothing about HTTP; events go to an Emitter.
package turn
