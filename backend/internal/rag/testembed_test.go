package rag

import (
	"net/http"
	"testing"
)

// mustEmbedClient builds an EmbedClient against a test endpoint; with BaseURL
// set llmwire never consults the environment.
func mustEmbedClient(t *testing.T, cfg EmbedConfig, hc *http.Client) *EmbedClient {
	t.Helper()
	c, err := NewEmbedClient(cfg, hc)
	if err != nil {
		t.Fatalf("NewEmbedClient: %v", err)
	}
	return c
}
