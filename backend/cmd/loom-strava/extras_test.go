package main

import (
	"math"
	"testing"
)

// hrSeries is a 1 Hz series of heart rate hr (watts w where given).
func hrSeries(hr, w []float64) series {
	n := len(hr)
	s := series{n: n, hr: hr, watts: w, elapsed: make([]float64, n), skipped: make([]float64, n), moving: make([]bool, n),
		dist: make([]float64, n)}
	for i := range n {
		s.elapsed[i], s.moving[i], s.dist[i] = float64(i), true, float64(i)*3
	}
	return s
}

func TestHRRecovery(t *testing.T) {
	hr := make([]float64, 400)
	for i := range hr {
		switch {
		case i < 100:
			hr[i] = 120
		case i < 300:
			hr[i] = 170 // hard
		default:
			hr[i] = 170 - math.Min(30, float64(i-300)*0.5) // down 30 in 60 s
		}
	}
	s := hrSeries(hr, nil)
	if r := hrRecoveryAt(s, 300); !within(r.Drop, 30, 1.5) || !within(r.From, 170, 1) {
		t.Errorf("drop %v from %v, want 30 from 170", r.Drop, r.From)
	}
	if r := hrRecoveryAt(s, 380); finite(r.Drop) {
		t.Errorf("past the end: %v, want none", r.Drop)
	}
	s.skipped[330] = 40 // the watch paused right after
	if r := hrRecoveryAt(s, 300); finite(r.Drop) {
		t.Errorf("across a pause: %v, want none", r.Drop)
	}
}

func TestCriticalPower(t *testing.T) {
	// P(t) = CP + W'/t with CP 250 W, W' 20 kJ.
	best := map[int]float64{}
	for _, d := range []int{120, 300, 600, 1200} {
		best[d] = 250 + 20000/float64(d)
	}
	cp, wp, ok := criticalPower(best)
	if !ok || !within(cp, 250, 0.5) || !within(wp, 20000, 50) {
		t.Errorf("CP %v W' %v ok %v", cp, wp, ok)
	}
	if _, _, ok := criticalPower(map[int]float64{300: 300}); ok {
		t.Error("a fit from one duration")
	}
	// W'bal: 60 s at CP + 100 W spends 6 kJ, then it refills below CP.
	w := make([]float64, 400)
	for i := range w {
		w[i] = 200
		if i >= 100 && i < 160 {
			w[i] = 350
		}
	}
	bal := wBalance(w, nil, 250, 20000)
	if !within(bal[159], 14000, 150) || bal[399] <= bal[159] || bal[399] > 20000 {
		t.Errorf("W'bal at the end of the effort %v, later %v", bal[159], bal[399])
	}
	// A 20-minute pause right after the effort refills it.
	skipped := make([]float64, len(w))
	skipped[160] = 1200
	if paused := wBalance(w, skipped, 250, 20000); paused[160] < 19900 {
		t.Errorf("after a 20-minute pause W'bal %v, want near full", paused[160])
	}
}

func TestCSS(t *testing.T) {
	laps := []lap{{Distance: 400, MovingTime: 360}, {Distance: 200, MovingTime: 170}, {Distance: 400, MovingTime: 380}, {Distance: 25, MovingTime: 20}}
	// A 200 kick far slower than the 400's pace: no CSS from that pair.
	if _, ok := cssOf([]lap{{Distance: 400, MovingTime: 360}, {Distance: 200, MovingTime: 300}}); ok {
		t.Error("CSS from a kick set")
	}
	ms, ok := cssOf(laps)
	if !ok || !within(ms, 200.0/(360-170), 1e-9) {
		t.Errorf("CSS %v %v, want 200/190 m/s", ms, ok)
	}
	if _, ok := cssOf(laps[1:2]); ok {
		t.Error("CSS without a 400")
	}
	// A yard pool: 400 yd and 200 yd, kept in metres.
	yd := []lap{{Distance: 365.76, MovingTime: 330}, {Distance: 182.88, MovingTime: 155}}
	if ms, ok := cssOf(yd); !ok || !within(ms, 182.88/175, 1e-9) {
		t.Errorf("yard CSS %v %v", ms, ok)
	}
	if !poolLaps(laps) || !poolLaps(yd) || poolLaps([]lap{{Distance: 437, MovingTime: 400}}) {
		t.Error("pool laps")
	}
	// A pause just past the minute: no recovery either.
	hr := make([]float64, 400)
	for i := range hr {
		hr[i] = 150
	}
	s := hrSeries(hr, nil)
	s.skipped[300+hrrS+1] = 30
	if r := hrRecoveryAt(s, 300); finite(r.Drop) {
		t.Errorf("a pause in the averaged seconds: %v", r.Drop)
	}
}

func TestGradeProfile(t *testing.T) {
	// Flat at 3 m/s, +5 % at 2.4 m/s, −5 % at 3.3 m/s.
	var runs []series
	for range 3 {
		n := 900
		s := series{n: n, speed: make([]float64, n), grade: make([]float64, n), moving: make([]bool, n), skipped: make([]float64, n)}
		for i := range n {
			s.moving[i] = true
			switch {
			case i < 300:
				s.speed[i], s.grade[i] = 3, 0
			case i < 600:
				s.speed[i], s.grade[i] = 2.4, 5
			default:
				s.speed[i], s.grade[i] = 3.3, -5
			}
		}
		runs = append(runs, s)
	}
	gp := &gradeProfile{}
	for _, s := range runs {
		gp.add(s)
	}
	if !within(gp.rel(5), 0.8, 1e-9) || !within(gp.rel(-5), 1.1, 1e-9) || !within(gp.rel(0), 1, 1e-9) {
		t.Errorf("rel +5 %v, −5 %v, 0 %v", gp.rel(5), gp.rel(-5), gp.rel(0))
	}
	// This run's own grade-adjusted speed: every stretch at the flat 3 m/s.
	if v, cov := gp.adjusted(runs[0]); !within(v, 3, 1e-9) || cov != 1 {
		t.Errorf("own GAP %v (covering %v), want 3 over all of it", v, cov)
	}
	if !math.IsNaN(gp.rel(14)) {
		t.Error("a grade nobody ran has no factor")
	}
}
