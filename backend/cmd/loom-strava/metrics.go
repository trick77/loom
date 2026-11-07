package main

import (
	"math"
	"sort"
)

// series is one activity's streams on a 1 Hz grid of recorded time: short
// recording gaps (smart recording, a dropout) are interpolated, a pause
// longer than maxInterpGap is left out, so a café stop never counts as zero
// watts in NP. elapsed keeps the real clock for every sample. A stream the
// activity lacks is nil.
type series struct {
	n       int
	elapsed []float64 // s since the start
	dist    []float64 // m
	lat     []float64
	lng     []float64
	alt     []float64 // m
	speed   []float64 // m/s
	hr      []float64 // bpm
	cad     []float64 // rpm (spm for runs)
	watts   []float64
	grade   []float64 // %
	temp    []float64 // °C
	moving  []bool
}

// maxInterpGap is the longest recording gap filled in; longer is a pause.
const maxInterpGap = 10

// curveDurations are the mean-max durations stored and shown, in seconds.
var curveDurations = []int{1, 5, 15, 30, 60, 120, 300, 600, 1200, 1800, 3600, 5400}

func buildSeries(set streamSet) series {
	if set.Time == nil || len(set.Time.Data) == 0 {
		return series{}
	}
	tm := set.Time.Data
	src := func(p *struct{ Data floats }) []float64 {
		if p == nil || len(p.Data) == 0 {
			return nil
		}
		return p.Data
	}
	in := map[string][]float64{
		"dist": src(set.Distance), "alt": src(set.Altitude), "speed": src(set.Velocity), "hr": src(set.HR),
		"cad": src(set.Cadence), "watts": src(set.Watts), "grade": src(set.Grade), "temp": src(set.Temp),
	}
	if set.LatLng != nil && len(set.LatLng.Data) > 0 {
		lat := make([]float64, len(set.LatLng.Data))
		lng := make([]float64, len(set.LatLng.Data))
		for i, p := range set.LatLng.Data {
			lat[i], lng[i] = math.NaN(), math.NaN()
			if len(p) == 2 {
				lat[i], lng[i] = p[0], p[1]
			}
		}
		in["lat"], in["lng"] = lat, lng
	}
	var mov []bool
	if set.Moving != nil {
		mov = set.Moving.Data
	}
	// Dropouts in power and cadence mean nothing was pushed: zero. Elsewhere
	// the last value holds.
	for k, v := range in {
		if v != nil {
			in[k] = fillNaN(v, k == "watts" || k == "cad")
		}
	}

	out := map[string][]float64{}
	var elapsed []float64
	var moving []bool
	t0 := tm[0]
	at := func(v []float64, i int) float64 {
		if i < len(v) {
			return v[i]
		}
		if len(v) > 0 {
			return v[len(v)-1]
		}
		return 0
	}
	push := func(i int, frac float64, t float64) {
		elapsed = append(elapsed, t-t0)
		for k, v := range in {
			if v == nil {
				continue
			}
			x := at(v, i)
			if frac > 0 && i+1 < len(v) {
				x += (v[i+1] - x) * frac
			}
			out[k] = append(out[k], x)
		}
		m := true
		if i < len(mov) {
			m = mov[i]
		}
		moving = append(moving, m)
	}
	for i := range tm {
		if math.IsNaN(tm[i]) {
			continue
		}
		push(i, 0, tm[i])
		if i+1 < len(tm) {
			gap := tm[i+1] - tm[i]
			if gap > 1 && gap <= maxInterpGap {
				for k := 1; k < int(gap); k++ {
					push(i, float64(k)/gap, tm[i]+float64(k))
				}
			}
		}
	}
	s := series{n: len(elapsed), elapsed: elapsed, moving: moving,
		dist: out["dist"], lat: out["lat"], lng: out["lng"], alt: out["alt"], speed: out["speed"], hr: out["hr"],
		cad: out["cad"], watts: out["watts"], grade: out["grade"], temp: out["temp"]}
	if s.speed == nil && s.dist != nil {
		s.speed = make([]float64, s.n)
		for i := 1; i < s.n; i++ {
			if dt := s.elapsed[i] - s.elapsed[i-1]; dt > 0 {
				s.speed[i] = max(0, (s.dist[i]-s.dist[i-1])/dt)
			}
		}
	}
	if set.Moving == nil && s.speed != nil {
		for i := range s.moving {
			s.moving[i] = s.speed[i] > 0.5
		}
	}
	return s
}

