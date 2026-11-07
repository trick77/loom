package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// tavily is Tavily's search API (POST /search), the one call this sidecar
// makes. Nothing about Tavily leaves this file: the tool speaks the search
// contract (docs/search.md).
type tavily struct {
	http    *http.Client
	baseURL string
	key     string
}

type tavilyRequest struct {
	Query       string `json:"query"`
	MaxResults  int    `json:"max_results,omitempty"`
	SearchDepth string `json:"search_depth,omitempty"`
	Topic       string `json:"topic,omitempty"`
	TimeRange   string `json:"time_range,omitempty"`
}

type tavilyResult struct {
	Title         string `json:"title"`
	URL           string `json:"url"`
	Content       string `json:"content"`
	PublishedDate string `json:"published_date"`
}

type tavilyResponse struct {
	Results []tavilyResult `json:"results"`
}

// maxTavilyBody bounds Tavily's reply: a page of results, never megabytes.
const maxTavilyBody = 2 << 20

func (c tavily) search(ctx context.Context, req tavilyRequest) ([]tavilyResult, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.baseURL, "/")+"/search", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.key)
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("search provider unreachable: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxTavilyBody+1))
	if err != nil {
		return nil, fmt.Errorf("search provider reply unreadable: %w", err)
	}
	if len(raw) > maxTavilyBody {
		// Cut JSON would only read as unreadable: say what happened.
		return nil, fmt.Errorf("search provider reply over %d MiB; ask for fewer results or basic depth", maxTavilyBody>>20)
	}
	if resp.StatusCode != http.StatusOK {
		// The status says what went wrong (401 key, 429 or 432 credits); the
		// body may echo the request, so only its start goes into the error.
		return nil, fmt.Errorf("search provider answered %d: %.200s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var out tavilyResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("search provider reply unreadable: %w", err)
	}
	return out.Results, nil
}
