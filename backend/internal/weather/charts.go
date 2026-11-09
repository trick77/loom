package weather

import (
	"bytes"
	"encoding/csv"
	"strconv"
	"strings"
	"time"

	"github.com/trick77/loom/internal/mcp"
	"github.com/trick77/loom/internal/sun"
)

// The CSV files outdoor_windows hands to run_python. Their columns are a
// contract with the sandbox's chart module (loomcharts/outdoor.py): change
// them together or not at all. Metric; an empty cell is no value.
const (
	ChartHours   = "outdoor_hours.csv"
	ChartWindows = "outdoor_windows.csv"
	ChartDays    = "outdoor_days.csv"
	ChartLimits  = "outdoor_limits.csv"

	hdrHours   = "time,daylight,temp_c,feels_c,sun_min,rain_mm,rain_pct,wind_kmh,gust_kmh,wind_dir_deg"
	hdrWindows = "date,rank,from,to,dry,wind,sun_min,rain_pct,wind_max_kmh,gust_max_kmh,wind_from"
	hdrDays    = "date,sunrise,sunset,high_c,low_c,rain_mm,rain_pct,sun_h,wind_max_kmh,gust_max_kmh,wind_from,rain,wind,rank"
	hdrLimits  = "activity,minutes,date,wind_kmh,wind_strong_kmh,gust_kmh,gust_strong_kmh"
)

// ChartInput is what the outdoor charts draw: the forecast's hours, the
// windows of the days planned (today and tomorrow) and the week, whose
// sunrise and sunset shade the night.
type ChartInput struct {
	Forecast Forecast
	Loc      *time.Location
	Activity Activity
	Minutes  int
	// Date is the day asked (YYYY-MM-DD): the one outdoor_day draws unless
	// told another.
	Date     string
	Outlooks []Outlook
	Week     []DayOutlook
}

// ChartFiles are the four CSVs of the contract.
func ChartFiles(in ChartInput) []mcp.File {
	loc := in.Loc
	if loc == nil {
		loc = time.UTC
	}
	week := map[string]DayOutlook{}
	for _, d := range in.Week {
		week[d.Date] = d
	}
	// An hour is night only when wholly dark: a window may start or end
	// inside the hour of sunrise or sunset.
	daylight := func(t time.Time) string {
		d, ok := week[t.Format(time.DateOnly)]
		switch {
		case !ok:
			return ""
		case d.Polar == string(sun.PolarDay):
			return "1"
		case d.Polar == string(sun.PolarNight):
			return "0"
		}
		clock := t.Format("15:04")
		end := t.Add(time.Hour).Format("15:04")
		if end == "00:00" {
			end = "24:00"
		}
		return flag(clock < d.Sunset && end > d.Sunrise)
	}
	hours := [][]string{}
	for _, h := range in.Forecast.Hourly {
		t, err := time.ParseInLocation("2006-01-02T15:04", h.Time, loc)
		if err != nil {
			continue
		}
		hours = append(hours, []string{h.Time, daylight(t), num(&h.Temperature), num(h.ApparentTemperature), integer(h.SunshineMinutes),
			num(h.Precipitation), integer(h.PrecipitationProbability), num(h.WindSpeed), num(h.WindGusts), integer(h.WindDirection)})
	}
	windows := [][]string{}
	for _, o := range in.Outlooks {
		for i, w := range o.Windows {
			windows = append(windows, []string{o.Date, strconv.Itoa(i + 1), w.From, w.To, flag(w.Dry), w.Wind,
				strconv.Itoa(w.SunMinutes), strconv.Itoa(w.RainChance), num(w.WindMax), num(w.GustsMax), w.WindFrom})
		}
	}
	days := [][]string{}
	for _, d := range in.Week {
		days = append(days, []string{d.Date, d.Sunrise, d.Sunset, num(&d.High), num(&d.Low), num(d.RainMM),
			integer(d.RainChance), num(d.SunHours), num(d.WindMax), num(d.GustsMax), d.WindFrom, d.Rain, d.Wind,
			strconv.Itoa(d.Rank)})
	}
	l := in.Activity.Limits()
	limits := [][]string{{string(in.Activity), strconv.Itoa(in.Minutes), in.Date, num(&l.Wind), num(&l.WindStrong),
		num(&l.Gusts), num(&l.GustsStrong)}}
	return []mcp.File{
		csvFile(ChartHours, hdrHours, hours),
		csvFile(ChartWindows, hdrWindows, windows),
		csvFile(ChartDays, hdrDays, days),
		csvFile(ChartLimits, hdrLimits, limits),
	}
}

func csvFile(name, header string, rows [][]string) mcp.File {
	var b bytes.Buffer
	w := csv.NewWriter(&b)
	// Writes to a buffer do not fail.
	_ = w.Write(strings.Split(header, ","))
	_ = w.WriteAll(rows)
	return mcp.File{Name: name, MimeType: "text/csv", Data: b.Bytes()}
}

func num(v *float64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatFloat(*v, 'f', -1, 64)
}

func integer(v *int) string {
	if v == nil {
		return ""
	}
	return strconv.Itoa(*v)
}

func flag(b bool) string {
	if b {
		return "1"
	}
	return "0"
}
