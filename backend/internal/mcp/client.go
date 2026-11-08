package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/trick77/loom/internal/artifact"
	"github.com/trick77/loom/internal/loomuser"
)

const (
	defaultHTTPTimeout  = 15 * time.Second
	maxRPCResponseBytes = 4 << 20
	maxToolOutputBytes  = 32 << 10
	// maxArtifactRPCBytes fits one artifact's worth of images and documents
	// (artifact.MaxArtifactSizeBytes, all of a reply together, the most loom
	// keeps) as base64, plus the rest of the reply: its text, the text files
	// a server may add for run_python (toolkit's mcpserver caps them at
	// 3 MiB) and the envelope.
	maxArtifactRPCBytes = artifact.MaxArtifactSizeBytes/3*4 + 4<<20
	// maxStdioLineBytes is a stdio server's reply line without artifacts.
	maxStdioLineBytes = 1 << 20
)

// ArtifactCallTimeout bounds one call to a server that makes artifacts:
// rendering an image or a document and sending it takes far longer than a
// text tool.
const ArtifactCallTimeout = 3 * time.Minute

// maxResponseBytes is the largest reply loom reads from an HTTP server: room
// for images and documents only when the server may make artifacts.
func (cfg ServerConfig) maxResponseBytes() int {
	if cfg.Artifacts {
		return maxArtifactRPCBytes
	}
	return maxRPCResponseBytes
}

// maxLineBytes is maxResponseBytes for a stdio server, whose reply is one line.
func (cfg ServerConfig) maxLineBytes() int {
	if cfg.Artifacts {
		return maxArtifactRPCBytes
	}
	return maxStdioLineBytes
}

// errReplyTooLarge names the cap a reply ran into, instead of the JSON
// decoder's bare "unexpected EOF" on a cut-off body.
func errReplyTooLarge(limit int) error {
	return fmt.Errorf("MCP reply larger than %d MiB", limit>>20)
}

// Tool is an MCP tool exposed to the model, with its server name prefix and description.
type Tool struct {
	Name         string
	OriginalName string
	// Title is the server's human-readable name for the tool, shown to the
	// user while it runs; empty when the server gave none.
	Title       string
	Description string
	InputSchema map[string]any
	ServerName  string
}

// Client is the common interface for MCP tool-serving clients.
type Client interface {
	ListTools(context.Context) ([]Tool, error)
	CallTool(context.Context, string, map[string]any) (CallResult, error)
	Close() error
}

