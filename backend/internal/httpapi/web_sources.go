package httpapi

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/net/publicsuffix"
)

// webSource is one web source gathered during an assistant turn: a stable Index
// the model cites inline as [n], the source URL, and a short display Label (the
// registrable domain's main label — the part immediately left of the public
// suffix, e.g. "truefoundry.com" -> "Truefoundry", "foo.co.uk" -> "Foo").
type webSource struct {
	Index int    `json:"index"`
	URL   string `json:"url"`
	Label string `json:"label"`
}

// webSourceRegistry accumulates the web sources gathered across one turn's tool
// rounds, assigning each unique URL a stable, monotonic [n] index. A URL seen
// again keeps its first-assigned index so the same page is never numbered twice.
// It also remembers the most recent obscura navigation so a following
// browser_snapshot (which carries no URL of its own) can inherit that source's
// marker.
type webSourceRegistry struct {
	sources      []webSource
	byKey        map[string]int
	obscuraNavHd string // header line to prepend to the next obscura snapshot
}

func newWebSourceRegistry() *webSourceRegistry {
	return &webSourceRegistry{byKey: map[string]int{}}
}

// add registers rawURL if new and returns its index. ok is false when the URL
// cannot be normalized (non-http(s) or unparseable), so callers skip labeling.
func (r *webSourceRegistry) add(rawURL string) (index int, ok bool) {
	key, host, ok := normalizeURL(rawURL)
	if !ok {
		return 0, false
	}
	if idx, seen := r.byKey[key]; seen {
		return idx, true
	}
	idx := len(r.sources) + 1
	r.byKey[key] = idx
	r.sources = append(r.sources, webSource{Index: idx, URL: strings.TrimSpace(rawURL), Label: deriveLabel(host)})
	return idx, true
}

func (r *webSourceRegistry) all() []webSource { return r.sources }

func (r *webSourceRegistry) len() int { return len(r.sources) }

// normalizeURL builds a dedupe key (host + path + query, minus fragment) and
// returns the lowercased host. Only http(s) URLs are accepted.
func normalizeURL(raw string) (key, host string, ok bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", "", false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return "", "", false
	}
	host = strings.ToLower(u.Hostname())
	key = host + strings.TrimRight(u.Path, "/")
	if u.RawQuery != "" {
		key += "?" + u.RawQuery
	}
	return key, host, true
}

// deriveLabel turns a host into the pill's display label: the registrable
// domain's main label (left of the public suffix), first letter capitalized.
// The internal casing of a brand (e.g. "TrueFoundry") cannot be recovered from
// a lowercase domain, so "truefoundry.com" yields "Truefoundry".
func deriveLabel(host string) string {
	host = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(host)), "www.")
	if host == "" {
		return ""
	}
	main := host
	// EffectiveTLDPlusOne collapses "www.truefoundry.co.uk" to "truefoundry.co.uk";
	// its first label is the site name. On error (host is itself a public suffix,
	// or an IP) fall back to the raw host's first label.
	if etld1, err := publicsuffix.EffectiveTLDPlusOne(host); err == nil && etld1 != "" {
		main = etld1
	}
	label := main
	if i := strings.IndexByte(main, '.'); i > 0 {
		label = main[:i]
	}
	return capitalizeFirst(label)
}

