package turn

import (
	"context"

	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/llm"
)

// The idea tools let the model find the user's ideas and propose to save,
// delete or complete one. Nothing they do changes a visible idea: a proposal
// is a card in the thread, and only the user's click on it (an authenticated
// request from the UI) applies it. A scheduled run never gets them (only
// scheduledLastStep built-ins run there), an incognito turn has no tools.
const (
	ideasLookupToolName = "ideas_lookup"
	ideaProposeToolName = "idea_propose"
)

// Idea card actions.
const (
	IdeaActionSave   = "save"
	IdeaActionRemove = "remove"
	IdeaActionDone   = "done"
)

// IdeaCard is the card a proposal shows in the thread. The card carries
// what the click commits (title, reason, dates) so the user sees it in
// full; never the passage, which is outside text. State is the idea's at
// read time (proposed, open, done, deleted, gone), overlaid on every load.
type IdeaCard struct {
	IdeaID   string  `json:"ideaId"`
	Action   string  `json:"action"`
	State    string  `json:"state"`
	Title    string  `json:"title"`
	Reason   string  `json:"reason"`
	DueOn    *string `json:"dueOn"`
	RemindOn *string `json:"remindOn"`
}

// IdeaTools runs the idea tools for the user and answers with model-facing
// text. Failures are plain text, never out-of-band errors.
type IdeaTools interface {
	// Lookup lists the user's open ideas (and done ones when asked), those
	// matching query when one is given, as data lines, never with their
	// passage.
	Lookup(ctx context.Context, user auth.User, includeDone bool, query string) string
	// Propose records a proposal for the user's click and returns the card
	// to show, or nil when there is nothing to confirm (a refusal).
	Propose(ctx context.Context, user auth.User, threadID string, args map[string]any) (string, *IdeaCard)
}

func ideaToolsOffered(e *Engine) bool { return e.ideas != nil }

var ideaToolSpecs = []toolSpec{
	{
		name:    ideasLookupToolName,
		schema:  ideasLookupTool,
		offered: func(e *Engine, _ chat.Thread, _ toolGate) bool { return ideaToolsOffered(e) },
		// A read tool marked userIntent on purpose: it refuses once outside
		// content is in the turn, so a page cannot have the model read the
		// user's ideas and carry them off with a later call.
		userIntent: true,
		run: withArgs(func(ctx context.Context, t *Run, args map[string]any) string {
			includeDone, _ := args["include_done"].(bool)
			query, _ := args["query"].(string)
			output := t.e.ideas.Lookup(ctx, t.user, includeDone, query)
			// What it read may have come from a page: a proposal's title and
			// reason can be a page's words the user saved with one click. So
			// the rest of the turn treats it as outside content (no memory or
			// schedule call after it; a further lookup still runs), stricter
			// than a userIntent tool's rule.
			t.ideasRead = true
			return capToolOutput(output)
		}),
	},
	{
		name:    ideaProposeToolName,
		schema:  ideaProposeTool,
		offered: func(e *Engine, _ chat.Thread, _ toolGate) bool { return ideaToolsOffered(e) },
		// Not userIntent: it does not act (the click does), so it still runs
		// after a web search in the same turn. sharesIntent: its output is
		// fixed text that brings in nothing from outside, so calling it does
		// not lock the memory and schedule tools for the rest of the turn.
		sharesIntent: true,
		run: withArgs(func(ctx context.Context, t *Run, args map[string]any) string {
			output, card := t.e.ideas.Propose(ctx, t.user, t.thread.ID, args)
			if card != nil {
				t.ideaCards = append(t.ideaCards, *card)
			}
			return capToolOutput(output)
		}),
	},
}

// takeIdeaCards returns the cards the last tool call proposed, once.
func (t *Run) takeIdeaCards() []IdeaCard {
	cards := t.ideaCards
	t.ideaCards = nil
	return cards
}

func ideasLookupTool() llm.Tool {
	return llm.Tool{
		Type: "function",
		Function: llm.ToolFunction{
			Name: ideasLookupToolName,
			Description: "List the user's saved ideas (things they want to follow up later, each with the reason why) as data lines with opaque ids. " +
				"Use it to find the idea the user means before proposing to remove it or mark it done. " +
				"Call it before any other tool; after one it refuses. Memory and schedule changes refuse after it.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"include_done": map[string]any{
						"type":        "boolean",
						"description": "Also list ideas already marked done.",
					},
					"query": map[string]any{
						"type":        "string",
						"description": "Only ideas whose title or reason contains these words.",
					},
				},
			},
		},
	}
}

func ideaProposeTool() llm.Tool {
	return llm.Tool{
		Type: "function",
		Function: llm.ToolFunction{
			Name: ideaProposeToolName,
			Description: "Propose to save, delete or complete one of the user's ideas; only when the user explicitly asks, never offer it on your own. " +
				"It shows the user a card and changes nothing: the idea is saved, deleted or marked done only when the user clicks it, so never say it already was. " +
				"save: a short title and the user's reason why they want to follow it up, in their words; without a reason, ask for one and do not call. " +
				"remove, done: call " + ideasLookupToolName + " first and pass the id of exactly one clear match; unclear or several: list the candidates and ask.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"action": map[string]any{
						"type": "string",
						"enum": []any{IdeaActionSave, IdeaActionRemove, IdeaActionDone},
					},
					"title": map[string]any{
						"type":        "string",
						"description": "save: 2 to 6 words naming the idea.",
					},
					"reason": map[string]any{
						"type":        "string",
						"description": "save: why the user wants to follow it up, as they said it.",
					},
					"quote": map[string]any{
						"type":        "string",
						"description": "save: the passage of the conversation it is about, verbatim, if any.",
					},
					"due_on": map[string]any{
						"type":        "string",
						"description": "save: YYYY-MM-DD, only when the user named a date it is due.",
					},
					"id": map[string]any{
						"type":        "string",
						"description": "remove, done: the idea's id from " + ideasLookupToolName + ".",
					},
				},
				"required": []any{"action"},
			},
		},
	}
}
