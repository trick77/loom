package chat

import "errors"

// ErrThreadNotFound and ErrProjectNotFound are returned when a write names a
// thread or project the user does not own. Handlers map them to a 404.
var (
	ErrThreadNotFound  = errors.New("thread not found")
	ErrProjectNotFound = errors.New("project not found")
)

// ValidationError rejects caller input (an empty title, content over the cap,
// a malformed JSON column). Its message is written for the client; handlers
// map it to a 400 and pass the message through.
type ValidationError struct {
	Msg string
}

func (e *ValidationError) Error() string { return e.Msg }

// validation builds a ValidationError.
func validation(msg string) error { return &ValidationError{Msg: msg} }
