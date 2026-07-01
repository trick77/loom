package llm

// appendLanguageDirective appends an instruction to write a user-facing utility
// generation (thread title, project description, reasoning title, project memory)
// in the given language. An empty language — the English default — leaves the
// prompt unchanged, mirroring how the main chat's systemPromptForUser omits the
// directive for the default so titles and summaries match the language the chat
// itself answers in.
func appendLanguageDirective(systemPrompt, responseLanguage string) string {
	if responseLanguage == "" {
		return systemPrompt
	}
	return systemPrompt + " Always write your reply in this language: " + responseLanguage + "."
}
