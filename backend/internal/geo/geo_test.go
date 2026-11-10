package geo

import (
	"math"
	"testing"
)

func TestDistanceKm(t *testing.T) {
	// Zürich to Bern, about 95 km.
	if d := DistanceKm(47.37, 8.54, 46.95, 7.45); math.Abs(d-95) > 3 {
		t.Fatalf("Zürich-Bern = %.1f km", d)
	}
	if d := DistanceKm(1, 2, 1, 2); d != 0 {
		t.Fatalf("same point = %v", d)
	}
}

func TestNearest(t *testing.T) {
	points := [][2]float64{{48.76, 8.24}, {47.47, 8.31}, {47.48, 8.32}}
	at := func(i int) (float64, float64) { return points[i][0], points[i][1] }
	if i := Nearest(47.477, 8.318, len(points), at); i != 2 {
		t.Fatalf("nearest = %d", i)
	}
	if i := Nearest(38.7, -9.1, len(points), at); i != -1 {
		t.Fatalf("none near = %d", i)
	}
}

func TestCoarse(t *testing.T) {
	if Coarse(47.477123) != 47.48 || Coarse(-9.13456) != -9.13 {
		t.Fatal("not rounded to two decimals")
	}
}
