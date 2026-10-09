package auth

import (
	"strings"

	"golang.org/x/text/language"
	"golang.org/x/text/language/display"
)

// ResponseLanguageName resolves the language the user pinned in their profile
// to its English name (for example "de" -> "German"), the form the model
// prompts name it in. Both the chat's answer-language directive and the
// user-facing utility generations (thread title, project description,
// reasoning title, project memory) use it, so they match. Unset returns "":
// nothing is pinned, and the output simply follows the user's own language. A
// legacy "auto" (predating its removal) is treated as unset, defensively.
func (u User) ResponseLanguageName() string {
	if u.ResponseLanguage == "" || strings.EqualFold(u.ResponseLanguage, "auto") {
		return ""
	}
	return languageName(u.ResponseLanguage)
}

// languageName resolves a profile language value to its English name (for
// example "de" -> "German"). Values that are not valid language tags — such as
// a name that is already spelled out — are returned unchanged.
func languageName(value string) string {
	tag, err := language.Parse(value)
	if err != nil {
		return value
	}
	if name := display.English.Tags().Name(tag); name != "" {
		return name
	}
	return value
}
