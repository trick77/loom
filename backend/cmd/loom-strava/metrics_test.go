package main

import (
	"compress/gzip"
	"encoding/json"
	"math"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

func near(t *testing.T, name string, got, want, tol float64) {
	t.Helper()
	if math.IsNaN(want) {
		if !math.IsNaN(got) {
			t.Errorf("%s = %v, want NaN", name, got)
		}
		return
	}
	if math.IsNaN(got) || math.Abs(got-want) > tol {
		t.Errorf("%s = %v, want %v (±%v)", name, got, want, tol)
	}
}

func constant(n int, v float64) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = v
	}
	return out
}

// The prototype's (pandas) values for the same series: rolling(30,
// min_periods=1) for NP, rolling(d).max() for mean-max.
func TestPowerMaths_matchThePrototype(t *testing.T) {
	w := make([]float64, 1800)
	for i := range w {
		w[i] = float64((i * 37) % 401)
	}
	near(t, "NP", normalizedPower(w), 200.37292787619126, 1e-9)
	for _, c := range []struct {
		d    int
		want float64
	}{{1, 400}, {5, 326}, {30, 224.4}, {60, 213.16666666666666}, {300, 203.14666666666668}, {1200, 200.4075}, {1800, 200.11388888888888}, {3600, math.NaN()}} {
		near(t, "mean-max", meanMax(w, c.d), c.want, 1e-9)
	}
	near(t, "TSS", tss(1800, normalizedPower(w), 265), 28.586194535904124, 1e-9)
}

func TestPowerMaths_handChecked(t *testing.T) {
	tests := []struct {
		name string
		w    []float64
		np   float64
	}{
		{"constant", constant(600, 200), 200},
		{"empty", nil, math.NaN()},
		{"zeros", constant(100, 0), 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			near(t, "NP", normalizedPower(tc.w), tc.np, 1e-9)
		})
	}
	near(t, "IF", intensity(250, 250), 1, 1e-12)
	near(t, "TSS 1 h at FTP", tss(3600, 250, 250), 100, 1e-9)
	near(t, "TSS without FTP", tss(3600, 250, 0), math.NaN(), 0)
	near(t, "IF without NP", intensity(math.NaN(), 250), math.NaN(), 0)
	if got := rolling([]float64{30, 60, 90, 120}, 2); got[0] != 30 || got[1] != 45 || got[3] != 105 {
		t.Errorf("rolling = %v", got)
	}
}

func TestZones(t *testing.T) {
	edges := []float64{124, 155, 170, 180}
	for _, c := range []struct {
		v    float64
		want int
	}{{90, 1}, {124, 2}, {160, 3}, {175, 4}, {180, 5}, {230, 5}} {
		if got := zoneOf(c.v, edges); got != c.want {
			t.Errorf("zoneOf(%v) = %d, want %d", c.v, got, c.want)
		}
	}
	e := powerEdges(athleteZones{}, 200)
	near(t, "Z2 from FTP", e[0], 110, 1e-9)
	near(t, "Z5 from FTP", e[3], 210, 1e-9)
	if powerEdges(athleteZones{}, 0) != nil || hrEdges(athleteZones{}) != nil {
		t.Error("edges without zones or FTP")
	}
	var z athleteZones
	if err := jsonUnmarshal(`{"heart_rate":{"custom_zones":false,"zones":[{"min":0,"max":123},{"min":123,"max":153},{"min":153,"max":169},{"min":169,"max":184},{"min":184,"max":-1}]},
		"power":{"zones":[{"min":0,"max":145},{"min":146,"max":198},{"min":199,"max":238},{"min":239,"max":278},{"min":279,"max":318},{"min":319,"max":397},{"min":398,"max":-1}]}}`, &z); err != nil {
		t.Fatal(err)
	}
	// Strava's Z5..Z7 fold into our Z5.
	if e := powerEdges(z, 265); e[0] != 146 || e[3] != 279 {
		t.Errorf("strava power edges = %v", e)
	}
	if e := hrEdges(z); e[0] != 123 || e[3] != 184 {
		t.Errorf("hr edges = %v", e)
	}
}

