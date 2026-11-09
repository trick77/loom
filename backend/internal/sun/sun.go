// Package sun computes sunrise and sunset for a place and day with NOAA's
// solar position equations: no provider, no table, about a minute of error
// outside the polar circles.
package sun

import (
	"math"
	"time"
)

// Polar says the sun neither rises nor sets that day.
type Polar string

const (
	// NotPolar is a day the sun rises and sets.
	NotPolar Polar = ""
	// PolarDay is a day the sun stays up.
	PolarDay Polar = "day"
	// PolarNight is a day the sun stays down.
	PolarNight Polar = "night"
)

// Day is a day's sunrise and sunset; both zero when Polar is set.
type Day struct {
	Rise, Set time.Time
	Polar     Polar
}

// Length is how long the sun is up: 24 h on a polar day, 0 on a polar night.
func (d Day) Length() time.Duration {
	switch d.Polar {
	case PolarDay:
		return 24 * time.Hour
	case PolarNight:
		return 0
	}
	return d.Set.Sub(d.Rise)
}

// zenith is the sun's centre at sunrise and sunset: 90° plus refraction and
// the solar disc's radius.
const zenith = 90.833

// On is the sunrise and sunset at lat, lon (degrees, east positive) on the
// calendar day of date in loc, returned in loc.
func On(date time.Time, lat, lon float64, loc *time.Location) Day {
	y, m, d := date.In(loc).Date()
	midnight := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	// Solar noon first, then rise and set from the sun's position at each.
	noon := eventMinutes(midnight, lat, lon, 0, 720-4*lon)
	if math.IsNaN(noon.minutes) {
		return Day{Polar: noon.polar}
	}
	rise := eventMinutes(midnight, lat, lon, -1, noon.minutes)
	set := eventMinutes(midnight, lat, lon, 1, noon.minutes)
	if rise.polar != NotPolar || set.polar != NotPolar {
		return Day{Polar: rise.polar}
	}
	at := func(minutes float64) time.Time {
		return midnight.Add(time.Duration(minutes * float64(time.Minute))).In(loc)
	}
	// Refined once, at the event's own time.
	rise = eventMinutes(midnight, lat, lon, -1, rise.minutes)
	set = eventMinutes(midnight, lat, lon, 1, set.minutes)
	if rise.polar != NotPolar || set.polar != NotPolar {
		return Day{Polar: rise.polar}
	}
	return Day{Rise: at(rise.minutes), Set: at(set.minutes)}
}

type event struct {
	minutes float64 // from 00:00 UTC of the day
	polar   Polar
}

// eventMinutes is the time of solar noon (side 0), sunrise (-1) or sunset
// (+1) in minutes from 00:00 UTC, with the sun's position taken at guess.
func eventMinutes(midnight time.Time, lat, lon float64, side int, guess float64) event {
	t := julianCentury(midnight, guess)
	decl, eqTime := declination(t), equationOfTime(t)
	noon := 720 - 4*lon - eqTime
	if side == 0 {
		cosH := hourAngleCos(lat, decl)
		switch {
		case cosH > 1:
			return event{minutes: math.NaN(), polar: PolarNight}
		case cosH < -1:
			return event{minutes: math.NaN(), polar: PolarDay}
		}
		return event{minutes: noon}
	}
	cosH := hourAngleCos(lat, decl)
	switch {
	case cosH > 1:
		return event{polar: PolarNight}
	case cosH < -1:
		return event{polar: PolarDay}
	}
	h := deg(math.Acos(cosH))
	return event{minutes: noon + float64(side)*4*h}
}

func julianCentury(midnight time.Time, minutes float64) float64 {
	jd := float64(midnight.Unix())/86400 + 2440587.5 + minutes/1440
	return (jd - 2451545) / 36525
}

func hourAngleCos(lat, decl float64) float64 {
	return math.Cos(rad(zenith))/(math.Cos(rad(lat))*math.Cos(decl)) - math.Tan(rad(lat))*math.Tan(decl)
}

// meanLongitude, meanAnomaly and eccentricity are the sun's orbit at t.
func meanLongitude(t float64) float64 {
	return math.Mod(280.46646+t*(36000.76983+t*0.0003032), 360)
}

func meanAnomaly(t float64) float64 { return 357.52911 + t*(35999.05029-0.0001537*t) }

func eccentricity(t float64) float64 { return 0.016708634 - t*(0.000042037+0.0000001267*t) }

func obliquity(t float64) float64 {
	eps0 := 23 + (26+(21.448-t*(46.815+t*(0.00059-t*0.001813)))/60)/60
	return eps0 + 0.00256*math.Cos(rad(125.04-1934.136*t))
}

// declination is the sun's declination (radians) at t.
func declination(t float64) float64 {
	m := rad(meanAnomaly(t))
	c := math.Sin(m)*(1.914602-t*(0.004817+0.000014*t)) + math.Sin(2*m)*(0.019993-0.000101*t) + math.Sin(3*m)*0.000289
	lambda := meanLongitude(t) + c - 0.00569 - 0.00478*math.Sin(rad(125.04-1934.136*t))
	return math.Asin(math.Sin(rad(obliquity(t))) * math.Sin(rad(lambda)))
}

// equationOfTime is how far apparent solar time runs ahead of mean, in
// minutes, at t.
func equationOfTime(t float64) float64 {
	eps, l0, e, m := rad(obliquity(t)), rad(meanLongitude(t)), eccentricity(t), rad(meanAnomaly(t))
	y := math.Pow(math.Tan(eps/2), 2)
	return 4 * deg(y*math.Sin(2*l0)-2*e*math.Sin(m)+4*e*y*math.Sin(m)*math.Cos(2*l0)-0.5*y*y*math.Sin(4*l0)-1.25*e*e*math.Sin(2*m))
}

func rad(d float64) float64 { return d * math.Pi / 180 }
func deg(r float64) float64 { return r * 180 / math.Pi }