// serverMetadata is what a client learned about its server on the last
// ListTools: the server's instructions for the model and how long the tool
// list stays fresh. Clients without either (the in-process fetch) skip it.
type serverMetadata interface {
	Instructions() string
	ToolListTTL() time.Duration
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcResponse struct {
	ID     int64           `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

type listToolsResult struct {
	Tools []toolResult `json:"tools"`
	TTLMs *int64       `json:"ttlMs"`
}

type toolResult struct {
	Name        string         `json:"name"`
	Title       string         `json:"title"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	Annotations struct {
		Title string `json:"title"`
	} `json:"annotations"`
}

// displayTitle is the tool's human-readable name: the top-level title the
// spec prefers, else the older annotations.title.
func (t toolResult) displayTitle() string {
	if title := strings.TrimSpace(t.Title); title != "" {
		return title
	}
	return strings.TrimSpace(t.Annotations.Title)
}

// discoverResult is the part of a modern server/discover (or a legacy
// initialize) result loom keeps.
type discoverResult struct {
	Instructions string `json:"instructions"`
}

type remoteClient struct {
	serverName string
	cfg        ServerConfig
	httpClient *http.Client
	nextID     atomic.Int64
	initMu     sync.Mutex
	inited     bool
	mu         sync.Mutex
	sessionID  string
	// era is what the server turned out to speak, cached for the client's
	// lifetime (the spec's per-origin cache); instructions and listTTL come
	// from the last ListTools. All three are guarded by mu.
	era          protocolEra
	instructions string
	listTTL      time.Duration
}

// mcpStatusError carries a non-2xx HTTP response from an MCP request so callers
// can distinguish a recoverable session failure from other errors.
type mcpStatusError struct {
	method string
	status int
	body   string
}

// Error names the method and status only. The body stays on the struct for
// isSessionError; it is never part of the message, which reaches the user in
// the /mcp panel and could echo request headers or tokens the server reflects.
func (e *mcpStatusError) Error() string {
	return fmt.Sprintf("MCP %s failed with status %d", e.method, e.status)
}

// errAuthRejected matches (errors.Is) any mcpStatusError carrying a 401/403.
var errAuthRejected = errors.New("MCP server refused the credentials")

// Is lets errors.Is find a refusal anywhere in a joined discover+legacy error,
// where errors.As would stop at the first status (a 404 ahead of the 401).
func (e *mcpStatusError) Is(target error) bool {
	return target == errAuthRejected && (e.status == http.StatusUnauthorized || e.status == http.StatusForbidden) //nolint:errorlint // sentinel identity
}

// isAuthError reports whether any status in err's tree is a 401/403.
func isAuthError(err error) bool {
	return errors.Is(err, errAuthRejected)
}

// isSessionError reports whether err is an expired/invalid Streamable HTTP
// session, which is recoverable by re-initializing. Servers reject such
// requests with 400 or 404 and mention the session in the body.
func isSessionError(err error) bool {
	var statusErr *mcpStatusError
	if !errors.As(err, &statusErr) {
		return false
	}
	if statusErr.status != http.StatusBadRequest && statusErr.status != http.StatusNotFound {
		return false
	}
	return strings.Contains(strings.ToLower(statusErr.body), "session")
}

// NewRemoteClient creates a Client that communicates with an MCP server over HTTP via the Streamable HTTP transport.
func NewRemoteClient(serverName string, cfg ServerConfig, httpClient *http.Client) Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultHTTPTimeout}
	}
	// A server that makes artifacts renders and sends tens of MiB: the
	// shared client's short timeout would cut every such call off.
	if cfg.Artifacts && httpClient.Timeout != 0 && httpClient.Timeout < ArtifactCallTimeout {
		longer := *httpClient
		longer.Timeout = ArtifactCallTimeout
		httpClient = &longer
	}
	return &remoteClient{serverName: serverName, cfg: cfg, httpClient: httpClient}
}

// ListTools discovers the server's tools. Until the server is known to be
// legacy it probes with a modern server/discover, which also carries the
// instructions. A probe answered like a legacy server tries the initialize
// handshake, and only a handshake that works marks the server legacy: a
// modern server that failed the probe for another reason (a cold start, a
// 503) must be probed again next time, not locked into a handshake it rejects.
func (c *remoteClient) ListTools(ctx context.Context) ([]Tool, error) {
	if c.currentEra() != eraLegacy {
		if err := c.discover(ctx); err != nil {
			return nil, err
		}
	}
	var result listToolsResult
	if err := c.request(ctx, "tools/list", nil, &result); err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.listTTL = toolListTTL(result.TTLMs)
	c.mu.Unlock()
	return exposeTools(c.serverName, c.cfg, result.Tools), nil
}

// discover sends server/discover and settles the era from the answer: modern
// on success; legacy only once a legacy handshake has actually worked after
// a probe that may have come from a legacy server.
func (c *remoteClient) discover(ctx context.Context) error {
	var discovered discoverResult
	err := c.call(ctx, "server/discover", nil, &discovered, true)
	if err == nil {
		c.mu.Lock()
		c.era, c.instructions = eraModern, discovered.Instructions
		c.mu.Unlock()
		return nil
	}
	// A server already known to be modern stays modern: a failed discover is
	// then an outage, not a hint about its protocol.
	if c.currentEra() == eraModern || !mayBeLegacy(err) {
		return err
	}
	if legacyErr := c.initialize(ctx); legacyErr != nil {
		return fmt.Errorf("%w (legacy handshake: %w)", err, legacyErr)
	}
	c.setEra(eraLegacy)
	return nil
}

func (c *remoteClient) CallTool(ctx context.Context, name string, arguments map[string]any) (CallResult, error) {
	// Belt and braces under Service's check (perUserReady): this client is
	// the one that names the caller, so it never sends a per-user call
	// without one.
	if err := perUserReady(ctx, c.cfg, name); err != nil {
		return CallResult{}, err
	}
	var result callToolResult
	if err := c.request(ctx, "tools/call", map[string]any{"name": name, "arguments": arguments}, &result); err != nil {
		return CallResult{}, err
	}
	if result.IsError {
		return CallResult{}, fmt.Errorf("MCP tool %q returned an error: %s", name, toolContentText(result.Content))
	}
	return result.toCallResult(c.cfg.Artifacts), nil
}