// synthRide is a 1 Hz ride: 5 km flat at 200 W, 2 km at 6 % at 300 W, 3 km
// flat at 200 W; HR 130 on the flat, 165 on the climb, drifting up 5 bpm in
// the second half.
func synthRide(stop bool) streamSet { return synthRideWith(1, 1, 0, stop) }

// synthRideWith scales the power by pf and the speed by vf, and adds hrOff
// to the heart rate.
func synthRideWith(pf, vf, hrOff float64, stop bool) streamSet {
	var tm, dist, alt, vel, w, hr, cad, grade, temp floats
	var ll [][]float64
	var moving []bool
	d, a, t := 0.0, 400.0, 0.0
	for d < 10000 {
		v, p, g, h := 30/3.6, 200.0, 0.0, 130.0
		if d >= 5000 && d < 7000 {
			v, p, g, h = 15/3.6, 300, 6, 165
		}
		if d > 5000 {
			h += 5
		}
		v, p, h = v*vf, p*pf, h+hrOff
		tm = append(tm, t)
		dist = append(dist, d)
		alt = append(alt, a)
		vel = append(vel, v)
		w = append(w, p)
		hr = append(hr, h)
		cad = append(cad, 88)
		grade = append(grade, g)
		temp = append(temp, 12)
		ll = append(ll, []float64{47.3 + d/111000, 8.5})
		moving = append(moving, true)
		d += v
		a += v * g / 100
		t++
		if stop && t == 600 {
			t += 300 // a pause: no samples for 5 minutes
		}
	}
	return streamSet{
		Time: &struct{ Data floats }{tm}, Distance: &struct{ Data floats }{dist}, Altitude: &struct{ Data floats }{alt},
		Velocity: &struct{ Data floats }{vel}, Watts: &struct{ Data floats }{w}, HR: &struct{ Data floats }{hr},
		Cadence: &struct{ Data floats }{cad}, Grade: &struct{ Data floats }{grade}, Temp: &struct{ Data floats }{temp},
		LatLng: &struct{ Data [][]float64 }{ll}, Moving: &struct{ Data []bool }{moving},
	}
}

