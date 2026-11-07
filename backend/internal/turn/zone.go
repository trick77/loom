package turn

import (
	"time"
	// The image ships no zoneinfo: without the embedded copy every zone fails
	// to load and each date line silently reads UTC.
	_ "time/tzdata"
)

// promptTimeLayout is how a prompt states a point in time.
const promptTimeLayout = "Monday, 2006-01-02 15:04 MST"

// zoneLocation resolves a turn's IANA zone. Empty, unknown and "Local" read
// UTC: a prompt never states the server's own time.
func zoneLocation(name string) *time.Location {
	if loc, err := time.LoadLocation(name); err == nil && loc != time.Local {
		return loc
	}
	return time.UTC
}

// zone is the turn's location, resolved once by Prepare.
func (t *Run) zone() *time.Location {
	if t.loc == nil {
		t.loc = zoneLocation(t.timezone)
	}
	return t.loc
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
