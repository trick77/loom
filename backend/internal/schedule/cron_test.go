package schedule

import (
	"errors"
	"math/rand/v2"
	"testing"
	"time"
)

func zurich(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Zurich")
	if err != nil {
		t.Fatalf("load zone: %v", err)
	}
	return loc
}

func mustCron(t *testing.T, expr string) Spec {
	t.Helper()
	spec, err := ParseCron(expr)
	if err != nil {
		t.Fatalf("ParseCron(%q): %v", expr, err)
	}
	return spec
}

func TestParseCronRejectsBadExpressions(t *testing.T) {
	for _, expr := range []string{"", "* * * *", "60 * * * *", "* 24 * * *", "* * 0 * *", "* * * 13 *", "* * * * 8", "5-1 * * * *", "*/0 * * * *", "a * * * *"} {
		if _, err := ParseCron(expr); err == nil {
			t.Errorf("ParseCron(%q) accepted a bad expression", expr)
		}
	}
}

func TestNextWeekly(t *testing.T) {
	loc := zurich(t)
	spec := mustCron(t, "37 14 * * 4")
	// Sunday 25 Oct 2026, 10:00.
	from := time.Date(2026, 10, 25, 10, 0, 0, 0, loc)
	got := spec.NextRuns(from, loc, 3)
	want := []time.Time{
		time.Date(2026, 10, 29, 14, 37, 0, 0, loc),
		time.Date(2026, 11, 5, 14, 37, 0, 0, loc),
		time.Date(2026, 11, 12, 14, 37, 0, 0, loc),
	}
	if len(got) != 3 {
		t.Fatalf("NextRuns = %v", got)
	}
	for i := range want {
		if !got[i].Equal(want[i]) {
			t.Errorf("run %d = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestNextIsStrictlyAfter(t *testing.T) {
	loc := zurich(t)
	spec := mustCron(t, "0 8 * * *")
	at := time.Date(2026, 10, 26, 8, 0, 0, 0, loc)
	if got := spec.Next(at, loc); !got.Equal(at.AddDate(0, 0, 1)) {
		t.Fatalf("Next(at a run) = %v, want the next day's", got)
	}
}

func TestNextDayOfMonthOrWeekday(t *testing.T) {
	loc := time.UTC
	// The 1st or any Monday, as cron combines restricted day fields.
	spec := mustCron(t, "0 9 1 * 1")
	from := time.Date(2026, 10, 27, 0, 0, 0, 0, loc) // a Tuesday
	got := spec.Next(from, loc)
	if want := time.Date(2026, 11, 1, 9, 0, 0, 0, loc); !got.Equal(want) {
		t.Fatalf("Next = %v, want %v", got, want)
	}
	if got = spec.Next(got, loc); !got.Equal(time.Date(2026, 11, 2, 9, 0, 0, 0, loc)) {
		t.Fatalf("Next after the 1st = %v, want Monday 2 Nov", got)
	}
}

func TestNextImpossibleDateIsZero(t *testing.T) {
	spec := mustCron(t, "0 0 31 2 *")
	if got := spec.Next(time.Now(), time.UTC); !got.IsZero() {
		t.Fatalf("Next = %v, want zero for 31 February", got)
	}
}

func TestNextSpringForwardSkipsToAValidInstant(t *testing.T) {
	loc := zurich(t)
	// 29 March 2026: 02:00 jumps to 03:00 in Zurich, so 02:30 does not exist.
	spec := mustCron(t, "30 2 * * *")
	from := time.Date(2026, 3, 28, 12, 0, 0, 0, loc)
	first := spec.Next(from, loc)
	if first.Day() != 29 || first.Hour() != 3 || first.Minute() != 30 {
		t.Fatalf("run on the switch day = %v, want 03:30 on 29 March", first)
	}
	second := spec.Next(first, loc)
	if want := time.Date(2026, 3, 30, 2, 30, 0, 0, loc); !second.Equal(want) {
		t.Fatalf("run after the switch = %v, want %v", second, want)
	}
}

func TestNextFallBackRunsOnce(t *testing.T) {
	loc := zurich(t)
	// 25 October 2026: 03:00 goes back to 02:00, so 02:30 happens twice.
	spec := mustCron(t, "30 2 * * *")
	from := time.Date(2026, 10, 24, 12, 0, 0, 0, loc)
	first := spec.Next(from, loc)
	second := spec.Next(first, loc)
	if first.Day() != 25 {
		t.Fatalf("first run = %v, want 25 October", first)
	}
	if second.Day() != 26 {
		t.Fatalf("second run = %v, want 26 October: 02:30 on the switch day runs once", second)
	}
}

func TestResolveTemplateDrawsFreeFields(t *testing.T) {
	loc := zurich(t)
	from := time.Date(2026, 10, 25, 10, 0, 0, 0, loc)
	rng := rand.New(rand.NewPCG(1, 2))
	spec, err := ResolveTemplate("~ 7~22 * * ~", loc, from, rng)
	if err != nil {
		t.Fatalf("ResolveTemplate: %v", err)
	}
	if len(spec.minutes) != 1 || len(spec.hours) != 1 || len(spec.weekdays) != 1 {
		t.Fatalf("resolved %q is not one weekly slot", spec)
	}
	if h := spec.hours[0]; h < 7 || h > 22 {
		t.Fatalf("hour %d outside 7~22", h)
	}
	// Same seed, same draw.
	again, _ := ResolveTemplate("~ 7~22 * * ~", loc, from, rand.New(rand.NewPCG(1, 2)))
	if again.String() != spec.String() {
		t.Fatalf("seeded draws differ: %q vs %q", again, spec)
	}
}

func TestResolveTemplateTwiceADayKeepsTwelveHours(t *testing.T) {
	loc := zurich(t)
	from := time.Date(2026, 10, 25, 10, 0, 0, 0, loc)
	for seed := uint64(0); seed < 20; seed++ {
		spec, err := ResolveTemplate("~ 7~10,19~22 * * *", loc, from, rand.New(rand.NewPCG(seed, 7)))
		if err != nil {
			t.Fatalf("seed %d: %v", seed, err)
		}
		if spec.hours[1]-spec.hours[0] != 12 {
			t.Fatalf("seed %d: hours %v are not 12 apart", seed, spec.hours)
		}
	}
}

func TestResolveTemplateRejectsTooOften(t *testing.T) {
	loc := time.UTC
	from := time.Date(2026, 10, 25, 10, 0, 0, 0, loc)
	for _, template := range []string{"*/5 * * * *", "~ * * * *", "0 8,14 * * *", "0,30 9 * * *"} {
		_, err := ResolveTemplate(template, loc, from, rand.New(rand.NewPCG(3, 4)))
		if !errors.Is(err, ErrTooOften) {
			t.Errorf("ResolveTemplate(%q) err = %v, want ErrTooOften", template, err)
		}
	}
}

func TestResolveTemplateAcceptsTwelveHoursApart(t *testing.T) {
	if _, err := ResolveTemplate("0 8,20 * * *", time.UTC, time.Now(), rand.New(rand.NewPCG(1, 1))); err != nil {
		t.Fatalf("08:00 and 20:00 rejected: %v", err)
	}
}

func TestTwiceADayAcrossADaylightSavingSwitch(t *testing.T) {
	loc := zurich(t)
	// A week before 28 March 2027, when the night of the switch makes 20:00
	// to 08:00 last eleven hours of elapsed time.
	from := time.Date(2027, 3, 22, 12, 0, 0, 0, loc)
	if _, err := ResolveTemplate("0 8,20 * * *", loc, from, rand.New(rand.NewPCG(1, 1))); err != nil {
		t.Fatalf("08:00 and 20:00 rejected before spring forward: %v", err)
	}
	if _, err := ResolveTemplate("~ 7~10,19~22 * * *", loc, from, rand.New(rand.NewPCG(5, 6))); err != nil {
		t.Fatalf("twice a day rejected before spring forward: %v", err)
	}
	autumn := time.Date(2026, 10, 20, 12, 0, 0, 0, loc)
	if _, err := ResolveTemplate("0 8,20 * * *", loc, autumn, rand.New(rand.NewPCG(1, 1))); err != nil {
		t.Fatalf("08:00 and 20:00 rejected before fall back: %v", err)
	}
}

func TestResolveTemplateNeverRunsAndSteppedDraws(t *testing.T) {
	if _, err := ResolveTemplate("0 9 30 2 *", time.UTC, time.Now(), rand.New(rand.NewPCG(1, 1))); !errors.Is(err, ErrNeverRuns) {
		t.Fatalf("30 February err = %v, want ErrNeverRuns", err)
	}
	if _, err := ResolveTemplate("0 9 1~5/2 * *", time.UTC, time.Now(), rand.New(rand.NewPCG(1, 1))); err == nil {
		t.Fatal("a stepped drawn range was drawn")
	}
	if ValidTemplate("0 9 1~5/2 * *") {
		t.Fatal("ValidTemplate accepted a stepped drawn range")
	}
}

func TestResolveTemplateRejectsBadTemplates(t *testing.T) {
	for _, template := range []string{"~ ~ ~", "~ 30~2 * * *", "0 0 31 2 *", "x * * * *"} {
		if _, err := ResolveTemplate(template, time.UTC, time.Now(), rand.New(rand.NewPCG(1, 1))); err == nil || errors.Is(err, ErrTooOften) {
			t.Errorf("ResolveTemplate(%q) err = %v, want a parse error", template, err)
		}
	}
}

func TestValidTemplate(t *testing.T) {
	for _, template := range []string{"~ 7~22 * * ~", "~ 6~10 * * 1", "~ 7~10,19~22 * * *", "0 9 1 * *", "~ ~ ~ * *"} {
		if !ValidTemplate(template) {
			t.Errorf("ValidTemplate(%q) = false", template)
		}
	}
	for _, template := range []string{"", "every week", "~ 7~22 * *", "~ 25 * * *", "~ 7~x * * *"} {
		if ValidTemplate(template) {
			t.Errorf("ValidTemplate(%q) = true", template)
		}
	}
}

func TestDescribe(t *testing.T) {
	cases := map[string]string{
		"37 14 * * 4":    "Weekly on Thursday at 14:37",
		"52 7 * * 1-5":   "Monday to Friday at 07:52",
		"5 19 * * *":     "Daily at 19:05",
		"12 8,20 * * *":  "Daily at 08:12 and 20:12",
		"14 10 4,19 * *": "Monthly on the 4th and 19th at 10:14",
		"0 9 * * 1,3":    "On Monday, Wednesday at 09:00",
		"0 9 1 1,7 *":    "Monthly on the 1st in Jan, Jul at 09:00",
		"0 9 1 * 1":      "On cron 0 9 1 * 1",
		"*/30 9 * * *":   "On cron */30 9 * * *",
		"0 9 2,3,11 * *": "Monthly on the 2nd and 3rd and 11th at 09:00",
	}
	for expr, want := range cases {
		if got := mustCron(t, expr).Describe(); got != want {
			t.Errorf("Describe(%q) = %q, want %q", expr, got, want)
		}
	}
}

func TestSundayIsZeroOrSeven(t *testing.T) {
	a, b := mustCron(t, "0 9 * * 0"), mustCron(t, "0 9 * * 7")
	from := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	if !a.Next(from, time.UTC).Equal(b.Next(from, time.UTC)) {
		t.Fatal("day of week 7 is not Sunday")
	}
}
