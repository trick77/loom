// Package schedule runs prompts on their own: a per-user schedule with a cron
// the user described in words, its runs, the memory those runs keep, and the
// notifications they raise.
package schedule

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // the container image has no zoneinfo; a schedule's timezone must load anyway
)

// MinRunGap is the densest a schedule may run: at most twice a day.
const MinRunGap = 12 * time.Hour

// gapHorizon is how far ahead the floor check looks. Eight days covers every
// weekly pattern once plus the wrap into the next week.
const gapHorizon = 8 * 24 * time.Hour

// resolveAttempts bounds the draws for a template whose free fields can land
// too close together ("twice a day" drawing 10:xx and 19:xx).
const resolveAttempts = 50

// ErrTooOften is a schedule that would run more often than MinRunGap allows.
var ErrTooOften = errors.New("schedules run at most every 12 hours")

// field is one cron field's range and its set of allowed values.
type field struct {
	name     string
	min, max int
}

var fields = [5]field{
	{"minute", 0, 59},
	{"hour", 0, 23},
	{"day of month", 1, 31},
	{"month", 1, 12},
	{"day of week", 0, 6},
}

// bits is the set of values a field allows.
type bits uint64

func (b bits) has(v int) bool { return b&(1<<uint(v)) != 0 }

// Spec is a parsed five-field cron: minute, hour, day of month, month, day of
// week (0 = Sunday). Day of month and day of week combine as cron does: when
// both are restricted, either matching is enough.
type Spec struct {
	expr                  string
	minute, hour, dom     bits
	month, dow            bits
	domStar, dowStar      bool
	minutes, hours        []int
	daysOfMonth, weekdays []int
}

// String is the expression the spec was parsed from.
func (s Spec) String() string { return s.expr }

// ParseCron parses a concrete five-field cron expression: "*", numbers,
// ranges "a-b", steps "*/n" and "a-b/n", and comma lists of those.
func ParseCron(expr string) (Spec, error) {
	parts := strings.Fields(expr)
	if len(parts) != len(fields) {
		return Spec{}, fmt.Errorf("cron needs 5 fields, got %d", len(parts))
	}
	spec := Spec{expr: strings.Join(parts, " ")}
	sets := make([]bits, len(fields))
	for i, part := range parts {
		set, err := parseField(part, fields[i])
		if err != nil {
			return Spec{}, err
		}
		sets[i] = set
	}
	spec.minute, spec.hour, spec.dom, spec.month, spec.dow = sets[0], sets[1], sets[2], sets[3], sets[4]
	spec.domStar = parts[2] == "*"
	spec.dowStar = parts[4] == "*"
	spec.minutes = members(spec.minute, fields[0])
	spec.hours = members(spec.hour, fields[1])
	spec.daysOfMonth = members(spec.dom, fields[2])
	spec.weekdays = members(spec.dow, fields[4])
	return spec, nil
}

func parseField(part string, f field) (bits, error) {
	var set bits
	for _, item := range strings.Split(part, ",") {
		step := 1
		if base, s, ok := strings.Cut(item, "/"); ok {
			n, err := strconv.Atoi(s)
			if err != nil || n < 1 {
				return 0, fmt.Errorf("%s: bad step %q", f.name, item)
			}
			item, step = base, n
		}
		lo, hi := f.min, f.max
		switch {
		case item == "*":
		case strings.Contains(item, "-"):
			a, b, _ := strings.Cut(item, "-")
			var err error
			if lo, err = fieldValue(a, f); err != nil {
				return 0, err
			}
			if hi, err = fieldValue(b, f); err != nil {
				return 0, err
			}
			if lo > hi {
				return 0, fmt.Errorf("%s: empty range %q", f.name, item)
			}
		default:
			v, err := fieldValue(item, f)
			if err != nil {
				return 0, err
			}
			lo, hi = v, v
		}
		for v := lo; v <= hi; v += step {
			set |= 1 << uint(v)
		}
	}
	return set, nil
}

