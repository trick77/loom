package weather

import (
	"encoding/csv"
	"strings"
	"testing"
	"time"

	"github.com/trick77/loom/internal/sun"
)

func readCSV(t *testing.T, data []byte) [][]string {
	t.Helper()
	rows, err := csv.NewReader(strings.NewReader(string(data))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestChartFiles(t *testing.T) {
	f := day(windFrom(10, 12, 18, 250), func(i int, h *Hour) {
		if i == 9 {
			h.WindSpeed, h.WindGusts, h.SunshineMinutes = nil, nil, nil
		}
	})
	today := plan(t, WindowsInput{Forecast: f, Now: at(0, 8, 0), Minutes: 120, Activity: Ride})
	week := Week(WeekInput{Forecast: Forecast{Daily: []Day{dayOf("2026-11-09", 9, 0, 0, 5, 18, 30, 250)}},
		Now: at(0, 8, 0), Loc: zurich, Latitude: 47.05, Longitude: 8.31, Activity: Ride})
	f.Hourly[11].ApparentTemperature = nil
	files := ChartFiles(ChartInput{Forecast: f, Loc: zurich, Activity: Ride, Minutes: 120, Date: "2026-11-10",
		Outlooks: []Outlook{today}, Week: week})
	if len(files) != 4 {
		t.Fatalf("files = %d", len(files))
	}
	byName := map[string][][]string{}
	for _, file := range files {
		if file.MimeType != "text/csv" || !strings.HasPrefix(file.Name, "outdoor_") {
			t.Fatalf("file = %+v", file.Name)
		}
		byName[file.Name] = readCSV(t, file.Data)
	}
	hours := byName[ChartHours]
	if strings.Join(hours[0], ",") != hdrHours || len(hours) != 49 {
		t.Fatalf("hours = %d rows, header %v", len(hours), hours[0])
	}
	// 03:00 is night, 10:00 day and windy; 09:00 lacks its wind: empty, not 0.
	if hours[4][1] != "false" || hours[11][1] != "true" || hours[11][7] != "18" || hours[11][9] != "250" {
		t.Fatalf("hours 03:00 %v, 10:00 %v", hours[4], hours[11])
	}
	if hours[10][4] != "" || hours[10][7] != "" {
		t.Fatalf("09:00 = %v", hours[10])
	}
	// No feels-like is no value, never the temperature again.
	if hours[12][3] != "" || hours[12][2] == "" {
		t.Fatalf("11:00 = %v", hours[12])
	}
	windows := byName[ChartWindows]
	if strings.Join(windows[0], ",") != hdrWindows || len(windows) != 1+len(today.Windows) || windows[1][0] != "2026-11-09" || windows[1][1] != "1" {
		t.Fatalf("windows = %v", windows)
	}
	// A ride on a 4-10 °C grey day is cold.
	if windows[1][11] != "true" {
		t.Fatalf("windows cold = %v", windows[1])
	}
	days := byName[ChartDays]
	if days[1][14] != "true" {
		t.Fatalf("days cold = %v", days[1])
	}
	if strings.Join(days[0], ",") != hdrDays || len(days) != 2 || days[1][11] != "dry" || days[1][12] != WindWindy || days[1][13] != "1" {
		t.Fatalf("days = %v", days)
	}
	limits := byName[ChartLimits]
	if strings.Join(limits[0], ",") != hdrLimits || strings.Join(limits[1], ",") != "ride,120,2026-11-10,15,39,30,45,10,7" {
		t.Fatalf("limits = %v", limits)
	}
}

// Polar days are wholly light or dark: true/false like every yes/no cell.
func TestChartFiles_polar(t *testing.T) {
	f := Forecast{Hourly: []Hour{{Time: "2026-06-21T12:00"}, {Time: "2026-12-21T12:00"}}}
	files := ChartFiles(ChartInput{Forecast: f, Loc: time.UTC, Activity: Run, Week: []DayOutlook{
		{Date: "2026-06-21", Polar: string(sun.PolarDay)}, {Date: "2026-12-21", Polar: string(sun.PolarNight)}}})
	hours := readCSV(t, files[0].Data)
	if hours[1][1] != "true" || hours[2][1] != "false" {
		t.Errorf("polar daylight = %v", hours)
	}
}
