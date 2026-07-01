package docgen

import (
	"encoding/json"
	"strings"
)

type pdfBlock struct {
	Type     string // heading | paragraph | bullets | table | columns | callout | code
	Level    int
	Text     string
	Language string // optional syntax-highlighting hint for code blocks
	Items    []string
	Rows     [][]string
	Left     []string
	Right    []string
}

// parseBlocks reads the optional typed "blocks" array. Returns nil if absent.
func parseBlocks(payload map[string]any) []pdfBlock {
	raw, ok := payload["blocks"].([]any)
	if !ok {
		// MiMo frequently serializes the blocks array as a JSON-encoded string
		// (blocks: "[{...}]") rather than a real array. Decode that form instead of
		// silently dropping the entire document and failing "content or blocks are
		// required".
		s, isStr := payload["blocks"].(string)
		if !isStr || strings.TrimSpace(s) == "" {
			return nil
		}
		if err := json.Unmarshal([]byte(s), &raw); err != nil {
			return nil
		}
	}
	var out []pdfBlock
	for _, r := range raw {
		item, ok := r.(map[string]any)
		if !ok {
			continue
		}
		b := pdfBlock{
			Type:     strings.TrimSpace(stringField(item, "type")),
			Text:     strings.TrimSpace(stringField(item, "text")),
			Language: strings.TrimSpace(stringField(item, "language")),
		}
		if lvl, ok := item["level"].(float64); ok {
			b.Level = int(lvl)
		}
		b.Items, _ = stringList(item["items"], "items")
		b.Rows = stringMatrix(item["rows"])
		b.Left, _ = stringList(item["left"], "left")
		b.Right, _ = stringList(item["right"], "right")
		if b.Type != "" {
			out = append(out, b)
		}
	}
	return out
}

// blocksFromMarkdown converts a plain content string into heading/paragraph/bullets blocks.
func blocksFromMarkdown(content string) []pdfBlock {
	var out []pdfBlock
	for _, line := range strings.Split(content, "\n") {
		t := strings.TrimSpace(line)
		switch {
		case t == "":
			continue
		case strings.HasPrefix(t, "## "):
			out = append(out, pdfBlock{Type: "heading", Level: 2, Text: strings.TrimSpace(t[3:])})
		case strings.HasPrefix(t, "# "):
			out = append(out, pdfBlock{Type: "heading", Level: 1, Text: strings.TrimSpace(t[2:])})
		case strings.HasPrefix(t, "- "):
			out = append(out, pdfBlock{Type: "bullets", Items: []string{strings.TrimSpace(t[2:])}})
		default:
			out = append(out, pdfBlock{Type: "paragraph", Text: t})
		}
	}
	return out
}

// Canonical status-marker runes. The many check/cross variants a model emits are
// normalized to these when markers are coloured (see markers() in pdf_html.go).
const (
	checkRune = '✓' // U+2713
	crossRune = '✗' // U+2717
)

// markerNormalize maps the many checkmark/cross glyphs a model may emit — plain,
// heavy, emoji-presentation, ballot — to the canonical checkRune/crossRune.
var markerNormalize = map[rune]rune{
	'✓': checkRune, '✔': checkRune, '✅': checkRune, '☑': checkRune, '🗸': checkRune, '🗹': checkRune,
	'✗': crossRune, '✘': crossRune, '❌': crossRune, '❎': crossRune, '✕': crossRune, '✖': crossRune, '☒': crossRune, '🗴': crossRune,
}
