package weather

import (
	"testing"
	"time"

	"github.com/trick77/loom/internal/sun"
)

var zurich = mustZone("Europe/Zurich")

func mustZone(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

// at is 2026-11-09 (or a day later) hh:mm in Zurich.
func at(day, hh, mm int) time.Time {
	return time.Date(2026, 11, 9+day, hh, mm, 0, 0, zurich)
}

// sunlit is a day with the sun up 07:30-17:00.
func sunlit(day int) sun.Day {
	return sun.Day{Rise: at(day, 7, 30), Set: at(day, 17, 0)}
}

// hourFn shapes one hour of a test day.
type hourFn func(h int, hour *Hour)

// day is 48 hours from 2026-11-09 00:00: dry and overcast, 4 °C at night
// rising to 10 °C at 14:00, unless shaped.
func day(shape ...hourFn) Forecast {
	var f Forecast
	for i := range 48 {
		h := i % 24
		temp := 4 + 6*max(0, 1-float64(abs(h-14))/7)
		zero, none := 0, 0.0
		hour := Hour{Time: at(0, 0, 0).Add(time.Duration(i) * time.Hour).Format("2006-01-02T15:04"),
			Temperature: temp, Code: 3, PrecipitationProbability: &zero, Precipitation: &none, SunshineMinutes: &zero}
		for _, fn := range shape {
			fn(i, &hour)
		}
		f.Hourly = append(f.Hourly, hour)
	}
	return f
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func rainFrom(from, to int) hourFn {
	return func(i int, h *Hour) {
		if i >= from && i < to {
			chance, mm := 90, 1.5
			h.Code, h.PrecipitationProbability, h.Precipitation = 61, &chance, &mm
		}
	}
}

func sunFrom(from, to, minutes int) hourFn {
	return func(i int, h *Hour) {
		if i >= from && i < to {
			m := minutes
			h.SunshineMinutes, h.Code = &m, 1
		}
	}
}

func gustsFrom(from, to int, kmh float64) hourFn {
	return func(i int, h *Hour) {
		if i >= from && i < to {
			g := kmh
			h.WindGusts = &g
		}
	}
}

func plan(t *testing.T, in WindowsInput) Outlook {
	t.Helper()
	if in.Loc == nil {
		in.Loc = zurich
	}
	if in.Sun.Rise.IsZero() && in.Sun.Polar == sun.NotPolar {
		in.Sun = sunlit(0)
	}
	if in.Day.IsZero() {
		in.Day = in.Now
	}
	out := Windows(in)
	for _, w := range out.Windows {
		if w.To > out.Sunset && out.Sunset != "" {
			t.Fatalf("window %s-%s ends after sunset %s", w.From, w.To, out.Sunset)
		}
		if w.From < out.Sunrise {
			t.Fatalf("window %s-%s starts before sunrise %s", w.From, w.To, out.Sunrise)
		}
	}
	return out
}

func spans(o Outlook) []string {
	var out []string
	for _, w := range o.Windows {
		out = append(out, w.From+"-"+w.To)
	}
	return out
}

// A dry, grey day always has a window: the mildest hours lead.
func TestDryGreyDayPicksTheMildestHours(t *testing.T) {
	o := plan(t, WindowsInput{Forecast: day(), Now: at(0, 8, 0), Minutes: 60, Activity: Run})
	if !o.Dry || o.SunExpected || o.None != "" {
		t.Fatalf("outlook = %+v", o)
	}
	if got := spans(o); len(got) != 3 || got[0] != "14:00-15:00" {
		t.Fatalf("windows = %v", got)
	}
	for _, w := range o.Windows {
		if !w.Dry {
			t.Fatalf("a dry day's window is wet: %+v", w)
		}
	}
	if o.Now != "08:00" || o.Sunrise != "07:30" || o.Sunset != "17:00" || o.Date != "2026-11-09" {
		t.Fatalf("outlook = %+v", o)
	}
}

// Clouds with sun expected: the sunny hours win over milder grey ones.
func TestSunWinsOverMildness(t *testing.T) {
	o := plan(t, WindowsInput{Forecast: day(sunFrom(10, 12, 50)), Now: at(0, 8, 0), Minutes: 60, Activity: Run})
	if !o.SunExpected {
		t.Fatalf("outlook = %+v", o)
	}
	if got := spans(o); got[0] != "11:00-12:00" && got[0] != "10:00-11:00" {
		t.Fatalf("windows = %v", got)
	}
	if o.Windows[0].SunMinutes != 50 {
		t.Fatalf("best = %+v", o.Windows[0])
	}
}

// Rain until 15:00: the ride takes the two dry hours before sunset, to the
// minute.
func TestRainyMorningLeavesTheEvening(t *testing.T) {
	f := day(rainFrom(0, 15))
	o := plan(t, WindowsInput{Forecast: f, Now: at(0, 8, 0), Minutes: 120, Activity: Ride})
	if o.Dry {
		t.Fatalf("a rainy day read dry: %+v", o)
	}
	if got := spans(o); len(got) != 1 || got[0] != "15:00-17:00" || !o.Windows[0].Dry {
		t.Fatalf("windows = %v", got)
	}
	run := plan(t, WindowsInput{Forecast: f, Now: at(0, 8, 0), Minutes: 60, Activity: Run})
	if got := spans(run); len(got) != 2 || got[0] != "15:00-16:00" || got[1] != "16:00-17:00" {
		t.Fatalf("run windows = %v", got)
	}
}

// Rain all day: the driest windows still come, marked wet.
func TestRainAllDayOffersTheDriest(t *testing.T) {
	f := day(rainFrom(0, 48), func(i int, h *Hour) {
		if i == 12 {
			chance, mm := 60, 0.3
			h.PrecipitationProbability, h.Precipitation = &chance, &mm
		}
	})
	o := plan(t, WindowsInput{Forecast: f, Now: at(0, 8, 0), Minutes: 60, Activity: Run})
	if o.Dry || len(o.Windows) == 0 || len(o.Windows) > 2 || o.Windows[0].Dry || o.Windows[0].From != "12:00" {
		t.Fatalf("outlook = %+v", o)
	}
}

// Late in the day the last window ends at sunset; past the last start there
// is none today.
func TestSunsetBoundsTheDay(t *testing.T) {
	o := plan(t, WindowsInput{Forecast: day(), Now: at(0, 15, 20), Minutes: 60, Activity: Run})
	if got := spans(o); len(got) != 1 || got[0] != "15:30-16:30" && got[0] != "16:00-17:00" {
		t.Fatalf("windows = %v", got)
	}
	late := plan(t, WindowsInput{Forecast: day(), Now: at(0, 16, 10), Minutes: 60, Activity: Run})
	if len(late.Windows) != 0 || late.None != NoneOver {
		t.Fatalf("after the last start = %+v", late)
	}
	ride := plan(t, WindowsInput{Forecast: day(), Now: at(0, 15, 30), Minutes: 120, Activity: Ride})
	if len(ride.Windows) != 0 || ride.None != NoneOver {
		t.Fatalf("two hours at 15:30 = %+v", ride)
	}
}

// Tomorrow starts at sunrise, whatever the hour now.
func TestTomorrowStartsAtSunrise(t *testing.T) {
	o := plan(t, WindowsInput{Forecast: day(sunFrom(31, 33, 55)), Now: at(0, 20, 0), Day: at(1, 12, 0),
		Sun: sunlit(1), Minutes: 60, Activity: Run})
	if o.Date != "2026-11-10" || o.Now != "" || len(o.Windows) == 0 || o.Windows[0].SunMinutes != 55 {
		t.Fatalf("tomorrow = %+v", o)
	}
	if got := o.Windows[0].From; got != "07:30" && got != "08:00" {
		t.Fatalf("tomorrow's best = %v", spans(o))
	}
}

func TestPolarDays(t *testing.T) {
	night := plan(t, WindowsInput{Forecast: day(), Now: at(0, 8, 0), Sun: sun.Day{Polar: sun.PolarNight}, Minutes: 60, Activity: Run})
	if len(night.Windows) != 0 || night.None != NoneDark || night.Polar != string(sun.PolarNight) {
		t.Fatalf("polar night = %+v", night)
	}
	lit := plan(t, WindowsInput{Forecast: day(sunFrom(22, 23, 50)), Now: at(0, 8, 0), Sun: sun.Day{Polar: sun.PolarDay}, Minutes: 60, Activity: Run})
	if got := spans(lit); len(got) == 0 || got[0] != "22:00-23:00" {
		t.Fatalf("midnight sun = %v", got)
	}
}

// A day shorter than the activity has no window.
func TestShortDay(t *testing.T) {
	o := plan(t, WindowsInput{Forecast: day(), Now: at(0, 6, 0), Sun: sun.Day{Rise: at(0, 11, 0), Set: at(0, 12, 30)},
		Minutes: 120, Activity: Ride})
	if len(o.Windows) != 0 || o.None != NoneShort {
		t.Fatalf("short day = %+v", o)
	}
	// Two minutes over two hours but no start on the grid fits: short, not
	// over, whatever the day.
	off := plan(t, WindowsInput{Forecast: day(), Now: at(0, 6, 0), Day: at(1, 12, 0),
		Sun: sun.Day{Rise: at(1, 8, 10), Set: at(1, 10, 12)}, Minutes: 120, Activity: Ride})
	if len(off.Windows) != 0 || off.None != NoneShort {
		t.Fatalf("off the grid = %+v", off)
	}
}

// Without sunshine minutes (another server) the weather code tells the sun.
func TestCodeStandsInForSunshine(t *testing.T) {
	f := day(func(i int, h *Hour) {
		h.SunshineMinutes = nil
		if i == 11 {
			h.Code = 0
		}
	})
	o := plan(t, WindowsInput{Forecast: f, Now: at(0, 8, 0), Minutes: 60, Activity: Run})
	if !o.SunExpected || o.Windows[0].From != "11:00" {
		t.Fatalf("outlook = %+v", o)
	}
}

// Gusts weigh on a ride more than on a run.
func TestGustsSteerTheRide(t *testing.T) {
	f := day(sunFrom(9, 17, 50), gustsFrom(12, 15, 55))
	ride := plan(t, WindowsInput{Forecast: f, Now: at(0, 8, 0), Minutes: 120, Activity: Ride})
	for _, w := range ride.Windows[:1] {
		if w.From >= "10:00" && w.From < "15:00" {
			t.Fatalf("the ride sits in the gusts: %v", spans(ride))
		}
	}
	if ride.Windows[0].GustsMax != nil && *ride.Windows[0].GustsMax > 50 {
		t.Fatalf("best ride = %+v", ride.Windows[0])
	}
}

func windFrom(from, to int, kmh float64, direction int) hourFn {
	return func(i int, h *Hour) {
		if i >= from && i < to {
			w, d := kmh, direction
			h.WindSpeed, h.WindDirection = &w, &d
		}
	}
}

// Wind is worth a word from 15 km/h on a ride, 20 on a run, or from gusts of
// 30 and 40; a warning from 39 or gusts of 45 and 55. The window says so,
// with where it blows from.
func TestWindIsNoted(t *testing.T) {
	for _, tc := range []struct {
		name     string
		activity Activity
		shape    hourFn
		want     string
	}{
		{"ride at 16", Ride, windFrom(0, 48, 16, 250), WindWindy},
		{"ride at 14", Ride, windFrom(0, 48, 14, 250), ""},
		{"run at 16", Run, windFrom(0, 48, 16, 250), ""},
		{"run at 21", Run, windFrom(0, 48, 21, 250), WindWindy},
		{"ride at 40", Ride, windFrom(0, 48, 40, 250), WindStrong},
		{"run at 40", Run, windFrom(0, 48, 40, 250), WindStrong},
		{"ride in gusts of 32", Ride, gustsFrom(0, 48, 32), WindWindy},
		{"ride in gusts of 46", Ride, gustsFrom(0, 48, 46), WindStrong},
		{"run in gusts of 32", Run, gustsFrom(0, 48, 32), ""},
		{"run in gusts of 42", Run, gustsFrom(0, 48, 42), WindWindy},
		{"run in gusts of 56", Run, gustsFrom(0, 48, 56), WindStrong},
	} {
		o := plan(t, WindowsInput{Forecast: day(tc.shape), Now: at(0, 8, 0), Minutes: tc.activity.DefaultMinutes(), Activity: tc.activity})
		w := o.Windows[0]
		if w.Wind != tc.want {
			t.Errorf("%s: wind %q, want %q (%+v)", tc.name, w.Wind, tc.want, w)
		}
	}
	o := plan(t, WindowsInput{Forecast: day(windFrom(0, 48, 18, 250)), Now: at(0, 8, 0), Minutes: 120, Activity: Ride})
	if w := o.Windows[0]; w.WindMax == nil || *w.WindMax != 18 || w.WindFrom != "W" {
		t.Fatalf("wind = %+v", w)
	}
}

// A ride keeps out of the wind when the rest is even; strong wind sinks a
// window below a colder calm one.
func TestWindSteersTheRide(t *testing.T) {
	f := day(sunFrom(9, 17, 50), windFrom(11, 15, 28, 90))
	ride := plan(t, WindowsInput{Forecast: f, Now: at(0, 8, 0), Minutes: 120, Activity: Ride})
	if w := ride.Windows[0]; w.Wind != "" || w.From >= "09:15" && w.From < "15:00" {
		t.Fatalf("the ride sits in the wind: %v", spans(ride))
	}
	storm := day(windFrom(12, 17, 45, 270))
	run := plan(t, WindowsInput{Forecast: storm, Now: at(0, 8, 0), Minutes: 60, Activity: Run})
	if w := run.Windows[0]; w.Wind != "" || w.From >= "11:15" {
		t.Fatalf("the run sits in the storm: %v", spans(run))
	}
	// A sunny afternoon in strong gusts loses to a grey calm morning, and one
	// strong hour sinks a window below one windy throughout.
	sunnyStorm := day(sunFrom(12, 17, 55), gustsFrom(12, 17, 50))
	ride = plan(t, WindowsInput{Forecast: sunnyStorm, Now: at(0, 8, 0), Minutes: 120, Activity: Ride})
	if w := ride.Windows[0]; w.Wind == WindStrong {
		t.Fatalf("the ride sits in strong gusts: %+v", ride.Windows)
	}
	// On a wet day too: less rain does not buy strong gusts.
	wet := day(rainFrom(0, 48), func(i int, h *Hour) {
		if i >= 12 && i < 14 {
			mm := 0.4
			h.Precipitation = &mm
		}
	}, gustsFrom(12, 14, 50))
	ride = plan(t, WindowsInput{Forecast: wet, Now: at(0, 8, 0), Minutes: 120, Activity: Ride})
	if w := ride.Windows[0]; w.Wind == WindStrong || w.Dry {
		t.Fatalf("a wet ride sits in strong gusts: %+v", ride.Windows)
	}
	mixed := day(windFrom(8, 10, 35, 270), gustsFrom(8, 10, 44), gustsFrom(14, 15, 46))
	ride = plan(t, WindowsInput{Forecast: mixed, Now: at(0, 8, 0), Minutes: 120, Activity: Ride})
	for _, w := range ride.Windows[:1] {
		if w.Wind == WindStrong {
			t.Fatalf("a strong window leads: %+v", ride.Windows)
		}
	}
}

func TestCompass(t *testing.T) {
	for deg, want := range map[int]string{0: "N", 22: "N", 23: "NE", 90: "E", 180: "S", 250: "W", 300: "NW", 338: "N", 360: "N"} {
		if got := compass(deg); got != want {
			t.Errorf("compass(%d) = %s, want %s", deg, got, want)
		}
	}
}

// In the heat the sun is no draw: a hazy cool morning beats a sunny
// sweltering noon.
func TestHeatOutweighsSun(t *testing.T) {
	f := day(func(i int, h *Hour) {
		feels := 33.0
		if i == 8 {
			feels = 19
		}
		sun := 60
		if i == 8 {
			sun = 40
		}
		h.ApparentTemperature, h.SunshineMinutes = &feels, &sun
	})
	o := plan(t, WindowsInput{Forecast: f, Now: at(0, 8, 0), Minutes: 60, Activity: Run})
	if o.Windows[0].From != "08:00" {
		t.Fatalf("windows = %v", spans(o))
	}
}

// The air is a note on the best window, never its rank.
func TestAirNote(t *testing.T) {
	aqi, clean := 55.0, 10.0
	air := &Air{
		Hourly: []AirHour{{Time: "2026-11-09T14:00", AQI: &aqi}, {Time: "2026-11-09T09:00", AQI: &clean}},
		Daily:  []AirDay{{Date: "2026-11-09", Pollen: map[string]float64{"birch": 80, "grass": 3}}},
	}
	o := plan(t, WindowsInput{Forecast: day(), Air: air, Now: at(0, 8, 0), Minutes: 60, Activity: Run})
	if o.Windows[0].From != "14:00" || o.Air == nil || o.Air.Level != "moderate" || o.Air.Pollen["birch"] != "high" {
		t.Fatalf("air = %+v", o.Air)
	}
	if _, ok := o.Air.Pollen["grass"]; ok {
		t.Fatalf("low pollen noted: %+v", o.Air)
	}
	clear := &Air{Hourly: []AirHour{{Time: "2026-11-09T14:00", AQI: &clean}}}
	if o := plan(t, WindowsInput{Forecast: day(), Air: clear, Now: at(0, 8, 0), Minutes: 60, Activity: Run}); o.Air != nil {
		t.Fatalf("clean air noted: %+v", o.Air)
	}
}

// Hours the forecast lacks are no window.
func TestMissingHours(t *testing.T) {
	o := plan(t, WindowsInput{Forecast: Forecast{Hourly: day().Hourly[:10]}, Now: at(0, 8, 0), Minutes: 60, Activity: Run})
	if len(o.Windows) == 0 {
		t.Fatalf("outlook = %+v", o)
	}
	for _, w := range o.Windows {
		if w.To > "10:00" {
			t.Fatalf("a window past the forecast: %v", spans(o))
		}
	}
	none := plan(t, WindowsInput{Forecast: Forecast{}, Now: at(0, 8, 0), Minutes: 60, Activity: Run})
	if none.None != NoneNoForecast {
		t.Fatalf("no hours = %+v", none)
	}
}

func TestDefaultMinutes(t *testing.T) {
	if Run.DefaultMinutes() != 60 || Ride.DefaultMinutes() != 120 {
		t.Fatal("defaults")
	}
}