// fillNaN replaces NaN with zero, or with the last (first, at the start)
// valid value.
func fillNaN(v []float64, zero bool) []float64 {
	out := make([]float64, len(v))
	copy(out, v)
	last := math.NaN()
	for i, x := range out {
		switch {
		case !math.IsNaN(x):
			last = x
		case zero:
			out[i] = 0
		default:
			out[i] = last
		}
	}
	if zero {
		return out
	}
	first := math.NaN()
	for _, x := range out {
		if !math.IsNaN(x) {
			first = x
			break
		}
	}
	if math.IsNaN(first) {
		return nil // nothing valid at all: the stream is missing
	}
	for i := 0; i < len(out) && math.IsNaN(out[i]); i++ {
		out[i] = first
	}
	return out
}

// sub is the samples whose distance lies in [fromM, toM); the whole series
// when it has no distance.
func (s series) sub(fromM, toM float64) (int, int) {
	if s.dist == nil {
		return 0, s.n
	}
	a, b := s.n, s.n
	for i, d := range s.dist {
		if d >= fromM && a == s.n {
			a = i
		}
		if d >= toM {
			b = i
			break
		}
	}
	if a > b {
		a = b
	}
	return a, b
}

// rideMetrics is everything computed from one stretch of streams. NaN means
// unknown: the stream is missing or the stretch too short.
type rideMetrics struct {
	// StoppedS is recorded time not moving; PausedS the clock the recording
	// skipped (watch paused, auto-pause): a pause has no samples, so stopped
	// alone never shows it.
	Samples, MovingS, StoppedS, PausedS int
	DistanceM                           float64
	HasWatts, HasHR                     bool
	AvgW, MaxW, NP, KJ                  float64
	AvgHR, MaxHR                        float64
	// HRMax10 and HR20m are the best 10 s and 20 min mean heart rate: the
	// inputs of hr_zones' max and threshold heart rate. 10 s, not the
	// single highest sample, so one strap spike does not count.
	HRMax10, HR20m float64
	// HR20mW is the mean power over those same 20 min: whether the best-HR
	// stretch was a threshold effort.
	HR20mW float64
	// EF is NP per bpm with power, else moving speed (m/min) per bpm.
	EF, Decoupling            float64
	AvgCad, MaxCad            float64
	AvgTemp, MinTemp, MaxTemp float64
	MaxGrade, MinGrade        float64
	GainM                     float64
	AvgSpeed, MaxSpeed        float64 // m/s, moving
	MeanMax                   []float64
	PowerZoneS, HRZoneS       [5]int
	HasPowerZones, HasHRZones bool
}

var nan = math.NaN()

