package imagegen

import "math"

// falMegapixel is the megapixel fal bills by (its pricing text: "1 megapixel
// is 1024 × 1024").
const falMegapixel = 1024 * 1024

// falPerMegapixel is a model's price on fal: the first output megapixel, then
// each further megapixel of output and of input images, each image rounded up
// to whole megapixels.
type falPerMegapixel struct {
	first, further float64
}

// falPrices are fal's published per-image prices (model pages, read
// 2026-11-22). fal reports no price per request (queue result, response
// headers, usage API by request id: none), so the figure booked is this
// table's: a model missing here books nothing, and a price change at fal goes
// unnoticed until the table follows it. [max] states no input rate; its
// output rate is assumed, as [pro] charges input like output.
var falPrices = map[string]falPerMegapixel{
	"fal-ai/flux-2-pro": {first: 0.03, further: 0.015},
	"fal-ai/flux-2-max": {first: 0.07, further: 0.03},
}

// gptImage2Prices is fal's per-image table for openai/gpt-image-2 at
// gptImageQuality (medium; read 2026-11-22), by output size. fal bills it by
// OpenAI's tokens, so the price is not linear in pixels: a square costs more
// than a larger landscape (1024×1024 $0.053, 1920×1080 $0.040). A size takes
// the row of its shape nearest in area; the table has one square and one
// portrait row, so a ~2 MP square or portrait books that row's price, an
// estimate. An edit's input tokens are not in the table and go unbooked.
var gptImage2Prices = []struct {
	w, h int
	usd  float64
}{
	{1024, 768, 0.037},
	{1024, 1024, 0.053},
	{1024, 1536, 0.042},
	{1920, 1080, 0.040},
	{2560, 1440, 0.056},
	{3840, 2160, 0.101},
}

// falImageCost is what fal bills in USD for one image of w×h made by model
// from inputs (each a source image's width and height); false for a model
// without a known price.
func falImageCost(model string, w, h int, inputs [][2]int) (float64, bool) {
	if model == "openai/gpt-image-2" {
		return nearestGPTImage2Price(w, h), true
	}
	price, ok := falPrices[model]
	if !ok {
		return 0, false
	}
	further := megapixels(w, h) - 1
	for _, in := range inputs {
		further += megapixels(in[0], in[1])
	}
	// Rounded to a millionth of a dollar: keeps float noise out of the booked figure.
	return math.Round((price.first+float64(further)*price.further)*1e6) / 1e6, true
}

// nearestGPTImage2Price is the price of the gptImage2Prices row of w×h's shape
// nearest to it in area. Every shape has a row.
func nearestGPTImage2Price(w, h int) float64 {
	best, bestDelta := 0.0, -1
	for _, row := range gptImage2Prices {
		if shapeOf(row.w, row.h) != shapeOf(w, h) {
			continue
		}
		if delta := abs(row.w*row.h - w*h); bestDelta < 0 || delta < bestDelta {
			best, bestDelta = row.usd, delta
		}
	}
	return best
}

// shapeOf is -1 portrait, 0 square (within 10%), 1 landscape.
func shapeOf(w, h int) int {
	switch {
	case w*10 > h*11:
		return 1
	case h*10 > w*11:
		return -1
	default:
		return 0
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// megapixels is w×h in fal's billed megapixels, rounded up, at least one.
func megapixels(w, h int) int {
	return max(1, (w*h+falMegapixel-1)/falMegapixel)
}
