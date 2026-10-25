package turn

import (
	"context"

	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/llm"
)

// The schedule tools let the model create and manage the user's scheduled
// tasks from any chat. A scheduled run never gets them (see Run.allowTools).
const (
	scheduleCreateToolName = "schedule_create"
	scheduleListToolName   = "schedule_list"
	scheduleUpdateToolName = "schedule_update"
	scheduleDeleteToolName = "schedule_delete"
	scheduleRunToolName    = "schedule_run"
)

// ScheduleTools runs the schedule tools for the user and answers with the
// model-facing text. Failures are plain text too, never out-of-band errors.
type ScheduleTools interface {
	Create(ctx context.Context, user auth.User, timezone string, args map[string]any) string
	List(ctx context.Context, user auth.User) string
	Update(ctx context.Context, user auth.User, timezone string, args map[string]any) string
	Delete(ctx context.Context, user auth.User, args map[string]any) string
	Run(ctx context.Context, user auth.User, args map[string]any) string
}

func scheduleToolsOffered(e *Engine) bool { return e.schedules != nil }

var scheduleToolSpecs = []toolSpec{
	{
		name:    scheduleCreateToolName,
		schema:  scheduleCreateTool,
		offered: func(e *Engine, _ chat.Thread, _ toolGate) bool { return scheduleToolsOffered(e) },
		run: withArgs(func(ctx context.Context, t *Run, args map[string]any) string {
			return capToolOutput(t.e.schedules.Create(ctx, t.user, t.timezone, args))
		}),
	},
	{
		name:    scheduleListToolName,
		schema:  scheduleListTool,
		offered: func(e *Engine, _ chat.Thread, _ toolGate) bool { return scheduleToolsOffered(e) },
		run: withArgs(func(ctx context.Context, t *Run, _ map[string]any) string {
			return capToolOutput(t.e.schedules.List(ctx, t.user))
		}),
	},
	{
		name:    scheduleUpdateToolName,
		schema:  scheduleUpdateTool,
		offered: func(e *Engine, _ chat.Thread, _ toolGate) bool { return scheduleToolsOffered(e) },
		run: withArgs(func(ctx context.Context, t *Run, args map[string]any) string {
			return capToolOutput(t.e.schedules.Update(ctx, t.user, t.timezone, args))
		}),
	},
	{
		name:    scheduleDeleteToolName,
		schema:  scheduleDeleteTool,
		offered: func(e *Engine, _ chat.Thread, _ toolGate) bool { return scheduleToolsOffered(e) },
		run: withArgs(func(ctx context.Context, t *Run, args map[string]any) string {
			return capToolOutput(t.e.schedules.Delete(ctx, t.user, args))
		}),
	},
	{
		name:    scheduleRunToolName,
		schema:  scheduleRunTool,
		offered: func(e *Engine, _ chat.Thread, _ toolGate) bool { return scheduleToolsOffered(e) },
		run: withArgs(func(ctx context.Context, t *Run, args map[string]any) string {
			return capToolOutput(t.e.schedules.Run(ctx, t.user, args))
		}),
	},
}

func scheduleTool(name, description string, properties map[string]any, required []any) func() llm.Tool {
	return func() llm.Tool {
		params := map[string]any{"type": "object", "properties": properties}
		if len(required) > 0 {
			params["required"] = required
		}
		return llm.Tool{Type: "function", Function: llm.ToolFunction{Name: name, Description: description, Parameters: params}}
	}
}

var scheduleCreateTool = scheduleTool(scheduleCreateToolName,
	"Create a scheduled task: a prompt loom runs on its own, again and again, notifying the user only about what is new. "+
		"Use it when the user asks for something recurring (\"every week look for …\", \"daily check …\", \"keep me posted on …\"). "+
		"Each run starts without this conversation, so the prompt must stand alone: what to look for, the user's interests, places and limits. "+
		scheduleRunIsShort+
		"Runs at most twice a day. Returns the schedule with its resolved time; tell the user that time.",
	map[string]any{
		"prompt":   map[string]any{"type": "string", "description": "The self-contained task each run carries out, at most 2000 characters."},
		"interval": map[string]any{"type": "string", "description": "When to run, in the user's words, e.g. \"once a week\", \"every weekday morning\". loom picks a random time inside it."},
		"name":     map[string]any{"type": "string", "description": "Optional short name; generated from the prompt when left out."},
	}, []any{"prompt", "interval"})

var scheduleListTool = scheduleTool(scheduleListToolName,
	"List the user's scheduled tasks with their ids, prompts, times and status.",
	map[string]any{}, nil)

// scheduleRunIsShort tells the model what a run can do, so the prompt it
// writes asks for no more: a deep-research brief once ran past the limit.
const scheduleRunIsShort = "A run is short: a few tool rounds and minutes. Write a focused task, and name only sources the user gave or that are well known, never a site you are unsure exists. "

var scheduleUpdateTool = scheduleTool(scheduleUpdateToolName,
	"Change one of the user's scheduled tasks: its name, prompt or interval, or turn it off (disable, pause, stop: enabled=false) and on again (enable, resume: enabled=true). "+
		"Find the id with schedule_list by the schedule's name. A new interval gets a new random time. "+scheduleRunIsShort+"Returns the updated schedule.",
	map[string]any{
		"id":       map[string]any{"type": "string", "description": "The schedule's id, from schedule_list."},
		"name":     map[string]any{"type": "string"},
		"prompt":   map[string]any{"type": "string"},
		"interval": map[string]any{"type": "string"},
		"enabled":  map[string]any{"type": "boolean"},
	}, []any{"id"})

var scheduleRunTool = scheduleTool(scheduleRunToolName,
	"Start one of the user's scheduled tasks now, outside its time, when they ask to run or test it (\"run the cinema scout now\"). "+
		"Pass the id from schedule_list. The run works in the background and answers in a new thread of its own, not in this chat; "+
		"tell the user that, and do not do the task yourself. Its next regular time stays.",
	map[string]any{
		"id": map[string]any{"type": "string", "description": "The schedule's id, from schedule_list."},
	}, []any{"id"})

var scheduleDeleteTool = scheduleTool(scheduleDeleteToolName,
	"Delete one of the user's scheduled tasks for good, when they ask to remove it. Pass the id from schedule_list. Run threads the user left untouched go with it; ones they followed up in, starred, archived, shared or filed into a project stay.",
	map[string]any{
		"id": map[string]any{"type": "string", "description": "The schedule's id, from schedule_list."},
	}, []any{"id"})
