package turn

import "errors"

// Cancel causes of a turn's stream context. The HTTP layer cancels with one of
// them; the turn reads them back via context.Cause to decide what still runs.
var (
	// ErrStopRequested is the cause of an explicit client stop.
	ErrStopRequested = errors.New("stream stop requested")
	// ErrSuperseded is the cause when a newer request on the same thread
	// replaces the running turn.
	ErrSuperseded = errors.New("stream superseded by newer request")
	// ErrThreadDeleted is the cause when the turn's thread is being deleted.
	ErrThreadDeleted = errors.New("stream canceled: thread deleted")
)
