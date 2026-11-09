package sun

import (
	"testing"
	"time"
)

func load(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

// Published times (NOAA's calculator) to the minute; the equations are good
// to about one.
func TestOn(t *testing.T) {
	for _, tc := range []struct {
		name          string
		lat, lon      float64
		zone          string
		date          time.Time
		rise, setTime string
	}{
		{"Zürich in November", 47.3769, 8.5417, "Europe/Zurich", time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC), "07:22", "16:59"},
		{"Zürich at midsummer", 47.3769, 8.5417, "Europe/Zurich", time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC), "05:29", "21:26"},
		{"New York in March", 40.7128, -74.006, "America/New_York", time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC), "06:59", "19:10"},
		{"Sydney in January", -33.8688, 151.2093, "Australia/Sydney", time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC), "06:01", "20:10"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			loc := load(t, tc.zone)
			day := On(tc.date, tc.lat, tc.lon, loc)
			if day.Polar != NotPolar {
				t.Fatalf("polar %q", day.Polar)
			}
			near(t, "rise", day.Rise, tc.date, tc.rise, loc)
			near(t, "set", day.Set, tc.date, tc.setTime, loc)
			if day.Length() != day.Set.Sub(day.Rise) {
				t.Fatal("length is not set minus rise")
			}
		})
	}
}

func near(t *testing.T, what string, got, date time.Time, want string, loc *time.Location) {
	t.Helper()
	w, err := time.ParseInLocation("2006-01-02 15:04", date.In(loc).Format("2006-01-02")+" "+want, loc)
	if err != nil {
		t.Fatal(err)
	}
	if d := got.Sub(w); d < -2*time.Minute || d > 2*time.Minute {
		t.Errorf("%s = %s, want %s (±2 min)", what, got.Format("15:04"), want)
	}
	if got.Location() != loc {
		t.Errorf("%s is not in the place's zone", what)
	}
}

func TestPolar(t *testing.T) {
	oslo := load(t, "Europe/Oslo")
	tromso := func(m time.Month) Day {
		return On(time.Date(2026, m, 21, 12, 0, 0, 0, time.UTC), 69.6492, 18.9553, oslo)
	}
	if d := tromso(time.December); d.Polar != PolarNight || d.Length() != 0 || !d.Rise.IsZero() {
		t.Fatalf("Tromsø in December = %+v", d)
	}
	if d := tromso(time.June); d.Polar != PolarDay || d.Length() != 24*time.Hour {
		t.Fatalf("Tromsø in June = %+v", d)
	}
}

// The day is the calendar day in the place's zone, not UTC's.
func TestOnUsesTheLocalDay(t *testing.T) {
	sydney := load(t, "Australia/Sydney")
	// 22:00 UTC on the 14th is already the 15th in Sydney.
	day := On(time.Date(2026, 1, 14, 22, 0, 0, 0, time.UTC), -33.8688, 151.2093, sydney)
	if got := day.Rise.Format("2006-01-02"); got != "2026-01-15" {
		t.Fatalf("rise on %s, want the local 15th", got)
	}
}