func computeMetrics(s series, a, b int, pEdges, hEdges []float64) rideMetrics {
	m := rideMetrics{AvgW: nan, MaxW: nan, NP: nan, KJ: nan, AvgHR: nan, MaxHR: nan, HRMax10: nan, HR20m: nan, HR20mW: nan, EF: nan, Decoupling: nan,
		AvgCad: nan, MaxCad: nan, AvgTemp: nan, MinTemp: nan, MaxTemp: nan, MaxGrade: nan, MinGrade: nan,
		GainM: nan, AvgSpeed: nan, MaxSpeed: nan, MeanMax: make([]float64, len(curveDurations))}
	for i := range m.MeanMax {
		m.MeanMax[i] = nan
	}
	if b <= a {
		return m
	}
	m.Samples = b - a
	var movIdx []int
	for i := a; i < b; i++ {
		if s.moving[i] {
			movIdx = append(movIdx, i)
		}
	}
	m.MovingS = len(movIdx)
	m.StoppedS = m.Samples - m.MovingS
	m.PausedS = max(0, int(math.Round(s.elapsed[b-1]-s.elapsed[a]))+1-m.Samples)
	if s.dist != nil {
		m.DistanceM = s.dist[b-1] - s.dist[a]
	}
	if s.watts != nil {
		w := s.watts[a:b]
		m.HasWatts = true
		m.NP = normalizedPower(w)
		m.AvgW = meanAt(s.watts, movIdx)
		m.MaxW = maxOf(w)
		m.KJ = sum(w) / 1000
		for i, d := range curveDurations {
			m.MeanMax[i] = meanMax(w, d)
		}
		if pEdges != nil {
			r := rolling(s.watts[a:b], 30)
			m.HasPowerZones = true
			for _, i := range movIdx {
				m.PowerZoneS[zoneOf(r[i-a], pEdges)-1]++
			}
		}
	}
	if s.hr != nil {
		m.HasHR = true
		m.AvgHR = meanAt(s.hr, movIdx)
		m.MaxHR = maxOf(s.hr[a:b])
		m.HRMax10 = meanMax(s.hr[a:b], 10)
		var at int
		m.HR20m, at = bestWindow(s.hr[a:b], 1200)
		if s.watts != nil && !math.IsNaN(m.HR20m) {
			m.HR20mW = mean(s.watts[a+at : a+at+1200])
		}
		if hEdges != nil {
			m.HasHRZones, m.HRZoneS = true, hrZoneTimes(s, a, b, hEdges)
		}
	}
	if s.speed != nil {
		m.AvgSpeed = meanAt(s.speed, movIdx)
		m.MaxSpeed = maxOf(s.speed[a:b])
	}
	if s.cad != nil {
		var nz []int
		for _, i := range movIdx {
			if s.cad[i] > 0 {
				nz = append(nz, i)
			}
		}
		m.AvgCad = meanAt(s.cad, nz)
		m.MaxCad = maxOf(s.cad[a:b])
	}
	if s.temp != nil {
		m.AvgTemp, m.MinTemp, m.MaxTemp = mean(s.temp[a:b]), minOf(s.temp[a:b]), maxOf(s.temp[a:b])
	}
	if s.grade != nil {
		m.MinGrade, m.MaxGrade = minOf(s.grade[a:b]), maxOf(s.grade[a:b])
	}
	if s.alt != nil {
		m.GainM = 0
		for i := a + 1; i < b; i++ {
			m.GainM += max(0, s.alt[i]-s.alt[i-1])
		}
	}
	if m.HasHR && m.AvgHR > 0 && len(movIdx) >= minEfficiencyS {
		switch {
		case m.HasWatts:
			m.EF = m.NP / m.AvgHR
		case !math.IsNaN(m.AvgSpeed):
			m.EF = m.AvgSpeed * 60 / m.AvgHR
		}
	}
	m.Decoupling = decoupling(s, movIdx)
	return m
}

// minEfficiencyS: EF and decoupling of a couple of minutes say nothing.
const minEfficiencyS = 120

// decoupling is the aerobic decoupling between the halves of the moving
// samples, (EF1-EF2)/EF1 in %: power per bpm when there is power, speed per
// bpm otherwise (a run). Positive means the heart rate drifted up.
func decoupling(s series, movIdx []int) float64 {
	if s.hr == nil || len(movIdx) < minEfficiencyS {
		return nan
	}
	ofHalf := func(idx []int) float64 {
		hr := meanAt(s.hr, idx)
		if hr <= 0 {
			return nan
		}
		if s.watts != nil {
			w := make([]float64, len(idx))
			for k, i := range idx {
				w[k] = s.watts[i]
			}
			return normalizedPower(w) / hr
		}
		if s.speed != nil {
			return meanAt(s.speed, idx) * 60 / hr
		}
		return nan
	}
	h := len(movIdx) / 2
	ef1, ef2 := ofHalf(movIdx[:h]), ofHalf(movIdx[h:])
	if math.IsNaN(ef1) || math.IsNaN(ef2) || ef1 == 0 {
		return nan
	}
	return (ef1 - ef2) / ef1 * 100
}

