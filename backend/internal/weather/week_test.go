package weather

import (
	"testing"
	"time"
)

func dayOf(date string, high float64, rain float64, chance int, sunH, wind, gusts float64, dir int) Day {
	return Day{Date: date, High: high, Low: high - 8, Code: 3, PrecipitationSum: &rain, PrecipitationProbability: &chance,
		SunshineHours: &sunH, WindSpeedMax: &wind, WindGustsMax: &gusts, WindDirection: &dir}
}

func TestWeekRanksTheDays(t *testing.T) {
	f := Forecast{Daily: []Day{
		dayOf("2026-11-08", 14, 0, 0, 5, 10, 20, 250),    // yesterday: left out
		dayOf("2026-11-09", 14, 6, 90, 0, 10, 20, 250),   // wet
		dayOf("2026-11-10", 15, 0.2, 10, 6, 18, 38, 270), // dry, windy for a ride
		dayOf("2026-11-11", 13, 0, 5, 7, 8, 18, 90),      // dry, calm, sunny: best
		dayOf("2026-11-12", 16, 1.2, 50, 3, 10, 20, 200), // showers
		dayOf("2026-11-13", 17, 0, 0, 2, 45, 60, 300),    // dry but stormy
	}}
	week := Week(WeekInput{Forecast: f, Now: at(0, 9, 0), Loc: zurich, Latitude: 47.05, Longitude: 8.31, Activity: Ride})
	if len(week) != 5 || week[0].Date != "2026-11-09" {
		t.Fatalf("week = %+v", week)
	}
	by := map[string]DayOutlook{}
	for _, d := range week {
		by[d.Date] = d
	}
	for date, want := range map[string]struct {
		rain, wind string
		rank       int
	}{
		"2026-11-09": {"wet", "", 4},
		"2026-11-10": {"dry", WindWindy, 2},
		"2026-11-11": {"dry", "", 1},
		"2026-11-12": {"showers", "", 3},
		// Dry, but strong gusts sink it below the rain.
		"2026-11-13": {"dry", WindStrong, 5},
	} {
		d := by[date]
		if d.Rain != want.rain || d.Wind != want.wind || d.Rank != want.rank {
			t.Errorf("%s = %+v, want %+v", date, d, want)
		}
	}
	if d := by["2026-11-11"]; d.Sunrise == "" || d.Sunset == "" || d.WindFrom != "E" {
		t.Fatalf("best day = %+v", d)
	}
	// A runner shrugs off the ride's windy day.
	run := Week(WeekInput{Forecast: f, Now: at(0, 9, 0), Loc: zurich, Latitude: 47.05, Longitude: 8.31, Activity: Run})
	for _, d := range run {
		if d.Date == "2026-11-10" && d.Wind != "" {
			t.Fatalf("run's windy day = %+v", d)
		}
	}
}

