package httpapi

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/schedule"
)

// scheduleToolRunner runs the chat's schedule tools on the schedule service.
// Every answer is model-facing text; failures are text too.
type scheduleToolRunner struct{ s *server }

func toolString(args map[string]any, key string) *string {
	v, ok := args[key].(string)
	if !ok {
		return nil
	}
	return &v
}

func (t scheduleToolRunner) Create(ctx context.Context, user auth.User, timezone string, args map[string]any) string {
	in := schedule.Input{Name: toolString(args, "name"), Prompt: toolString(args, "prompt"), Interval: toolString(args, "interval")}
	if timezone != "" {
		in.Timezone = &timezone
	}
	sch, err := t.s.scheduleSvc.Create(ctx, user.ID, in)
	if err != nil {
		return scheduleToolError(err)
	}
	return "Created. " + t.describe(sch)
}

func (t scheduleToolRunner) List(ctx context.Context, user auth.User) string {
	list, err := t.s.schedules.ListSchedules(ctx, user.ID)
	if err != nil {
		return "tool failed: " + err.Error()
	}
	if len(list) == 0 {
		return "The user has no scheduled tasks."
	}
	lines := make([]string, len(list))
	for i, sch := range list {
		lines[i] = "- " + t.describe(sch)
	}
	return "The user's scheduled tasks:\n" + strings.Join(lines, "\n")
}

func (t scheduleToolRunner) Update(ctx context.Context, user auth.User, _ string, args map[string]any) string {
	id := strings.TrimSpace(stringOr(toolString(args, "id")))
	if id == "" {
		return "tool failed: id is required; get it from schedule_list."
	}
	in := schedule.Input{Name: toolString(args, "name"), Prompt: toolString(args, "prompt"), Interval: toolString(args, "interval")}
	if enabled, ok := args["enabled"].(bool); ok {
		in.Enabled = &enabled
	}
	sch, err := t.s.scheduleSvc.Update(ctx, user.ID, id, in)
	if err != nil {
		return scheduleToolError(err)
	}
	return "Updated. " + t.describe(sch)
}

func (t scheduleToolRunner) Delete(ctx context.Context, user auth.User, args map[string]any) string {
	id := strings.TrimSpace(stringOr(toolString(args, "id")))
	if id == "" {
		return "tool failed: id is required; get it from schedule_list."
	}
	sch, err := t.s.schedules.GetSchedule(ctx, user.ID, id)
	if err != nil {
		return scheduleToolError(err)
	}
	if err := t.s.schedules.DeleteSchedule(ctx, user.ID, id); err != nil {
		return scheduleToolError(err)
	}
	return fmt.Sprintf("Deleted the schedule %q. Its thread stays.", sch.Name)
}

// describe is one schedule for the model: what loom resolved, never what the
// model would guess, so it reports the real time.
func (t scheduleToolRunner) describe(sch schedule.Schedule) string {
	p := t.s.scheduleSvc.Describe(sch)
	state := "on"
	switch {
	case sch.DisabledReason == schedule.DisabledTimeout:
		state = "off, disabled automatically: two runs in a row took longer than the time limit"
	case !sch.Enabled:
		state = "off"
	}
	next := "none"
	if len(p.NextRuns) > 0 {
		next = p.NextRuns[0].Format("Mon 2 Jan 2006 15:04") + " " + sch.Timezone
		if loc, err := time.LoadLocation(sch.Timezone); err == nil {
			next = p.NextRuns[0].In(loc).Format("Mon 2 Jan 2006 15:04") + " " + sch.Timezone
		}
	}
	return fmt.Sprintf("%q (id %s): %s (%q), next run %s, %s. Prompt: %s",
		sch.Name, sch.ID, p.Summary, sch.IntervalText, next, state, sch.Prompt)
}

func stringOr(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func scheduleToolError(err error) string {
	var validation schedule.ValidationError
	switch {
	case errors.As(err, &validation):
		return "Could not save: " + validation.Message + "."
	case errors.Is(err, schedule.ErrTooOften):
		return "Could not save: schedules run at most twice a day. Ask the user for a less frequent interval."
	case errors.Is(err, schedule.ErrNotUnderstood):
		return "Could not save: the interval names no recurring time. Ask the user how often or when it should run."
	case errors.Is(err, schedule.ErrLimitReached):
		return fmt.Sprintf("Could not save: the user already has %d schedules, the most allowed. Ask which one to delete first.", schedule.MaxSchedulesPerUser)
	case errors.Is(err, schedule.ErrNotFound):
		return "No schedule has that id; get the ids from schedule_list."
	default:
		return "tool failed: " + err.Error()
	}
}
