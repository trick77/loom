package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const (
	searxngToolName      = "web_search"
	defaultSearchResults = 5
	maxSearchResults     = 10
)

type searxngClient struct {
	serverName string
	baseURL    string
	httpClient *http.Client
}

type searxngSearchResponse struct {
	Results []searxngResult `json:"results"`
}

type searxngResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Content string `json:"content"`
	Engine  string `json:"engine"`
}

// NewSearxngClient exposes a configured SearXNG instance as an in-process
// Spark tool provider. It intentionally does not require MCP proxy transport.
func NewSearxngClient(serverName, baseURL string, httpClient *http.Client) Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultHTTPTimeout}
	}
	return &searxngClient{
		serverName: serverName,
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: httpClient,
	}
}

func (c *searxngClient) ListTools(context.Context) ([]Tool, error) {
	return []Tool{{
		Name:         ExposedToolName(c.serverName, searxngToolName),
		OriginalName: searxngToolName,
		Description:  "Search the web using the configured SearXNG instance.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"q":           map[string]any{"type": "string", "description": "Search query."},
				"categories":  map[string]any{"type": "string", "description": "Comma-separated SearXNG categories."},
				"engines":     map[string]any{"type": "string", "description": "Comma-separated SearXNG engines."},
				"language":    map[string]any{"type": "string", "description": "Search language code."},
				"pageno":      map[string]any{"type": "integer", "minimum": 1, "description": "SearXNG result page."},
				"time_range":  map[string]any{"type": "string", "enum": []string{"day", "month", "year"}, "description": "Optional time range."},
				"safesearch":  map[string]any{"type": "integer", "enum": []int{0, 1, 2}, "description": "Safe search level."},
				"max_results": map[string]any{"type": "integer", "minimum": 1, "maximum": maxSearchResults, "description": "Maximum results to return."},
			},
			"required": []string{"q"},
		},
		ServerName: c.serverName,
	}}, nil
}

func (c *searxngClient) CallTool(ctx context.Context, name string, arguments map[string]any) (string, error) {
	if name != searxngToolName {
		return "", fmt.Errorf("unknown SearXNG tool %q", name)
	}
	query := stringArg(arguments, "q")
	if strings.TrimSpace(query) == "" {
		return "", fmt.Errorf("SearXNG tool %q requires q", name)
	}
	requestURL, maxResults, err := c.searchURL(arguments, query)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("SearXNG search failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(bytes)))
	}
	var payload searxngSearchResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxRPCResponseBytes)).Decode(&payload); err != nil {
		return "", fmt.Errorf("decode SearXNG search response: %w", err)
	}
	return formatSearxngResults(query, payload.Results, maxResults), nil
}

func (c *searxngClient) Close() error { return nil }

func (c *searxngClient) Probe(ctx context.Context) error {
	base, err := url.Parse(c.baseURL + "/config")
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base.String(), nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("SearXNG config failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(bytes)))
	}
	_, err = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	return err
}

func (c *searxngClient) searchURL(arguments map[string]any, query string) (string, int, error) {
	base, err := url.Parse(c.baseURL + "/search")
	if err != nil {
		return "", 0, err
	}
	values := base.Query()
	values.Set("format", "json")
	values.Set("q", query)
	for _, key := range []string{"categories", "engines", "language", "time_range"} {
		if value := stringArg(arguments, key); value != "" {
			values.Set(key, value)
		}
	}
	for _, key := range []string{"pageno", "safesearch"} {
		if value, ok := intArg(arguments, key); ok {
			values.Set(key, strconv.Itoa(value))
		}
	}
	maxResults := defaultSearchResults
	if value, ok := intArg(arguments, "max_results"); ok {
		if value < 1 {
			value = 1
		}
		if value > maxSearchResults {
			value = maxSearchResults
		}
		maxResults = value
	}
	base.RawQuery = values.Encode()
	return base.String(), maxResults, nil
}

func formatSearxngResults(query string, results []searxngResult, maxResults int) string {
	if len(results) == 0 {
		return fmt.Sprintf("No SearXNG results for %q.", query)
	}
	if len(results) > maxResults {
		results = results[:maxResults]
	}
	var builder strings.Builder
	for i, result := range results {
		if i > 0 {
			builder.WriteByte('\n')
		}
		title := strings.TrimSpace(result.Title)
		if title == "" {
			title = "Untitled result"
		}
		builder.WriteString(strconv.Itoa(i + 1))
		builder.WriteString(". ")
		builder.WriteString(title)
		if strings.TrimSpace(result.URL) != "" {
			builder.WriteString("\nURL: ")
			builder.WriteString(strings.TrimSpace(result.URL))
		}
		if strings.TrimSpace(result.Content) != "" {
			builder.WriteString("\nSnippet: ")
			builder.WriteString(strings.TrimSpace(result.Content))
		}
		if strings.TrimSpace(result.Engine) != "" {
			builder.WriteString("\nSource: ")
			builder.WriteString(strings.TrimSpace(result.Engine))
		}
		builder.WriteByte('\n')
	}
	return strings.TrimSpace(builder.String())
}

func stringArg(arguments map[string]any, key string) string {
	value, ok := arguments[key]
	if !ok {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

func intArg(arguments map[string]any, key string) (int, bool) {
	value, ok := arguments[key]
	if !ok {
		return 0, false
	}
	switch typed := value.(type) {
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case float64:
		return int(typed), true
	case json.Number:
		parsed, err := typed.Int64()
		return int(parsed), err == nil
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(typed))
		return parsed, err == nil
	default:
		return 0, false
	}
}
