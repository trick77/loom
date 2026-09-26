package inference

// WireError phrases a wire failure for logs and user messages without dropping
// the error it describes: errors.Is/As still reach llmwire's classes
// (ErrRateLimited, ErrAuth, ...) and the concrete *APIError through it.
func WireError(msg string, err error) error {
	return &wireError{msg: msg, err: err}
}

type wireError struct {
	msg string
	err error
}

func (e *wireError) Error() string { return e.msg }
func (e *wireError) Unwrap() error { return e.err }
