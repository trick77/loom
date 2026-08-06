package llm

// appendLanguageDirective appends an instruction to write a user-facing utility
// generation (thread title, project description, reasoning title, project memory)
// in the given language.
//
// An empty language means the user pinned nothing, not that English was chosen.
// This path used to append nothing at all in that case, which left the
// short-gate model — trained largely on Chinese — free to pick a language on its
// own. The chat path never had that hole: languageDirective still emits "Answer
// in the language the user writes in." for the unset case, so this mirrors it,
// with an explicit tie-break for material whose language is genuinely unclear
// (a bare proper noun, a code snippet).
func appendLanguageDirective(systemPrompt, responseLanguage string) string {
	if responseLanguage == "" {
		return systemPrompt + " Always write your reply in the same language as the user's own words in the material below. If that is unclear, write it in English."
	}
	return systemPrompt + " Always write your reply in this language: " + responseLanguage + "."
}
