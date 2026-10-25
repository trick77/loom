package schedule_test

import (
	"context"
	"errors"
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"github.com/trick77/loom/internal/schedule"
)

type fakeInterpreter struct {
	template string
	ok       bool
	err      error
	calls    int
}

func (f *fakeInterpreter) ParseSchedule(_ context.Context, _ string) (string, bool, error) {
	f.calls++
	return f.template, f.ok, f.err
}

type fakeNamer struct{ name string }

func (f fakeNamer) NameSchedule(context.Context, string, string) (string, error) { return f.name, nil }

// sunday is Sunday 25 Oct 2026, 10:00 in Zurich.
var sunday = time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)

func newService(t *testing.T, interp *fakeInterpreter) *schedule.Service {
	t.Helper()
	return &schedule.Service{
		Store:       schedule.NewStore(openDB(t)),
		Interpreter: interp,
		Namer:       fakeNamer{name: "Cinema scout"},
		Now:         func() time.Time { return sunday },
		Rand:        rand.New(rand.NewPCG(1, 2)),
	}
}

func ptr[T any](v T) *T { return &v }

func TestInterpret(t *testing.T) {
	svc := newService(t, &fakeInterpreter{template: "~ 7~22 * * ~", ok: true})
	p, err := svc.Interpret(context.Background(), " once a week ", "Europe/Zurich")
	if err != nil {
		t.Fatalf("Interpret: %v", err)
	}
	if p.Interval != "once a week" || p.Template != "~ 7~22 * * ~" || p.Timezone != "Europe/Zurich" || len(p.NextRuns) != 3 {
		t.Fatalf("preview = %+v", p)
	}
	if !strings.HasPrefix(p.Summary, "Weekly on ") {
		t.Fatalf("summary = %q", p.Summary)
	}
	if gap := p.NextRuns[1].Sub(p.NextRuns[0]); gap != 7*24*time.Hour {
		t.Fatalf("weekly runs %v apart", gap)
	}
}

func TestInterpretErrors(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		interp   *fakeInterpreter
		interval string
		tz       string
		want     func(error) bool
	}{
		{&fakeInterpreter{ok: false}, "when it rains", "", func(err error) bool { return errors.Is(err, schedule.ErrNotUnderstood) }},
		{&fakeInterpreter{template: "garbage", ok: true}, "x", "", func(err error) bool { return errors.Is(err, schedule.ErrNotUnderstood) }},
		{&fakeInterpreter{template: "*/5 * * * *", ok: true}, "every 5 minutes", "", func(err error) bool { return errors.Is(err, schedule.ErrTooOften) }},
		{&fakeInterpreter{template: "0 9 30 2 *", ok: true}, "every February 30th", "", func(err error) bool { return errors.Is(err, schedule.ErrNotUnderstood) }},
		{&fakeInterpreter{err: errors.New("down")}, "daily", "", func(err error) bool { return err != nil && !errors.As(err, new(schedule.ValidationError)) }},
		{&fakeInterpreter{ok: true}, "  ", "", func(err error) bool { return errors.As(err, new(schedule.ValidationError)) }},
		{&fakeInterpreter{ok: true}, strings.Repeat("x", 201), "", func(err error) bool { return errors.As(err, new(schedule.ValidationError)) }},
		{&fakeInterpreter{ok: true}, "daily", "Mars/Olympus", func(err error) bool { return errors.As(err, new(schedule.ValidationError)) }},
	}
	for i, c := range cases {
		svc := newService(t, c.interp)
		if _, err := svc.Interpret(ctx, c.interval, c.tz); !c.want(err) {
			t.Errorf("case %d: err = %v", i, err)
		}
	}
}