// rolling is the trailing mean over n samples, over fewer at the start.
func rolling(v []float64, n int) []float64 {
	out := make([]float64, len(v))
	acc := 0.0
	for i, x := range v {
		acc += x
		if i >= n {
			acc -= v[i-n]
		}
		out[i] = acc / float64(min(i+1, n))
	}
	return out
}

// normalizedPower: the fourth root of the mean of the 30 s rolling power to
// the fourth.
func normalizedPower(w []float64) float64 {
	if len(w) == 0 {
		return nan
	}
	r := rolling(w, 30)
	acc := 0.0
	for _, x := range r {
		acc += x * x * x * x
	}
	return math.Pow(acc/float64(len(r)), 0.25)
}

// meanMax is the best average over any d consecutive samples; NaN when the
// stretch is shorter.
func meanMax(w []float64, d int) float64 {
	best, _ := bestWindow(w, d)
	return best
}

// bestWindow is the highest mean of d consecutive samples and where that
// window starts; NaN when w is shorter than d.
func bestWindow(w []float64, d int) (float64, int) {
	if d <= 0 || d > len(w) {
		return nan, 0
	}
	acc := sum(w[:d])
	best, at := acc, 0
	for i := d; i < len(w); i++ {
		acc += w[i] - w[i-d]
		if acc > best {
			best, at = acc, i-d+1
		}
	}
	return best / float64(d), at
}

// intensity and tss use the FTP of the moment; NaN without one.
func intensity(np, ftp float64) float64 {
	if ftp <= 0 || math.IsNaN(np) {
		return nan
	}
	return np / ftp
}

func tss(movingS int, np, ftp float64) float64 {
	if ftp <= 0 || math.IsNaN(np) {
		return nan
	}
	return float64(movingS) * np * (np / ftp) / (ftp * 3600) * 100
}

// hrZoneTimes is the moving seconds of a..b in each HR zone of hEdges; s
// has an HR stream.
func hrZoneTimes(s series, a, b int, hEdges []float64) [5]int {
	var z [5]int
	for i := a; i < b; i++ {
		if s.moving[i] {
			z[zoneOf(s.hr[i], hEdges)-1]++
		}
	}
	return z
}

// zoneOf is the 1..5 zone of v given the lower bounds of zones 2..5.
func zoneOf(v float64, edges []float64) int {
	z := 1
	for _, e := range edges {
		if v >= e {
			z++
		}
	}
	return min(z, 5)
}

// powerEdges are the lower bounds of power zones 2..5: Strava's own zones
// when it sends them (its Z5..Z7 fold into our 5), else Coggan's from FTP.
func powerEdges(z athleteZones, ftp float64) []float64 {
	if z.Power != nil && len(z.Power.Zones) >= 5 {
		e := make([]float64, 4)
		for i := range e {
			e[i] = z.Power.Zones[i+1].Min
		}
		return e
	}
	if ftp > 0 {
		return []float64{0.55 * ftp, 0.75 * ftp, 0.90 * ftp, 1.05 * ftp}
	}
	return nil
}

func hrEdges(z athleteZones) []float64 {
	if z.HeartRate != nil && len(z.HeartRate.Zones) >= 5 {
		e := make([]float64, 4)
		for i := range e {
			e[i] = z.HeartRate.Zones[i+1].Min
		}
		return e
	}
	return nil
}

// climb is a stretch of at least minClimbM at minClimbGrade or more.
type climb struct {
	FromM, ToM, GainM, GradePct float64
	Secs                        int
	VAM, AvgW, WKg, AvgHR       float64
}

const (
	climbGridM     = 100
	minClimbM      = 500
	minClimbGrade  = 0.03
	keepClimbGrade = 0.02
	// climbMergeM and climbMergeDropM: two climbs this close, with no more
	// lost between them, are one.
	climbMergeM     = 300
	climbMergeDropM = 10
)