// Instructions returns the server's guidance for the model, from the last
// server/discover (modern) or initialize (legacy).
func (c *remoteClient) Instructions() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.instructions
}

// ToolListTTL returns how long the last tool list stays fresh.
func (c *remoteClient) ToolListTTL() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.listTTL == 0 {
		return defaultToolListTTL
	}
	return c.listTTL
}

func (c *remoteClient) currentEra() protocolEra {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.era
}

func (c *remoteClient) setEra(era protocolEra) {
	c.mu.Lock()
	c.era = era
	c.mu.Unlock()
}

// request issues a request in the server's era, settling the era first when
// no ListTools has run yet.
func (c *remoteClient) request(ctx context.Context, method string, params map[string]any, out any) error {
	if c.currentEra() == eraUnknown {
		if err := c.discover(ctx); err != nil {
			return err
		}
	}
	if c.currentEra() == eraLegacy {
		return c.callWithSession(ctx, method, params, out)
	}
	return c.call(ctx, method, params, out, true)
}

func (c *remoteClient) Probe(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, c.cfg.URL, nil)
	if err != nil {
		return scrubURLError(err)
	}
	for key, value := range c.cfg.Headers {
		req.Header.Set(key, value)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return scrubURLError(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 500 {
		return &mcpStatusError{method: "probe", status: resp.StatusCode}
	}
	return nil
}

func (c *remoteClient) Close() error { return nil }

// callWithSession initializes the session if needed and issues the request. If
// the server rejects it with an expired/invalid session, it re-initializes once
// and retries — Streamable HTTP servers may expire idle sessions, and
// the session established at startup discovery would otherwise stay stale.
func (c *remoteClient) callWithSession(ctx context.Context, method string, params map[string]any, out any) error {
	if err := c.initialize(ctx); err != nil {
		return err
	}
	err := c.call(ctx, method, params, out, false)
	if err == nil || !isSessionError(err) {
		return err
	}
	// initMu keeps the re-init handshake atomic. Concurrent callers that each
	// hit a session error may re-initialize more than once (a caller can reset a
	// session another goroutine just refreshed); that only costs an extra
	// handshake and never corrupts state, so it is acceptable at this scale.
	c.resetSession()
	if err := c.initialize(ctx); err != nil {
		return err
	}
	return c.call(ctx, method, params, out, false)
}

func (c *remoteClient) initialize(ctx context.Context) error {
	c.initMu.Lock()
	defer c.initMu.Unlock()
	if c.inited {
		return nil
	}
	var result discoverResult
	if err := c.call(ctx, "initialize", map[string]any{
		"protocolVersion": legacyProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      clientInfo,
	}, &result, false); err != nil {
		return err
	}
	c.mu.Lock()
	c.instructions = result.Instructions
	c.mu.Unlock()
	c.inited = true
	return nil
}

// resetSession forces the next request to perform a fresh initialize handshake.
func (c *remoteClient) resetSession() {
	c.initMu.Lock()
	c.inited = false
	c.initMu.Unlock()
	c.mu.Lock()
	c.sessionID = ""
	c.mu.Unlock()
}

// call posts one JSON-RPC request. modern adds the per-request _meta and the
// mirrored headers and leaves the session id out; a legacy call carries the
// session the handshake established.
func (c *remoteClient) call(ctx context.Context, method string, params map[string]any, out any, modern bool) error {
	id := c.nextID.Add(1)
	var wireParams any
	if modern {
		wireParams = modernParams(params)
	} else if params != nil {
		wireParams = params
	}
	body, err := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: wireParams})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	// The Streamable HTTP transport mandates that clients accept both content
	// types; spec-compliant servers answer 406 otherwise and may reply with an
	// SSE stream instead of a bare JSON body.
	req.Header.Set("Accept", "application/json, text/event-stream")
	if modern {
		setModernHeaders(req, method, params)
	} else {
		c.mu.Lock()
		sessionID := c.sessionID
		c.mu.Unlock()
		if sessionID != "" {
			req.Header.Set("Mcp-Session-Id", sessionID)
		}
	}
	for key, value := range c.cfg.Headers {
		req.Header.Set(key, value)
	}
	// After the configured headers: no header map can name the user.
	req.Header.Del(loomuser.Header)
	if p, ok := PrincipalFrom(ctx); ok && c.cfg.PerUser {
		req.Header.Set(loomuser.Header, p.ID)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return scrubURLError(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if id := resp.Header.Get("Mcp-Session-Id"); id != "" && !modern {
		c.mu.Lock()
		c.sessionID = id
		c.mu.Unlock()
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return &mcpStatusError{method: method, status: resp.StatusCode, body: strings.TrimSpace(string(bytes))}
	}
	rpcResp, err := decodeRPCResponse(resp, id, c.cfg.maxResponseBytes())
	if err != nil {
		return err
	}
	if rpcResp.Error != nil {
		return &rpcCallError{method: method, code: rpcResp.Error.Code, message: rpcResp.Error.Message, data: rpcResp.Error.Data}
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(rpcResp.Result, out)
}

// decodeRPCResponse reads a JSON-RPC reply from either a bare JSON body or a
// text/event-stream body, returning the response whose id matches the request.
func decodeRPCResponse(resp *http.Response, id int64, limit int) (rpcResponse, error) {
	// One byte past the cap tells a reply that was cut off from one that fit.
	body := &countingReader{r: io.LimitReader(resp.Body, int64(limit)+1)}
	if strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		rpcResp, err := decodeSSEResponse(body, id, limit)
		if err != nil && body.n > int64(limit) {
			return rpcResponse{}, errReplyTooLarge(limit)
		}
		return rpcResp, err
	}
	// Decoded from the stream, not read whole first: an image reply is tens
	// of MiB, and a buffered copy would hold it once more.
	var rpcResp rpcResponse
	if err := json.NewDecoder(body).Decode(&rpcResp); err != nil {
		if body.n > int64(limit) {
			return rpcResponse{}, errReplyTooLarge(limit)
		}
		return rpcResponse{}, err
	}
	return rpcResp, nil
}

// countingReader counts the bytes read through it.
type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// decodeSSEResponse scans an SSE stream and returns the first JSON-RPC message
// whose id matches the request, skipping unrelated events such as notifications.
func decodeSSEResponse(r io.Reader, id int64, limit int) (rpcResponse, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), limit)
	// Bytes, not a string: an image reply is tens of MiB, and every
	// conversion would copy it once more.
	var data bytes.Buffer
	flush := func() (rpcResponse, bool) {
		if data.Len() == 0 {
			return rpcResponse{}, false
		}
		defer data.Reset()
		var rpcResp rpcResponse
		if err := json.Unmarshal(data.Bytes(), &rpcResp); err != nil {
			return rpcResponse{}, false
		}
		return rpcResp, rpcResp.ID == id
	}
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			if rpcResp, ok := flush(); ok {
				return rpcResp, nil
			}
			continue
		}
		if value, found := bytes.CutPrefix(line, []byte("data:")); found {
			data.Write(bytes.TrimPrefix(value, []byte(" ")))
		}
	}
	if err := scanner.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			return rpcResponse{}, errReplyTooLarge(limit)
		}
		return rpcResponse{}, err
	}
	if rpcResp, ok := flush(); ok {
		return rpcResp, nil
	}
	return rpcResponse{}, fmt.Errorf("MCP response: no JSON-RPC message with id %d in SSE stream", id)
}

