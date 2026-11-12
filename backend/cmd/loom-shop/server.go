package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/trick77/loom/internal/sidecar"
	"github.com/trick77/toolkit/mcpserver"
)

// The shop contract (docs/shop.md). loom keeps its own copy of the names
// (mcp.ShopServer, mcp.ShopSearchTool); the tests hold the two together.
const (
	serverName = "shop"
	toolName   = "search_products"
	// defaultMaxResults and maxMaxResults bound max_results; the cap is
	// loom's own limit of sources per call.
	defaultMaxResults = 8
	maxMaxResults     = 20
	// maxPrice is the shop's own bound on a price filter.
	maxPrice = 1_000_000
	// maxDescriptionRunes and maxReviewPoints keep a product's text short:
	// twenty products stay a few thousand tokens.
	maxDescriptionRunes = 400
	maxReviewPoints     = 3
)

// languages are the shop's; any other conversation language asks in English.
var languages = []string{"en", "de", "fr", "it", "nl"}

// newServer mounts kit's health probes (loopback only, the container's own
// healthcheck) and, behind the bearer token, the shop tool at /mcp.
func newServer(cfg config, client *http.Client, version string) http.Handler {
	api := http.NewServeMux()
	api.Handle("/mcp", mcpserver.New(mcpServer(cfg, client, version)))
	return sidecar.Handler(cfg.token, api)
}

// mcpServer is the shop server: the search tool when an upstream is set, else
// no tool at all (loom then has no product search).
func mcpServer(cfg config, client *http.Client, version string) mcpserver.Server {
	srv := mcpserver.Server{Name: serverName, Version: version}
	if cfg.upstreamURL == "" {
		return srv
	}
	shop := newUpstream(client, cfg.upstreamURL, version)
	srv.Tools = []mcpserver.Tool{{
		Name:  toolName,
		Title: "Product search",
		Description: "Search an online shop's catalog (Switzerland) for products to buy. Returns the matching products, each " +
			"with name, brand, type, price incl. VAT in the shop's currency, average rating, review pros and cons, a short " +
			"description and its product page. Not in the results: stock, delivery times, full reviews. Use it to find, " +
			"compare or price products; search again with other words when the results miss.",
		Schema: mcpserver.ObjectSchema(map[string]any{
			"query": map[string]any{"type": "string",
				"description": "What to look for: a product name, brand, category or keywords, as a shop search."},
			"language": map[string]any{"type": "string", "enum": languages,
				"description": "The language of the user's conversation; en for any other. Product data comes back in it."},
			"max_results": map[string]any{"type": "integer", "minimum": 1, "maximum": maxMaxResults,
				"description": fmt.Sprintf("How many products, default %d.", defaultMaxResults)},
			"min_price": map[string]any{"type": "integer", "minimum": 1, "maximum": maxPrice,
				"description": "Lowest price, whole units of the shop's currency."},
			"max_price": map[string]any{"type": "integer", "minimum": 1, "maximum": maxPrice,
				"description": "Highest price, whole units of the shop's currency."},
		}, "query"),
		ReadOnly:  true,
		OpenWorld: true,
		Run: func(ctx context.Context, raw json.RawMessage) mcpserver.Result {
			return runSearch(ctx, shop, raw)
		},
	}}
	return srv
}

type searchArgs struct {
	Query      string   `json:"query"`
	Language   string   `json:"language"`
	MaxResults wholeInt `json:"max_results"`
	MinPrice   wholeInt `json:"min_price"`
	MaxPrice   wholeInt `json:"max_price"`
}

// wholeInt is an integer argument as models send it: 10, 10.0 or "10". A
// fraction or a word is still an error the model reads.
type wholeInt int

func (w *wholeInt) UnmarshalJSON(raw []byte) error {
	text := strings.Trim(strings.TrimSpace(string(raw)), `"`)
	f, err := strconv.ParseFloat(text, 64)
	if err != nil || f != math.Trunc(f) || math.Abs(f) > math.MaxInt32 {
		return fmt.Errorf("%s is not a whole number", raw)
	}
	*w = wholeInt(f)
	return nil
}

// runSearch is one call: arguments checked against the contract, the shop
// asked, its products as unnumbered text plus sources in the same order (loom
// numbers them).
func runSearch(ctx context.Context, shop *upstream, raw json.RawMessage) mcpserver.Result {
	req, err := searchRequest(raw)
	if err != nil {
		return mcpserver.ErrorResult(err.Error())
	}
	res, err := shop.search(ctx, req)
	if err != nil {
		return mcpserver.ErrorResult("product search failed: " + err.Error())
	}
	return render(req.Query, res)
}

// searchRequest checks the arguments against the contract. A max_results
// above the cap is lowered to it; anything else out of the contract is an
// error the model can read.
func searchRequest(raw json.RawMessage) (shopSearch, error) {
	var args searchArgs
	if err := mcpserver.DecodeArgs(raw, &args); err != nil {
		return shopSearch{}, fmt.Errorf("invalid arguments: %w", err)
	}
	req := shopSearch{
		Query:     strings.TrimSpace(args.Query),
		AgentInfo: agentInfo,
		Language:  strings.ToLower(strings.TrimSpace(args.Language)),
		First:     int(args.MaxResults),
		MinPrice:  int(args.MinPrice),
		MaxPrice:  int(args.MaxPrice),
	}
	switch {
	case req.Query == "":
		return shopSearch{}, errors.New("query is required")
	case req.First < 0:
		return shopSearch{}, fmt.Errorf("max_results must be between 1 and %d", maxMaxResults)
	case req.First == 0:
		req.First = defaultMaxResults
	case req.First > maxMaxResults:
		req.First = maxMaxResults
	}
	if req.Language == "" {
		req.Language = "en"
	} else if !slices.Contains(languages, req.Language) {
		return shopSearch{}, fmt.Errorf("language must be one of %s", strings.Join(languages, ", "))
	}
	for name, v := range map[string]int{"min_price": req.MinPrice, "max_price": req.MaxPrice} {
		if v < 0 || v > maxPrice {
			return shopSearch{}, fmt.Errorf("%s must be between 1 and %d", name, maxPrice)
		}
	}
	if req.MinPrice > 0 && req.MaxPrice > 0 && req.MinPrice > req.MaxPrice {
		return shopSearch{}, errors.New("min_price must not be above max_price")
	}
	return req, nil
}

