package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
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
}

func NewRemoteClient(serverName string, cfg ServerConfig, httpClient *http.Client) Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
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
	req.Header.Set("Accept", "application/json")
	for key, value := range c.cfg.Headers {
		req.Header.Set(key, value)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("MCP %s failed with status %d: %s", method, resp.StatusCode, strings.TrimSpace(string(bytes)))
	}
	var rpcResp rpcResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
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
	defer c.mu.Unlock()
	if c.stdin != nil {
		_ = c.stdin.Close()
	}
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
		_, _ = c.cmd.Process.Wait()
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
	cmd := exec.CommandContext(ctx, c.cfg.Command, c.cfg.Args...)
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
	_ = ctx
	c.mu.Lock()
	defer c.mu.Unlock()
	id := c.nextID.Add(1)
	payload, err := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	if err != nil {
		return err
	}
	if _, err := c.stdin.Write(append(payload, '\n')); err != nil {
		return err
	}
	for c.scanner.Scan() {
		var resp rpcResponse
		if err := json.Unmarshal(c.scanner.Bytes(), &resp); err != nil {
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
	if err := c.scanner.Err(); err != nil {
		return err
	}
	return io.ErrUnexpectedEOF
}

func toolContentText(content []toolContent) string {
	var parts []string
	for _, item := range content {
		if item.Type == "text" && item.Text != "" {
			parts = append(parts, item.Text)
		}
	}
	return strings.Join(parts, "\n")
}
