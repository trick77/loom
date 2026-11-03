package mcp

import (
	"encoding/json"
	"regexp"
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

// CallResult is a tool call's model-facing text plus the sources it cited and
// the files it handed to run_python.
type CallResult struct {
	Text    string
	Sources []Source
	Files   []File
}

// File is data a tool result carried as an MCP embedded resource, for
// run_python to read during the running turn. Never shown, never part of the
// prompt; loom keeps it for that turn only.
type File struct {
	Name     string
	MimeType string
	Data     []byte
}

// maxToolFiles caps how many files one tool result may hand on.
const maxToolFiles = 10

// toolFileTypes are the resource types loom hands to run_python, by the
// extension the name must carry.
var toolFileTypes = map[string]string{"text/csv": ".csv", "application/json": ".json"}

type callToolResult struct {
	Content           []toolContent   `json:"content"`
	StructuredContent json.RawMessage `json:"structuredContent"`
	IsError           bool            `json:"isError"`
}

type toolContent struct {
	Type     string        `json:"type"`
	Text     string        `json:"text"`
	Resource *toolResource `json:"resource"`
}

type toolResource struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType"`
	Text     string `json:"text"`
}

// toCallResult flattens a decoded tools/call result. A structuredContent that
// is not an object with a sources array is ignored: the 2026-07-28 revision
// allows any JSON value there, and only this one shape means something to loom.
func (r callToolResult) toCallResult() CallResult {
	return CallResult{Text: toolContentText(r.Content), Sources: structuredSources(r.StructuredContent), Files: toolFiles(r.Content)}
}

// toolFiles keeps the text resources run_python can use: CSV or JSON, under
// a plain file name of the matching extension (file:///name.csv). Anything
// else (a path, a blob, another type) is dropped: the name becomes a file in
// the sandbox's input directory.
func toolFiles(content []toolContent) []File {
	var out []File
	for _, item := range content {
		if item.Type != "resource" || item.Resource == nil || item.Resource.Text == "" || len(out) == maxToolFiles {
			continue
		}
		ext, ok := toolFileTypes[item.Resource.MimeType]
		name := strings.TrimPrefix(item.Resource.URI, "file:///")
		if !ok || !toolFileName.MatchString(name) || strings.Contains(name, "..") || !strings.HasSuffix(name, ext) {
			continue
		}
		out = append(out, File{Name: name, MimeType: item.Resource.MimeType, Data: []byte(item.Resource.Text)})
	}
	return out
}

// toolFileName is a plain file name the sandbox accepts (its safeName): no
// directory, no leading dot.
var toolFileName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)

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
	// Positions are kept, unusable entries included: sources[i] belongs to the
	// text's item i+1, so dropping one would shift every later citation onto
	// the wrong item. The consumer skips an entry it cannot register.
	sources := payload.Sources
	if len(sources) > maxStructuredSources {
		sources = sources[:maxStructuredSources]
	}
	usable := false
	for i := range sources {
		sources[i].URL = strings.TrimSpace(sources[i].URL)
		usable = usable || sources[i].URL != ""
	}
	if !usable {
		return nil
	}
	return sources
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