func fieldValue(s string, f field) (int, error) {
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("%s: %q is not a number", f.name, s)
	}
	// Sunday is 0 or 7, as in most crons.
	if f.name == "day of week" && v == 7 {
		v = 0
	}
	if v < f.min || v > f.max {
		return 0, fmt.Errorf("%s: %d is outside %d-%d", f.name, v, f.min, f.max)
	}
	return v, nil
}

func members(b bits, f field) []int {
	var out []int
	for v := f.min; v <= f.max; v++ {
		if b.has(v) {
			out = append(out, v)
		}
	}
	return out
}

func (s Spec) dayMatches(wall time.Time) bool {
	dom := s.dom.has(wall.Day())
	dow := s.dow.has(int(wall.Weekday()))
	switch {
	case s.domStar && s.dowStar:
		return true
	case s.domStar:
		return dow
	case s.dowStar:
		return dom
	default:
		return dom || dow
	}
}

// Next returns the first run strictly after after, on the wall clock of loc;
// zero when none falls within five years (a 31st of February). It walks the
// wall clock in a zone without daylight saving, so a run at a time the switch
// skips lands at the instant time.Date gives it, and one at a time the switch
// repeats runs once.
func (s Spec) Next(after time.Time, loc *time.Location) time.Time {
	local := after.In(loc)
	wall := time.Date(local.Year(), local.Month(), local.Day(), local.Hour(), local.Minute(), 0, 0, time.UTC).Add(time.Minute)
	limit := wall.AddDate(5, 0, 0)
	for wall.Before(limit) {
		switch {
		case !s.month.has(int(wall.Month())):
			wall = time.Date(wall.Year(), wall.Month()+1, 1, 0, 0, 0, 0, time.UTC)
		case !s.dayMatches(wall):
			wall = time.Date(wall.Year(), wall.Month(), wall.Day()+1, 0, 0, 0, 0, time.UTC)
		case !s.hour.has(wall.Hour()):
			wall = wall.Truncate(time.Hour).Add(time.Hour)
		case !s.minute.has(wall.Minute()):
			wall = wall.Add(time.Minute)
		default:
			at := time.Date(wall.Year(), wall.Month(), wall.Day(), wall.Hour(), wall.Minute(), 0, 0, loc)
			if at.After(after) {
				return at
			}
			wall = wall.Add(time.Minute)
		}
	}
	return time.Time{}
}

// NextRuns returns the next n runs after after.
func (s Spec) NextRuns(after time.Time, loc *time.Location, n int) []time.Time {
	runs := make([]time.Time, 0, n)
	for len(runs) < n {
		next := s.Next(after, loc)
		if next.IsZero() {
			break
		}
		runs = append(runs, next)
		after = next
	}
	return runs
}

// minGap is the shortest time between two consecutive runs within the
// horizon after from; MinRunGap's double when there are fewer than two.
func (s Spec) minGap(from time.Time, loc *time.Location) time.Duration {
	gap := 2 * MinRunGap
	prev := s.Next(from, loc)
	if prev.IsZero() {
		return gap
	}
	end := prev.Add(gapHorizon)
	for {
		next := s.Next(prev, loc)
		if next.IsZero() || next.After(end) {
			return gap
		}
		gap = min(gap, next.Sub(prev))
		prev = next
	}
}

// ResolveTemplate draws a concrete cron from a template: "~" in a field is a
// random value in the field's whole range, "a~b" one in that range; every
// other token is kept. It redraws while the result runs closer together than
// MinRunGap and returns ErrTooOften when no draw satisfies it.
func ResolveTemplate(template string, loc *time.Location, from time.Time, rng *rand.Rand) (Spec, error) {
	parts := strings.Fields(template)
	if len(parts) != len(fields) {
		return Spec{}, fmt.Errorf("cron template needs 5 fields, got %d", len(parts))
	}
	for attempt := 0; attempt < resolveAttempts; attempt++ {
		drawn := make([]string, len(parts))
		for i, part := range parts {
			value, err := drawField(part, fields[i], rng)
			if err != nil {
				return Spec{}, err
			}
			drawn[i] = value
		}
		spec, err := ParseCron(strings.Join(drawn, " "))
		if err != nil {
			return Spec{}, err
		}
		if s := spec.Next(from, loc); s.IsZero() {
			return Spec{}, errors.New("the schedule never runs")
		}
		if spec.minGap(from, loc) >= MinRunGap {
			return spec, nil
		}
		if !strings.Contains(template, "~") {
			break
		}
	}
	return Spec{}, ErrTooOften
}

