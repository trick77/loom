package turn

import (
	"time"

	"github.com/trick77/loom/internal/schedule"
)

// promptTimeLayout is how a prompt states a point in time.
const promptTimeLayout = "Monday, 2006-01-02 15:04 MST"

// zoneLocation resolves a turn's IANA zone the way schedules do (one rule for
// the date line and the schedule slots); one that fails reads UTC.
func zoneLocation(name string) *time.Location {
	if loc, err := schedule.LoadLocation(name); err == nil {
		return loc
	}
	return time.UTC
}

// zone is the turn's location: the one Prepare resolved, else resolved now.
func (t *Run) zone() *time.Location {
	if t.loc != nil {
		return t.loc
	}
	return zoneLocation(t.timezone)
}

// now is the turn's time in its user's zone: the browser's for a chat turn,
// the schedule's for a run. It is the turn's start, so every line of the
// prompt reads the same instant.
func (t *Run) now() time.Time {
	start := t.start
	if start.IsZero() {
		start = t.scheduledNow()
	}
	return start.In(t.zone())
}

// startedNote is when a scheduled run fired: the date line says which day it
// is, this the time of day.
func (t *Run) startedNote() string {
	return "Run started: " + t.now().Format(promptTimeLayout) + "."
}