// render is the shop's answer as loom reads it: a line on the whole search,
// then each product with a page; sources carry the same products, in order,
// with picture, price and rating for the cards.
func render(query string, res shopResult) mcpserver.Result {
	var text strings.Builder
	sources := []mcpserver.Source{}
	for _, p := range res.Products {
		page := withoutTracking(p.PageURL)
		if page == "" {
			continue
		}
		title := productTitle(p)
		text.WriteString("\n")
		fmt.Fprintf(&text, "Product: %s\n", title)
		if t := strings.TrimSpace(p.ProductType); t != "" {
			fmt.Fprintf(&text, "Type: %s\n", t)
		}
		src := mcpserver.Source{URL: page, Title: title}
		product := &mcpserver.Product{}
		var snippet []string
		if pr := p.PriceIfAvailable; pr != nil && pr.AmountInclusive > 0 && strings.TrimSpace(pr.Currency) != "" {
			price := pr.AmountInclusive
			product.Price, product.Currency = &price, strings.TrimSpace(pr.Currency)
			fmt.Fprintf(&text, "Price: %s %.2f incl. VAT\n", product.Currency, price)
			snippet = append(snippet, fmt.Sprintf("%s %.2f", product.Currency, price))
		} else {
			text.WriteString("Price: not listed\n")
		}
		if r := p.RatingSummaryIfAvailable; r != nil && r.RatingCount > 0 {
			product.Rating, product.RatingCount = r.AverageRating, r.RatingCount
			fmt.Fprintf(&text, "Rating: %.1f/5 from %d ratings\n", r.AverageRating, r.RatingCount)
			snippet = append(snippet, fmt.Sprintf("★ %.1f (%d)", r.AverageRating, r.RatingCount))
		}
		if rv := p.ProductReviewsSummaryIfAvailable; rv != nil {
			if pros := reviewPoints(rv.Pros); pros != "" {
				fmt.Fprintf(&text, "Reviewers like: %s\n", pros)
			}
			if cons := reviewPoints(rv.Cons); cons != "" {
				fmt.Fprintf(&text, "Reviewers dislike: %s\n", cons)
			}
		}
		fmt.Fprintf(&text, "URL: %s\n", page)
		if d := mcpserver.OneLine(p.Description, maxDescriptionRunes); d != "" {
			fmt.Fprintf(&text, "Description: %s\n", d)
		}
		if img := p.PreviewImage; img != nil {
			src.Image = withoutTracking(img.URL)
		}
		src.Snippet = strings.Join(snippet, " · ")
		if *product != (mcpserver.Product{}) {
			src.Product = product
		}
		sources = append(sources, src)
	}
	var head strings.Builder
	if c := res.CorrectedSearchTerm; c != nil && strings.TrimSpace(*c) != "" {
		fmt.Fprintf(&head, "The shop searched for %q instead of %q.\n", strings.TrimSpace(*c), query)
	}
	if len(sources) == 0 {
		head.WriteString("No products for: " + query)
		if msg := strings.TrimSpace(res.ErrorMessage); msg != "" {
			head.WriteString(" (" + msg + ")")
		}
		// An empty, non-nil list: loom reads it as "no sources", not as a
		// server that sends none.
		return mcpserver.Result{Text: head.String(), Sources: sources}
	}
	// A total the shop left out (or below what it sent) would read as
	// "5 of 0": the products in hand are the floor.
	fmt.Fprintf(&head, "%d of %d matching products:\n", len(sources), max(res.TotalCount, len(sources)))
	return mcpserver.Result{Text: head.String() + text.String(), Sources: sources}
}

// productTitle is brand, name and the properties that tell variants apart,
// the way the shop lists them ("Ugreen Nexode (65 W, 3 Ports)").
func productTitle(p shopProduct) string {
	name := strings.TrimSpace(p.Name)
	if brand := strings.TrimSpace(p.Brand); brand != "" && !strings.HasPrefix(strings.ToLower(name), strings.ToLower(brand)) {
		name = brand + " " + name
	}
	if props := strings.TrimSpace(p.Properties); props != "" {
		name += " (" + props + ")"
	}
	return mcpserver.OneLine(name, 200)
}

// reviewPoints joins the first few points of a review summary.
func reviewPoints(points []string) string {
	var kept []string
	for _, p := range points {
		if p = mcpserver.OneLine(p, 120); p != "" {
			kept = append(kept, p)
		}
		if len(kept) == maxReviewPoints {
			break
		}
	}
	return strings.Join(kept, "; ")
}

// withoutTracking is an https URL without the shop's utm_ parameters (they
// mark the visit as coming from an AI tool); anything else is "". Only those
// pairs go: the rest of the query keeps its order and escaping, which a
// signed image URL depends on.
func withoutTracking(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return ""
	}
	var kept []string
	for _, pair := range strings.Split(u.RawQuery, "&") {
		if pair != "" && !strings.HasPrefix(strings.ToLower(pair), "utm_") {
			kept = append(kept, pair)
		}
	}
	u.RawQuery = strings.Join(kept, "&")
	return u.String()
}
