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

// falImageCost is what fal bills in USD for one image of w×h made by model
// from inputs (each a source image's width and height); false for a model
// without a known price.
func falImageCost(model string, w, h int, inputs [][2]int) (float64, bool) {
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

// megapixels is w×h in fal's billed megapixels, rounded up, at least one.
func megapixels(w, h int) int {
	return max(1, (w*h+falMegapixel-1)/falMegapixel)
}
