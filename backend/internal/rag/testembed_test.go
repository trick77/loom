package rag

import (
	"net/http"
	"testing"

	"github.com/trick77/llmwire/llmwiretest"
)

// mustEmbedClient builds an EmbedClient against a test endpoint; with BaseURL
// set llmwire never consults the environment. Unset model and registry default
// to llmwiretest's synthetic embedding model.
func mustEmbedClient(t *testing.T, cfg EmbedConfig, hc *http.Client) *EmbedClient {
	t.Helper()
	if cfg.Registry == nil {
		cfg.Registry = llmwiretest.Registry()
	}
	if cfg.Model == "" {
		cfg.Model = llmwiretest.EmbedModel
	}
	c, err := NewEmbedClient(cfg, hc)
	if err != nil {
		t.Fatalf("NewEmbedClient: %v", err)
	}
	return c
}
