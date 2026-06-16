package chat

import (
	"strconv"
	"strings"

	"github.com/trick77/slopr/internal/titletext"
)

func NormalizeThreadTitle(title string) string {
	title = strings.TrimSpace(title)
	title = titletext.NormalizeQuotes(title)
	if unquoted, err := strconv.Unquote(title); err == nil {
		title = strings.TrimSpace(unquoted)
	} else {
		title = strings.TrimSpace(titletext.StripWrappingQuotes(title))
	}
	title = firstNonEmptyLine(title)
	title = trimMarkdownTitleSyntax(title)
	title = stripEmoji(title)
	title = strings.Join(strings.Fields(title), " ")
	return truncateThreadTitle(title)
}

// firstNonEmptyLine returns the first line that has non-whitespace content.
// Title models sometimes ignore the "2 to 6 words" instruction and return a
// full markdown answer (a heading line followed by body paragraphs); only that
// first line is a usable title. Collapsing the whole response with
// strings.Fields instead would mash the heading and body into one long title.
func firstNonEmptyLine(title string) string {
	for _, line := range strings.Split(title, "\n") {
		if strings.TrimSpace(line) != "" {
			return line
		}
	}
	return title
}

func trimMarkdownTitleSyntax(title string) string {
	for {
		before := title
		title = strings.TrimSpace(title)
		title = strings.TrimLeft(title, "#")
		title = strings.TrimSpace(title)
		for _, wrapper := range []string{"**", "__", "`", "*", "_"} {
			if strings.HasPrefix(title, wrapper) && strings.HasSuffix(title, wrapper) && len(title) >= len(wrapper)*2 {
				title = strings.TrimSpace(title[len(wrapper) : len(title)-len(wrapper)])
			}
		}
		if title == before {
			return title
		}
	}
}

func stripEmoji(title string) string {
	return strings.Map(func(r rune) rune {
		if isEmojiRune(r) {
			return -1
		}
		return r
	}, title)
}

func isEmojiRune(r rune) bool {
	return r == 0x200d ||
		r == 0x20e3 ||
		(r >= 0xfe00 && r <= 0xfe0f) ||
		(r >= 0x1f000 && r <= 0x1faff) ||
		(r >= 0x2600 && r <= 0x27bf)
}

func truncateThreadTitle(title string) string {
	runes := []rune(title)
	if len(runes) <= MaxThreadTitleLength {
		return title
	}
	return string(runes[:MaxThreadTitleLength-1]) + "…"
}
