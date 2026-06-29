package httpapi

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/llm"
)

// conversationSearchToolName is the built-in tool the model calls to full-text
// search the user's entire conversation history (every thread, across every
// project). Its reason to exist — versus read_project_threads — is whole-history,
// cross-project reach: "did we discuss this before?". It pairs with read_thread,
// which loads a hit's thread in full.
const conversationSearchToolName = "conversation_search"

// conversationSearchMaxResults caps how many hits are returned to the model. Kept
// small so recalled history doesn't crowd out the live context budget.
const conversationSearchMaxResults = 8

// conversationSearchTool is the schema advertised to the model.
func conversationSearchTool() llm.Tool {
	return llm.Tool{
		Type: "function",
		Function: llm.ToolFunction{
			Name: conversationSearchToolName,
			Description: "Search the full text of ALL the user's past conversations — every thread, across every project — for a word or phrase. " +
				"Use it whenever the user might be returning to something discussed before (\"didn't we talk about X?\", \"what did we decide about Y\"), " +
				"or proactively before answering a question that earlier conversations likely already covered. " +
				"Returns the most relevant matching messages, each with its thread title, date, and thread id. " +
				"To read a match in full, call read_thread with the thread id from a result.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "The words or phrase to search for. Keyword / full-text match over message text; all words must appear.",
					},
					"current_project_only": map[string]any{
						"type":        "boolean",
						"description": "When true and the current thread belongs to a project, restrict the search to that project's threads. Defaults to false (search the whole history).",
					},
				},
				"required": []any{"query"},
			},
		},
	}
}

// conversationSearchDigest runs the full-text search and renders the hits for the
// model. It is user-scoped via SearchMessages, excludes the active thread (already
// in context), and never errors out — failures and empty results surface as plain
// notes, since this feeds a tool result.
func (s *server) conversationSearchDigest(ctx context.Context, userID string, thread chat.Thread, args map[string]any) string {
	query, _ := args["query"].(string)
	query = strings.TrimSpace(query)
	if query == "" {
		return "tool failed: query is required."
	}

	var projectID *string
	if only, _ := args["current_project_only"].(bool); only && thread.ProjectID != nil {
		projectID = thread.ProjectID
	}

	hits, err := s.thread.SearchMessages(ctx, userID, query, projectID, thread.ID, conversationSearchMaxResults)
	if err != nil {
		slog.Warn("conversation_search: search failed", "error", err)
		return "tool failed: search failed: " + err.Error()
	}
	if len(hits) == 0 {
		scope := "your past conversations"
		if projectID != nil {
			scope = "this project's conversations"
		}
		return fmt.Sprintf("No messages in %s matched %q.", scope, query)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Found %d matching message(s), most relevant first. Each shows the thread it came from; "+
		"call read_thread with a thread id to read that conversation in full.\n", len(hits))
	for i, h := range hits {
		fmt.Fprintf(&b, "\n%d. %s — %s — thread %s [%s]\n",
			i+1,
			strings.TrimSpace(displayThreadTitle(chat.Thread{Title: h.ThreadTitle})),
			h.CreatedAt.Format("2006-01-02"),
			h.ThreadID,
			roleLabel(h.Role),
		)
		b.WriteString(strings.TrimSpace(h.Snippet))
		b.WriteString("\n")
	}
	return capToolOutput(b.String())
}
