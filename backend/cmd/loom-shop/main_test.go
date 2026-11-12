package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/trick77/loom/internal/mcp"
)

const testToken = "0123456789abcdef0123456789abcdef"

func vars(values map[string]string) func(string) string {
	return func(k string) string { return values[k] }
}

func TestLoadConfig(t *testing.T) {
	cfg, err := loadConfig(vars(map[string]string{}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.addr != defaultAddr || cfg.upstreamURL != defaultUpstreamURL || cfg.token != "" {
		t.Fatalf("defaults %+v", cfg)
	}
	cfg, err = loadConfig(vars(map[string]string{"SHOP_TOKEN": testToken, "SHOP_UPSTREAM_URL": "OFF", "SHOP_ADDR": ":9000"}))
	if err != nil || cfg.upstreamURL != "" || cfg.addr != ":9000" || cfg.token != testToken {
		t.Fatalf("overrides %+v, %v", cfg, err)
	}
	for name, values := range map[string]map[string]string{
		"short token":  {"SHOP_TOKEN": "short"},
		"bad upstream": {"SHOP_UPSTREAM_URL": "mcp.galaxus.ch"},
	} {
		if _, err := loadConfig(vars(values)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestServerNeedsTheToken(t *testing.T) {
	for _, token := range []string{testToken, ""} {
		h := newServer(config{token: token, upstreamURL: "https://shop.example/"}, http.DefaultClient, "test")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{}`)))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("token %q, no bearer: %d", token, rec.Code)
		}
	}
}

// fakeShop is the shop's MCP server as measured: initialize hands out a
// session, every later request must carry it (400 "Session ID required"
// otherwise), answers come as SSE. dropAfter > 0 forgets every session after
// that many tool calls, as a restarted shop would.
type fakeShop struct {
	mu        sync.Mutex
	sessions  map[string]bool
	inits     int
	calls     int
	dropAfter int
	args      []map[string]any
	agents    []string
	result    string // the tool's JSON text
	note      string // a text block sent before the result
	isError   bool
	status    int
	// expire404 answers an unknown session as the spec says: 404, no body.
	expire404 bool
	// meta is the result's _meta; a shape the sidecar cannot read when nil.
	meta any
}

func (f *fakeShop) metaOr() any {
	if f.meta != nil {
		return f.meta
	}
	return map[string]any{"response": strings.Repeat("x", 1000)}
}

// shopMeta is a _meta as the shop sends it, around the given nodes.
func shopMeta(nodes ...string) any {
	var meta any
	_ = json.Unmarshal([]byte(`{"toolName":"search_products","response":{"data":{"serverToolShopSearch":{"products":{"edges":[`+
		func() string {
			var edges []string
			for _, n := range nodes {
				edges = append(edges, `{"node":`+n+`}`)
			}
			return strings.Join(edges, ",")
		}()+`]}}}}}`), &meta)
	return meta
}

func (f *fakeShop) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.agents = append(f.agents, r.Header.Get("User-Agent"))
		if f.status != 0 {
			http.Error(w, "down", f.status)
			return
		}
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			} `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode: %v", err)
		}
		if req.Method == "initialize" {
			f.inits++
			id := fmt.Sprintf("s%d", f.inits)
			if f.sessions == nil {
				f.sessions = map[string]bool{}
			}
			f.sessions[id] = true
			w.Header().Set("Mcp-Session-Id", id)
			writeSSE(w, req.ID, map[string]any{"protocolVersion": upstreamProtocolVersion})
			return
		}
		if !f.sessions[r.Header.Get("Mcp-Session-Id")] {
			if f.expire404 {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","error":{"code":-32000,"message":"Bad Request: Session ID required"},"id":null}`))
			return
		}
		switch req.Method {
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/call":
			if req.Params.Name != upstreamTool {
				t.Errorf("tool %q", req.Params.Name)
			}
			f.calls++
			f.args = append(f.args, req.Params.Arguments)
			content := []map[string]any{{"type": "text", "text": f.result}}
			if f.note != "" {
				content = append([]map[string]any{{"type": "text", "text": f.note}}, content...)
			}
			writeSSE(w, req.ID, map[string]any{
				"content": content,
				"isError": f.isError,
				"_meta":   f.metaOr(),
			})
			if f.dropAfter > 0 && f.calls >= f.dropAfter {
				f.sessions = map[string]bool{}
			}
		default:
			t.Errorf("method %q", req.Method)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func writeSSE(w http.ResponseWriter, id json.RawMessage, result any) {
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = fmt.Fprintf(w, "event: message\ndata: %s\n\n", body)
}

const twoProducts = `{"correctedSearchTerm":"usb-c ladegerät","totalCount":913,"products":[
 {"id":1,"name":"GaN 3-Port Fast Charger","properties":"65 W, 3 Ports","description":"Der Digitec GaN-Charger.\n\nHinweis: mehr.",
  "productType":"USB Ladegerät","brand":"Digitec","priceIfAvailable":{"amountInclusive":33,"currency":"CHF"},
  "pageUrl":"https://www.galaxus.ch/de/s1/product/a-1?utm_source=shop-mcp&utm_medium=tool&utm_campaign=search_products",
  "previewImage":{"url":"https://www.galaxus.ch/productimages/a.jpg?utm_source=shop-mcp&utm_medium=tool","width":799,"height":1606},
  "ratingSummaryIfAvailable":{"averageRating":4.7,"ratingCount":1118},
  "productReviewsSummaryIfAvailable":{"pros":["kompakt","klein","viele Anschlüsse","vierter"],"cons":["schwer"]}},
 {"id":2,"name":"Digitec Kabel","properties":"","description":"","productType":"Kabel","brand":"Digitec","priceIfAvailable":null,
  "pageUrl":"https://www.galaxus.ch/de/s1/product/b-2?utm_source=shop-mcp","previewImage":null,"ratingSummaryIfAvailable":null},
 {"id":3,"name":"No page","pageUrl":""}]}`

// loomService is loom's own MCP client on a sidecar in front of shop.
func loomService(t *testing.T, cfg config) *mcp.Service {
	t.Helper()
	cfg.token = testToken
	srv := httptest.NewServer(newServer(cfg, http.DefaultClient, "test"))
	t.Cleanup(srv.Close)
	svc, err := mcp.NewServiceFromConfigs(context.Background(), mcp.Config{}, mcp.Config{Servers: map[string]mcp.ServerConfig{
		mcp.ShopServer: mcp.ShopServerConfig(srv.URL, testToken),
	}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

var shopTool = mcp.ExposedToolName(mcp.ShopServer, mcp.ShopSearchTool)

// loom, through its own MCP client, gets the products as text and as sources
// with picture, price and rating, in the same order: the contract end to end.
func TestLoomGetsTheProducts(t *testing.T) {
	fake := &fakeShop{result: twoProducts}
	svc := loomService(t, config{upstreamURL: fake.server(t).URL})
	got, err := svc.CallToolResult(context.Background(), shopTool, map[string]any{
		"query": " usb-c ladegerat ", "language": "DE", "max_results": 50, "min_price": 10, "max_price": 40,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"query": "usb-c ladegerat", "agentInfo": "loom", "language": "de", "first": float64(maxMaxResults),
		"minPrice": float64(10), "maxPrice": float64(40)}
	if len(fake.args) != 1 || len(fake.args[0]) != len(want) {
		t.Fatalf("shop got %v", fake.args)
	}
	for k, v := range want {
		if fake.args[0][k] != v {
			t.Fatalf("shop got %v, want %s=%v", fake.args[0], k, v)
		}
	}
	for _, ua := range fake.agents {
		if ua != "loom-shop/test (+https://github.com/trick77/loom)" {
			t.Fatalf("user agent %q", ua)
		}
	}
	if len(got.Sources) != 2 {
		t.Fatalf("sources %+v", got.Sources)
	}
	a, b := got.Sources[0], got.Sources[1]
	if a.URL != "https://www.galaxus.ch/de/s1/product/a-1" || a.Title != "Digitec GaN 3-Port Fast Charger (65 W, 3 Ports)" ||
		a.Image != "https://www.galaxus.ch/productimages/a.jpg" || a.Snippet != "CHF 33.00 · ★ 4.7 (1118)" {
		t.Fatalf("first source %+v", a)
	}
	if p := a.Product; p == nil || p.Price == nil || *p.Price != 33 || p.Currency != "CHF" || p.Rating != 4.7 || p.RatingCount != 1118 {
		t.Fatalf("first product %+v", a.Product)
	}
	// No price, no rating: no product details at all; the brand already
	// leading the name is not repeated.
	if b.URL != "https://www.galaxus.ch/de/s1/product/b-2" || b.Title != "Digitec Kabel" || b.Product != nil || b.Image != "" {
		t.Fatalf("second source %+v", b)
	}
	for _, want := range []string{
		`The shop searched for "usb-c ladegerät" instead of "usb-c ladegerat".`,
		"2 of 913 matching products:",
		"Product: Digitec GaN 3-Port Fast Charger (65 W, 3 Ports)\nType: USB Ladegerät\nPrice: CHF 33.00 incl. VAT\nRating: 4.7/5 from 1118 ratings\n" +
			"Reviewers like: kompakt; klein; viele Anschlüsse\nReviewers dislike: schwer\nURL: https://www.galaxus.ch/de/s1/product/a-1\n" +
			"Description: Der Digitec GaN-Charger. Hinweis: mehr.",
		"Product: Digitec Kabel\nType: Kabel\nPrice: not listed\nURL: https://www.galaxus.ch/de/s1/product/b-2\n",
	} {
		if !strings.Contains(got.Text, want) {
			t.Fatalf("text %q lacks %q", got.Text, want)
		}
	}
	if strings.Contains(got.Text, "utm_") || strings.Contains(got.Text, "vierter") || strings.Contains(got.Text, "No page") {
		t.Fatalf("text %q", got.Text)
	}
}

// The undocumented _meta adds a sale, labels and how soon an item ships, by
// product id; what it lacks or garbles costs only those.
func TestOffersFromMeta(t *testing.T) {
	fake := &fakeShop{result: twoProducts, meta: shopMeta(
		`{"trackingId":1,"labels":["DISCOUNT","VOLUME_DISCOUNT"],"availability":{"availabilityClassification":"HALF"},
		  "insteadOfPrice":{"type":"SALES_PRICE_BEFORE","rebate":15,"price":{"amountInclusive":39}}}`,
		`{"trackingId":2,"labels":["USED",{"odd":1}],"availability":{"availabilityClassification":"QUARTER"}}`,
		`{"trackingId":"broken"}`,
	)}
	svc := loomService(t, config{upstreamURL: fake.server(t).URL})
	got, err := svc.CallToolResult(context.Background(), shopTool, map[string]any{"query": "x"})
	if err != nil {
		t.Fatal(err)
	}
	a, b := got.Sources[0].Product, got.Sources[1].Product
	if a == nil || a.WasPrice == nil || *a.WasPrice != 39 || a.Availability != "on_order" || a.Condition != "" {
		t.Fatalf("first product %+v", a)
	}
	if got.Sources[0].Snippet != "CHF 33.00 (was 39.00) · ★ 4.7 (1118)" {
		t.Fatalf("snippet %q", got.Sources[0].Snippet)
	}
	// Two unconfirmed icons both read as uncertain, never as soon.
	if b == nil || b.Availability != "uncertain" || b.Condition != "used" || b.WasPrice != nil {
		t.Fatalf("second product %+v", b)
	}
	for _, want := range []string{
		"Price: CHF 33.00 incl. VAT, reduced from 39.00 (15% off)\nVolume discount: cheaper by the piece in quantity\n" +
			"Availability: not in stock, ordered from the supplier, delivered in about one to three weeks\nRating:",
		"Price: not listed\nAvailability: not in stock, no firm delivery date (weeks, or not available now)\nCondition: used\n",
	} {
		if !strings.Contains(got.Text, want) {
			t.Fatalf("text %q lacks %q", got.Text, want)
		}
	}
}

// An id sent as a string still joins its extras; one that is neither number
// nor numeric string costs only them, never the search.
func TestProductIDLenient(t *testing.T) {
	for raw, want := range map[string]int64{`24556944`: 24556944, `"24556944"`: 24556944, `"x"`: 0, `null`: 0, `{}`: 0, ``: 0} {
		if got := productID(json.RawMessage(raw)); got != want {
			t.Errorf("productID(%s) = %d, want %d", raw, got, want)
		}
	}
	fake := &fakeShop{result: strings.Replace(twoProducts, `"id":1,`, `"id":{"odd":1},`, 1), meta: shopMeta(
		`{"trackingId":2,"availability":{"availabilityClassification":"TICK"}}`)}
	svc := loomService(t, config{upstreamURL: fake.server(t).URL})
	got, err := svc.CallToolResult(context.Background(), shopTool, map[string]any{"query": "x"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Sources) != 2 || got.Sources[1].Product == nil || got.Sources[1].Product.Availability != "in_stock" {
		t.Fatalf("sources %+v", got.Sources)
	}
}

// An earlier price at or below today's is no sale, and an icon the shop
// newly invents says nothing.
func TestOfferNoSaleNoGuess(t *testing.T) {
	fake := &fakeShop{result: twoProducts, meta: shopMeta(
		`{"trackingId":1,"availability":{"availabilityClassification":"SPARKLE"},
		  "insteadOfPrice":{"type":"SALES_PRICE_BEFORE","rebate":0,"price":{"amountInclusive":33}}}`,
	)}
	svc := loomService(t, config{upstreamURL: fake.server(t).URL})
	got, err := svc.CallToolResult(context.Background(), shopTool, map[string]any{"query": "x"})
	if err != nil {
		t.Fatal(err)
	}
	if p := got.Sources[0].Product; p.WasPrice != nil || p.Availability != "" {
		t.Fatalf("product %+v", p)
	}
	if strings.Contains(got.Text, "reduced") || strings.Contains(got.Text, "Availability") {
		t.Fatalf("text %q", got.Text)
	}
}

// The shop forgets sessions (a restart): the next call opens a new one once,
// and the session lives across calls otherwise.
func TestSessionReopened(t *testing.T) {
	fake := &fakeShop{result: twoProducts, dropAfter: 2}
	svc := loomService(t, config{upstreamURL: fake.server(t).URL})
	for i := range 3 {
		if _, err := svc.CallToolResult(context.Background(), shopTool, map[string]any{"query": "x"}); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if fake.inits != 2 || fake.calls != 3 {
		t.Fatalf("inits %d, calls %d; want 2 and 3", fake.inits, fake.calls)
	}
	if fake.args[0]["language"] != "en" || fake.args[0]["first"] != float64(defaultMaxResults) {
		t.Fatalf("defaults %v", fake.args[0])
	}
	if _, ok := fake.args[0]["minPrice"]; ok {
		t.Fatalf("unset price filter sent: %v", fake.args[0])
	}
}

// The spec's expiry (404, empty body) reopens the session like the shop's
// own 400 does.
func TestSessionReopenedOn404(t *testing.T) {
	fake := &fakeShop{result: twoProducts, dropAfter: 1, expire404: true}
	svc := loomService(t, config{upstreamURL: fake.server(t).URL})
	for i := range 2 {
		if _, err := svc.CallToolResult(context.Background(), shopTool, map[string]any{"query": "x"}); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if fake.inits != 2 || fake.calls != 2 {
		t.Fatalf("inits %d, calls %d; want 2 and 2", fake.inits, fake.calls)
	}
}

// A note beside the products is skipped, and a total the shop left out
// never reads below the products in hand.
func TestNoteAndMissingTotal(t *testing.T) {
	result := strings.Replace(twoProducts, `"totalCount":913`, `"totalCount":0`, 1)
	fake := &fakeShop{result: result, note: "Prices incl. VAT."}
	svc := loomService(t, config{upstreamURL: fake.server(t).URL})
	got, err := svc.CallToolResult(context.Background(), shopTool, map[string]any{"query": "x"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Sources) != 2 || !strings.Contains(got.Text, "2 of 2 matching products:") {
		t.Fatalf("result %+v", got)
	}
}

// Arguments as models send them: 6.0 and "30" are 6 and 30, null is unset,
// a regional tag or a language's name is its language, anything else English.
func TestLenientArguments(t *testing.T) {
	for _, tc := range []struct {
		args map[string]any
		want map[string]any
	}{
		{map[string]any{"query": "x", "max_results": 6.0, "max_price": "30", "language": "de-CH"},
			map[string]any{"first": float64(6), "maxPrice": float64(30), "language": "de"}},
		{map[string]any{"query": "x", "max_results": nil, "min_price": nil, "max_price": nil, "language": "French"},
			map[string]any{"first": float64(defaultMaxResults), "language": "fr"}},
		{map[string]any{"query": "x", "language": "es"}, map[string]any{"language": "en"}},
		{map[string]any{"query": "x", "language": "pt_BR"}, map[string]any{"language": "en"}},
	} {
		fake := &fakeShop{result: twoProducts}
		svc := loomService(t, config{upstreamURL: fake.server(t).URL})
		if _, err := svc.CallToolResult(context.Background(), shopTool, tc.args); err != nil {
			t.Fatalf("%v: %v", tc.args, err)
		}
		for k, v := range tc.want {
			if fake.args[0][k] != v {
				t.Fatalf("%v: shop got %v, want %s=%v", tc.args, fake.args[0], k, v)
			}
		}
		if _, ok := fake.args[0]["minPrice"]; ok && tc.args["min_price"] == nil {
			t.Fatalf("null min_price sent: %v", fake.args[0])
		}
	}
}

// A correction that only repeats the query (case, spacing) is no correction.
func TestSameTermIsNoCorrection(t *testing.T) {
	fake := &fakeShop{result: strings.Replace(twoProducts, `"usb-c ladegerät"`, `"USB-C  Ladegerät"`, 1)}
	svc := loomService(t, config{upstreamURL: fake.server(t).URL})
	got, err := svc.CallToolResult(context.Background(), shopTool, map[string]any{"query": "usb-c ladegerät"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got.Text, "instead of") {
		t.Fatalf("text %q", got.Text)
	}
}

func TestNoProducts(t *testing.T) {
	fake := &fakeShop{result: `{"correctedSearchTerm":null,"totalCount":0,"products":[],"errorMessage":"nothing found"}`}
	svc := loomService(t, config{upstreamURL: fake.server(t).URL})
	got, err := svc.CallToolResult(context.Background(), shopTool, map[string]any{"query": "zzz"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Sources) != 0 || got.Text != "No products for: zzz (nothing found)" {
		t.Fatalf("result %+v", got)
	}
}

// A bad argument or a failing shop is an error result the model reads, never
// a crash.
func TestErrorsAreToolErrors(t *testing.T) {
	for name, tc := range map[string]struct {
		shop *fakeShop
		args map[string]any
		want string
	}{
		"shop refuses":  {&fakeShop{result: "min > max", isError: true}, map[string]any{"query": "x"}, "product search failed: min > max"},
		"silent refuse": {&fakeShop{result: "", isError: true}, map[string]any{"query": "x"}, "product search failed: the shop refused the search"},
		"shop down":     {&fakeShop{status: http.StatusBadGateway}, map[string]any{"query": "x"}, "status 502"},
		"garbled":       {&fakeShop{result: "<html>"}, map[string]any{"query": "x"}, "decode products"},
		"no query":      {&fakeShop{}, map[string]any{"query": "  "}, "query is required"},
		"negative":      {&fakeShop{}, map[string]any{"query": "x", "max_results": -1}, "max_results"},
		"fraction":      {&fakeShop{}, map[string]any{"query": "x", "max_results": 2.5}, "invalid arguments"},
		"word count":    {&fakeShop{}, map[string]any{"query": "x", "max_results": "ten"}, "invalid arguments"},
		"price range":   {&fakeShop{}, map[string]any{"query": "x", "min_price": 50, "max_price": 10}, "min_price must not be above"},
		"price bound":   {&fakeShop{}, map[string]any{"query": "x", "max_price": maxPrice + 1}, "max_price must be between"},
		"intent unsent": {&fakeShop{}, map[string]any{"query": "x", "userIntent": "for my wife"}, "invalid arguments"},
	} {
		svc := loomService(t, config{upstreamURL: tc.shop.server(t).URL})
		got, err := svc.CallToolResult(context.Background(), shopTool, tc.args)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %+v, %v; want error with %q", name, got, err, tc.want)
		}
	}
}

func TestOversizedAnswer(t *testing.T) {
	fake := &fakeShop{result: `{"products":[{"description":"` + strings.Repeat("x", maxUpstreamBytes) + `"}]}`}
	svc := loomService(t, config{upstreamURL: fake.server(t).URL})
	if _, err := svc.CallToolResult(context.Background(), shopTool, map[string]any{"query": "x"}); err == nil || !strings.Contains(err.Error(), "answer over") {
		t.Fatalf("err %v", err)
	}
}

func TestFindReplyPlainJSON(t *testing.T) {
	reply, err := findReply(strings.NewReader(`{"jsonrpc":"2.0","id":7,"result":{"ok":true}}`), "application/json", 7)
	if err != nil || string(reply.Result) != `{"ok":true}` {
		t.Fatalf("reply %+v, %v", reply, err)
	}
	// An id sent back as a string is the same request.
	if reply, err := findReply(strings.NewReader(`{"id":"7","result":1}`), "application/json", 7); err != nil || string(reply.Result) != "1" {
		t.Fatalf("string id: %+v, %v", reply, err)
	}
	if _, err := findReply(strings.NewReader(`{"id":8,"result":1}`), "application/json", 7); err == nil {
		t.Fatal("an answer to another request was accepted")
	}
	// A request-level error comes with id null: its message is the answer.
	reply, err = findReply(strings.NewReader(`{"jsonrpc":"2.0","error":{"code":-32602,"message":"first must be <= 50"},"id":null}`), "application/json", 7)
	if err != nil || reply.Error == nil || reply.Error.Message != "first must be <= 50" {
		t.Fatalf("null-id error: %+v, %v", reply, err)
	}
	if _, err := findReply(strings.NewReader("event: message\ndata: {\"id\":1}\n\n"), "text/event-stream", 2); err == nil {
		t.Fatal("a stream without the answer was accepted")
	}
}

// One event's data may span several lines (joined by newlines), and the last
// event may end the stream without its blank line.
func TestFindReplyMultilineEvent(t *testing.T) {
	stream := "event: message\ndata: {\"jsonrpc\":\"2.0\",\ndata: \"id\":3,\"result\":{\"ok\":true}}\n\n"
	reply, err := findReply(strings.NewReader(stream), "text/event-stream; charset=utf-8", 3)
	if err != nil || string(reply.Result) != `{"ok":true}` {
		t.Fatalf("reply %+v, %v", reply, err)
	}
	reply, err = findReply(strings.NewReader("data: {\"id\":4,\"result\":1}"), "text/event-stream", 4)
	if err != nil || string(reply.Result) != "1" {
		t.Fatalf("unterminated: %+v, %v", reply, err)
	}
}

// A stream the server keeps open after the answer is not waited out.
func TestFindReplyOpenStream(t *testing.T) {
	r, w := io.Pipe()
	t.Cleanup(func() { _ = w.Close() })
	go func() {
		_, _ = w.Write([]byte("event: message\ndata: {\"id\":5,\"result\":2}\n\n: keep-alive\n"))
	}()
	done := make(chan error, 1)
	go func() {
		reply, err := findReply(r, "text/event-stream", 5)
		if err == nil && string(reply.Result) != "2" {
			err = fmt.Errorf("result %s", reply.Result)
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("waited for the stream to close")
	}
}

// Only an error naming the session id reopens it; a bad argument that
// mentions a session reaches the model as it is.
func TestSessionRefused(t *testing.T) {
	for msg, want := range map[string]bool{
		"Bad Request: Session ID required":           true,
		"session not found":                          true,
		"invalid params: session-scoped filter used": false,
		"min > max": false,
	} {
		if got := sessionRefused(msg); got != want {
			t.Errorf("sessionRefused(%q) = %v", msg, got)
		}
	}
}

// A caller waiting behind another's handshake gives up at its own deadline.
func TestWaitForHandshakeHonoursDeadline(t *testing.T) {
	u := newUpstream(http.DefaultClient, "https://shop.example/", "test")
	if err := u.lock(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := u.ensureSession(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err %v", err)
	}
	u.unlock()
}

func TestWithoutTracking(t *testing.T) {
	for in, want := range map[string]string{
		"https://s.example/p?utm_source=a&UTM_Medium=b&color=red": "https://s.example/p?color=red",
		// Order and escaping of the rest stay as sent (a signed URL).
		"https://s.example/i.jpg?z=1&utm_x=2&a=b+c%2F&sig=Q": "https://s.example/i.jpg?z=1&a=b+c%2F&sig=Q",
		"https://s.example/p":                                "https://s.example/p",
		"http://s.example/p":                                 "",
		"javascript:alert(1)":                                "",
		" https://s.example/img.jpg?utm_campaign=x":          "https://s.example/img.jpg",
	} {
		if got := withoutTracking(in); got != want {
			t.Errorf("withoutTracking(%q) = %q, want %q", in, got, want)
		}
	}
}

// Off, the sidecar answers but serves no tool: product search is off, loom
// boots as usual.
func TestOffNoTool(t *testing.T) {
	svc := loomService(t, config{})
	if svc.HasTool(shopTool) || len(svc.Tools()) != 0 {
		t.Fatalf("tools %+v", svc.Tools())
	}
}

// loom addresses the contract by its own copy of the names: they must match
// the server's.
func TestContractNamesMatchLoom(t *testing.T) {
	if mcp.ShopServer != serverName || mcp.ShopSearchTool != toolName {
		t.Fatal("loom's shop contract names drifted from loom-shop's")
	}
}
