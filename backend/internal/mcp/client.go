package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultHTTPTimeout  = 15 * time.Second
	maxRPCResponseBytes = 4 << 20
	maxToolOutputBytes  = 32 << 10
)

type Tool struct {
	Name         string
	OriginalName string
	Description  string
	InputSchema  map[string]any
	ServerName   string
}

type Client interface {
	ListTools(context.Context) ([]Tool, error)
	CallTool(context.Context, string, map[string]any) (string, error)
	Close() error
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
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type listToolsResult struct {
	Tools []toolResult `json:"tools"`
}

type toolResult struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type callToolResult struct {
	Content []toolContent `json:"content"`
	IsError bool          `json:"isError"`
}

type toolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type remoteClient struct {
	serverName string
	cfg        ServerConfig
	httpClient *http.Client
	nextID     atomic.Int64
	initOnce   sync.Once
	initErr    error
	mu         sync.Mutex
	sessionID  string
}

func NewRemoteClient(serverName string, cfg ServerConfig, httpClient *http.Client) Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultHTTPTimeout}
	}
	return &remoteClient{serverName: serverName, cfg: cfg, httpClient: httpClient}
}

func (c *remoteClient) ListTools(ctx context.Context) ([]Tool, error) {
	if err := c.initialize(ctx); err != nil {
		return nil, err
	}
	var result listToolsResult
	if err := c.call(ctx, "tools/list", nil, &result); err != nil {
		return nil, err
	}
	return c.exposeTools(result.Tools), nil
}

func (c *remoteClient) CallTool(ctx context.Context, name string, arguments map[string]any) (string, error) {
	if err := c.initialize(ctx); err != nil {
		return "", err
	}
	var result callToolResult
	if err := c.call(ctx, "tools/call", map[string]any{"name": name, "arguments": arguments}, &result); err != nil {
		return "", err
	}
	if result.IsError {
		return "", fmt.Errorf("MCP tool %q returned an error: %s", name, toolContentText(result.Content))
	}
	return toolContentText(result.Content), nil
}

func (c *remoteClient) Close() error { return nil }

func (c *remoteClient) initialize(ctx context.Context) error {
	c.initOnce.Do(func() {
		c.initErr = c.call(ctx, "initialize", map[string]any{
			"protocolVersion": "2025-06-18",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]string{"name": "spark", "version": "dev"},
		}, nil)
	})
	return c.initErr
}

func (c *remoteClient) call(ctx context.Context, method string, params any, out any) error {
	id := c.nextID.Add(1)
	body, err := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params})
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
	c.mu.Lock()
	sessionID := c.sessionID
	c.mu.Unlock()
	if sessionID != "" {
		req.Header.Set("Mcp-Session-Id", sessionID)
	}
	for key, value := range c.cfg.Headers {
		req.Header.Set(key, value)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if id := resp.Header.Get("Mcp-Session-Id"); id != "" {
		c.mu.Lock()
		c.sessionID = id
		c.mu.Unlock()
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("MCP %s failed with status %d: %s", method, resp.StatusCode, strings.TrimSpace(string(bytes)))
	}
	rpcResp, err := decodeRPCResponse(resp, id)
	if err != nil {
		return err
	}
	if rpcResp.Error != nil {
		return fmt.Errorf("MCP %s failed: %s", method, rpcResp.Error.Message)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(rpcResp.Result, out)
}

// decodeRPCResponse reads a JSON-RPC reply from either a bare JSON body or a
// text/event-stream body, returning the response whose id matches the request.
func decodeRPCResponse(resp *http.Response, id int64) (rpcResponse, error) {
	limited := io.LimitReader(resp.Body, maxRPCResponseBytes)
	if strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		return decodeSSEResponse(limited, id)
	}
	var rpcResp rpcResponse
	if err := json.NewDecoder(limited).Decode(&rpcResp); err != nil {
		return rpcResponse{}, err
	}
	return rpcResp, nil
}

