// Package geo is the point arithmetic the weather sidecar and loom share:
// which of a name's places lies at a point, and how coarse a point leaves.
// No dependencies, so the sidecar pulls in nothing of loom's.
package geo

import "math"

// NearKm is how far a place may lie from a point and still be the one meant
// there: the sidecar names a point's town by it, loom keeps that town by it
// on save. One constant, or a town named could be refused on save.
const NearKm = 25

// DistanceKm is the great-circle distance between two points.
func DistanceKm(lat1, lon1, lat2, lon2 float64) float64 {
	const earthKm = 6371.0
	rad := math.Pi / 180
	dLat, dLon := (lat2-lat1)*rad, (lon2-lon1)*rad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * earthKm * math.Asin(math.Min(1, math.Sqrt(a)))
}

// Nearest is the index of the one of n places (at(i) its point) nearest
// lat, lon, if it lies within NearKm; -1 when none does.
func Nearest(lat, lon float64, n int, at func(i int) (float64, float64)) int {
	best, bestKm := -1, math.Inf(1)
	for i := range n {
		pLat, pLon := at(i)
		if d := DistanceKm(lat, lon, pLat, pLon); d <= NearKm && d < bestKm {
			best, bestKm = i, d
		}
	}
	return best
}

// Coarse rounds a coordinate to two decimals, about a kilometre: a town is
// all a position is asked for, and every point within one is the same.
func Coarse(v float64) float64 { return math.Round(v*100) / 100 }
