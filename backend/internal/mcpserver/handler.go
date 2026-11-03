// Package mcpserver is a small stateless MCP server over streamable HTTP
// (JSON responses only), copied from peeq's internal/mcpserver with the tool
// registry made a parameter. It answers the 2026-07-28 revision and the
// initialize-era ones. loom's sidecars share it; it imports nothing of loom,
// so a sidecar image copies this directory alone.
package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// maxBodyBytes bounds one request body; tool arguments are a few strings.
	maxBodyBytes = 1 << 20
	// toolTimeout bounds one tool call, under loom's own tool deadline.
	toolTimeout = 25 * time.Second
	// maxToolTextBytes caps one tool's text, under loom's 32 KB cut, so the
	// cut is ours, at a line.
	maxToolTextBytes = 24 * 1024
	truncatedNote    = "\n[output truncated]"
)

// Source is one citable link in a tool result's structuredContent: loom's
// sources convention. Image is a preview picture loom proxies.
type Source struct {
	URL     string `json:"url"`
	Title   string `json:"title"`
	Label   string `json:"label,omitempty"`
	Snippet string `json:"snippet,omitempty"`
	Image   string `json:"image,omitempty"`
}

// File is data a tool hands to loom's run_python for the running turn (an
// MCP embedded resource): never shown in the chat, never part of the prompt.
type File struct {
	Name     string // a plain file name, e.g. strava_123_streams.csv
	MimeType string // text/csv or application/json
	Text     string
}

// Result is what a tool produced. Sources[i] belongs to the text's item i+1.
type Result struct {
	Text    string
	Sources []Source
	Files   []File
	IsError bool
}

// UserHeader carries the loom user id on every call loom makes to a
// per-user server. Only loom holds the bearer token the routes sit behind, so
// the header is as trustworthy as that token.
const UserHeader = "X-Loom-User"

type userKey struct{}

// WithUser returns ctx carrying the loom user id.
func WithUser(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, userKey{}, id)
}

// User is the loom user id a call is made for; "" when loom sent none.
func User(ctx context.Context) string {
	id, _ := ctx.Value(userKey{}).(string)
	return id
}

// UserFromRequest is the request's loom user id (UserHeader), trimmed.
func UserFromRequest(r *http.Request) string {
	return strings.TrimSpace(r.Header.Get(UserHeader))
}

// ErrorResult is a failed call the model reads and explains.
func ErrorResult(msg string) Result { return Result{Text: msg, IsError: true} }

// Tool is one callable tool.
type Tool struct {
	Name        string
	Title       string
	Description string
	Schema      map[string]any
	Run         func(ctx context.Context, args json.RawMessage) Result
}

// Server describes one MCP endpoint.
type Server struct {
	Name         string
	Version      string
	Instructions string
	Tools        []Tool
}

type handler struct {
	name, version, instructions string
	order                       []string
	tools                       map[string]Tool
}

// New returns the endpoint's handler. Auth is the caller's: it mounts this
// behind its bearer check.
func New(s Server) http.Handler {
	h := &handler{name: s.Name, version: s.Version, instructions: s.Instructions, tools: map[string]Tool{}}
	for _, t := range s.Tools {
		h.order = append(h.order, t.Name)
		h.tools[t.Name] = t
	}
	return h
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Only loom calls, server to server: a browser Origin is never legitimate.
	if r.Header.Get("Origin") != "" {
		http.Error(w, "forbidden origin", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		// Stateless and JSON-only: no GET stream to open, no session to DELETE.
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		writeRPCError(w, http.StatusBadRequest, nil, codeParseError, "could not read the request body", nil)
		return
	}
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) > 0 && trimmed[0] == '[' {
		writeRPCError(w, http.StatusBadRequest, nil, codeInvalidRequest, "batch requests are not supported", nil)
		return
	}
	var req request
	if err := json.Unmarshal(trimmed, &req); err != nil {
		writeRPCError(w, http.StatusBadRequest, nil, codeParseError, "parse error", nil)
		return
	}
	if req.JSONRPC != "2.0" || req.Method == "" {
		writeRPCError(w, http.StatusBadRequest, req.ID, codeInvalidRequest, "invalid request", nil)
		return
	}
	h.dispatch(w, r, req)
}

func (h *handler) toolDefs() []map[string]any {
	out := make([]map[string]any, 0, len(h.order))
	for _, name := range h.order {
		t := h.tools[name]
		// The title goes out twice: top-level for the current spec,
		// annotations.title for clients that still read the older place.
		out = append(out, map[string]any{
			"name":        t.Name,
			"title":       t.Title,
			"description": t.Description,
			"inputSchema": t.Schema,
			"annotations": map[string]any{"title": t.Title, "readOnlyHint": true, "openWorldHint": true},
		})
	}
	return out
}

func (h *handler) callTool(w http.ResponseWriter, r *http.Request, req request, modern bool) {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil || p.Name == "" {
		writeRPCError(w, http.StatusOK, req.ID, codeInvalidParams, "tools/call needs a tool name", nil)
		return
	}
	t, ok := h.tools[p.Name]
	if !ok {
		writeRPCError(w, http.StatusOK, req.ID, codeInvalidParams, "unknown tool: "+p.Name, nil)
		return
	}
	if len(p.Arguments) == 0 || string(p.Arguments) == "null" {
		p.Arguments = json.RawMessage("{}")
	}
	ctx, cancel := context.WithTimeout(WithUser(r.Context(), UserFromRequest(r)), toolTimeout)
	defer cancel()
	start := time.Now()
	res := t.Run(ctx, p.Arguments)
	slog.Info("mcp tool call", "server", h.name, "tool", p.Name, "is_error", res.IsError, "ms", time.Since(start).Milliseconds())

	content := []map[string]any{{"type": "text", "text": capText(res.Text, maxToolTextBytes)}}
	for _, f := range res.Files {
		content = append(content, map[string]any{"type": "resource", "resource": map[string]any{
			"uri": "file:///" + f.Name, "mimeType": f.MimeType, "text": f.Text,
		}})
	}
	result := map[string]any{"content": content}
	if res.Sources != nil {
		result["structuredContent"] = map[string]any{"sources": res.Sources}
	}
	if res.IsError {
		result["isError"] = true
	}
	if modern {
		result = h.modern(result)
	}
	writeResult(w, req.ID, result)
}

// DecodeArgs unmarshals tool arguments strictly, so a misspelt field is an
// error the model can read rather than a silently ignored filter.
func DecodeArgs(raw json.RawMessage, into any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	return dec.Decode(into)
}

// ObjectSchema is a closed JSON object schema.
func ObjectSchema(props map[string]any, required ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

// capText trims s to at most max bytes on a line boundary when it can, a rune
// boundary otherwise, and says so.
func capText(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max - len(truncatedNote)
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	if nl := bytes.LastIndexByte([]byte(s[:cut]), '\n'); nl > cut/2 {
		cut = nl
	}
	return s[:cut] + truncatedNote
}
