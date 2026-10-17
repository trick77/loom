package mcp

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// Protocol revisions loom speaks. The modern revision (2026-07-28) is
// stateless: no initialize handshake, no session id, version and client
// identity on every request's _meta. The legacy revision keeps the handshake
// for servers that predate it; a remote client tries modern first and falls
// back per server (see remoteClient.ListTools).
const (
	modernProtocolVersion = "2026-07-28"
	legacyProtocolVersion = "2025-06-18"

	metaProtocolVersion    = "io.modelcontextprotocol/protocolVersion"
	metaClientInfo         = "io.modelcontextprotocol/clientInfo"
	metaClientCapabilities = "io.modelcontextprotocol/clientCapabilities"

	// JSON-RPC codes only a modern server sends. Seeing one on a failed probe
	// means the server is modern and the request itself was wrong, so the client
	// must not fall back to the legacy handshake.
	codeHeaderMismatch             = -32020
	codeMissingRequiredCapability  = -32021
	codeUnsupportedProtocolVersion = -32022
	codeMethodNotFound             = -32601

	// defaultToolListTTL is how long a tool list stays fresh when the server
	// gives no ttlMs (every legacy server).
	defaultToolListTTL = 15 * time.Minute
	// minToolListTTL keeps a server's tiny ttlMs from turning the refresh loop
	// into a poll.
	minToolListTTL = time.Minute
)

// protocolEra is what a remote server turned out to speak.
type protocolEra int

const (
	eraUnknown protocolEra = iota
	eraModern
	eraLegacy
)

var clientInfo = map[string]string{"name": "loom", "version": "dev"}

// rpcCallError is a JSON-RPC error a server answered with on a 2xx response.
type rpcCallError struct {
	method  string
	code    int
	message string
	data    json.RawMessage
}

func (e *rpcCallError) Error() string {
	return fmt.Sprintf("MCP %s failed: %s", e.method, e.message)
}

// modernParams returns params with the per-request _meta every modern request
// carries. The caller's map is never written.
func modernParams(params map[string]any) map[string]any {
	out := make(map[string]any, len(params)+1)
	for key, value := range params {
		out[key] = value
	}
	out["_meta"] = map[string]any{
		metaProtocolVersion:    modernProtocolVersion,
		metaClientInfo:         clientInfo,
		metaClientCapabilities: map[string]any{},
	}
	return out
}

// setModernHeaders mirrors the method (and, for tools/call, the tool name)
// into the headers a modern server validates against the body.
func setModernHeaders(req *http.Request, method string, params map[string]any) {
	req.Header.Set("MCP-Protocol-Version", modernProtocolVersion)
	req.Header.Set("Mcp-Method", method)
	if name, ok := params["name"].(string); ok && method == "tools/call" {
		req.Header.Set("Mcp-Name", headerSafeValue(name))
	}
}

// headerSafeValue returns s as is when it is plain visible ASCII, else in the
// spec's base64 sentinel form. A plain value that happens to look like the
// sentinel is encoded too, so the server never decodes something it was not
// meant to.
func headerSafeValue(s string) string {
	safe := s != "" && s[0] != ' ' && s[len(s)-1] != ' '
	for i := 0; safe && i < len(s); i++ {
		if s[i] < 0x20 || s[i] > 0x7e {
			safe = false
		}
	}
	if safe && !(len(s) >= 11 && s[:9] == "=?base64?" && s[len(s)-2:] == "?=") {
		return s
	}
	return "=?base64?" + base64.StdEncoding.EncodeToString([]byte(s)) + "?="
}

// modernErrorCode reads a JSON-RPC error body and reports its code when it is
// one only a modern server sends.
func modernErrorCode(body string) (int, bool) {
	var resp struct {
		Error *struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil || resp.Error == nil {
		return 0, false
	}
	switch resp.Error.Code {
	case codeHeaderMismatch, codeMissingRequiredCapability, codeUnsupportedProtocolVersion, codeMethodNotFound:
		return resp.Error.Code, true
	}
	return 0, false
}

// isLegacyAnswer reports whether a failed modern request means the server
// only speaks the legacy revision. A 4xx without a recognised modern error
// body is legacy (the spec's rule for Streamable HTTP), and so is a 2xx
// JSON-RPC error: a modern server answers an unknown method with 404. An
// UnsupportedProtocolVersion listing a legacy revision we speak is legacy too.
// The server/discover probe (anyStatus) also reads a 5xx that way: a legacy
// server may choke on a method it has never heard of, and a working server
// must not fail discovery over it. Transport failures are never an answer.
func isLegacyAnswer(err error, anyStatus bool) bool {
	var statusErr *mcpStatusError
	if errors.As(err, &statusErr) {
		if !anyStatus && (statusErr.status < 400 || statusErr.status >= 500) {
			return false
		}
		code, modern := modernErrorCode(statusErr.body)
		if !modern {
			return true
		}
		return code == codeUnsupportedProtocolVersion && supportsLegacy(errorData(statusErr.body))
	}
	var rpcErr *rpcCallError
	if errors.As(err, &rpcErr) {
		switch rpcErr.code {
		case codeHeaderMismatch, codeMissingRequiredCapability:
			return false
		case codeUnsupportedProtocolVersion:
			return supportsLegacy(rpcErr.data)
		}
		return true
	}
	return false
}

// errorData returns the data member of a JSON-RPC error body.
func errorData(body string) json.RawMessage {
	var resp struct {
		Error struct {
			Data json.RawMessage `json:"data"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return nil
	}
	return resp.Error.Data
}

// supportsLegacy reports whether an UnsupportedProtocolVersion error's data
// lists a legacy revision loom can speak.
func supportsLegacy(data json.RawMessage) bool {
	var payload struct {
		Supported []string `json:"supported"`
	}
	if len(data) == 0 || json.Unmarshal(data, &payload) != nil {
		return false
	}
	for _, version := range payload.Supported {
		switch version {
		case legacyProtocolVersion, "2025-11-25", "2025-03-26":
			return true
		}
	}
	return false
}

// toolListTTL turns a list result's ttlMs into the refresh interval.
func toolListTTL(ttlMs *int64) time.Duration {
	if ttlMs == nil || *ttlMs <= 0 {
		return defaultToolListTTL
	}
	ttl := time.Duration(*ttlMs) * time.Millisecond
	if ttl < minToolListTTL {
		return minToolListTTL
	}
	return ttl
}
