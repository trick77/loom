package imagegen

import (
	"context"
	"math"
	"testing"
)

func TestFalImageCost(t *testing.T) {
	cases := []struct {
		name   string
		model  string
		w, h   int
		inputs [][2]int
		want   float64
		priced bool
	}{
		{"pro under one MP", "fal-ai/flux-2-pro", 1024, 576, nil, 0.03, true},
		{"pro one MP", "fal-ai/flux-2-pro", 1024, 1024, nil, 0.03, true},
		{"pro 2.25 MP rounds up to 3", "fal-ai/flux-2-pro", 2048, 1152, nil, 0.06, true},
		{"pro edit adds the source's MP", "fal-ai/flux-2-pro", 1024, 576, [][2]int{{1920, 1080}}, 0.06, true},
		{"max under one MP", "fal-ai/flux-2-max", 1024, 576, nil, 0.07, true},
		{"max 2.25 MP", "fal-ai/flux-2-max", 2048, 1152, nil, 0.13, true},
		{"max edit adds the source's MP", "fal-ai/flux-2-max", 1024, 1024, [][2]int{{1024, 1024}}, 0.10, true},
		{"unknown model has no price", "fal-ai/some-other", 1024, 1024, nil, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := falImageCost(tc.model, tc.w, tc.h, tc.inputs)
			if ok != tc.priced || math.Abs(got-tc.want) > 1e-9 {
				t.Fatalf("falImageCost = %v, %v; want %v, %v", got, ok, tc.want, tc.priced)
			}
		})
	}
}

// The client reports what fal will bill for the request it made: fal's result
// carries no price.
func TestFalClientReportsCost(t *testing.T) {
	stub := newFalStub(t)
	client := stub.client(stub.start())
	result, err := client.Generate(context.Background(), GenerateRequest{Prompt: "a fox", Width: 1024, Height: 576})
	if err != nil {
		t.Fatal(err)
	}
	if result.CostUSD == nil || math.Abs(*result.CostUSD-0.03) > 1e-9 {
		t.Fatalf("CostUSD = %v, want 0.03", result.CostUSD)
	}
}
