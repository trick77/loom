package mcpserver

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testHandler() http.Handler {
	return New(Server{Name: "t", Version: "v1", Instructions: "hello", Tools: []Tool{
		{
			Name: "echo", Title: "Echo", Description: "echoes",
			Schema: ObjectSchema(map[string]any{"text": map[string]any{"type": "string"}}, "text"),
			Run: func(_ context.Context, args json.RawMessage) Result {
				var a struct {
					Text string `json:"text"`
				}
				if err := DecodeArgs(args, &a); err != nil {
					return ErrorResult("bad args: " + err.Error())
				}
				return Result{Text: a.Text, Sources: []Source{{URL: "https://x.example/" + a.Text, Title: a.Text}}}
			},
		},
		{Name: "big", Run: func(context.Context, json.RawMessage) Result {
			return Result{Text: strings.Repeat("line of text\n", 4000)}
		}},
	}})
}

type rpcReply struct {
	Result map[string]any `json:"result"`
	Error  *rpcError      `json:"error"`
}

func call(t *testing.T, h http.Handler, body string, headers map[string]string) (int, rpcReply, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var r rpcReply
	_ = json.Unmarshal(rec.Body.Bytes(), &r)
	return rec.Code, r, rec.Body.String()
}

func TestHandler_transportRules(t *testing.T) {
	h := testHandler()
	for _, m := range []string{http.MethodGet, http.MethodDelete} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(m, "/mcp", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: %d", m, rec.Code)
		}
	}
	if code, _, _ := call(t, h, `{"jsonrpc":"2.0","id":1,"method":"ping"}`, map[string]string{"Origin": "https://evil.example"}); code != http.StatusForbidden {
		t.Errorf("origin: %d", code)
	}
	if code, r, _ := call(t, h, `{nope`, nil); code != http.StatusBadRequest || r.Error.Code != codeParseError {
		t.Errorf("parse: %d %+v", code, r.Error)
	}
	if code, r, _ := call(t, h, `[{"jsonrpc":"2.0","id":1,"method":"ping"}]`, nil); code != http.StatusBadRequest || r.Error.Code != codeInvalidRequest {
		t.Errorf("batch: %d", code)
	}
	if code, r, _ := call(t, h, `{"jsonrpc":"1.0","id":1,"method":"ping"}`, nil); code != http.StatusBadRequest || r.Error.Code != codeInvalidRequest {
		t.Errorf("version: %d", code)
	}
	big := `{"jsonrpc":"2.0","id":1,"method":"ping","params":{"p":"` + strings.Repeat("x", maxBodyBytes) + `"}}`
	if code, _, _ := call(t, h, big, nil); code != http.StatusRequestEntityTooLarge {
		t.Errorf("size: %d", code)
	}
}