func TestComputeMetrics_synthRide(t *testing.T) {
	s := buildSeries(synthRide(false))
	m := computeMetrics(s, 0, s.n, powerEdges(athleteZones{}, 250), []float64{124, 155, 170, 180})
	near(t, "distance", m.DistanceM, 10000, 10)
	near(t, "avg W", m.AvgW, (200*(5000/(30/3.6)+3000/(30/3.6))+300*2000/(15/3.6))/float64(m.MovingS), 2)
	near(t, "max W", m.MaxW, 300, 0)
	near(t, "gain", m.GainM, 120, 1)
	if m.MeanMax[0] != 300 || !math.IsNaN(m.MeanMax[len(m.MeanMax)-1]) {
		t.Errorf("mean-max = %v", m.MeanMax)
	}
	if m.PowerZoneS[2] == 0 || m.PowerZoneS[4] == 0 {
		t.Errorf("power zones = %v: want time in Z3 (200 W) and Z5 (300 W)", m.PowerZoneS)
	}
	// 130/135 bpm is Z2, 170 on the climb Z4.
	if !m.HasHRZones || m.HRZoneS[1] == 0 || m.HRZoneS[3] == 0 || m.HRZoneS[0] != 0 {
		t.Errorf("hr zones = %v", m.HRZoneS)
	}
	near(t, "EF", m.EF, m.NP/m.AvgHR, 1e-9)
	// 170 bpm on the climb is the best 10 s; the ride is 24 min, so it has a
	// best 20 min, between the flat 130/135 and the climb's 170.
	near(t, "HR max 10 s", m.HRMax10, 170, 0)
	if m.HR20m <= 135 || m.HR20m >= 170 {
		t.Errorf("HR best 20 min = %v", m.HR20m)
	}
	st := storedFrom(m, time.Time{})
	if st.Vals["hr_max10"] != 170 || st.Vals["hr_20m"] != math.Round(m.HR20m*100)/100 || st.Rev != entryRev {
		t.Errorf("stored HR keys: %v (rev %d)", st.Vals, st.Rev)
	}
	// The power of the same 20 minutes as the best HR, not the best 20 min
	// of power: the ride's last 20 min, climb included, at 200..300 W.
	if w := st.Vals["hr_20m_w"]; w <= 200 || w >= 300 {
		t.Errorf("power over the best-HR 20 min = %v", w)
	}
	// Under 20 minutes: a best 10 s, no best 20 min; no HR stream: neither.
	a0, b0 := s.sub(5000, 7000)
	if st := storedFrom(computeMetrics(s, a0, b0, nil, nil), time.Time{}); st.Vals["hr_max10"] != 170 {
		t.Errorf("section HR keys: %v", st.Vals)
	} else if _, has := st.Vals["hr_20m"]; has {
		t.Errorf("a 8 min section has a best 20 min: %v", st.Vals)
	}
	noHR := synthRide(false)
	noHR.HR = nil
	sn := buildSeries(noHR)
	if st := storedFrom(computeMetrics(sn, 0, sn.n, nil, nil), time.Time{}); st.Vals["hr_max10"] != 0 || st.Vals["hr_20m"] != 0 {
		t.Errorf("HR keys without an HR stream: %v", st.Vals)
	}

	cl := climbs(s, 0, s.n, 75)
	if len(cl) != 1 {
		t.Fatalf("climbs = %+v, want one", cl)
	}
	c := cl[0]
	near(t, "climb from", c.FromM, 5000, 100)
	near(t, "climb to", c.ToM, 7000, 100)
	near(t, "climb grade", c.GradePct, 6, 0.3)
	near(t, "climb W", c.AvgW, 300, 10)
	near(t, "W/kg", c.WKg, c.AvgW/75, 1e-9)
	near(t, "VAM", c.VAM, c.GainM/(float64(c.Secs)/3600), 1e-9)

	// 300 W is 120 % of a 250 W FTP: the climb is the one hard effort.
	ef := efforts(s, 0, s.n, 250)
	if len(ef) != 1 || ef[0].B-ef[0].A < 400 {
		t.Fatalf("efforts = %+v", ef)
	}
	if efforts(s, 0, s.n, 0) != nil {
		t.Error("efforts without FTP")
	}

	rows, width := kmRows(s, 0, s.n, 1000)
	if len(rows) != 10 || width != 1000 {
		t.Fatalf("km rows = %d (width %v)", len(rows), width)
	}
	near(t, "km 6 grade", rows[5].GradePct, 6, 0.5)

	// A section: the climb alone.
	a, b := s.sub(5000, 7000)
	sm := computeMetrics(s, a, b, nil, nil)
	near(t, "section W", sm.AvgW, 300, 1)
	near(t, "section distance", sm.DistanceM, 2000, 20)
}

// A GPS dropout that jumps past a whole bucket (3.3 → 5.6 km) skips that
// bucket's row and keeps the rows after it.
func TestKmRows_dropout(t *testing.T) {
	var tm, dist floats
	for i := range 1000 {
		d := float64(i) * 10
		if d > 3300 {
			d += 2300
		}
		tm, dist = append(tm, float64(i)), append(dist, d)
	}
	s := buildSeries(streamSet{Time: &struct{ Data floats }{tm}, Distance: &struct{ Data floats }{dist}})
	rows, _ := kmRows(s, 0, s.n, 1000)
	var ends []string
	for _, r := range rows {
		ends = append(ends, num(r.ToM/1000, 2))
	}
	// km 4–5 has no sample: no row; km 3–4 ends where the signal went.
	if got := strings.Join(ends, " "); got != "0.99 1.99 2.99 3.30 5.99 6.99 7.99 8.99 9.99 10.99 11.99 12.29" {
		t.Errorf("row ends = %s", got)
	}
}

