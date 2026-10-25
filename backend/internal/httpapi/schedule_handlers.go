package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/trick77/loom/internal/schedule"
	"github.com/trick77/ship/kit/httpx"
)

// timezoneHeader carries the browser's IANA timezone; schedule slots are
// drawn in it.
const timezoneHeader = "X-Loom-Timezone"

// rememberTimezone keeps the timezone the user's browser last sent, so a
// schedule created from chat draws its slot in it.
func (s *server) rememberTimezone(userID string, r *http.Request) {
	tz := strings.TrimSpace(r.Header.Get(timezoneHeader))
	if tz == "" || len(tz) > 64 {
		return
	}
	if _, err := time.LoadLocation(tz); err != nil {
		return
	}
	s.timezones.Store(userID, tz)
}

// timezoneFor is the timezone rememberTimezone kept for the user; "" when
// none, which schedules read as UTC.
func (s *server) timezoneFor(userID string) string {
	if tz, ok := s.timezones.Load(userID); ok {
		return tz.(string)
	}
	return ""
}

// scheduleResponse is a schedule as the UI shows it: the stored fields, its
// slot read back, and its latest run.
type scheduleResponse struct {
	schedule.Schedule
	Summary  string        `json:"summary"`
	NextRuns []time.Time   `json:"nextRuns"`
	LastRun  *schedule.Run `json:"lastRun"`
	Template string        `json:"template"`
}

func (s *server) scheduleView(sch schedule.Schedule, last *schedule.Run) scheduleResponse {
	p := s.scheduleSvc.Describe(sch)
	return scheduleResponse{Schedule: sch, Summary: p.Summary, NextRuns: p.NextRuns, LastRun: last, Template: sch.CronTemplate}
}

// schedulesOn answers 503 while scheduled tasks are off.
func (s *server) schedulesOn(w http.ResponseWriter) bool {
	if s.schedules == nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, "scheduled tasks are not configured")
		return false
	}
	return true
}

// writeScheduleError maps the schedule package's errors to responses.
func writeScheduleError(w http.ResponseWriter, r *http.Request, err error) {
	var validation schedule.ValidationError
	switch {
	case errors.As(err, &validation):
		httpx.WriteError(w, http.StatusBadRequest, validation.Message)
	case errors.Is(err, schedule.ErrNotFound):
		writeNotFound(w)
	case errors.Is(err, schedule.ErrTooOften):
		httpx.WriteJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error(), "code": "too_often"})
	case errors.Is(err, schedule.ErrNotUnderstood):
		httpx.WriteJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error(), "code": "not_understood"})
	case errors.Is(err, schedule.ErrLimitReached):
		httpx.WriteJSON(w, http.StatusConflict, map[string]string{"error": err.Error(), "code": "limit_reached"})
	default:
		serverError(w, r, err, "schedule request failed")
	}
}

func (s *server) handleListSchedules(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !s.schedulesOn(w) {
		return
	}
	list, err := s.schedules.ListSchedules(r.Context(), user.ID)
	if err != nil {
		serverError(w, r, err, "list schedules failed")
		return
	}
	last, err := s.schedules.LastRuns(r.Context(), user.ID)
	if err != nil {
		serverError(w, r, err, "list schedules failed")
		return
	}
	out := make([]scheduleResponse, len(list))
	for i, sch := range list {
		var run *schedule.Run
		if l, found := last[sch.ID]; found {
			run = &l
		}
		out[i] = s.scheduleView(sch, run)
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (s *server) handleGetSchedule(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !s.schedulesOn(w) {
		return
	}
	sch, err := s.schedules.GetSchedule(r.Context(), user.ID, r.PathValue("scheduleID"))
	if err != nil {
		writeScheduleError(w, r, err)
		return
	}
	runs, err := s.schedules.ListRuns(r.Context(), user.ID, sch.ID, 1)
	if err != nil {
		serverError(w, r, err, "get schedule failed")
		return
	}
	var last *schedule.Run
	if len(runs) > 0 {
		last = &runs[0]
	}
	httpx.WriteJSON(w, http.StatusOK, s.scheduleView(sch, last))
}

// scheduleRequest is the body of a create, an update and a preview.
type scheduleRequest struct {
	Name     *string `json:"name"`
	Prompt   *string `json:"prompt"`
	Interval *string `json:"interval"`
	Timezone *string `json:"timezone"`
	Enabled  *bool   `json:"enabled"`
	Template string  `json:"template"`
	Cron     string  `json:"cron"`
	Reroll   bool    `json:"reroll"`
}

func (b scheduleRequest) input(r *http.Request) schedule.Input {
	in := schedule.Input{Name: b.Name, Prompt: b.Prompt, Interval: b.Interval, Timezone: b.Timezone, Enabled: b.Enabled,
		Template: b.Template, Cron: b.Cron, Reroll: b.Reroll}
	if in.Timezone == nil {
		if tz := strings.TrimSpace(r.Header.Get(timezoneHeader)); tz != "" {
			in.Timezone = &tz
		}
	}
	return in
}

func (s *server) handleCreateSchedule(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !s.schedulesOn(w) {
		return
	}
	var body scheduleRequest
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeDecodeError(w, err)
		return
	}
	sch, err := s.scheduleSvc.Create(r.Context(), user.ID, body.input(r))
	if err != nil {
		writeScheduleError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, s.scheduleView(sch, nil))
}

func (s *server) handleUpdateSchedule(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !s.schedulesOn(w) {
		return
	}
	var body scheduleRequest
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeDecodeError(w, err)
		return
	}
	in := body.input(r)
	if body.Timezone == nil {
		// An update keeps the schedule's timezone unless the body names one.
		in.Timezone = nil
	}
	sch, err := s.scheduleSvc.Update(r.Context(), user.ID, r.PathValue("scheduleID"), in)
	if err != nil {
		writeScheduleError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, s.scheduleView(sch, nil))
}