// exposeTools maps a server's advertised tools onto loom's exposed names,
// dropping those the server's optional allowlist (cfg.Tools) does not permit.
// Both transports go through here so the allowlist means the same everywhere.
func exposeTools(serverName string, cfg ServerConfig, tools []toolResult) []Tool {
	out := make([]Tool, 0, len(tools))
	for _, tool := range tools {
		if !cfg.toolAllowed(tool.Name) {
			continue
		}
		out = append(out, Tool{
			Name:         ExposedToolName(serverName, tool.Name),
			OriginalName: tool.Name,
			Title:        tool.displayTitle(),
			Description:  tool.Description,
			InputSchema:  tool.InputSchema,
			ServerName:   serverName,
		})
	}
	return out
}

// toolAllowed reports whether a server-side tool name passes the optional
// per-server allowlist (Tools). An empty allowlist permits every tool.
func (cfg ServerConfig) toolAllowed(name string) bool {
	if len(cfg.Tools) == 0 {
		return true
	}
	for _, allowed := range cfg.Tools {
		if allowed == name {
			return true
		}
	}
	return false
}

// scrubURLError redacts credentials from *url.Error values before they
// propagate into logs, upstream error messages, or the /mcp status panel: the
// query string (which may carry secrets such as an mcp.json URL's ?apiKey=) and any
// userinfo (user:pass@host in a configured server URL).
func scrubURLError(err error) error {
	var urlErr *url.Error
	if !errors.As(err, &urlErr) {
		return err
	}
	if u, parseErr := url.Parse(urlErr.URL); parseErr == nil && u.Host != "" {
		u.RawQuery = ""
		u.User = nil
		urlErr.URL = u.String()
	} else {
		// A scheme-less/opaque URL (e.g. "admin:pw@host/path") parses with an empty
		// Host and its user:pass@ folded into the scheme/opaque part, so clearing
		// u.User does nothing and u.String() reproduces the password. Strip userinfo
		// (and any query) by hand instead.
		urlErr.URL = stripURLUserinfo(urlErr.URL)
		if i := strings.IndexByte(urlErr.URL, '?'); i >= 0 {
			urlErr.URL = urlErr.URL[:i]
		}
	}
	return urlErr
}

