package llm

import (
	"net/http"
	"os"
	"testing"

	"github.com/trick77/llmwire/llmwiretest"
)

// testModel is the synthetic chat model every test client runs on, so no test
// names a real model, its wire spellings or its rates. It fills every role.
const testModel = llmwiretest.ChatModel

// mustClient builds a Client against a test endpoint. cfg.BaseURL must be set:
// with it llmwire never consults the environment, so the test never needs a
// key and never sends one unless cfg.APIKey says so. Unset roles and registry
// default to llmwiretest's synthetic model.
func mustClient(t *testing.T, cfg Config, hc *http.Client) *Client {
	t.Helper()
	if cfg.BaseURL == "" {
		t.Fatal("mustClient: cfg.BaseURL is required so the environment is never consulted")
	}
	if cfg.Registry == nil {
		cfg.Registry = llmwiretest.Registry()
	}
	if cfg.Models.Chat == "" {
		cfg.Models.Chat = testModel
	}
	c, err := NewClient(cfg, hc)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

func readDirNames(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names, nil
}