// decodeSSEResponse scans an SSE stream and returns the first JSON-RPC message
// whose id matches the request, skipping unrelated events such as notifications.
func decodeSSEResponse(r io.Reader, id int64) (rpcResponse, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), maxRPCResponseBytes)
	var data strings.Builder
	flush := func() (rpcResponse, bool) {
		if data.Len() == 0 {
			return rpcResponse{}, false
		}
		raw := data.String()
		data.Reset()
		var rpcResp rpcResponse
		if err := json.Unmarshal([]byte(raw), &rpcResp); err != nil {
			return rpcResponse{}, false
		}
		return rpcResp, rpcResp.ID == id
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if rpcResp, ok := flush(); ok {
				return rpcResp, nil
			}
			continue
		}
		if value, found := strings.CutPrefix(line, "data:"); found {
			data.WriteString(strings.TrimPrefix(value, " "))
		}
	}
	if err := scanner.Err(); err != nil {
		return rpcResponse{}, err
	}
	if rpcResp, ok := flush(); ok {
		return rpcResp, nil
	}
	return rpcResponse{}, fmt.Errorf("MCP response: no JSON-RPC message with id %d in SSE stream", id)
}

func (c *remoteClient) exposeTools(tools []toolResult) []Tool {
	out := make([]Tool, 0, len(tools))
	for _, tool := range tools {
		out = append(out, Tool{
			Name:         ExposedToolName(c.serverName, tool.Name),
			OriginalName: tool.Name,
			Description:  tool.Description,
			InputSchema:  tool.InputSchema,
			ServerName:   c.serverName,
		})
	}
	return out
}

type stdioClient struct {
	serverName string
	cfg        ServerConfig
	nextID     atomic.Int64
	mu         sync.Mutex
	callMu     sync.Mutex
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	scanner    *bufio.Scanner
	startErr   error
	initOnce   sync.Once
	initErr    error
}

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
	out := make([]Tool, 0, len(result.Tools))
	for _, tool := range result.Tools {
		out = append(out, Tool{
			Name:         ExposedToolName(c.serverName, tool.Name),
			OriginalName: tool.Name,
			Description:  tool.Description,
			InputSchema:  tool.InputSchema,
			ServerName:   c.serverName,
		})
	}
	return out, nil
}

func (c *stdioClient) CallTool(ctx context.Context, name string, arguments map[string]any) (string, error) {
	if err := c.initialize(ctx); err != nil {
		return "", err
	}
	var result callToolResult
	if err := c.call(ctx, "tools/call", map[string]any{"name": name, "arguments": arguments}, &result); err != nil {
		return "", err
	}
	if result.IsError {
		return "", fmt.Errorf("MCP tool %q returned an error: %s", name, toolContentText(result.Content))
	}
	return toolContentText(result.Content), nil
}

func (c *stdioClient) Close() error {
	c.mu.Lock()
	stdin := c.stdin
	cmd := c.cmd
	c.stdin = nil
	c.scanner = nil
	c.cmd = nil
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
	if err := c.start(ctx); err != nil {
		return err
	}
	c.initOnce.Do(func() {
		c.initErr = c.call(ctx, "initialize", map[string]any{
			"protocolVersion": "2025-06-18",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]string{"name": "spark", "version": "dev"},
		}, nil)
	})
	return c.initErr
}

func (c *stdioClient) start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cmd != nil || c.startErr != nil {
		return c.startErr
	}
	_ = ctx
	cmd := exec.Command(c.cfg.Command, c.cfg.Args...)
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
	c.scanner = bufio.NewScanner(stdout)
	c.scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	return nil
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
		<-done
		return ctx.Err()
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
		if resp.Error != nil {
			return fmt.Errorf("MCP %s failed: %s", method, resp.Error.Message)
		}
		if out == nil {
			return nil
		}
		return json.Unmarshal(resp.Result, out)
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return io.ErrUnexpectedEOF
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
