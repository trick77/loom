package mcp

import (
	"encoding/json"
	"strings"
)

// maxStructuredSources caps how many sources one tool result may contribute, so
// a misbehaving server cannot flood the sources sidebar.
const maxStructuredSources = 20

// Source is one citable item a tool result carries in
// structuredContent.sources: the convention loom reads so any MCP server can
// feed the sources sidebar and the model's [n] markers. URL is required; the
// rest is display detail; Image is a preview picture (a video thumbnail).
type Source struct {
	URL     string `json:"url"`
	Title   string `json:"title"`
	Label   string `json:"label"`
	Snippet string `json:"snippet"`
	Image   string `json:"image"`
}

// CallResult is a tool call's model-facing text plus the sources it cited.
type CallResult struct {
	Text    string
	Sources []Source
}

type callToolResult struct {
	Content           []toolContent   `json:"content"`
	StructuredContent json.RawMessage `json:"structuredContent"`
	IsError           bool            `json:"isError"`
}

type toolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// toCallResult flattens a decoded tools/call result. A structuredContent that
// is not an object with a sources array is ignored: the 2026-07-28 revision
// allows any JSON value there, and only this one shape means something to loom.
func (r callToolResult) toCallResult() CallResult {
	return CallResult{Text: toolContentText(r.Content), Sources: structuredSources(r.StructuredContent)}
}

func structuredSources(raw json.RawMessage) []Source {
	if len(raw) == 0 {
		return nil
	}
	var payload struct {
		Sources []Source `json:"sources"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil
	}
	out := make([]Source, 0, len(payload.Sources))
	for _, src := range payload.Sources {
		src.URL = strings.TrimSpace(src.URL)
		if src.URL == "" {
			continue
		}
		out = append(out, src)
		if len(out) == maxStructuredSources {
			break
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func toolContentText(content []toolContent) string {
	var builder strings.Builder
	for _, item := range content {
		if item.Type == "text" && item.Text != "" {
			if builder.Len() > 0 {
				builder.WriteByte('\n')
			}
			remaining := maxToolOutputBytes - builder.Len()
			if remaining <= 0 {
				break
			}
			if len(item.Text) > remaining {
				builder.WriteString(item.Text[:remaining])
				break
			}
			builder.WriteString(item.Text)
		}
	}
	return builder.String()
}