// A day whose high is below the cold limit sinks below every warm one, rain
// or not; a sunny one (sun 3/4 of daylight) only 3 °C lower. Strong wind
// stays worse. Every day reads it off its high, a planned one too: its
// window's stricter feels-like would sink it below a later day as warm.
func TestWeekColdDays(t *testing.T) {
	f := Forecast{Daily: []Day{
		dayOf("2026-11-09", 12, 0, 0, 0, 5, 10, 90),   // planned, its window cold: warm by the high
		dayOf("2026-11-10", 11, 6, 90, 0, 5, 10, 90),  // wet but warm
		dayOf("2026-11-11", 8, 0, 0, 2, 5, 10, 90),    // dry, cold
		dayOf("2026-11-12", 8, 0, 0, 8, 5, 10, 90),    // dry, 8 h of ~9.5: sunny, warm enough
		dayOf("2026-11-13", 14, 0, 0, 2, 45, 60, 300), // warm but stormy
		dayOf("2026-11-14", 6, 0, 0, 8.5, 5, 10, 90),  // sunny but too cold even so
	}}
	planned := []Outlook{{Date: "2026-11-09", Windows: []Window{{From: "10:00", To: "12:00", Dry: true, Cold: true}}}}
	week := Week(WeekInput{Forecast: f, Now: at(0, 9, 0), Loc: zurich, Latitude: 47.05, Longitude: 8.31,
		Activity: Ride, Minutes: 120, Planned: planned})
	want := map[string]struct {
		cold bool
		rank int
	}{
		"2026-11-12": {false, 1},
		"2026-11-09": {false, 2},
		"2026-11-10": {false, 3},
		// The cold ones among themselves: the warmer first, sun or not.
		"2026-11-11": {true, 4},
		"2026-11-14": {true, 5},
		"2026-11-13": {false, 6},
	}
	for _, d := range week {
		if w := want[d.Date]; d.Cold != w.cold || d.Rank != w.rank {
			t.Errorf("%s = cold %v rank %d, want %+v", d.Date, d.Cold, d.Rank, w)
		}
	}
	// A runner's limit is 5: the same 8 °C day is warm.
	run := Week(WeekInput{Forecast: f, Now: at(0, 9, 0), Loc: zurich, Latitude: 47.05, Longitude: 8.31, Activity: Run})
	for _, d := range run {
		if d.Date == "2026-11-11" && d.Cold {
			t.Fatalf("run's 8 °C day = %+v", d)
		}
	}
}

// After dark today has no time left: it ranks last, whatever its weather.
func TestWeekTodayOver(t *testing.T) {
	f := Forecast{Daily: []Day{
		dayOf("2026-11-09", 12, 0, 0, 8, 5, 10, 90),
		dayOf("2026-11-10", 8, 1.2, 50, 2, 10, 20, 90),
	}}
	week := Week(WeekInput{Forecast: f, Now: at(0, 20, 0), Loc: zurich, Latitude: 47.05, Longitude: 8.31,
		Activity: Ride, Minutes: 120})
	if week[0].None != NoneOver || week[0].Rank != 2 || week[1].Rank != 1 || week[1].None != "" {
		t.Fatalf("week = %+v", week)
	}
	morning := Week(WeekInput{Forecast: f, Now: at(0, 9, 0), Loc: zurich, Latitude: 47.05, Longitude: 8.31,
		Activity: Ride, Minutes: 120})
	if morning[0].None != "" || morning[0].Rank != 1 {
		t.Fatalf("week in the morning = %+v", morning)
	}
}

// A planned day takes its time left and wind from its windows: gusts at
// 03:00 do not sink a calm day, and a day planned over is over.
func TestWeekTakesThePlannedDays(t *testing.T) {
	f := Forecast{Daily: []Day{
		dayOf("2026-11-09", 12, 0, 0, 8, 10, 60, 90), // the daily max: a night gust
		dayOf("2026-11-10", 8, 0, 0, 2, 10, 20, 90),
	}}
	planned := []Outlook{
		{Date: "2026-11-09", Windows: []Window{{From: "10:00", To: "12:00", Dry: true}}},
		{Date: "2026-11-10", None: NoneOver},
	}
	week := Week(WeekInput{Forecast: f, Now: at(0, 9, 0), Loc: zurich, Latitude: 47.05, Longitude: 8.31,
		Activity: Ride, Minutes: 120, Planned: planned})
	if week[0].Wind != "" || week[0].None != "" || week[0].Rank != 1 || week[1].None != NoneOver || week[1].Rank != 2 {
		t.Fatalf("week = %+v", week)
	}
}

// Missing values stay unknown and never fail: the code stands in for the
// rain.
func TestWeekWithoutValues(t *testing.T) {
	f := Forecast{Daily: []Day{{Date: "2026-11-09", High: 9, Low: 2, Code: 61}}}
	week := Week(WeekInput{Forecast: f, Now: at(0, 9, 0), Loc: zurich, Activity: Run})
	if len(week) != 1 || week[0].Rain != "wet" || week[0].Rank != 1 {
		t.Fatalf("week = %+v", week)
	}
	if got := Week(WeekInput{Now: time.Now(), Loc: zurich}); len(got) != 0 {
		t.Fatalf("no days = %+v", got)
	}
}