func TestHandler_legacyFlow(t *testing.T) {
	h := testHandler()
	_, r, _ := call(t, h, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`, nil)
	if r.Result["protocolVersion"] != "2025-06-18" || r.Result["instructions"] != "hello" {
		t.Fatalf("initialize: %+v", r.Result)
	}
	_, r, _ = call(t, h, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"1999-01-01"}}`, nil)
	if r.Result["protocolVersion"] != defaultLegacyVersion {
		t.Errorf("unknown version answered %v", r.Result["protocolVersion"])
	}
	if code, _, _ := call(t, h, `{"jsonrpc":"2.0","method":"notifications/initialized"}`, nil); code != http.StatusAccepted {
		t.Errorf("notification: %d", code)
	}
	_, r, _ = call(t, h, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`, nil)
	tools, _ := r.Result["tools"].([]any)
	if len(tools) != 2 || tools[0].(map[string]any)["name"] != "echo" {
		t.Fatalf("tools/list: %+v", r.Result)
	}
	_, r, raw := call(t, h, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"echo","arguments":{"text":"hi"}}}`, nil)
	if !strings.Contains(raw, `"text":"hi"`) || !strings.Contains(raw, `"sources":[{"url":"https://x.example/hi"`) {
		t.Errorf("call: %s", raw)
	}
	_, r, raw = call(t, h, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"echo","arguments":{"txt":"hi"}}}`, nil)
	if r.Result["isError"] != true || !strings.Contains(raw, "bad args") {
		t.Errorf("strict args: %s", raw)
	}
	_, r, _ = call(t, h, `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"nope"}}`, nil)
	if r.Error == nil || r.Error.Code != codeInvalidParams {
		t.Errorf("unknown tool: %+v", r)
	}
	_, r, _ = call(t, h, `{"jsonrpc":"2.0","id":6,"method":"bogus"}`, nil)
	if r.Error == nil || r.Error.Code != codeMethodNotFound {
		t.Errorf("bogus: %+v", r)
	}
	if code, r, _ := call(t, h, `{"jsonrpc":"2.0","id":7,"method":"ping"}`, map[string]string{"MCP-Protocol-Version": "1999-01-01"}); code != http.StatusBadRequest || r.Error.Code != codeUnsupportedVersion {
		t.Errorf("bad header version: %d", code)
	}
}

func TestHandler_modernFlow(t *testing.T) {
	h := testHandler()
	meta := `"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28"}`
	hdr := func(method, name string) map[string]string {
		m := map[string]string{"MCP-Protocol-Version": ModernProtocolVersion, "Mcp-Method": method}
		if name != "" {
			m["Mcp-Name"] = name
		}
		return m
	}
	_, r, _ := call(t, h, `{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{`+meta+`}}`, hdr("server/discover", ""))
	if r.Result["resultType"] != "complete" || r.Result["instructions"] != "hello" {
		t.Fatalf("discover: %+v", r)
	}
	_, r, _ = call(t, h, `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{`+meta+`}}`, hdr("tools/list", ""))
	if len(r.Result["tools"].([]any)) != 2 {
		t.Fatalf("list: %+v", r)
	}
	encoded := "=?base64?" + base64.StdEncoding.EncodeToString([]byte("echo")) + "?="
	_, _, raw := call(t, h, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{`+meta+`,"name":"echo","arguments":{"text":"x"}}}`, hdr("tools/call", encoded))
	if !strings.Contains(raw, `"resultType":"complete"`) || !strings.Contains(raw, `"text":"x"`) {
		t.Errorf("call: %s", raw)
	}
	if code, _, _ := call(t, h, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{`+meta+`,"name":"echo"}}`, hdr("tools/call", "big")); code != http.StatusBadRequest {
		t.Errorf("name mismatch: %d", code)
	}
	if code, _, _ := call(t, h, `{"jsonrpc":"2.0","id":5,"method":"tools/list","params":{`+meta+`}}`, hdr("tools/call", "")); code != http.StatusBadRequest {
		t.Errorf("method mismatch: %d", code)
	}
	if code, _, _ := call(t, h, `{"jsonrpc":"2.0","id":6,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2030-01-01"}}}`, hdr("tools/list", "")); code != http.StatusBadRequest {
		t.Errorf("unsupported: %d", code)
	}
	if code, _, _ := call(t, h, `{"jsonrpc":"2.0","method":"notifications/x","params":{`+meta+`}}`, hdr("notifications/x", "")); code != http.StatusAccepted {
		t.Errorf("notification: %d", code)
	}
}

func TestHandler_longTextIsCappedAtALine(t *testing.T) {
	_, _, raw := call(t, testHandler(), `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"big"}}`, nil)
	var r struct {
		Result struct {
			Content []struct{ Text string } `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatal(err)
	}
	text := r.Result.Content[0].Text
	if len(text) > maxToolTextBytes || !strings.HasSuffix(text, "line of text"+truncatedNote) {
		t.Errorf("len %d, tail %q", len(text), text[len(text)-40:])
	}
	if !bytes.Equal([]byte(capText("short", 10)), []byte("short")) {
		t.Error("short text changed")
	}
}
