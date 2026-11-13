package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/trick77/toolkit/mcpserver"
)

// history is the weather of a past day: what loom-strava weighs heat by when
// a watch records no temperature. The past does not change, so an archived
// day is kept as long as the cache holds it; a recent one (the forecast
// model's past hours, before the archive has them) for six hours.
const (
	historyTool = "history"
	// recentDays: the forecast API answers this far back; older days come
	// from the archive (days behind today, so the two overlap).
	recentDays = 80
	// earliestYear: the archive's reanalysis starts in 1940.
	earliestYear = 1940
)

// History is the history tool's answer, as JSON text: the day's hours at
// the place, local times.
type History struct {
	Latitude    float64       `json:"latitude"`
	Longitude   float64       `json:"longitude"`
	Timezone    string        `json:"timezone"`
	Date        string        `json:"date"`
	Hourly      []HistoryHour `json:"hourly"`
	Attribution Attribution   `json:"attribution"`
}

type HistoryHour struct {
	Time                string   `json:"time"`
	Temperature         float64  `json:"temperature"`
	ApparentTemperature *float64 `json:"apparentTemperature,omitempty"`
	Humidity            *int     `json:"humidity,omitempty"`
}

// history is one past day at the point, from the forecast API while it is
// recent, else from the archive.
func (p openMeteo) history(ctx context.Context, lat, lon float64, date string, recent bool) (History, error) {
	q := forecastQuery(lat, lon, "auto", 1)
	q.Del("forecast_days")
	q.Set("start_date", date)
	q.Set("end_date", date)
	q.Set("hourly", "temperature_2m,apparent_temperature,relative_humidity_2m")
	target := p.archiveURL + "/v1/archive?" + q.Encode()
	if recent {
		target = p.forecastURL + "/v1/forecast?" + q.Encode()
	}
	var raw struct {
		forecastHeader
		Hourly struct {
			Time        []string   `json:"time"`
			Temperature []*float64 `json:"temperature_2m"`
			Apparent    []*float64 `json:"apparent_temperature"`
			Humidity    []*float64 `json:"relative_humidity_2m"`
		} `json:"hourly"`
	}
	if err := p.get(ctx, target, &raw); err != nil {
		return History{}, err
	}
	out := History{Latitude: raw.Latitude, Longitude: raw.Longitude, Timezone: raw.Timezone, Date: date, Attribution: attribution}
	h := raw.Hourly
	for i, t := range h.Time {
		temp := at(h.Temperature, i)
		if temp == nil {
			continue
		}
		out.Hourly = append(out.Hourly, HistoryHour{Time: t, Temperature: *temp, ApparentTemperature: at(h.Apparent, i),
			Humidity: whole(at(h.Humidity, i))})
	}
	if len(out.Hourly) == 0 {
		return History{}, fmt.Errorf("weather provider sent no hours for %s", date)
	}
	return out, nil
}

type historyArgs struct {
	Latitude  *float64 `json:"latitude"`
	Longitude *float64 `json:"longitude"`
	Date      string   `json:"date"`
}

func (w *weather) runHistory(ctx context.Context, raw json.RawMessage) mcpserver.Result {
	var args historyArgs
	if err := mcpserver.DecodeArgs(raw, &args); err != nil {
		return mcpserver.ErrorResult("history: " + err.Error())
	}
	if args.Latitude == nil || args.Longitude == nil || math.Abs(*args.Latitude) > 90 || math.Abs(*args.Longitude) > 180 {
		return mcpserver.ErrorResult("history needs latitude (-90..90) and longitude (-180..180)")
	}
	day, err := time.Parse("2006-01-02", args.Date)
	today := w.now().UTC().Truncate(24 * time.Hour)
	// date is the place's local day: east of UTC (Auckland) it is a day
	// ahead of UTC's for half the day.
	if err != nil || day.Year() < earliestYear || day.After(today.AddDate(0, 0, 1)) {
		return mcpserver.ErrorResult(fmt.Sprintf("history needs date as YYYY-MM-DD, %d or later and not in the future", earliestYear))
	}
	// Two decimals, about a kilometre: the model's grid is coarser, and
	// nearby activities share the one request.
	lat, lon := math.Round(*args.Latitude*100)/100, math.Round(*args.Longitude*100)/100
	recent := !day.Before(today.AddDate(0, 0, -recentDays))
	key := strconv.FormatFloat(lat, 'f', 2, 64) + "," + strconv.FormatFloat(lon, 'f', 2, 64) + "," + args.Date
	expires := func(now time.Time) time.Time { return now.AddDate(1, 0, 0) }
	if recent {
		expires = nextSixHours
	}
	h, err := w.history.get(ctx, key, expires, func(ctx context.Context) (History, error) {
		return w.provider.history(ctx, lat, lon, args.Date, recent)
	})
	if err != nil {
		return mcpserver.ErrorResult("history failed: " + err.Error())
	}
	result := jsonResult(h)
	result.Sources = []mcpserver.Source{{URL: h.Attribution.URL, Title: h.Attribution.Text}}
	return result
}

func historyToolSpec(w *weather) mcpserver.Tool {
	return mcpserver.Tool{
		Name:  historyTool,
		Title: "Past weather",
		Description: "The weather of a past day at a point, as JSON: hourly temperature and feels-like (°C) and humidity (%), local " +
			"times. For an activity's conditions after the fact.",
		Schema: mcpserver.ObjectSchema(map[string]any{
			"latitude":  map[string]any{"type": "number", "minimum": -90, "maximum": 90},
			"longitude": map[string]any{"type": "number", "minimum": -180, "maximum": 180},
			"date":      map[string]any{"type": "string", "pattern": `^\d{4}-\d{2}-\d{2}$`, "description": "The day, YYYY-MM-DD, local to the place."},
		}, "latitude", "longitude", "date"),
		ReadOnly:  true,
		OpenWorld: true,
		Run:       w.runHistory,
	}
}
