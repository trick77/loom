package httpapi

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

const (
	// faviconPageMaxBytes caps how much of a page's HTML we read to find its icon
	// links — the <head> is near the top, so a modest cap avoids pulling whole pages.
	faviconPageMaxBytes = 512 << 10
	// faviconGoogleSize is the size we ask Google's favicon service for (last resort).
	faviconGoogleSize = 64
	// faviconBrowserUA is a browser-like User-Agent for the page fetch: some sites
	// serve a stripped page (or none) to unknown agents, hiding their icon links.
	faviconBrowserUA = "Mozilla/5.0 (compatible; loom-favicon-cache/1.0)"
)

// googleFaviconURL returns Google's favicon service URL for host — the last-resort
// candidate: consistently reachable and rendered on a neutral badge (so it stays
// visible on the dark UI), though low-resolution.
func googleFaviconURL(host string) string {
	return "https://www.google.com/s2/favicons?domain=" + url.QueryEscape(host) + "&sz=" + strconv.Itoa(faviconGoogleSize)
}

// resolveIconCandidates returns an ordered, best-first, de-duplicated list of icon
// URLs to try for a site. It prefers apple-touch-icons and large raster icons —
// which are opaque, high-resolution and brand-faithful, so they stay visible on the
// dark-only UI — over a site's default favicon, which is often a small dark glyph
// designed for a light browser chrome (e.g. GitHub's black octocat, invisible on
// dark). SVG is never a candidate: the proxy refuses it as a stored-XSS vector.
//
// The page HTML is parsed best-effort to discover declared icons; on any failure
// (site blocks the fetch, no <head>, etc.) we still fall back to well-known icon
// paths and finally Google's favicon service, so a source is never left iconless
// when an icon actually exists.
func (s *server) resolveIconCandidates(ctx context.Context, scheme, host, pageURL string) []string {
	root := scheme + "://" + host
	var apple, icons []string
	if doc, base, err := s.fetchPageHTML(ctx, pageURL); err == nil {
		apple, icons = parseIconLinks(doc, base)
	}
	out := make([]string, 0, 8)
	// 1. apple-touch-icons declared in the page (largest first) — the most reliably
	//    visible, high-res, brand-faithful option.
	out = append(out, apple...)
	// 2. well-known apple-touch paths: many sites serve these unlinked (e.g. GitHub
	//    only references its adaptive .svg in <head> but still hosts this file).
	out = append(out, root+"/apple-touch-icon.png", root+"/apple-touch-icon-precomposed.png")
	// 3. large raster <link rel="icon"> declared in the page (largest first).
	out = append(out, icons...)
	// 4. the classic favicon.
	out = append(out, root+"/favicon.ico")
	// 5. last resort: a rendered, always-visible service icon.
	out = append(out, s.faviconServiceURL(host))
	return dedupeStrings(out)
}

// faviconServiceURL returns the last-resort favicon URL for host. Overridable in
// tests so they never reach out to the real Google service.
func (s *server) faviconServiceURL(host string) string {
	if s.faviconService != nil {
		return s.faviconService(host)
	}
	return googleFaviconURL(host)
}

// fetchPageHTML GETs pageURL and parses it as HTML, returning the parsed tree and
// the (post-redirect) base URL for resolving relative icon hrefs. It reads at most
// faviconPageMaxBytes and only parses when the response looks like HTML.
func (s *server) fetchPageHTML(ctx context.Context, pageURL string) (*html.Node, *url.URL, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Accept", "text/html")
	req.Header.Set("User-Agent", faviconBrowserUA)

	client := s.faviconClient
	if client == nil {
		client = faviconDefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("page status %d", resp.StatusCode)
	}
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	if ct != "" && !strings.Contains(ct, "html") {
		return nil, nil, fmt.Errorf("not html: %q", ct)
	}
	doc, err := html.Parse(io.LimitReader(resp.Body, faviconPageMaxBytes))
	if err != nil {
		return nil, nil, err
	}
	base := resp.Request.URL // final URL after redirects
	return doc, base, nil
}

// parseIconLinks walks an HTML tree and returns declared icon URLs, split into
// apple-touch-icons and other raster icons, each ordered largest-declared-size
// first. Relative hrefs are resolved against base; SVG and non-http(s) icons are
// dropped, as are decorative link types (mask-icon, fluid-icon).
func parseIconLinks(doc *html.Node, base *url.URL) (apple, icons []string) {
	type cand struct {
		href string
		size int
	}
	var appleC, iconC []cand
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && strings.EqualFold(n.Data, "link") {
			var rel, href, typ, sizes string
			for _, a := range n.Attr {
				switch strings.ToLower(a.Key) {
				case "rel":
					rel = strings.ToLower(a.Val)
				case "href":
					href = strings.TrimSpace(a.Val)
				case "type":
					typ = strings.ToLower(strings.TrimSpace(a.Val))
				case "sizes":
					sizes = strings.ToLower(a.Val)
				}
			}
			if href != "" && strings.Contains(rel, "icon") &&
				!strings.Contains(rel, "mask-icon") && !strings.Contains(rel, "fluid-icon") {
				if abs := resolveHref(base, href); abs != "" && !isSVGIcon(abs, typ) {
					c := cand{abs, parseIconSize(sizes)}
					if strings.Contains(rel, "apple-touch-icon") {
						appleC = append(appleC, c)
					} else {
						iconC = append(iconC, c)
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	byd := func(cs []cand) []string {
		sort.SliceStable(cs, func(i, j int) bool { return cs[i].size > cs[j].size })
		out := make([]string, 0, len(cs))
		for _, c := range cs {
			out = append(out, c.href)
		}
		return out
	}
	return byd(appleC), byd(iconC)
}

// resolveHref resolves a possibly-relative icon href against base and returns it
// only if it is an absolute http(s) URL (dropping data:, javascript:, etc.).
func resolveHref(base *url.URL, href string) string {
	u, err := url.Parse(href)
	if err != nil {
		return ""
	}
	if base != nil {
		u = base.ResolveReference(u)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return ""
	}
	return u.String()
}

// isSVGIcon reports whether a candidate is an SVG, by declared type or path suffix.
func isSVGIcon(rawURL, typ string) bool {
	if typ == "image/svg+xml" {
		return true
	}
	if u, err := url.Parse(rawURL); err == nil {
		return strings.HasSuffix(strings.ToLower(u.Path), ".svg")
	}
	return strings.Contains(strings.ToLower(rawURL), ".svg")
}

// parseIconSize turns a sizes attribute ("32x32", "16x16 32x32", "180x180") into
// the largest pixel dimension it names, or 0 when absent/unparseable ("any").
func parseIconSize(sizes string) int {
	best := 0
	for _, tok := range strings.Fields(sizes) {
		if i := strings.IndexByte(tok, 'x'); i > 0 {
			if n, err := strconv.Atoi(tok[:i]); err == nil && n > best {
				best = n
			}
		}
	}
	return best
}

// dedupeStrings returns xs with duplicates removed, preserving first-seen order.
func dedupeStrings(xs []string) []string {
	seen := make(map[string]struct{}, len(xs))
	out := make([]string, 0, len(xs))
	for _, x := range xs {
		if x == "" {
			continue
		}
		if _, ok := seen[x]; ok {
			continue
		}
		seen[x] = struct{}{}
		out = append(out, x)
	}
	return out
}