func drawField(part string, f field, rng *rand.Rand) (string, error) {
	items := strings.Split(part, ",")
	for i, item := range items {
		if !strings.Contains(item, "~") {
			continue
		}
		lo, hi := f.min, f.max
		if item != "~" {
			a, b, _ := strings.Cut(item, "~")
			var err error
			if lo, err = fieldValue(a, f); err != nil {
				return "", err
			}
			if hi, err = fieldValue(b, f); err != nil {
				return "", err
			}
			if lo > hi {
				return "", fmt.Errorf("%s: empty range %q", f.name, item)
			}
		}
		items[i] = strconv.Itoa(lo + rng.IntN(hi-lo+1))
	}
	return strings.Join(items, ","), nil
}

// ValidTemplate reports whether template is five fields of known tokens, so a
// gate reply that is not a template fails before anything is drawn.
func ValidTemplate(template string) bool {
	parts := strings.Fields(template)
	if len(parts) != len(fields) {
		return false
	}
	for i, part := range parts {
		items := strings.Split(part, ",")
		for j, item := range items {
			if item == "~" {
				items[j] = "*"
			} else {
				items[j] = strings.Replace(item, "~", "-", 1)
			}
		}
		if _, err := parseField(strings.Join(items, ","), fields[i]); err != nil {
			return false
		}
	}
	return true
}

var weekdayNames = [7]string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}

// Describe renders the spec in plain English for the model and as the UI's
// fallback: "Weekly on Thursday at 14:37", "Daily at 07:52".
func (s Spec) Describe() string {
	times := s.describeTimes()
	if times == "" {
		return "On cron " + s.expr
	}
	monthStar := len(members(s.month, fields[3])) == 12
	var days string
	switch {
	case s.domStar && s.dowStar:
		days = "Daily"
	case s.domStar && isWeekdays(s.weekdays):
		days = "Monday to Friday"
	case s.domStar && len(s.weekdays) == 1:
		days = "Weekly on " + weekdayNames[s.weekdays[0]]
	case s.domStar:
		names := make([]string, len(s.weekdays))
		for i, d := range s.weekdays {
			names[i] = weekdayNames[d]
		}
		days = "On " + strings.Join(names, ", ")
	case s.dowStar:
		ords := make([]string, len(s.daysOfMonth))
		for i, d := range s.daysOfMonth {
			ords[i] = ordinal(d)
		}
		days = "Monthly on the " + strings.Join(ords, " and ")
	default:
		return "On cron " + s.expr
	}
	if !monthStar {
		var months []string
		for _, m := range members(s.month, fields[3]) {
			months = append(months, time.Month(m).String()[:3])
		}
		days += " in " + strings.Join(months, ", ")
	}
	return days + " at " + times
}

func (s Spec) describeTimes() string {
	if len(s.minutes) != 1 || len(s.hours) == 0 || len(s.hours) > 3 {
		return ""
	}
	out := make([]string, len(s.hours))
	for i, h := range s.hours {
		out[i] = fmt.Sprintf("%02d:%02d", h, s.minutes[0])
	}
	return strings.Join(out, " and ")
}

func isWeekdays(days []int) bool {
	return len(days) == 5 && days[0] == 1 && days[4] == 5
}

func ordinal(n int) string {
	suffix := "th"
	switch {
	case n%100 >= 11 && n%100 <= 13:
	case n%10 == 1:
		suffix = "st"
	case n%10 == 2:
		suffix = "nd"
	case n%10 == 3:
		suffix = "rd"
	}
	return strconv.Itoa(n) + suffix
}