// climbs finds the climbs on a 100 m grid: a climb starts on a step of at
// least 3 %, runs on while steps stay at 2 % or more (a short flat or dip
// bridged), and counts when it is 500 m or longer at 3 % or more on
// average.
func climbs(s series, a, b int, weight float64) []climb {
	if s.dist == nil || s.alt == nil || b-a < 2 {
		return nil
	}
	d0, d1 := s.dist[a], s.dist[b-1]
	var km, alt []float64
	j := a
	for d := d0; d <= d1; d += climbGridM {
		for j < b-1 && s.dist[j+1] < d {
			j++
		}
		x := s.alt[j]
		if j+1 < b && s.dist[j+1] > s.dist[j] {
			x += (s.alt[j+1] - s.alt[j]) * (d - s.dist[j]) / (s.dist[j+1] - s.dist[j])
		}
		km, alt = append(km, d), append(alt, x)
	}
	type span struct{ i, k int } // grid indices of the foot and the top
	var spans []span
	for i := 0; i+1 < len(km); {
		if (alt[i+1]-alt[i])/climbGridM < minClimbGrade {
			i++
			continue
		}
		k := i
		for k+1 < len(km) && (alt[k+1]-alt[k])/climbGridM >= keepClimbGrade {
			k++
		}
		// A flat bit or a dip of a few metres (a hairpin, altimeter noise)
		// does not end a climb.
		if n := len(spans); n > 0 && km[i]-km[spans[n-1].k] <= climbMergeM && alt[i] >= alt[spans[n-1].k]-climbMergeDropM {
			spans[n-1].k = k
		} else {
			spans = append(spans, span{i, k})
		}
		i = max(k, i+1)
	}
	var out []climb
	for _, sp := range spans {
		from, to := km[sp.i], km[sp.k]
		gain := alt[sp.k] - alt[sp.i]
		if to-from >= minClimbM && gain/(to-from) >= minClimbGrade {
			ca, cb := s.sub(from, to)
			c := climb{FromM: from, ToM: to, GainM: gain, GradePct: gain / (to - from) * 100, Secs: cb - ca,
				AvgW: nan, WKg: nan, AvgHR: nan, VAM: nan}
			if c.Secs > 0 {
				c.VAM = gain / (float64(c.Secs) / 3600)
				if s.watts != nil {
					c.AvgW = mean(s.watts[ca:cb])
					if weight > 0 {
						c.WKg = c.AvgW / weight
					}
				}
				if s.hr != nil {
					c.AvgHR = mean(s.hr[ca:cb])
				}
			}
			out = append(out, c)
		}
	}
	return out
}

// effort is a hard stretch: at least minEffortS with the 10 s power above
// effortFTP of FTP, gaps of up to effortGapS bridged.
type effort struct {
	A, B                      int // sample range
	AvgW, AvgHR, EndHR, MaxHR float64
}

const (
	minEffortS = 30
	effortGapS = 5
	effortFTP  = 1.05
)

func efforts(s series, a, b int, ftp float64) []effort {
	if s.watts == nil || ftp <= 0 || b-a < minEffortS {
		return nil
	}
	r := rolling(s.watts[a:b], 10)
	thr := effortFTP * ftp
	var out []effort
	start, lastHigh := -1, -1
	flush := func() {
		if start >= 0 && lastHigh-start+1 >= minEffortS {
			e := effort{A: a + start, B: a + lastHigh + 1, AvgHR: nan, EndHR: nan, MaxHR: nan}
			e.AvgW = mean(s.watts[e.A:e.B])
			if s.hr != nil {
				e.AvgHR, e.EndHR, e.MaxHR = mean(s.hr[e.A:e.B]), s.hr[e.B-1], maxOf(s.hr[e.A:e.B])
			}
			out = append(out, e)
		}
		start, lastHigh = -1, -1
	}
	for i, x := range r {
		if x > thr {
			if start < 0 {
				start = i
			}
			lastHigh = i
		} else if start >= 0 && i-lastHigh > effortGapS {
			flush()
		}
	}
	flush()
	return out
}

