package httpapi

import (
	"strings"
	"unicode"
)

// isTypographyImageRequest reports whether a (compiled) image prompt is about
// typography / logos / legible text — work for which FLUX.2 [flex] renders text
// far better than the klein default. It runs on the prompt the compiler produced
// (which is told to preserve "text requirements"), so logo/text intent surfaces
// as vocab even when the raw user message lacked it.
//
// It fires on three cues, biased toward firing because the cost of a miss (text
// rendered on the weaker klein model) is worse than the cost of a false positive
// (a non-text image generated on the slightly slower/costlier flex):
//
//   - a quoted span ("a banner reading 'OPEN TODAY'") — the strongest signal that
//     specific letters must be drawn legibly (see hasRenderableQuotedText);
//   - a typography/text noun ("logo", "wordmark", "poster", "infographic",
//     German "schriftzug"/"plakat"); or
//   - a "render this exact text" phrase ("a sign that says …", "the text reads …").
//
// Lexical detection cannot enumerate every phrasing; this is common-case, in the
// same spirit as image_heuristics.go.
func isTypographyImageRequest(prompt string) bool {
	if strings.TrimSpace(prompt) == "" {
		return false
	}
	if hasRenderableQuotedText(prompt) {
		return true
	}
	if containsAnyToken(wordTokens(prompt), typographyImageWords) {
		return true
	}
	lower := strings.ToLower(prompt)
	for _, phrase := range typographyImagePhrases {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}

// hasRenderableQuotedText reports whether the prompt contains a quoted span — the
// highest-signal cue that specific text must render. It accepts straight double
// quotes ("…"), typographic pairs (“…” «…» ‘…’) and token-bounded straight single
// quotes ('OPEN'), while deliberately ignoring apostrophes inside words ("don't",
// "the dog's") so contractions/possessives do not misfire.
func hasRenderableQuotedText(prompt string) bool {
	if strings.Count(prompt, "\"") >= 2 {
		return true
	}
	if strings.ContainsAny(prompt, "“«‘") && strings.ContainsAny(prompt, "”»’") {
		return true
	}
	return hasSingleQuotedSpan(prompt)
}

// hasSingleQuotedSpan reports whether a straight single quote opens at a token
// boundary (start/space) on non-space content and a later single quote closes at
// a token boundary (end/space/closing punctuation) — i.e. a real 'quoted' span,
// not an apostrophe inside a word.
func hasSingleQuotedSpan(prompt string) bool {
	runes := []rune(prompt)
	openIdx := -1
	for i, r := range runes {
		if r != '\'' {
			continue
		}
		atOpenBoundary := (i == 0 || unicode.IsSpace(runes[i-1]) || runes[i-1] == '(') &&
			i+1 < len(runes) && !unicode.IsSpace(runes[i+1])
		atCloseBoundary := i == len(runes)-1 || unicode.IsSpace(runes[i+1]) || isClosingPunct(runes[i+1])
		if openIdx == -1 {
			if atOpenBoundary {
				openIdx = i
			}
			continue
		}
		if atCloseBoundary && i > openIdx+1 {
			return true
		}
	}
	return false
}

func isClosingPunct(r rune) bool {
	switch r {
	case '.', ',', '!', '?', ';', ':', ')', ']', '}':
		return true
	default:
		return false
	}
}

// typographyImageWords are nouns whose images are inherently text-forward, so a
// single one is enough to read the request as typography work. German tokens use
// Swiss orthography (ss, no ß). wordTokens lowercases and splits on non-alphanumeric,
// so entries are bare lowercase stems.
var typographyImageWords = map[string]bool{
	// Logos / marks.
	"logo": true, "logos": true, "wordmark": true, "wordmarks": true,
	"monogram": true, "monograms": true, "emblem": true, "emblems": true,
	"watermark": true, "watermarks": true,
	// Lettering / type.
	"typography": true, "typographic": true, "typeface": true, "typefaces": true,
	"lettering": true, "calligraphy": true, "font": true, "fonts": true,
	"text": true, "letters": true, "slogan": true, "slogans": true,
	"tagline": true, "taglines": true, "headline": true, "headlines": true,
	"caption": true, "captions": true, "subtitle": true, "subtitles": true,
	// Text-heavy artifacts.
	"poster": true, "posters": true, "banner": true, "banners": true,
	"signage": true, "billboard": true, "billboards": true, "sign": true, "signs": true,
	"label": true, "labels": true, "menu": true, "menus": true,
	"flyer": true, "flyers": true, "brochure": true, "brochures": true,
	"certificate": true, "certificates": true, "infographic": true, "infographics": true,
	// German (Swiss orthography).
	"schriftzug": true, "schrift": true, "plakat": true, "plakate": true,
	"beschriftung": true, "aufschrift": true, "infografik": true, "infografiken": true,
}

// typographyImagePhrases are "render this exact text" cues matched as substrings
// of the lowercased prompt. Kept tight so ordinary description ("a person reading
// a book") does not misfire — the quote detector handles "reading '…'" cases.
var typographyImagePhrases = []string{
	"that says", "that reads", "which says", "which reads",
	"the text reads", "with the text", "with the word", "the words",
	"spelling out", "spelled out", "the phrase",
}
