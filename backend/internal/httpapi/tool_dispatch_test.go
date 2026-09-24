package httpapi

import (
	"context"
	"strings"
	"testing"

	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/llm"
)

// Every argument-taking built-in tool used to parse its arguments in its own
// copy of the same block; one path now serves them all.
func TestExecuteBuiltInToolReportsInvalidArgumentsForEveryArgTool(t *testing.T) {
	s := &server{}
	for _, name := range []string{conversationSearchToolName, readThreadToolName, addUserDirectiveToolName, removeUserDirectiveToolName, replaceUserDirectiveToolName} {
		call := llm.ToolCall{ID: "c1", Type: "function", Function: llm.ToolCallFunction{Name: name, Arguments: "{not json"}}
		output, resp, handled := s.executeBuiltInTool(context.Background(), nil, testUser, chat.Thread{ID: "t1"}, call, nil, false)
		if !handled || resp != nil {
			t.Fatalf("%s: handled=%v resp=%v, want handled with no artifact", name, handled, resp)
		}
		if !strings.HasPrefix(output, "tool failed: invalid arguments") {
			t.Fatalf("%s: output = %q, want the invalid-arguments failure", name, output)
		}
	}
}
