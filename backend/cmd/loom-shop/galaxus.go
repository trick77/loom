package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/trick77/loom/internal/sidecar"
)

// The shop's MCP server (Galaxus, docs/shop.md): Streamable HTTP, no auth, a
// session from initialize that every later request carries. Measured
// 2026-11-12: a request without the session header is 400 "Session ID
// required"; an unknown session id is accepted.
const (
	upstreamProtocolVersion = "2025-06-18"
	upstreamTool            = "search_products"
	// agentInfo is required by the shop "for analytics only": loom names
	// itself, never a model.
	agentInfo = "loom"
	// maxUpstreamBytes bounds one answer. The shop's _meta repeats its whole
	// GraphQL payload next to the text, about 6 KB a product: 20 products
	// stay far below.
	maxUpstreamBytes = 2 << 20
)

// upstream is a minimal MCP client for the one call the shop answers: a
// session opened on first use and once more when the shop drops it.
type upstream struct {
	http    *http.Client
	url     string
	version string
	nextID  atomic.Int64
	// turn guards session and serialises the handshake: a one-slot channel,
	// so a caller waiting behind a slow handshake gives up at its own
	// deadline instead of past it.
	turn    chan struct{}
	session string
}

func newUpstream(client *http.Client, url, version string) *upstream {
	return &upstream{http: client, url: url, version: version, turn: make(chan struct{}, 1)}
}