func (s *server) handleParseSchedule(w http.ResponseWriter, r *http.Request) {
	if _, ok := currentUser(w, r); !ok || !s.schedulesOn(w) {
		return
	}
	var body scheduleRequest
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeDecodeError(w, err)
		return
	}
	in := body.input(r)
	interval, timezone := "", ""
	if in.Interval != nil {
		interval = *in.Interval
	}
	if in.Timezone != nil {
		timezone = *in.Timezone
	}
	p, err := s.scheduleSvc.Interpret(r.Context(), interval, timezone)
	if err != nil {
		writeScheduleError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, p)
}

func (s *server) handleDeleteSchedule(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !s.schedulesOn(w) {
		return
	}
	if err := s.schedules.DeleteSchedule(r.Context(), user.ID, r.PathValue("scheduleID")); err != nil {
		writeScheduleError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleRunSchedule starts a run now, outside the schedule's slots. The run
// goes on in the background; the response only says it started.
func (s *server) handleRunSchedule(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !s.schedulesOn(w) {
		return
	}
	sch, err := s.schedules.GetSchedule(r.Context(), user.ID, r.PathValue("scheduleID"))
	if err != nil {
		writeScheduleError(w, r, err)
		return
	}
	if !s.runs.admit(user.ID) {
		httpx.WriteError(w, http.StatusConflict, "another scheduled run is in progress, try again in a few minutes")
		return
	}
	claimed, err := s.schedules.Claim(r.Context(), user.ID, sch.ID, s.clock())
	if err != nil || !claimed {
		s.runs.done(user.ID)
		if err != nil {
			serverError(w, r, err, "start schedule run failed")
			return
		}
		httpx.WriteError(w, http.StatusConflict, "this schedule is already running")
		return
	}
	s.startScheduleRun(context.WithoutCancel(r.Context()), sch)
	w.WriteHeader(http.StatusAccepted)
}

func (s *server) handleListScheduleRuns(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !s.schedulesOn(w) {
		return
	}
	sch, err := s.schedules.GetSchedule(r.Context(), user.ID, r.PathValue("scheduleID"))
	if err != nil {
		writeScheduleError(w, r, err)
		return
	}
	runs, err := s.schedules.ListRuns(r.Context(), user.ID, sch.ID, 50)
	if err != nil {
		serverError(w, r, err, "list runs failed")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, runs)
}

type scheduleMemoryResponse struct {
	Findings []schedule.Finding `json:"findings"`
	Sources  []schedule.Source  `json:"sources"`
	Queries  []schedule.Query   `json:"queries"`
}

func (s *server) handleGetScheduleMemory(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !s.schedulesOn(w) {
		return
	}
	sch, err := s.schedules.GetSchedule(r.Context(), user.ID, r.PathValue("scheduleID"))
	if err != nil {
		writeScheduleError(w, r, err)
		return
	}
	m, err := s.schedules.LoadMemory(r.Context(), user.ID, sch.ID)
	if err != nil {
		serverError(w, r, err, "load schedule memory failed")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, scheduleMemoryResponse{Findings: m.Findings, Sources: m.Sources, Queries: m.Queries})
}

// handleDeleteScheduleMemory forgets one memory item (?key=) or a whole kind:
// findings, sources or queries.
func (s *server) handleDeleteScheduleMemory(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !s.schedulesOn(w) {
		return
	}
	sch, err := s.schedules.GetSchedule(r.Context(), user.ID, r.PathValue("scheduleID"))
	if err != nil {
		writeScheduleError(w, r, err)
		return
	}
	key := r.URL.Query().Get("key")
	switch r.PathValue("kind") {
	case "findings":
		err = s.schedules.DeleteFindings(r.Context(), user.ID, sch.ID, key)
	case "sources":
		err = s.schedules.DeleteSources(r.Context(), user.ID, sch.ID, key)
	case "queries":
		err = s.schedules.DeleteQueries(r.Context(), user.ID, sch.ID, key)
	default:
		writeNotFound(w)
		return
	}
	if err != nil {
		serverError(w, r, err, "forget schedule memory failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) handleListNotifications(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !s.schedulesOn(w) {
		return
	}
	limit := 50
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 && v < limit {
		limit = v
	}
	list, err := s.schedules.ListNotifications(r.Context(), user.ID, limit)
	if err != nil {
		serverError(w, r, err, "list notifications failed")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, list)
}

func (s *server) handleUnreadNotifications(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}
	if s.schedules == nil {
		httpx.WriteJSON(w, http.StatusOK, map[string]int{"count": 0})
		return
	}
	n, err := s.schedules.UnreadCount(r.Context(), user.ID)
	if err != nil {
		serverError(w, r, err, "count notifications failed")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]int{"count": n})
}

func (s *server) handleReadNotification(w http.ResponseWriter, r *http.Request) {
	s.markNotificationsRead(w, r, r.PathValue("notificationID"))
}

func (s *server) handleReadAllNotifications(w http.ResponseWriter, r *http.Request) {
	s.markNotificationsRead(w, r, "")
}

func (s *server) markNotificationsRead(w http.ResponseWriter, r *http.Request, id string) {
	user, ok := currentUser(w, r)
	if !ok || !s.schedulesOn(w) {
		return
	}
	if err := s.schedules.MarkRead(r.Context(), user.ID, id); err != nil {
		serverError(w, r, err, "mark notifications read failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
