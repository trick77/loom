package llm

import (
	"fmt"

	"github.com/trick77/llmwire"
)

// chatProfile is the llmwire profile of the chat model, resolved once so a
// typo in textModel, a model llmwire does not know, or an effort level the
// model does not take fails at init. Model facts (context window, key
// variable, rates, accepted levels) are read from it, never restated in loom.
var chatProfile = mustChatProfile()

func mustChatProfile() *llmwire.Profile {
	p, err := llmwire.Default().Lookup(textModel)
	if err != nil {
		panic(err)
	}
	for _, level := range []string{turnReasoningEffort, helperReasoningEffort} {
		if err := checkEffort(p, level); err != nil {
			panic(err)
		}
	}
	return p
}

// checkEffort reports a level the profile does not accept. The levels are
// loom's choice; which exist is the profile's.
func checkEffort(p *llmwire.Profile, level string) error {
	if !p.Reasoning.Accepts(level) {
		return fmt.Errorf("llm: %s does not accept reasoning effort %q (accepts %v)", p.ID, level, p.Reasoning.EffortValues)
	}
	return nil
}

// ContextWindow is the chat model's context window in tokens, from its
// profile. The UI divides a turn's context tokens by it for the context-%
// segment.
func ContextWindow() int64 {
	return chatProfile.Limits.Context
}

// APIKeyEnv is the variable the chat key is read from (llmwire.FromEnv): its
// profile's provider decides the name. A set value turns chat on.
func APIKeyEnv() string {
	return chatProfile.APIKeyEnv()
}
