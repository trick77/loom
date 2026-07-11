package mcp

import (
	"context"

	"github.com/trick77/webfetch"
)

// fetchClientToolName is the server-side (original) name of the single tool the
// in-process fetch client exposes; combined with the server name "fetch" it
// yields the exposed tool name "fetch__fetch" that the rest of the app keys on.
const fetchClientToolName = "fetch"

// fetchClientDescription is the fetch tool description shown to the model. The
// first line is upstream mcp-server-fetch's. Upstream's trailing "Although
// originally you did not have internet access…" paragraph is intentionally
// dropped: it is legacy framing that carries no operational intent and only
// costs tokens in every tool-list injection. This is a deliberate divergence
// from byte-for-byte sidecar parity, kept minimal so tool dispatch is unchanged.
const fetchClientDescription = `Fetches a URL from the internet and optionally extracts its contents as markdown.`

// fetchClient is an in-process Client that replaces the external fetch MCP
// sidecar. It performs the fetch directly in the backend via the shared
// github.com/trick77/webfetch module (a faithful Go port of mcp-server-fetch),
// so no separate container, Python runtime, or stdio bridge is required. It
// advertises exactly one tool, "fetch", with the same schema and behaviour the
// sidecar did, so dispatch, citation, obscura fallback, and usage counting are
// unchanged.
type fetchClient struct {
	serverName string
}

// NewFetchClient builds the in-process fetch client for the given server name
// (always "fetch" in practice).
func NewFetchClient(serverName string) Client {
	return &fetchClient{serverName: serverName}
}

func (c *fetchClient) ListTools(context.Context) ([]Tool, error) {
	return []Tool{{
		Name:         ExposedToolName(c.serverName, fetchClientToolName),
		OriginalName: fetchClientToolName,
		Description:  fetchClientDescription,
		ServerName:   c.serverName,
		// Schema matches the JSON Schema upstream's pydantic model emits, so the
		// tool the model sees is identical to the sidecar's.
		InputSchema: map[string]any{
			"type":  "object",
			"title": "Fetch",
			"properties": map[string]any{
				"url": map[string]any{
					"description": "URL to fetch",
					"format":      "uri",
					"minLength":   1,
					"title":       "Url",
					"type":        "string",
				},
				"max_length": map[string]any{
					"default":          5000,
					"description":      "Maximum number of characters to return.",
					"exclusiveMaximum": 1000000,
					"exclusiveMinimum": 0,
					"title":            "Max Length",
					"type":             "integer",
				},
				"start_index": map[string]any{
					"default":     0,
					"description": "On return output starting at this character index, useful if a previous fetch was truncated and more context is required.",
					"minimum":     0,
					"title":       "Start Index",
					"type":        "integer",
				},
				"raw": map[string]any{
					"default":     false,
					"description": "Get the actual HTML content of the requested page, without simplification.",
					"title":       "Raw",
					"type":        "boolean",
				},
			},
			"required": []any{"url"},
		},
	}}, nil
}

func (c *fetchClient) CallTool(ctx context.Context, name string, arguments map[string]any) (string, error) {
	url, _ := arguments["url"].(string)
	opts := webfetch.Options{
		MaxLength:  argInt(arguments, "max_length"),
		StartIndex: argInt(arguments, "start_index"),
		Raw:        argBool(arguments, "raw"),
	}
	// A non-nil error keeps the deterministic fetch->obscura fallback working:
	// the dispatch layer treats a CallTool error on fetch__fetch as "try
	// obscura" (see httpapi.fetchObscuraFallback).
	return webfetch.Fetch(ctx, url, opts)
}

func (c *fetchClient) Close() error { return nil }

// argInt coerces a JSON tool argument (which arrives as float64) to an int.
// Missing or non-numeric values yield 0, which webfetch.Fetch treats as the
// upstream default.
func argInt(args map[string]any, key string) int {
	switch v := args[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	}
	return 0
}

// argBool coerces a JSON tool argument to a bool; missing/non-bool yields false.
func argBool(args map[string]any, key string) bool {
	b, _ := args[key].(bool)
	return b
}