func TestDecoupling(t *testing.T) {
	// Constant 200 W; HR 140 in the first half, 150 in the second:
	// (200/140 - 200/150) / (200/140) = 1 - 140/150.
	n := 1200
	hr := constant(n, 140)
	for i := n / 2; i < n; i++ {
		hr[i] = 150
	}
	tm := make(floats, n)
	for i := range tm {
		tm[i] = float64(i)
	}
	s := buildSeries(streamSet{Time: &struct{ Data floats }{tm}, Watts: &struct{ Data floats }{constant(n, 200)}, HR: &struct{ Data floats }{hr}})
	m := computeMetrics(s, 0, s.n, nil, nil)
	near(t, "decoupling", m.Decoupling, (1-140.0/150)*100, 1e-9)
	near(t, "EF", m.EF, 200/145.0, 1e-9)

	// A run without power: speed per bpm.
	s = buildSeries(streamSet{Time: &struct{ Data floats }{tm}, Velocity: &struct{ Data floats }{constant(n, 3)}, HR: &struct{ Data floats }{hr}})
	m = computeMetrics(s, 0, s.n, nil, nil)
	near(t, "run decoupling", m.Decoupling, (1-140.0/150)*100, 1e-9)
	near(t, "run EF", m.EF, 180/145.0, 1e-9)

	// Too short to say anything.
	s = buildSeries(streamSet{Time: &struct{ Data floats }{tm[:60]}, Watts: &struct{ Data floats }{constant(60, 200)}, HR: &struct{ Data floats }{hr[:60]}})
	m = computeMetrics(s, 0, s.n, nil, nil)
	near(t, "short decoupling", m.Decoupling, math.NaN(), 0)
	near(t, "short EF", m.EF, math.NaN(), 0)
}

func TestBuildSeries_gapsAndNulls(t *testing.T) {
	var w floats
	if err := jsonUnmarshal(`[100, null, 300, 400]`, &w); err != nil {
		t.Fatal(err)
	}
	if !math.IsNaN(w[1]) {
		t.Fatalf("null = %v, want NaN", w[1])
	}
	set := streamSet{
		Time:  &struct{ Data floats }{floats{0, 1, 4, 100}},
		Watts: &struct{ Data floats }{w},
		HR:    &struct{ Data floats }{floats{math.NaN(), 120, 130, 140}},
	}
	s := buildSeries(set)
	// 0, 1, (2, 3 filled), 4, then a pause: 100 follows directly.
	if s.n != 6 {
		t.Fatalf("n = %d, elapsed %v", s.n, s.elapsed)
	}
	if s.elapsed[5] != 100 || s.elapsed[3] != 3 {
		t.Errorf("elapsed = %v", s.elapsed)
	}
	if s.watts[1] != 0 {
		t.Errorf("a power dropout is zero, got %v", s.watts[1])
	}
	if s.hr[0] != 120 {
		t.Errorf("a leading HR gap takes the first value, got %v", s.hr[0])
	}
	if s.dist != nil || s.speed != nil {
		t.Error("streams the activity lacks must stay nil")
	}
	if buildSeries(streamSet{}).n != 0 {
		t.Error("no time stream: empty series")
	}

	// A pause leaves no zero samples: NP is the riding's.
	paused := buildSeries(synthRide(true))
	plain := buildSeries(synthRide(false))
	if paused.n != plain.n {
		t.Errorf("pause added samples: %d vs %d", paused.n, plain.n)
	}
	if paused.elapsed[paused.n-1] != plain.elapsed[plain.n-1]+300 {
		t.Error("elapsed must keep the pause")
	}
}

// A pause leaves no samples, so stopped (recorded, not moving) never sees
// it: paused is the clock the recording skipped.
func TestComputeMetrics_paused(t *testing.T) {
	// 0..599 s running, 120 s paused, 720..1199 s running; 30 s standing
	// still (recorded) at 300..329.
	var tm floats
	for i := range 1200 {
		if i < 600 || i >= 720 {
			tm = append(tm, float64(i))
		}
	}
	speed := constant(len(tm), 3)
	for i := 300; i < 330; i++ {
		speed[i] = 0
	}
	s := buildSeries(streamSet{Time: &struct{ Data floats }{tm}, Velocity: &struct{ Data floats }{speed}})
	m := computeMetrics(s, 0, s.n, nil, nil)
	if m.PausedS != 120 || m.StoppedS != 30 || m.MovingS != 1050 {
		t.Errorf("paused %d, stopped %d, moving %d; want 120, 30, 1050", m.PausedS, m.StoppedS, m.MovingS)
	}
	// A section that skips the pause has none; a filled dropout is no pause.
	if m := computeMetrics(s, 0, 600, nil, nil); m.PausedS != 0 {
		t.Errorf("section before the pause: paused %d", m.PausedS)
	}
	s = buildSeries(streamSet{Time: &struct{ Data floats }{floats{0, 1, 2, 8, 9}}, Velocity: &struct{ Data floats }{constant(5, 3)}})
	if m := computeMetrics(s, 0, s.n, nil, nil); m.PausedS != 0 {
		t.Errorf("an interpolated dropout counted as paused: %d", m.PausedS)
	}
}

