package turn

import (
	"fmt"
	"net"
	"net/url"
	"strings"
	"unicode"

	"golang.org/x/net/publicsuffix"

	"github.com/trick77/loom/internal/mcp"
)

// webSource is one web source gathered during an assistant turn: a stable Index
// the model cites inline as [n], the source URL, and a short display Label (the
// registrable domain's main label — the part immediately left of the public
// suffix, e.g. "truefoundry.com" -> "Truefoundry", "foo.co.uk" -> "Foo"). Title,
// Snippet and Favicon feed the sources sidebar (what the source delivered); they
// may be empty when the tool didn't provide them.
type webSource struct {
	Index   int    `json:"index"`
	URL     string `json:"url"`
	Label   string `json:"label"`
	Title   string `json:"title,omitempty"`
	Snippet string `json:"snippet,omitempty"`
	Favicon string `json:"favicon,omitempty"`
	// Image is a preview picture a structured source supplied (an https URL).
	Image string `json:"image,omitempty"`
	// Site is the host-derived site name, the same as Label unless a
	// structured source named itself (Label "peeq · Bike Physics", Site
	// "Peeq"): the favicon row groups by site, the sidebar shows the label.
	Site string `json:"site,omitempty"`
}

// maxSourceSnippetChars caps the per-source snippet stored for the sidebar.
const maxSourceSnippetChars = 240

// webSourceRegistry accumulates the web sources gathered across one turn's tool
// rounds, assigning each unique URL a stable, monotonic [n] index. A URL seen
// again keeps its first-assigned index so the same page is never numbered twice.
// It also remembers the most recent obscura navigation so a following
// browser_snapshot (which carries no URL of its own) can inherit that source's
// marker.
type webSourceRegistry struct {
	sources []webSource
	byKey   map[string]int
	// offset is how many markers were already handed out before the tool loop ran —
	// the user's uploaded documents, numbered while the knowledge context was built.
	// Web sources continue after them so both kinds share one numbering space and a
	// marker in the answer is never ambiguous.
	offset int
	// browserPage is the browser policy's (web_tools.go): the page navigated
	// last, for the next snapshot's citation.
	browserPage browserPage
}

// newWebSourceRegistryAfter starts numbering at offset+1.
func newWebSourceRegistryAfter(offset int) *webSourceRegistry {
	if offset < 0 {
		offset = 0
	}
	return &webSourceRegistry{byKey: map[string]int{}, offset: offset}
}

// add registers rawURL (no sidebar detail) and returns its index.
func (r *webSourceRegistry) add(rawURL string) (index int, ok bool) {
	return r.addDetailed(rawURL, "", "", "")
}

// addDetailed registers rawURL if new and returns its index; ok is false when the
// URL cannot be normalized (non-http(s) or unparseable). A URL seen again keeps
// its first index, but empty detail fields (title/snippet/favicon) are backfilled
// if this call supplies them — so a page first seen as a bare fetch URL gains its
// title/snippet when a search later returns it (or vice versa).
func (r *webSourceRegistry) addDetailed(rawURL, title, snippet, favicon string) (index int, ok bool) {
	key, host, ok := normalizeURL(rawURL)
	if !ok {
		return 0, false
	}
	title = strings.TrimSpace(title)
	snippet = snippetFromText(snippet)
	favicon = strings.TrimSpace(favicon)
	if idx, seen := r.byKey[key]; seen {
		src := &r.sources[idx-r.offset-1]
		if src.Title == "" {
			src.Title = title
		}
		if src.Snippet == "" {
			src.Snippet = snippet
		}
		if src.Favicon == "" {
			src.Favicon = favicon
		}
		return idx, true
	}
	idx := r.offset + len(r.sources) + 1
	r.byKey[key] = idx
	label := deriveLabel(host)
	r.sources = append(r.sources, webSource{
		Index:   idx,
		URL:     strings.TrimSpace(rawURL),
		Label:   label,
		Site:    siteName(rawURL, host, label),
		Title:   title,
		Snippet: snippet,
		Favicon: favicon,
	})
	return idx, true
}

