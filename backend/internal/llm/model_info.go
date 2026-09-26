package llm

import "github.com/trick77/llmwire"

// chatProfile is the llmwire profile of the chat model, resolved once so a
// typo in textModel or a model llmwire does not know fails at init. Model
// facts (context window, rates, reasoning levels) are read from it, never
// restated in loom.
var chatProfile = mustChatProfile()

func mustChatProfile() *llmwire.Profile {
	p, err := llmwire.Default().Lookup(textModel)
	if err != nil {
		panic(err)
	}
	return p
}

// ContextWindow is the chat model's context window in tokens, from its
// profile. The UI divides a turn's context tokens by it for the context-%
// segment.
func ContextWindow() int64 {
	return chatProfile.Limits.Context
}
