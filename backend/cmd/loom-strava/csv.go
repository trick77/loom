package main

import (
	"encoding/csv"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/trick77/toolkit/mcpserver"
)

// The CSV files the tools attach for run_python. Their columns are a
// contract with the sandbox's chart module: change them together or not at
// all. Always metric, whatever the athlete's units.
const (
	hdrStreams    = "elapsed_s,distance_km,lat,lng,altitude_m,speed_kmh,hr,cadence,watts,grade_pct,temp_c,moving,power_zone,hr_zone"
	hdrClimbs     = "n,from_km,to_km,len_km,gain_m,grade_pct,secs,vam,avg_w,wkg,avg_hr"
	hdrPowerCurve = "duration_s,this_w,window_best_w,window_median_w"
	hdrBaseline   = "activity_id,date,name,sport_type,is_this,same_route,distance_km,moving_s,avg_kmh,gain_m,np_w,tss,ef,decoupling_pct,best_20m_w,avg_hr"
	hdrWeeks      = "week_start,iso_week,sport_type,count,distance_km,moving_s,gain_m,tss"
	hdrActivities = "activity_id,date,name,sport_type,trainer,distance_km,moving_s,gain_m,avg_kmh,avg_hr,avg_w,weighted_w,kj,suffer_score"
	// hdrSameRoutePrefix is followed by one gap_s_<YYYY-MM-DD> column per
	// earlier ride.
	hdrSameRoutePrefix = "distance_km"
)

// maxFileBytes is loom's cap on one tool file; maxFilesBytes on all files
// of one result together (mcpserver drops what is over).
const (
	maxFileBytes  = 4 << 20
	maxFilesBytes = 3 << 20
)

// streamStepS is the streams file's resample step.
const streamStepS = 5

type csvTable struct {
	b    strings.Builder
	w    *csv.Writer
	rows int
}

func newCSV(header string) *csvTable {
	t := &csvTable{}
	t.w = csv.NewWriter(&t.b)
	t.row(strings.Split(header, ",")...)
	t.rows = 0
	return t
}

func (t *csvTable) row(cells ...string) {
	_ = t.w.Write(cells)
	t.rows++
}

func (t *csvTable) text() string {
	t.w.Flush()
	return t.b.String()
}

func (t *csvTable) file(name string) mcpserver.File {
	return mcpserver.File{Name: name, MimeType: "text/csv", Text: t.text()}
}

// cell is v with prec decimals; empty when unknown.
func cell(v float64, prec int) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return ""
	}
	return strconv.FormatFloat(v, 'f', prec, 64)
}

func intCell(v int) string { return strconv.Itoa(v) }

func boolCell(v bool) string {
	if v {
		return "1"
	}
	return "0"
}

// streamsCSV resamples s every 5 s of elapsed time, every 10 s, 20 s … when
// that does not fit budget bytes (a 6 h ride at 5 s is ~400 KB). ok false:
// not even a coarse one fits.
func streamsCSV(id int64, s series, pEdges, hEdges []float64, budget int) (mcpserver.File, bool) {
	name := fmt.Sprintf("strava_%d_streams.csv", id)
	budget = min(budget, maxFileBytes)
	var p30 []float64
	if s.watts != nil {
		p30 = rolling(s.watts, 30)
	}
	for step := float64(streamStepS); step <= maxStreamStepS; step *= 2 {
		t := newCSV(hdrStreams)
		next := 0.0
		for i := 0; i < s.n; i++ {
			if s.elapsed[i] < next {
				continue
			}
			next = s.elapsed[i] + step
			at := func(v []float64, scale float64, prec int) string {
				if v == nil {
					return ""
				}
				return cell(v[i]*scale, prec)
			}
			pz, hz := "", ""
			if p30 != nil && pEdges != nil {
				pz = intCell(zoneOf(p30[i], pEdges))
			}
			if s.hr != nil && hEdges != nil {
				hz = intCell(zoneOf(s.hr[i], hEdges))
			}
			t.row(cell(s.elapsed[i], 0), at(s.dist, 0.001, 3), at(s.lat, 1, 6), at(s.lng, 1, 6), at(s.alt, 1, 1),
				at(s.speed, 3.6, 1), at(s.hr, 1, 0), at(s.cad, 1, 0), at(s.watts, 1, 0), at(s.grade, 1, 1),
				at(s.temp, 1, 1), boolCell(s.moving[i]), pz, hz)
		}
		if f := t.file(name); len(f.Text) <= budget {
			return f, true
		}
	}
	return mcpserver.File{}, false
}

