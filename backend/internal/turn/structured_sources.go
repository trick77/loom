package turn

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/trick77/loom/internal/mcp"
)

// relabelStructuredSources registers the sources an MCP tool returned in
// structuredContent and prepends a header naming each one's [n] marker, so any
// server that follows the convention feeds the sources sidebar and inline
// citations. sources[i] belongs to the text's result item i+1, which the header
// says; the text itself is never rewritten — a numbered list inside a result
// (chapters, key points) must reach the model as the server wrote it.
func relabelStructuredSources(output string, sources []mcp.Source, reg *webSourceRegistry) string {
	var header strings.Builder
	for i, src := range sources {
		idx, ok := reg.addSource(src)
		if !ok {
			continue
		}
		title := strings.TrimSpace(src.Title)
		if title == "" {
			title = src.URL
		}
		if len(sources) > 1 {
			fmt.Fprintf(&header, "Source [%d] (result %d): %s — %s\n", idx, i+1, title, strings.TrimSpace(src.URL))
		} else {
			fmt.Fprintf(&header, "Source [%d]: %s — %s\n", idx, title, strings.TrimSpace(src.URL))
		}
	}
	if header.Len() == 0 {
		return output
	}
	return header.String() + "\n" + output
}

// addSource registers a structured source. The server's label, when given,
// replaces the host-derived one: it names the source better than a domain
// can (a channel on a self-hosted library rather than the library's host).
func (r *webSourceRegistry) addSource(src mcp.Source) (int, bool) {
	idx, ok := r.addDetailed(src.URL, src.Title, src.Snippet, "")
	if !ok {
		return 0, false
	}
	if pos := idx - r.offset - 1; pos >= 0 && pos < len(r.sources) {
		if label := strings.TrimSpace(src.Label); label != "" {
			r.sources[pos].Label = label
		}
		if image := httpsURL(src.Image); image != "" && r.sources[pos].Image == "" {
			r.sources[pos].Image = image
		}
	}
	return idx, true
}

// httpsURL returns raw when it is an absolute https URL, else "": a server's
// image URL is fetched by loom's image proxy, so nothing else gets that far.
func httpsURL(raw string) string {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return ""
	}
	return raw
}