// stripURLUserinfo removes a "user:pass@" authority segment from a URL string,
// including the scheme-less/opaque forms url.Parse can't isolate. It targets the
// '@' that precedes the host (before the first path '/'), leaving the path intact.
func stripURLUserinfo(raw string) string {
	authStart := 0
	if i := strings.Index(raw, "://"); i >= 0 {
		authStart = i + 3
	}
	rest := raw[authStart:]
	at := strings.IndexByte(rest, '@')
	if at < 0 {
		return raw
	}
	if slash := strings.IndexByte(rest, '/'); slash >= 0 && at > slash {
		return raw // the '@' is in the path, not userinfo
	}
	return raw[:authStart] + rest[at+1:]
}

type stdioClient struct {
	serverName string
	cfg        ServerConfig
	nextID     atomic.Int64
	mu         sync.Mutex
	callMu     sync.Mutex
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	stdout     io.Reader
	scanner    *bufio.Scanner
	startErr   error
	// initialized records that the CURRENT process has completed the
	// initialize handshake. Close resets it: a cancelled call kills the shared
	// process, and the next call starts a fresh one that must be initialized
	// again (a sync.Once here left every later request to a restarted server
	// failing with "not initialized").
	initialized bool
	// initMu serialises the check-then-initialize so two concurrent first
	// calls send one handshake: a second "initialize" on an initialized
	// process is an error for servers built on the reference SDKs.
	initMu sync.Mutex
}

// NewStdioClient creates a Client that communicates with an MCP server via stdin/stdout.
func NewStdioClient(serverName string, cfg ServerConfig) Client {
	return &stdioClient{serverName: serverName, cfg: cfg}
}

func (c *stdioClient) ListTools(ctx context.Context) ([]Tool, error) {
	if err := c.initialize(ctx); err != nil {
		return nil, err
	}
	var result listToolsResult
	if err := c.call(ctx, "tools/list", nil, &result); err != nil {
		return nil, err
	}
	return exposeTools(c.serverName, c.cfg, result.Tools), nil
}

func (c *stdioClient) CallTool(ctx context.Context, name string, arguments map[string]any) (CallResult, error) {
	if err := c.initialize(ctx); err != nil {
		return CallResult{}, err
	}
	var result callToolResult
	if err := c.call(ctx, "tools/call", map[string]any{"name": name, "arguments": arguments}, &result); err != nil {
		return CallResult{}, err
	}
	if result.IsError {
		return CallResult{}, fmt.Errorf("MCP tool %q returned an error: %s", name, toolContentText(result.Content))
	}
	return result.toCallResult(c.cfg.Artifacts), nil
}

func (c *stdioClient) Close() error {
	return c.closeIf(nil)
}

// closeIf is Close for the process scanner belongs to: when scanner is set
// and another call has meanwhile started a new process (its own scanner),
// that process is left alone. nil closes whatever runs.
func (c *stdioClient) closeIf(scanner *bufio.Scanner) error {
	c.mu.Lock()
	if scanner != nil && c.scanner != scanner {
		c.mu.Unlock()
		return nil
	}
	stdin := c.stdin
	cmd := c.cmd
	c.stdin = nil
	c.stdout = nil
	c.scanner = nil
	c.cmd = nil
	c.initialized = false
	c.startErr = nil
	c.mu.Unlock()

	if stdin != nil {
		_ = stdin.Close()
	}
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}
	return nil
}

