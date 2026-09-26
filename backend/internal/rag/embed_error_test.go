package rag

import (
	"errors"
	"testing"

	"github.com/trick77/llmwire"
)

// The phrasing must not cost callers llmwire's classification.
func TestEmbedError_KeepsTheWireClass(t *testing.T) {
	err := embedError(&llmwire.APIError{StatusCode: 429, Message: "slow down", Class: llmwire.ErrRateLimited})
	if err.Error() != "embedding failed with status 429: slow down" {
		t.Fatalf("error = %q", err)
	}
	var apiErr *llmwire.APIError
	if !errors.Is(err, llmwire.ErrRateLimited) || !errors.As(err, &apiErr) {
		t.Fatalf("embed error lost its class or its *APIError: %v", err)
	}
}