// siteName is the key the favicon row groups sources by: the registrable
// domain's label, except for an IP host, whose "label" is just its last octet
// (192.168.1.10 and 10.0.1.20 would both read "1"). There the site is the host
// with its port, so two self-hosted services stay two sites.
func siteName(rawURL, host, label string) string {
	if net.ParseIP(host) == nil {
		return label
	}
	if u, err := url.Parse(strings.TrimSpace(rawURL)); err == nil && u.Host != "" {
		return strings.ToLower(u.Host)
	}
	return host
}

// snippetFromText collapses whitespace and caps a source snippet for the sidebar.
func snippetFromText(s string) string {
	s = strings.TrimSpace(strings.Join(strings.Fields(s), " "))
	if len(s) <= maxSourceSnippetChars {
		return s
	}
	// Trim to the cap on a rune boundary, then back off to the last space.
	cut := truncateBytesOnRuneBoundary(s, maxSourceSnippetChars)
	if sp := strings.LastIndexByte(cut, ' '); sp > maxSourceSnippetChars/2 {
		cut = cut[:sp]
	}
	return strings.TrimSpace(cut) + truncationEllipsis
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

// relabelWebToolOutput annotates a tool's result with [n] citation markers and
// registers each source URL, so the model can cite them inline: the tool's
// own relabel when it has one (toolSpec.relabel, which sees the structured
// sources), else the structured sources the result carried. A result with
// neither is left alone. The raw (un-capped) output is passed in; the caller
// caps the returned string.
func (s *Engine) relabelWebToolOutput(toolName string, arguments map[string]any, output string, sources []mcp.Source, reg *webSourceRegistry) string {
	if reg == nil {
		return output
	}
	if relabel := s.toolPolicy(toolName).relabel; relabel != nil {
		return relabel(arguments, output, sources, reg)
	}
	if len(sources) > 0 {
		return relabelStructuredSources(output, sources, reg)
	}
	return output
}

// relabelPageAtURL cites a fetched page: through its structured sources
// (the built-in's), else as the page at its url argument (an mcp.json
// replacement that sends none), its text the source's snippet.
func relabelPageAtURL(arguments map[string]any, output string, sources []mcp.Source, reg *webSourceRegistry) string {
	if len(sources) > 0 {
		return relabelStructuredSources(output, sources, reg)
	}
	idx, ok := reg.addDetailed(argURL(arguments), "", output, "")
	if !ok {
		return output
	}
	return fmt.Sprintf("Source [%d]: %s\n\n%s", idx, strings.TrimSpace(argURL(arguments)), output)
}

func argURL(arguments map[string]any) string {
	if arguments == nil {
		return ""
	}
	u, _ := arguments["url"].(string)
	return u
}

// prependURLSource registers url and prepends a "Source [n]: url" header to
// output so the marker sits next to the content the model reads. If url can't be
// registered, output is returned unchanged.
func prependURLSource(rawURL, output string, reg *webSourceRegistry) string {
	idx, ok := reg.add(rawURL)
	if !ok {
		return output
	}
	return fmt.Sprintf("Source [%d]: %s\n\n%s", idx, strings.TrimSpace(rawURL), output)
}

// webSourceCitations converts gathered web sources into citation records for
// streaming and persistence. Web citations carry URL + Index and reuse Filename
// as the display label; they have no DocumentID (RAG-only) so the frontend
// distinguishes them by the presence of url.
func webSourceCitations(sources []webSource) []Citation {
	if len(sources) == 0 {
		return nil
	}
	out := make([]Citation, 0, len(sources))
	for _, ws := range sources {
		out = append(out, Citation{
			Filename: ws.Label,
			URL:      ws.URL,
			Index:    ws.Index,
			Title:    ws.Title,
			Snippet:  ws.Snippet,
			Favicon:  ws.Favicon,
			Image:    ws.Image,
			Site:     ws.Site,
		})
	}
	return out
}