func TestCreateNamesAndSchedules(t *testing.T) {
	interp := &fakeInterpreter{template: "~ 7~22 * * ~", ok: true}
	svc := newService(t, interp)
	sch, err := svc.Create(context.Background(), "u1", schedule.Input{
		Prompt: ptr("find films"), Interval: ptr("once a week"), Timezone: ptr("Europe/Zurich"),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if sch.Name != "Cinema scout" || !sch.Enabled || sch.NextRunAt == nil || !sch.NextRunAt.After(sunday) {
		t.Fatalf("created = %+v", sch)
	}
	if _, err := svc.Create(context.Background(), "u1", schedule.Input{Prompt: ptr("x")}); err == nil {
		t.Fatal("create without interval succeeded")
	}
	if _, err := svc.Create(context.Background(), "u1", schedule.Input{Prompt: ptr(" "), Interval: ptr("daily")}); err == nil {
		t.Fatal("create with an empty prompt succeeded")
	}
	if _, err := svc.Create(context.Background(), "u1", schedule.Input{Prompt: ptr("p"), Interval: ptr("daily"), Name: ptr(strings.Repeat("n", 121))}); err == nil {
		t.Fatal("create with a long name succeeded")
	}
}

// A prompt past the cap is refused with a message that tells the model how to
// shorten it: the model reads it as the tool result and retries.
func TestCreateRefusesALongPromptWithHowToShortenIt(t *testing.T) {
	svc := newService(t, &fakeInterpreter{template: "~ 7~22 * * ~", ok: true})
	long := strings.Repeat("ä", schedule.MaxPromptLength+1)
	_, err := svc.Create(context.Background(), "u1", schedule.Input{Prompt: &long, Interval: ptr("daily")})
	var invalid schedule.ValidationError
	if !errors.As(err, &invalid) || !strings.Contains(invalid.Message, "2001 characters, at most 2000") || !strings.Contains(invalid.Message, "one or two sources") {
		t.Fatalf("err = %v", err)
	}
	fits := strings.Repeat("ä", schedule.MaxPromptLength)
	sch, err := svc.Create(context.Background(), "u1", schedule.Input{Prompt: &fits, Interval: ptr("daily")})
	if err != nil {
		t.Fatalf("a prompt at the cap was refused: %v", err)
	}
	// A prompt stored before the cap was lowered still saves unchanged, as
	// the edit dialog sends it back with every other change.
	sch.Prompt = long
	if _, err := svc.Store.SaveSchedule(context.Background(), sch); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Update(context.Background(), "u1", sch.ID, schedule.Input{Prompt: &long, Name: ptr("Renamed")}); err != nil {
		t.Fatalf("an unchanged long prompt blocked another edit: %v", err)
	}
}

func TestCreateFallsBackToPromptAsName(t *testing.T) {
	svc := newService(t, &fakeInterpreter{template: "0 9 * * *", ok: true})
	svc.Namer = nil
	sch, err := svc.Create(context.Background(), "u1", schedule.Input{Prompt: ptr("look for new sci-fi films in Zurich cinemas every week please"), Interval: ptr("daily")})
	if err != nil {
		t.Fatal(err)
	}
	if sch.Name != "look for new sci-fi films in Zurich cine" {
		t.Fatalf("name = %q", sch.Name)
	}
}

func TestCreateWithDrawnSlotSkipsTheModel(t *testing.T) {
	interp := &fakeInterpreter{template: "~ ~ * * *", ok: true}
	svc := newService(t, interp)
	sch, err := svc.Create(context.Background(), "u1", schedule.Input{
		Prompt: ptr("p"), Interval: ptr("once a week"), Timezone: ptr("Europe/Zurich"),
		Template: "~ 7~22 * * ~", Cron: "37 14 * * 4",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if interp.calls != 0 || sch.Cron != "37 14 * * 4" || sch.CronTemplate != "~ 7~22 * * ~" {
		t.Fatalf("created = %+v after %d model calls", sch, interp.calls)
	}
	want := time.Date(2026, 10, 29, 13, 37, 0, 0, time.UTC)
	if !sch.NextRunAt.Equal(want) {
		t.Fatalf("next run = %v, want %v", sch.NextRunAt, want)
	}
	for _, bad := range []schedule.Input{
		{Prompt: ptr("p"), Interval: ptr("x"), Template: "~ 7~22 * * ~", Cron: "0,30 9 * * *"},
		{Prompt: ptr("p"), Interval: ptr("x"), Template: "nope", Cron: "37 14 * * 4"},
		{Prompt: ptr("p"), Interval: ptr("x"), Template: "~ * * * *", Cron: "~ 14 * * 4"},
		{Prompt: ptr("p"), Interval: ptr("x"), Template: "~ * * * *", Cron: "0 0 31 2 *"},
	} {
		if _, err := svc.Create(context.Background(), "u1", bad); err == nil {
			t.Errorf("drawn slot %q accepted", bad.Cron)
		}
	}
}

func TestDrawnSlotStillChecksTheInterval(t *testing.T) {
	svc := newService(t, &fakeInterpreter{ok: true})
	for _, interval := range []string{" ", strings.Repeat("x", 201)} {
		_, err := svc.Create(context.Background(), "u1", schedule.Input{
			Prompt: ptr("p"), Interval: ptr(interval), Template: "0 9 * * *", Cron: "0 9 * * *",
		})
		if !errors.As(err, new(schedule.ValidationError)) {
			t.Errorf("interval %q err = %v, want a validation error", interval, err)
		}
	}
}

func TestUpdate(t *testing.T) {
	ctx := context.Background()
	interp := &fakeInterpreter{template: "~ 7~22 * * ~", ok: true}
	svc := newService(t, interp)
	sch, _ := svc.Create(ctx, "u1", schedule.Input{Prompt: ptr("p"), Interval: ptr("once a week"), Timezone: ptr("Europe/Zurich")})
	calls := interp.calls

	// Same interval: no new interpretation, same slot.
	got, err := svc.Update(ctx, "u1", sch.ID, schedule.Input{Interval: ptr("once a week"), Name: ptr("Renamed")})
	if err != nil || got.Cron != sch.Cron || got.Name != "Renamed" || interp.calls != calls {
		t.Fatalf("update with the same interval = %+v, %v, %d calls", got, err, interp.calls-calls)
	}

	// Reroll draws again from the stored template.
	svc.Rand = rand.New(rand.NewPCG(99, 98))
	rerolled, err := svc.Update(ctx, "u1", sch.ID, schedule.Input{Reroll: true})
	if err != nil || rerolled.Cron == sch.Cron || interp.calls != calls {
		t.Fatalf("reroll = %+v, %v", rerolled, err)
	}

	// A new interval is interpreted.
	interp.template = "0 9 * * *"
	daily, err := svc.Update(ctx, "u1", sch.ID, schedule.Input{Interval: ptr("daily")})
	if err != nil || daily.Cron != "0 9 * * *" || daily.IntervalText != "daily" {
		t.Fatalf("new interval = %+v, %v", daily, err)
	}

	// Disable and re-enable after an auto-disable.
	_ = svc.Store.Disable(ctx, "u1", sch.ID, schedule.DisabledTimeout, sunday)
	enabled, err := svc.Update(ctx, "u1", sch.ID, schedule.Input{Enabled: ptr(true)})
	if err != nil || !enabled.Enabled || enabled.DisabledReason != "" || enabled.DisabledAt != nil || enabled.NextRunAt == nil {
		t.Fatalf("re-enabled = %+v, %v", enabled, err)
	}
	paused, _ := svc.Update(ctx, "u1", sch.ID, schedule.Input{Enabled: ptr(false)})
	if paused.Enabled || paused.NextRunAt != nil || paused.DisabledReason != "" {
		t.Fatalf("paused = %+v", paused)
	}
	if d := svc.Describe(paused); len(d.NextRuns) != 0 || d.Cron != "0 9 * * *" {
		t.Fatalf("Describe(paused) = %+v", d)
	}
	if _, err := svc.Update(ctx, "u2", sch.ID, schedule.Input{Name: ptr("x")}); !errors.Is(err, schedule.ErrNotFound) {
		t.Fatalf("update by another user err = %v", err)
	}
}

func TestNextRun(t *testing.T) {
	sch := schedule.Schedule{Enabled: true, Cron: "37 14 * * 4", Timezone: "Europe/Zurich"}
	if got := schedule.NextRun(sch, sunday); got == nil || !got.Equal(time.Date(2026, 10, 29, 13, 37, 0, 0, time.UTC)) {
		t.Fatalf("NextRun = %v", got)
	}
	sch.Enabled = false
	if schedule.NextRun(sch, sunday) != nil {
		t.Fatal("a disabled schedule has a next run")
	}
	sch.Enabled, sch.Cron = true, "bad"
	if schedule.NextRun(sch, sunday) != nil {
		t.Fatal("a bad cron has a next run")
	}
}