// kmRow is one distance bucket of the per-km series.
type kmRow struct {
	ToM, Secs, SpeedMS, HR, W, Cad, AltM, GradePct float64
}

// maxKmRows bounds the per-km series; longer activities get wider buckets.
const maxKmRows = 200

func kmRows(s series, a, b int, unitM float64) ([]kmRow, float64) {
	if s.dist == nil || b-a < 2 {
		return nil, unitM
	}
	span := s.dist[b-1] - s.dist[a]
	width := unitM
	for span/width > maxKmRows {
		width *= 2
	}
	var out []kmRow
	start := a
	for k := 1; start < b; k++ {
		edge := s.dist[a] + float64(k)*width
		end := start
		for end < b && s.dist[end] < edge {
			end++
		}
		if end == start {
			// A GPS dropout jumped past this whole bucket: no row for it,
			// the rows after it still come.
			continue
		}
		var mov []int
		for i := start; i < end; i++ {
			if s.moving[i] {
				mov = append(mov, i)
			}
		}
		// Absolute distance, a section's rows too: they say where it happened.
		row := kmRow{ToM: s.dist[end-1], Secs: float64(end - start), SpeedMS: nan, HR: nan, W: nan, Cad: nan, AltM: nan, GradePct: nan}
		if s.speed != nil {
			row.SpeedMS = meanAt(s.speed, mov)
		}
		if s.hr != nil {
			row.HR = meanAt(s.hr, mov)
		}
		if s.watts != nil {
			row.W = meanAt(s.watts, mov)
		}
		if s.cad != nil {
			row.Cad = meanAt(s.cad, mov)
		}
		if s.alt != nil {
			row.AltM = s.alt[end-1]
			if d := s.dist[end-1] - s.dist[start]; d > 0 {
				row.GradePct = (s.alt[end-1] - s.alt[start]) / d * 100
			}
		}
		out = append(out, row)
		start = end
	}
	return out, width
}

// elapsedAt is the elapsed time at which the activity first reached each
// distance step; NaN past its end.
func elapsedAt(s series, stepM float64, n int) []float64 {
	out := make([]float64, n)
	j := 0
	for k := range out {
		d := float64(k) * stepM
		for j < s.n && s.dist[j] < d {
			j++
		}
		if j >= s.n {
			out[k] = nan
			continue
		}
		out[k] = s.elapsed[j]
	}
	return out
}

func sum(v []float64) float64 {
	acc := 0.0
	for _, x := range v {
		acc += x
	}
	return acc
}

func mean(v []float64) float64 {
	if len(v) == 0 {
		return nan
	}
	return sum(v) / float64(len(v))
}

func meanAt(v []float64, idx []int) float64 {
	if len(idx) == 0 {
		return nan
	}
	acc := 0.0
	for _, i := range idx {
		acc += v[i]
	}
	return acc / float64(len(idx))
}

func maxOf(v []float64) float64 {
	m := nan
	for _, x := range v {
		if math.IsNaN(m) || x > m {
			m = x
		}
	}
	return m
}

func minOf(v []float64) float64 {
	m := nan
	for _, x := range v {
		if math.IsNaN(m) || x < m {
			m = x
		}
	}
	return m
}

// median of the known values; NaN when there are none.
func median(v []float64) float64 {
	var k []float64
	for _, x := range v {
		if !math.IsNaN(x) {
			k = append(k, x)
		}
	}
	if len(k) == 0 {
		return nan
	}
	sort.Float64s(k)
	if len(k)%2 == 1 {
		return k[len(k)/2]
	}
	return (k[len(k)/2-1] + k[len(k)/2]) / 2
}

// haversine is the distance between two points in m.
func haversine(a, b latlng) float64 {
	const r = 6371000.0
	rad := math.Pi / 180
	dLat, dLng := (b[0]-a[0])*rad, (b[1]-a[1])*rad
	h := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(a[0]*rad)*math.Cos(b[0]*rad)*math.Sin(dLng/2)*math.Sin(dLng/2)
	return 2 * r * math.Asin(math.Sqrt(h))
}
