package turn

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/trick77/loom/internal/mcp"
)

// numberedItemPattern matches a result item's "N. " at the start of a line,
// the shape a server uses to tie its text to structuredContent.sources (item
// N is sources[N-1]).
var numberedItemPattern = regexp.MustCompile(`(?m)^(\d+)\. `)

// relabelStructuredSources registers the sources an MCP tool returned in
// structuredContent and points the text at their [n] markers, so any server
// that follows the convention feeds the sources sidebar and inline citations.
// Item numbers in the text become the registry's markers; text without
// numbered items gets a header listing each source's marker instead.
func relabelStructuredSources(output string, sources []mcp.Source, reg *webSourceRegistry) string {
	markers := make([]int, len(sources))
	registered := false
	for i, src := range sources {
		idx, ok := reg.addSource(src)
		if ok {
			markers[i], registered = idx, true
		}
	}
	if !registered {
		return output
	}
	matched := false
	relabeled := numberedItemPattern.ReplaceAllStringFunc(output, func(item string) string {
		n, err := strconv.Atoi(strings.TrimSuffix(item, ". "))
		if err != nil || n < 1 || n > len(markers) || markers[n-1] == 0 {
			return item
		}
		matched = true
		return fmt.Sprintf("[%d] ", markers[n-1])
	})
	if matched {
		return relabeled
	}
	var header strings.Builder
	for i, src := range sources {
		if markers[i] == 0 {
			continue
		}
		title := strings.TrimSpace(src.Title)
		if title == "" {
			title = src.URL
		}
		fmt.Fprintf(&header, "Source [%d]: %s — %s\n", markers[i], title, strings.TrimSpace(src.URL))
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
	if label := strings.TrimSpace(src.Label); label != "" {
		if pos := idx - r.offset - 1; pos >= 0 && pos < len(r.sources) {
			r.sources[pos].Label = label
		}
	}
	return idx, true
}