func TestMedianAndRank(t *testing.T) {
	near(t, "odd", median([]float64{3, 1, 2}), 2, 0)
	near(t, "even", median([]float64{4, 1, 3, 2}), 2.5, 0)
	near(t, "unknown skipped", median([]float64{math.NaN(), 5}), 5, 0)
	near(t, "none", median(nil), math.NaN(), 0)
	vals := []float64{10, 20, 30, math.NaN()}
	for _, c := range []struct {
		this       float64
		kind, r, n int
		flag       string
	}{
		{35, kindPerf, 1, 4, "best in window"},
		{5, kindPerf, 4, 4, "worst in window"},
		{35, kindVolume, 1, 4, "highest in window"},
		{5, kindVolume, 4, 4, "lowest in window"},
		{5, kindLower, 1, 4, "best in window"},
		{25, kindPerf, 2, 4, ""},
		{35, kindNeutral, 1, 4, ""},
		{20, kindPerf, 2, 4, ""}, // a tie shares the better place
		{30, kindPerf, 1, 4, ""}, // a shared first place is no "best"
	} {
		r, n, tied := rank(c.this, vals, c.kind)
		if r != c.r || n != c.n || flag(c.kind, r, n, tied) != c.flag {
			t.Errorf("rank(%v, kind %d) = %d of %d %q, want %d of %d %q", c.this, c.kind, r, n, flag(c.kind, r, n, tied), c.r, c.n, c.flag)
		}
	}
	if flag(kindPerf, 1, 2, false) != "" {
		t.Error("two activities are too few for a flag")
	}
}

// testdata/albis_streams.json.gz is the design prototype's simulated 74 km
// ride (1 Hz, three stops), as Strava sends streams; the values are the
// prototype's (pandas) on the same rounded data.
func TestPrototypeRide(t *testing.T) {
	fh, err := os.Open("testdata/albis_streams.json.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = fh.Close() }()
	zr, err := gzip.NewReader(fh)
	if err != nil {
		t.Fatal(err)
	}
	var set streamSet
	if err := json.NewDecoder(zr).Decode(&set); err != nil {
		t.Fatal(err)
	}
	s := buildSeries(set)
	m := computeMetrics(s, 0, s.n, powerEdges(athleteZones{}, 265), nil)
	near(t, "NP", m.NP, 208.16788615451375, 1e-6)
	near(t, "avg W", m.AvgW, 183.09934115019544, 1e-6)
	near(t, "avg HR", m.AvgHR, 144.8607146845338, 1e-6)
	near(t, "TSS", tss(m.MovingS, m.NP, 265), 153.49661616912923, 1e-6)
	near(t, "best 20 min", m.MeanMax[slices.Index(curveDurations, 1200)], 260.38233333333335, 1e-6)
	near(t, "best 5 s", m.MeanMax[slices.Index(curveDurations, 5)], 675.64, 1e-6)
	near(t, "decoupling", m.Decoupling, 7.38, 0.1)
	if m.MovingS != 8955 {
		t.Errorf("moving = %d", m.MovingS)
	}
	cl := climbs(s, 0, s.n, 74)
	if len(cl) != 2 {
		t.Fatalf("climbs = %+v", cl)
	}
	for i, want := range [][2]float64{{17900, 24300}, {47000, 51800}} {
		near(t, "climb from", cl[i].FromM, want[0], 200)
		near(t, "climb to", cl[i].ToM, want[1], 200)
	}
}

func TestHaversine(t *testing.T) {
	near(t, "1 km north", haversine(latlng{47, 8}, latlng{47 + 1000/111195.0, 8}), 1000, 1)
}
