package schedule

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"strings"
	"sync"
	"time"
)

// ErrNotUnderstood is interval text the gate model could not turn into a
// schedule ("when it rains").
var ErrNotUnderstood = errors.New("not a time that can be scheduled")

// ValidationError is input the user can fix; its message is shown as is.
type ValidationError struct{ Message string }

func (e ValidationError) Error() string { return e.Message }

func invalid(format string, args ...any) error {
	return ValidationError{Message: fmt.Sprintf(format, args...)}
}

// Interpreter reads interval text as a cron template whose free fields are
// "~" (see ResolveTemplate). ok is false when the text names no schedule.
type Interpreter interface {
	ParseSchedule(ctx context.Context, text string) (template string, ok bool, err error)
}

// Namer names a schedule from its prompt when the user left the name empty.
type Namer interface {
	NameSchedule(ctx context.Context, userID, prompt string) (string, error)
}

// ThreadTitles keeps a schedule's thread titled like the schedule.
type ThreadTitles interface {
	SetThreadTitle(ctx context.Context, userID, threadID, title string) error
}

// Service is the schedule logic the HTTP handlers and the chat tools share.
type Service struct {
	Store       *Store
	Interpreter Interpreter
	Namer       Namer
	Threads     ThreadTitles
	// Now and Rand are the clock and the slot draws; tests fix them.
	Now  func() time.Time
	Rand *rand.Rand

	mu sync.Mutex
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) resolve(template string, loc *time.Location) (Spec, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rng := s.Rand
	if rng == nil {
		rng = rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64())) //nolint:gosec // a slot draw spreads load, it guards nothing
	}
	spec, err := ResolveTemplate(template, loc, s.now(), rng)
	switch {
	case err == nil, errors.Is(err, ErrTooOften):
		return spec, err
	default:
		// A template of known tokens that still cannot be drawn or never
		// fires ("February 30th") is an interval the user can rephrase.
		return Spec{}, ErrNotUnderstood
	}
}

// Preview is an interval read and drawn, before anything is saved.
type Preview struct {
	Interval string      `json:"interval"`
	Template string      `json:"template"`
	Cron     string      `json:"cron"`
	Timezone string      `json:"timezone"`
	Summary  string      `json:"summary"`
	NextRuns []time.Time `json:"nextRuns"`
}

// LoadLocation resolves a timezone name; "" is UTC.
func LoadLocation(name string) (*time.Location, error) {
	if strings.TrimSpace(name) == "" {
		return time.UTC, nil
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, invalid("unknown timezone %q", name)
	}
	return loc, nil
}

// Interpret reads interval text and draws a slot in timezone.
func (s *Service) Interpret(ctx context.Context, interval, timezone string) (Preview, error) {
	interval = strings.TrimSpace(interval)
	if interval == "" {
		return Preview{}, invalid("say when the schedule should run")
	}
	if len([]rune(interval)) > MaxIntervalLength {
		return Preview{}, invalid("the interval is too long")
	}
	loc, err := LoadLocation(timezone)
	if err != nil {
		return Preview{}, err
	}
	template, ok, err := s.Interpreter.ParseSchedule(ctx, interval)
	if err != nil {
		return Preview{}, fmt.Errorf("interpret interval: %w", err)
	}
	if !ok || !ValidTemplate(template) {
		return Preview{}, ErrNotUnderstood
	}
	spec, err := s.resolve(template, loc)
	if err != nil {
		return Preview{}, err
	}
	return s.preview(interval, template, spec, loc), nil
}

func (s *Service) preview(interval, template string, spec Spec, loc *time.Location) Preview {
	return Preview{
		Interval: interval,
		Template: template,
		Cron:     spec.String(),
		Timezone: loc.String(),
		Summary:  spec.Describe(),
		NextRuns: spec.NextRuns(s.now(), loc, 3),
	}
}

// Describe previews a stored schedule's slot.
func (s *Service) Describe(sch Schedule) Preview {
	loc, err := LoadLocation(sch.Timezone)
	if err != nil {
		loc = time.UTC
	}
	spec, err := ParseCron(sch.Cron)
	if err != nil {
		return Preview{Interval: sch.IntervalText, Cron: sch.Cron, Timezone: sch.Timezone}
	}
	p := s.preview(sch.IntervalText, sch.CronTemplate, spec, loc)
	if !sch.Enabled {
		p.NextRuns = []time.Time{}
	}
	return p
}

// Input is a create or an update. A nil field keeps the stored value.
type Input struct {
	Name     *string
	Prompt   *string
	Interval *string
	Timezone *string
	Enabled  *bool
	// Template and Cron, when both set, are a slot the client already drew
	// with Interpret: they are checked, not drawn again, so the saved slot is
	// the one the user saw.
	Template string
	Cron     string
	// Reroll draws a new slot for an unchanged interval.
	Reroll bool
}

// Create stores a new schedule for the user.
func (s *Service) Create(ctx context.Context, userID string, in Input) (Schedule, error) {
	if in.Prompt == nil || in.Interval == nil {
		return Schedule{}, invalid("a schedule needs a prompt and an interval")
	}
	sch := Schedule{UserID: userID, Enabled: true}
	if in.Enabled != nil {
		sch.Enabled = *in.Enabled
	}
	if in.Timezone != nil {
		sch.Timezone = *in.Timezone
	}
	if err := s.apply(ctx, &sch, in, true); err != nil {
		return Schedule{}, err
	}
	return s.Store.CreateSchedule(ctx, sch)
}