func (c *stdioClient) initialize(ctx context.Context) error {
	c.initMu.Lock()
	defer c.initMu.Unlock()
	if err := c.start(ctx); err != nil {
		return err
	}
	c.mu.Lock()
	done := c.initialized
	c.mu.Unlock()
	if done {
		return nil
	}
	if err := c.call(ctx, "initialize", map[string]any{
		"protocolVersion": legacyProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      clientInfo,
	}, nil); err != nil {
		return err
	}
	c.mu.Lock()
	c.initialized = true
	c.mu.Unlock()
	return nil
}

func (c *stdioClient) start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cmd != nil || c.startErr != nil {
		return c.startErr
	}
	// The process outlives this call (it is shared by every later call), so
	// ctx must not own it; it only decides whether to start at all.
	if err := ctx.Err(); err != nil {
		return err
	}
	cmd := exec.Command(c.cfg.Command, c.cfg.Args...) //nolint:gosec // MCP servers are launched from the operator-supplied config file, never from request data
	cmd.Env = os.Environ()
	for key, value := range c.cfg.Env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		c.startErr = err
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		c.startErr = err
		return err
	}
	if err := cmd.Start(); err != nil {
		c.startErr = err
		return err
	}
	c.cmd = cmd
	c.stdin = stdin
	c.stdout = stdout
	c.scanner = c.newScanner(stdout)
	return nil
}

// newScanner reads the server's reply lines. One reply is one line, so a
// server with artifacts needs room for images.
func (c *stdioClient) newScanner(stdout io.Reader) *bufio.Scanner {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), c.cfg.maxLineBytes())
	return scanner
}

func (c *stdioClient) call(ctx context.Context, method string, params any, out any) error {
	done := make(chan error, 1)
	go func() {
		done <- c.doCall(method, params, out)
	}()
	select {
	case err := <-done:
		if ctx.Err() != nil && (errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe)) {
			return ctx.Err()
		}
		return err
	case <-ctx.Done():
		_ = c.Close()
		// Wait for doCall to unwind after the close. Its error is mostly the
		// closed pipe; the cancel already says why the call ended.
		if err := <-done; err != nil {
			slog.Debug("mcp stdio call unwound after cancel", "server", c.serverName, "err", err)
		}
		return ctx.Err()
	}
}

// shrinkScanner replaces a scanner whose buffer grew past a plain reply (an
// image line) with a fresh one, so a long-running server does not pin tens of
// MiB between calls. Calls are serialised and the reply has just been read,
// so nothing waits in the old buffer but, at most, a notification.
func (c *stdioClient) shrinkScanner(scanner *bufio.Scanner) {
	if len(scanner.Bytes()) <= maxStdioLineBytes {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.scanner == scanner && c.stdout != nil {
		c.scanner = c.newScanner(c.stdout)
	}
}

func (c *stdioClient) doCall(method string, params any, out any) error {
	c.callMu.Lock()
	defer c.callMu.Unlock()
	c.mu.Lock()
	stdin := c.stdin
	scanner := c.scanner
	c.mu.Unlock()
	if stdin == nil || scanner == nil {
		return io.ErrClosedPipe
	}
	id := c.nextID.Add(1)
	payload, err := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	if err != nil {
		return err
	}
	if _, err := stdin.Write(append(payload, '\n')); err != nil {
		return err
	}
	for scanner.Scan() {
		var resp rpcResponse
		if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
			return err
		}
		if resp.ID != id {
			continue
		}
		c.shrinkScanner(scanner)
		if resp.Error != nil {
			return fmt.Errorf("MCP %s failed: %s", method, resp.Error.Message)
		}
		if out == nil {
			return nil
		}
		return json.Unmarshal(resp.Result, out)
	}
	// A stopped scanner never reads again (a line past the cap, or the process
	// gone): drop the process so the next call starts a fresh one instead of
	// failing until loom restarts. Only this call's process: another call may
	// already have started the next one.
	_ = c.closeIf(scanner)
	if err := scanner.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			return errReplyTooLarge(c.cfg.maxLineBytes())
		}
		return err
	}
	return io.ErrUnexpectedEOF
}
