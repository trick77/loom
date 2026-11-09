package weather

import (
	"math"
	"slices"
	"time"

	"github.com/trick77/loom/internal/sun"
)

// Rain levels of a day.
const (
	RainDry     = "dry"
	RainShowers = "showers"
	RainWet     = "wet"
)

// A day is dry under dryMM and dryChance, wet from wetDayMM, or from
// likelyChance with likelyMM; showers between. The sums take in the night:
// a day's numbers are rough next to its hours.
const (
	dryMM        = 0.5
	dryChance    = 40
	wetDayMM     = 3
	likelyChance = 70
	likelyMM     = 1
)

// WeekInput is the week ahead at a place: the forecast's days from today.
type WeekInput struct {
	Forecast            Forecast
	Now                 time.Time
	Loc                 *time.Location
	Latitude, Longitude float64
	Activity            Activity
	Minutes             int
	// Planned are the days already planned hour by hour (today, tomorrow):
	// their time left and wind come from those windows, so the week and the
	// day never disagree.
	Planned []Outlook
}

// DayOutlook is a day of the week ahead read off its daily forecast. Wind
// and gusts are the day's strongest, night included.
type DayOutlook struct {
	Date       string   `json:"date"`
	Sunrise    string   `json:"sunrise,omitempty"`
	Sunset     string   `json:"sunset,omitempty"`
	Polar      string   `json:"polar,omitempty"`
	High       float64  `json:"high"`
	Low        float64  `json:"low"`
	Code       int      `json:"code"`
	RainMM     *float64 `json:"rainMM,omitempty"`
	RainChance *int     `json:"rainChance,omitempty"`
	SunHours   *float64 `json:"sunHours,omitempty"`
	WindMax    *float64 `json:"windMax,omitempty"`
	GustsMax   *float64 `json:"gustsMax,omitempty"`
	WindFrom   string   `json:"windFrom,omitempty"`
	// Rain is RainDry, RainShowers or RainWet; Wind the level for the
	// activity.
	Rain string `json:"rain"`
	Wind string `json:"wind,omitempty"`
	// None is why the day has no time to go out (NoneDark, NoneShort,
	// NoneOver: today's daylight left is too short); such a day ranks last.
	None string `json:"none,omitempty"`
	// Rank is 1 for the best day to go out: one with time for it, then
	// drier, then calmer, then sunnier, then milder, then sooner.
	Rank int `json:"rank"`
}

// Week is the days from today on, each with its rank.
func Week(in WeekInput) []DayOutlook {
	loc := in.Loc
	if loc == nil {
		loc = time.UTC
	}
	now := in.Now.In(loc)
	today := now.Format(time.DateOnly)
	c := in.Activity.comfort()
	dur := time.Duration(max(in.Minutes, 1)) * time.Minute
	planned := map[string]Outlook{}
	for _, p := range in.Planned {
		planned[p.Date] = p
	}
	out := []DayOutlook{}
	for _, d := range in.Forecast.Daily {
		if d.Date < today {
			continue
		}
		date, err := time.ParseInLocation(time.DateOnly, d.Date, loc)
		if err != nil {
			continue
		}
		o := DayOutlook{Date: d.Date, High: d.High, Low: d.Low, Code: d.Code, RainMM: d.PrecipitationSum,
			RainChance: d.PrecipitationProbability, SunHours: d.SunshineHours, WindMax: d.WindSpeedMax,
			GustsMax: d.WindGustsMax, Rain: rainLevel(d), Wind: c.limits.Level(d.WindSpeedMax, d.WindGustsMax)}
		if d.WindDirection != nil {
			o.WindFrom = compass(*d.WindDirection)
		}
		s := sun.On(date.Add(12*time.Hour), in.Latitude, in.Longitude, loc)
		o.Polar = string(s.Polar)
		switch s.Polar {
		case sun.PolarNight:
			o.None = NoneDark
		case sun.NotPolar:
			o.Sunrise, o.Sunset = s.Rise.In(loc).Format("15:04"), s.Set.In(loc).Format("15:04")
			if s.Set.Sub(s.Rise) < dur {
				o.None = NoneShort
			} else if d.Date == today && now.After(s.Set.Add(-dur)) {
				o.None = NoneOver
			}
		}
		if p, ok := planned[d.Date]; ok {
			// The day's hours know better than its totals: the wind in
			// daylight, not at 03:00, and the time left on the step grid.
			o.None, o.Wind = p.None, ""
			if len(p.Windows) > 0 {
				o.Wind = p.Windows[0].Wind
			} else if o.None == "" {
				o.None = NoneOver
			}
		}
		out = append(out, o)
	}
	order := slices.Clone(out)
	slices.SortStableFunc(order, func(a, b DayOutlook) int {
		if na, nb := a.None != "", b.None != ""; na != nb {
			if na {
				return 1
			}
			return -1
		}
		// Strong wind sinks a day below every other, as it does a window.
		if sa, sb := a.Wind == WindStrong, b.Wind == WindStrong; sa != sb {
			if sa {
				return 1
			}
			return -1
		}
		if ra, rb := rainRank(a.Rain), rainRank(b.Rain); ra != rb {
			return ra - rb
		}
		if wa, wb := windRank(a.Wind), windRank(b.Wind); wa != wb {
			return wa - wb
		}
		if sa, sb := math.Round(value(a.SunHours)), math.Round(value(b.SunHours)); sa != sb {
			return cmp(sb, sa)
		}
		if pa, pb := c.dayPain(a.High), c.dayPain(b.High); pa != pb {
			return cmp(pa, pb)
		}
		return 0
	})
	rank := map[string]int{}
	for i, d := range order {
		rank[d.Date] = i + 1
	}
	for i := range out {
		out[i].Rank = rank[out[i].Date]
	}
	return out
}

// rainLevel reads a day's rain; without numbers its code stands in.
func rainLevel(d Day) string {
	sum, chance := 0.0, 0
	switch {
	case d.Code >= 61:
		sum, chance = wetDayMM, 80
	case d.Code >= wetCode:
		sum, chance = likelyMM, 80
	}
	if d.PrecipitationSum != nil {
		sum = *d.PrecipitationSum
	}
	if d.PrecipitationProbability != nil {
		chance = *d.PrecipitationProbability
	}
	switch {
	case sum >= wetDayMM || chance >= likelyChance && sum >= likelyMM:
		return RainWet
	case sum < dryMM && chance < dryChance:
		return RainDry
	}
	return RainShowers
}

func rainRank(level string) int {
	return slices.Index([]string{RainDry, RainShowers, RainWet}, level)
}

func windRank(level string) int {
	return slices.Index([]string{"", WindWindy, WindStrong}, level)
}

// dayPain is how far a day's high is from the activity's band. The band is
// feels-like and days carry no feels-like: the high stands in, the last
// tie-break after rain, wind and sun.
func (c comfort) dayPain(high float64) float64 {
	return math.Round(max(0, c.low-high) + max(0, high-c.high))
}

func value(v *float64) float64 {
	if v == nil {
		return 0
	}
	return *v
}