// Update changes one of the user's schedules.
func (s *Service) Update(ctx context.Context, userID, id string, in Input) (Schedule, error) {
	sch, err := s.Store.GetSchedule(ctx, userID, id)
	if err != nil {
		return Schedule{}, err
	}
	oldName := sch.Name
	if err := s.apply(ctx, &sch, in, false); err != nil {
		return Schedule{}, err
	}
	saved, err := s.Store.SaveSchedule(ctx, sch)
	if err != nil {
		return Schedule{}, err
	}
	// The schedule is saved by now: a thread that cannot be renamed is logged,
	// not reported as a failed save.
	if saved.Name != oldName && saved.ThreadID != nil && s.Threads != nil {
		if err := s.Threads.SetThreadTitle(ctx, userID, *saved.ThreadID, saved.Name); err != nil {
			slog.Warn("rename schedule thread failed", "schedule_id", saved.ID, "err", err)
		}
	}
	return saved, nil
}

func (s *Service) apply(ctx context.Context, sch *Schedule, in Input, creating bool) error {
	if in.Prompt != nil {
		prompt := strings.TrimSpace(*in.Prompt)
		if prompt == "" {
			return invalid("the prompt is required")
		}
		if len([]rune(prompt)) > MaxPromptLength {
			return invalid("the prompt is too long")
		}
		sch.Prompt = prompt
	}
	if in.Name != nil {
		sch.Name = strings.TrimSpace(*in.Name)
		if len([]rune(sch.Name)) > MaxNameLength {
			return invalid("the name is too long")
		}
	}
	if sch.Name == "" {
		sch.Name = s.name(ctx, sch.UserID, sch.Prompt)
	}
	if in.Timezone != nil && *in.Timezone != sch.Timezone {
		sch.Timezone = *in.Timezone
	}
	loc, err := LoadLocation(sch.Timezone)
	if err != nil {
		return err
	}
	sch.Timezone = loc.String()

	intervalChanged := in.Interval != nil && strings.TrimSpace(*in.Interval) != sch.IntervalText
	switch {
	case in.Template != "" && in.Cron != "":
		spec, err := checkDrawn(in.Template, in.Cron, loc, s.now())
		if err != nil {
			return err
		}
		if in.Interval != nil {
			interval := strings.TrimSpace(*in.Interval)
			if interval == "" {
				return invalid("say when the schedule should run")
			}
			if len([]rune(interval)) > MaxIntervalLength {
				return invalid("the interval is too long")
			}
			sch.IntervalText = interval
		}
		sch.CronTemplate, sch.Cron = in.Template, spec.String()
	case creating || intervalChanged:
		p, err := s.Interpret(ctx, *in.Interval, sch.Timezone)
		if err != nil {
			return err
		}
		sch.IntervalText, sch.CronTemplate, sch.Cron = p.Interval, p.Template, p.Cron
	case in.Reroll:
		spec, err := s.resolve(sch.CronTemplate, loc)
		if err != nil {
			return err
		}
		sch.Cron = spec.String()
	}

	if in.Enabled != nil {
		if *in.Enabled && !sch.Enabled {
			sch.DisabledReason, sch.DisabledAt = "", nil
		}
		sch.Enabled = *in.Enabled
	}
	sch.NextRunAt = nil
	if sch.Enabled {
		spec, err := ParseCron(sch.Cron)
		if err != nil {
			return err
		}
		if next := spec.Next(s.now(), loc); !next.IsZero() {
			sch.NextRunAt = &next
		}
	}
	return nil
}

// checkDrawn verifies a slot the client drew: a template of known tokens and
// a concrete cron that respects the floor.
func checkDrawn(template, cron string, loc *time.Location, now time.Time) (Spec, error) {
	if !ValidTemplate(template) || strings.Contains(cron, "~") {
		return Spec{}, invalid("the schedule's slot is not valid")
	}
	spec, err := ParseCron(cron)
	if err != nil {
		return Spec{}, invalid("the schedule's slot is not valid")
	}
	if spec.Next(now, loc).IsZero() {
		return Spec{}, invalid("the schedule never runs")
	}
	if spec.minGap(now, loc) < MinRunGap {
		return Spec{}, ErrTooOften
	}
	return spec, nil
}

func (s *Service) name(ctx context.Context, userID, prompt string) string {
	if s.Namer != nil {
		if name, err := s.Namer.NameSchedule(ctx, userID, prompt); err == nil {
			if name = strings.TrimSpace(name); name != "" {
				return trimText(name, MaxNameLength)
			}
		}
	}
	return trimText(strings.Join(strings.Fields(prompt), " "), 40)
}

// NextRun returns when a schedule runs next after now; nil when it is off or
// never runs again.
func NextRun(sch Schedule, now time.Time) *time.Time {
	if !sch.Enabled {
		return nil
	}
	loc, err := LoadLocation(sch.Timezone)
	if err != nil {
		return nil
	}
	spec, err := ParseCron(sch.Cron)
	if err != nil {
		return nil
	}
	next := spec.Next(now, loc)
	if next.IsZero() {
		return nil
	}
	return &next
}