// maxStreamStepS is the coarsest resample still worth a chart.
const maxStreamStepS = 80

// filesSize is the bytes files take together.
func filesSize(files []mcpserver.File) int {
	n := 0
	for _, f := range files {
		n += len(f.Text)
	}
	return n
}

// weekStart is the Monday 00:00 of t's ISO week, in t's (local) clock.
func weekStart(t time.Time) time.Time {
	d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	return d.AddDate(0, 0, -((int(d.Weekday()) + 6) % 7))
}

func isoWeek(t time.Time) string {
	y, w := t.ISOWeek()
	return fmt.Sprintf("%d-W%02d", y, w)
}

// weekAgg sums one sport's activities in one ISO week.
type weekAgg struct {
	start                 time.Time
	sport                 string
	count                 int
	distM, movingS, gainM float64
	tss                   float64
	tssKnown              int
	efs                   []float64
}

// weekTable groups activities by ISO week and sport; weeks with nothing in
// them still get a row per sport, so a gap in training shows.
type weekTable struct {
	from, to time.Time // Monday of the first and of the last week
	sports   []string
	aggs     map[string]*weekAgg
}

func newWeekTable(from, to time.Time, sports []string) *weekTable {
	return &weekTable{from: weekStart(from), to: weekStart(to), sports: sports, aggs: map[string]*weekAgg{}}
}

func (w *weekTable) add(a summaryActivity, tssV, ef float64) {
	ws := weekStart(a.localStart())
	key := ws.Format("2006-01-02") + "|" + a.SportType
	g, ok := w.aggs[key]
	if !ok {
		g = &weekAgg{start: ws, sport: a.SportType}
		w.aggs[key] = g
		found := false
		for _, s := range w.sports {
			found = found || s == a.SportType
		}
		if !found {
			w.sports = append(w.sports, a.SportType)
		}
	}
	g.count++
	g.distM += a.Distance
	g.movingS += float64(a.MovingTime)
	g.gainM += a.TotalElevationGain
	if !math.IsNaN(tssV) {
		g.tss += tssV
		g.tssKnown++
	}
	if !math.IsNaN(ef) {
		g.efs = append(g.efs, ef)
	}
}

// weeks lists every week from..to, each sport's row (empty ones included).
func (w *weekTable) weeks() [][]*weekAgg {
	var out [][]*weekAgg
	for ws := w.from; !ws.After(w.to); ws = ws.AddDate(0, 0, 7) {
		var row []*weekAgg
		for _, s := range w.sports {
			g, ok := w.aggs[ws.Format("2006-01-02")+"|"+s]
			if !ok {
				g = &weekAgg{start: ws, sport: s}
			}
			row = append(row, g)
		}
		out = append(out, row)
	}
	return out
}

func (w *weekTable) file() mcpserver.File {
	t := newCSV(hdrWeeks)
	for _, row := range w.weeks() {
		for _, g := range row {
			tssCell := ""
			if g.tssKnown > 0 {
				tssCell = cell(g.tss, 0)
			}
			t.row(g.start.Format("2006-01-02"), isoWeek(g.start), g.sport, intCell(g.count), cell(g.distM/1000, 2),
				cell(g.movingS, 0), cell(g.gainM, 0), tssCell)
		}
	}
	return t.file("strava_weeks.csv")
}

// fileNames is the one line naming the attached files.
func fileNames(files []mcpserver.File) string {
	if len(files) == 0 {
		return ""
	}
	names := make([]string, 0, len(files))
	for _, f := range files {
		names = append(names, fmt.Sprintf("%s (%d rows)", f.Name, max(0, strings.Count(f.Text, "\n")-1)))
	}
	return "attached for run_python: " + strings.Join(names, ", ") + "\n"
}
