package main

import (
	"math"
	"strings"
	"testing"
)

// poolSwim is the layout of a real stored pool swim: a 200 m lap, then
// single lengths with rest laps (0 m) between them.
func poolSwim() detailedActivity {
	d := detailedActivity{summaryActivity: summaryActivity{SportType: "Swim", Distance: 300, MovingTime: 430, AverageCadence: 22}}
	d.Laps = []lap{
		{Distance: 200, MovingTime: 316, ElapsedTime: 316, AverageCadence: 15},
		{Distance: 0, MovingTime: 97, ElapsedTime: 97},
		{Distance: 25, MovingTime: 35, ElapsedTime: 35, AverageCadence: 29},
		{Distance: 0, MovingTime: 11, ElapsedTime: 11},
		{Distance: 25, MovingTime: 36, ElapsedTime: 36, AverageCadence: 30},
		{Distance: 0, MovingTime: 10, ElapsedTime: 10},
		{Distance: 50, MovingTime: 49, ElapsedTime: 49, AverageCadence: 29},
	}
	return d
}

func TestSwimFigures(t *testing.T) {
	f, ok := computeSwim(poolSwim(), series{}, 0)
	if !ok || f.Pool != 25 || f.PoolAssumed || f.Open {
		t.Fatalf("pool %d assumed %v open %v ok %v", f.Pool, f.PoolAssumed, f.Open, ok)
	}
	if len(f.Laps) != 4 || f.Lengths != 12 || f.RestS != 118 {
		t.Errorf("laps %d lengths %d rest %d", len(f.Laps), f.Lengths, f.RestS)
	}
	l := f.Laps[1] // 25 m in 35 s at 29 strokes a minute
	if !within(l.Strokes, 29*35/60.0, 1e-9) || !within(l.SWOLF, 35+29*35/60.0, 1e-9) || l.RestAfter != 11 {
		t.Errorf("lap: strokes %.2f SWOLF %.2f rest %d", l.Strokes, l.SWOLF, l.RestAfter)
	}
	strokes := (15*316 + 29*35 + 30*36 + 29*49) / 60.0
	if !within(f.Strokes, strokes, 1e-9) || !within(f.SPL, strokes/12, 1e-9) || !within(f.DPS, 300/strokes, 1e-9) ||
		!within(f.SWOLF, (316+35+36+49)/12.0+strokes/12, 1e-9) {
		t.Errorf("totals: strokes %.1f SPL %.2f DPS %.2f SWOLF %.1f", f.Strokes, f.SPL, f.DPS, f.SWOLF)
	}

	// Every lap a multiple of 50: 25 assumed, pool_m 50 overrides.
	d := poolSwim()
	d.Laps = []lap{{Distance: 100, MovingTime: 100, AverageCadence: 30}, {Distance: 50, MovingTime: 50, AverageCadence: 30}}
	if f, _ := computeSwim(d, series{}, 0); f.Pool != 25 || !f.PoolAssumed || f.Lengths != 6 {
		t.Errorf("multiples of 50: pool %d assumed %v lengths %d", f.Pool, f.PoolAssumed, f.Lengths)
	}
	if f, _ := computeSwim(d, series{}, 50); f.Pool != 50 || f.PoolAssumed || f.Lengths != 3 {
		t.Errorf("pool_m 50: pool %d assumed %v lengths %d", f.Pool, f.PoolAssumed, f.Lengths)
	}

	// A 25 yd pool: Strava keeps its laps in metres (200, 100, 25 yd).
	ydSwim := poolSwim()
	ydSwim.Laps = []lap{{Distance: 182.88, MovingTime: 200, AverageCadence: 30}, {Distance: 0, ElapsedTime: 30},
		{Distance: 91.44, MovingTime: 90, AverageCadence: 30}, {Distance: 22.86, MovingTime: 20, AverageCadence: 30}}
	if f, _ := computeSwim(ydSwim, series{}, 0); !f.Yards || f.PoolAssumed || f.Lengths != 13 || f.RestS != 30 {
		t.Errorf("25 yd: yards %v assumed %v lengths %d rest %d", f.Yards, f.PoolAssumed, f.Lengths, f.RestS)
	}

	// No stroke rate: lengths and pace stay, nothing stroke-made.
	for i := range d.Laps {
		d.Laps[i].AverageCadence = 0
	}
	d.AverageCadence = 0
	if f, _ := computeSwim(d, series{}, 0); !math.IsNaN(f.Strokes) || !math.IsNaN(f.SWOLF) || f.Lengths != 6 {
		t.Errorf("without cadence: strokes %v SWOLF %v lengths %d", f.Strokes, f.SWOLF, f.Lengths)
	}

	// Open water: a GPS track, no lengths, no SWOLF.
	ow := detailedActivity{summaryActivity: summaryActivity{SportType: "Swim", Distance: 1500, MovingTime: 1800, AverageCadence: 26}}
	f, _ = computeSwim(ow, series{n: 2, lat: []float64{47, 47.001}}, 0)
	if !f.Open || f.Lengths != 0 || !math.IsNaN(f.SWOLF) || !within(f.DPS, 1500/(26*1800/60.0), 1e-9) {
		t.Errorf("open water: %+v", f)
	}

	// Imperial: swim distances in yards, never feet.
	var yd strings.Builder
	f, _ = computeSwim(poolSwim(), series{}, 0)
	writeSwimSummary(&yd, f, poolSwim(), units{imperial: true})
	if !strings.Contains(yd.String(), "328 yd") || strings.Contains(yd.String(), " ft") {
		t.Errorf("imperial swim:\n%s", yd.String())
	}

	var b strings.Builder
	f, _ = computeSwim(poolSwim(), series{}, 0)
	writeSwimSummary(&b, f, poolSwim(), units{})
	for _, want := range []string{"swim summary tables", "estimated from Strava's average stroke rate", "strokes ≈ ", "SWOLF ≈ ",
		"12 lengths of 25 m", "stroke rate 22 /min", "intervals", "1. 200 m", "rest 1:37", "pool 25 m: a lap is not a multiple of 50 m"} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("lacks %q:\n%s", want, b.String())
		}
	}
}