func capitalizeFirst(s string) string {
	if s == "" {
		return ""
	}
	runes := []rune(s)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

// urlPattern extracts absolute http(s) URLs, stopping at whitespace and common
// trailing delimiters so a URL at the end of a sentence isn't captured with its
// punctuation.
var urlPattern = regexp.MustCompile(`https?://[^\s<>"'` + "`" + `)\]}]+`)

// relabelWebToolOutput annotates a web tool's result with [n] citation markers
// and registers each source URL, so the model can cite them inline. It is a
// no-op for non-web tools. The raw (un-capped) output is passed in; the caller
// caps the returned string.
func (s *server) relabelWebToolOutput(toolName string, arguments map[string]any, output string, reg *webSourceRegistry) string {
	if reg == nil {
		return output
	}
	switch toolName {
	case tavilySearchExposedName:
		return relabelTavilyResult(output, reg)
	case fetchToolName:
		return prependURLSource(argURL(arguments), output, reg)
	case obscuraNavigateToolName:
		out := prependURLSource(argURL(arguments), output, reg)
		if idx, ok := reg.add(argURL(arguments)); ok {
			// Remember this source so the following browser_snapshot (the call that
			// actually returns the page text) inherits the same marker.
			reg.obscuraNavHd = fmt.Sprintf("Web source [%d]: %s", idx, strings.TrimSpace(argURL(arguments)))
		}
		return out
	case obscuraSnapshotToolName:
		if reg.obscuraNavHd == "" {
			return output
		}
		return reg.obscuraNavHd + "\n\n" + output
	default:
		return output
	}
}

func argURL(arguments map[string]any) string {
	if arguments == nil {
		return ""
	}
	u, _ := arguments["url"].(string)
	return u
}

// prependURLSource registers url and prepends a "Web source [n]: url" header to
// output so the marker sits next to the content the model reads. If url can't be
// registered, output is returned unchanged.
func prependURLSource(rawURL, output string, reg *webSourceRegistry) string {
	idx, ok := reg.add(rawURL)
	if !ok {
		return output
	}
	return fmt.Sprintf("Web source [%d]: %s\n\n%s", idx, strings.TrimSpace(rawURL), output)
}

// tavilyJSON mirrors the subset of Tavily's response we need if a deployment
// returns raw JSON instead of the MCP server's formatted text.
type tavilyJSON struct {
	Results []struct {
		Title string `json:"title"`
		URL   string `json:"url"`
	} `json:"results"`
}

// relabelTavilyResult rewrites a tavily_search result so each result carries a
// [n] marker and registers every result URL. The Tavily MCP server returns
// human-readable text ("Title:/URL:/Content:" blocks); this also handles a raw
// JSON payload and, failing both, a plain URL sweep — so no source is ever lost
// to an unexpected format.
func relabelTavilyResult(raw string, reg *webSourceRegistry) string {
	if strings.TrimSpace(raw) == "" {
		return raw
	}
	// 1) Formatted text: prefix each "Title:" line with its [n] once we find the
	//    block's "URL:" line. This is the shape the Tavily MCP server emits.
	if out, n := relabelTavilyText(raw, reg); n > 0 {
		return out
	}
	// 2) Raw JSON fallback.
	var parsed tavilyJSON
	if err := json.Unmarshal([]byte(raw), &parsed); err == nil && len(parsed.Results) > 0 {
		var b strings.Builder
		for _, res := range parsed.Results {
			idx, ok := reg.add(res.URL)
			if !ok {
				continue
			}
			title := strings.TrimSpace(res.Title)
			if title == "" {
				title = strings.TrimSpace(res.URL)
			}
			fmt.Fprintf(&b, "[%d] %s — %s\n", idx, title, strings.TrimSpace(res.URL))
		}
		if b.Len() > 0 {
			return "Web sources (cite with [n]):\n" + b.String() + "\n" + raw
		}
	}
	// 3) Last resort: sweep any URLs out of the text and append a numbered index.
	var b strings.Builder
	for _, u := range urlPattern.FindAllString(raw, -1) {
		if idx, ok := reg.add(u); ok {
			fmt.Fprintf(&b, "[%d] %s\n", idx, strings.TrimSpace(u))
		}
	}
	if b.Len() == 0 {
		return raw
	}
	return raw + "\n\nWeb sources (cite with [n]):\n" + b.String()
}

// relabelTavilyText handles the Tavily MCP server's formatted-text output. It
// walks the lines, and for each result block (started by a "Title:" line)
// registers the block's "URL:" and rewrites the Title line to lead with [n].
// Returns the rewritten text and the number of sources registered.
func relabelTavilyText(raw string, reg *webSourceRegistry) (string, int) {
	lines := strings.Split(raw, "\n")
	// First pass: map each Title-line index to the URL found later in its block.
	type block struct {
		titleLine int
		url       string
	}
	var blocks []block
	current := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "Title:"):
			blocks = append(blocks, block{titleLine: i})
			current = len(blocks) - 1
		case strings.HasPrefix(trimmed, "URL:") && current >= 0 && blocks[current].url == "":
			blocks[current].url = strings.TrimSpace(strings.TrimPrefix(trimmed, "URL:"))
		}
	}
	registered := 0
	for _, blk := range blocks {
		if blk.url == "" {
			continue
		}
		idx, ok := reg.add(blk.url)
		if !ok {
			continue
		}
		registered++
		lines[blk.titleLine] = fmt.Sprintf("[%d] %s", idx, strings.TrimLeft(lines[blk.titleLine], " \t"))
	}
	if registered == 0 {
		return raw, 0
	}
	return strings.Join(lines, "\n"), registered
}

// webSourceCitations converts gathered web sources into citation records for
// streaming and persistence. Web citations carry URL + Index and reuse Filename
// as the display label; they have no DocumentID (RAG-only) so the frontend
// distinguishes them by the presence of url.
func webSourceCitations(sources []webSource) []citation {
	if len(sources) == 0 {
		return nil
	}
	out := make([]citation, 0, len(sources))
	for _, ws := range sources {
		out = append(out, citation{
			Filename: ws.Label,
			URL:      ws.URL,
			Index:    ws.Index,
		})
	}
	return out
}