func (u *upstream) lock(ctx context.Context) error {
	select {
	case u.turn <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (u *upstream) unlock() { <-u.turn }

// shopSearch is search_products' arguments as the shop names them. There is
// no userIntent: the shop asks for it "for analytics", and the user's words
// stay with loom.
type shopSearch struct {
	Query     string `json:"query"`
	AgentInfo string `json:"agentInfo"`
	Language  string `json:"language"`
	First     int    `json:"first"`
	MinPrice  int    `json:"minPrice,omitempty"`
	MaxPrice  int    `json:"maxPrice,omitempty"`
}

// shopResult is search_products' JSON text.
type shopResult struct {
	CorrectedSearchTerm *string       `json:"correctedSearchTerm"`
	TotalCount          int           `json:"totalCount"`
	Products            []shopProduct `json:"products"`
	ErrorMessage        string        `json:"errorMessage"`
	// offers are what the result's _meta adds, by product id (offer.go).
	offers map[int64]offer
}

type shopProduct struct {
	// ID joins a product to _meta's extras. Raw, read leniently (productID):
	// a changed id costs the extras, never the search.
	ID               json.RawMessage `json:"id"`
	Name             string          `json:"name"`
	Properties       string          `json:"properties"`
	Description      string          `json:"description"`
	ProductType      string          `json:"productType"`
	Brand            string          `json:"brand"`
	PriceIfAvailable *struct {
		AmountInclusive float64 `json:"amountInclusive"`
		Currency        string  `json:"currency"`
	} `json:"priceIfAvailable"`
	PageURL      string `json:"pageUrl"`
	PreviewImage *struct {
		URL string `json:"url"`
	} `json:"previewImage"`
	RatingSummaryIfAvailable *struct {
		AverageRating float64 `json:"averageRating"`
		RatingCount   int     `json:"ratingCount"`
	} `json:"ratingSummaryIfAvailable"`
	ProductReviewsSummaryIfAvailable *struct {
		Pros []string `json:"pros"`
		Cons []string `json:"cons"`
	} `json:"productReviewsSummaryIfAvailable"`
}

// errShop is the shop's own failure (an MCP error result): its message is
// meant for the caller.
type errShop struct{ msg string }

func (e errShop) Error() string {
	if e.msg == "" {
		return "the shop refused the search"
	}
	return e.msg
}

// errSession is a request the shop refused for its session.
var errSession = errors.New("session refused")

// search runs search_products. A refused session is opened again once.
func (u *upstream) search(ctx context.Context, args shopSearch) (shopResult, error) {
	session, err := u.ensureSession(ctx)
	if err != nil {
		return shopResult{}, err
	}
	res, err := u.callTool(ctx, session, args)
	if !errors.Is(err, errSession) {
		return res, err
	}
	if err := u.dropSession(ctx, session); err != nil {
		return shopResult{}, err
	}
	if session, err = u.ensureSession(ctx); err != nil {
		return shopResult{}, err
	}
	return u.callTool(ctx, session, args)
}

func (u *upstream) ensureSession(ctx context.Context) (string, error) {
	if err := u.lock(ctx); err != nil {
		return "", err
	}
	defer u.unlock()
	if u.session != "" {
		return u.session, nil
	}
	_, header, err := u.post(ctx, "", "initialize", map[string]any{
		"protocolVersion": upstreamProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]string{"name": "loom-shop", "version": u.version},
	})
	if err != nil {
		return "", fmt.Errorf("initialize: %w", err)
	}
	session := header.Get("Mcp-Session-Id")
	if session == "" {
		return "", errors.New("initialize: the shop sent no session")
	}
	if _, _, err := u.post(ctx, session, "notifications/initialized", nil); err != nil {
		return "", fmt.Errorf("initialized: %w", err)
	}
	u.session = session
	return session, nil
}

// dropSession forgets session unless another call already replaced it.
func (u *upstream) dropSession(ctx context.Context, session string) error {
	if err := u.lock(ctx); err != nil {
		return err
	}
	if u.session == session {
		u.session = ""
	}
	u.unlock()
	return nil
}

func (u *upstream) callTool(ctx context.Context, session string, args shopSearch) (shopResult, error) {
	raw, _, err := u.post(ctx, session, "tools/call", map[string]any{"name": upstreamTool, "arguments": args})
	if err != nil {
		return shopResult{}, err
	}
	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool            `json:"isError"`
		Meta    json.RawMessage `json:"_meta"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return shopResult{}, fmt.Errorf("decode result: %w", err)
	}
	var texts []string
	for _, c := range result.Content {
		if c.Type == "text" && strings.TrimSpace(c.Text) != "" {
			texts = append(texts, strings.TrimSpace(c.Text))
		}
	}
	if result.IsError {
		return shopResult{}, errShop{msg: strings.Join(texts, " ")}
	}
	// The products are one JSON text; a note beside it (a second block) is
	// not the payload.
	decodeErr := errors.New("no text in the result")
	for _, text := range texts {
		var out shopResult
		if decodeErr = json.Unmarshal([]byte(text), &out); decodeErr == nil {
			out.offers = offers(result.Meta)
			return out, nil
		}
	}
	return shopResult{}, fmt.Errorf("decode products: %w", decodeErr)
}

type rpcReply struct {
	ID     json.RawMessage `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// post sends one JSON-RPC message; params nil and a method under
// notifications/ make it a notification (no id, no answer read). It returns
// the result and the response's headers.
func (u *upstream) post(ctx context.Context, session, method string, params any) (json.RawMessage, http.Header, error) {
	msg := map[string]any{"jsonrpc": "2.0", "method": method}
	notification := strings.HasPrefix(method, "notifications/")
	var id int64
	if !notification {
		id = u.nextID.Add(1)
		msg["id"] = id
	}
	if params != nil {
		msg["params"] = params
	}
	body, err := json.Marshal(msg)
	if err != nil {
		return nil, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.url, bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("User-Agent", sidecar.UserAgent("loom-shop", u.version))
	if session != "" {
		req.Header.Set("Mcp-Session-Id", session)
		req.Header.Set("Mcp-Protocol-Version", upstreamProtocolVersion)
	}
	resp, err := u.http.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		// The spec's answer to an expired session is a 404 (any body); the
		// shop's own is a 400 naming the session id.
		if session != "" && (resp.StatusCode == http.StatusNotFound ||
			resp.StatusCode == http.StatusBadRequest && sessionRefused(string(raw))) {
			return nil, nil, errSession
		}
		return nil, nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	if notification {
		return nil, resp.Header, nil
	}
	reply, err := findReply(resp.Body, resp.Header.Get("Content-Type"), id)
	if err != nil {
		return nil, nil, err
	}
	if reply.Error != nil {
		if session != "" && sessionRefused(reply.Error.Message) {
			return nil, nil, errSession
		}
		return nil, nil, fmt.Errorf("error %d: %s", reply.Error.Code, reply.Error.Message)
	}
	return reply.Result, resp.Header, nil
}

// sessionRefused reports whether an error names the session as the problem
// ("Session ID required", "session not found"), not merely the word: a
// bad argument mentioning one must reach the model, not trigger a retry.
func sessionRefused(msg string) bool {
	msg = strings.ToLower(msg)
	for _, phrase := range []string{"session id", "session not found", "session expired", "invalid session", "unknown session"} {
		if strings.Contains(msg, phrase) {
			return true
		}
	}
	return false
}

// sameID reports whether a reply's id is the request's, sent back as a
// number or as a string.
func sameID(raw json.RawMessage, id int64) bool {
	var n json.Number
	if json.Unmarshal(raw, &n) == nil {
		got, err := n.Int64()
		return err == nil && got == id
	}
	var s string
	return json.Unmarshal(raw, &s) == nil && s == strconv.FormatInt(id, 10)
}

// countingReader counts what was read through it.
type countingReader struct {
	r io.Reader
	n int
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += n
	return n, err
}

// findReply reads the answer to id from a JSON body or an SSE stream, as it
// arrives: an event's data lines joined by newlines (SSE lets one message
// span several), events ended by a blank line. The matching event ends the
// read, so a server that keeps the stream open is not waited out.
func findReply(body io.Reader, contentType string, id int64) (rpcReply, error) {
	counted := &countingReader{r: io.LimitReader(body, maxUpstreamBytes+1)}
	tooBig := fmt.Errorf("answer over %d bytes", maxUpstreamBytes)
	if !strings.HasPrefix(strings.ToLower(contentType), "text/event-stream") {
		raw, err := io.ReadAll(counted)
		if err != nil {
			return rpcReply{}, err
		}
		if counted.n > maxUpstreamBytes {
			return rpcReply{}, tooBig
		}
		var reply rpcReply
		if err := json.Unmarshal(raw, &reply); err != nil {
			return rpcReply{}, fmt.Errorf("decode answer: %w", err)
		}
		// A request-level error comes back with id null: its message is
		// still the answer the caller needs.
		if !sameID(reply.ID, id) && (reply.Error == nil || string(reply.ID) != "null") {
			return rpcReply{}, errors.New("the answer is to another request")
		}
		return reply, nil
	}
	var data []string
	matches := func() (rpcReply, bool) {
		var reply rpcReply
		ok := len(data) > 0 && json.Unmarshal([]byte(strings.Join(data, "\n")), &reply) == nil && sameID(reply.ID, id)
		data = data[:0]
		return reply, ok
	}
	scanner := bufio.NewScanner(counted)
	scanner.Buffer(make([]byte, 64<<10), maxUpstreamBytes)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if reply, ok := matches(); ok {
				return reply, nil
			}
			continue
		}
		if value, ok := strings.CutPrefix(line, "data:"); ok {
			data = append(data, strings.TrimPrefix(value, " "))
		}
	}
	if err := scanner.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			return rpcReply{}, tooBig
		}
		return rpcReply{}, fmt.Errorf("read stream: %w", err)
	}
	if counted.n > maxUpstreamBytes {
		return rpcReply{}, tooBig
	}
	// A stream may end without the blank line after its last event.
	if reply, ok := matches(); ok {
		return reply, nil
	}
	return rpcReply{}, errors.New("no answer in the stream")
}
