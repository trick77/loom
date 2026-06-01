package chat

import (
	"strconv"
	"strings"
)

func NormalizeThreadTitle(title string) string {
	title = strings.TrimSpace(title)
	if unquoted, err := strconv.Unquote(title); err == nil {
		title = strings.TrimSpace(unquoted)
	} else {
		title = strings.Trim(title, `"'`)
		title = strings.TrimSpace(title)
	}
	title = trimMarkdownTitleSyntax(title)
	title = stripEmoji(title)
	title = strings.Join(strings.Fields(title), " ")
	return truncateThreadTitle(title)
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
