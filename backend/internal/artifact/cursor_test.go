package artifact

import "testing"

func TestEffectiveArtifactLimit(t *testing.T) {
	if got := EffectiveArtifactLimit(0); got != 100 {
		t.Fatalf("EffectiveArtifactLimit(0) = %d, want the 100-row default", got)
	}
	if got := EffectiveArtifactLimit(5000); got != maxArtifactLimit {
		t.Fatalf("EffectiveArtifactLimit(5000) = %d, want the %d maximum", got, maxArtifactLimit)
	}
	if got := EffectiveArtifactLimit(25); got != 25 {
		t.Fatalf("EffectiveArtifactLimit(25) = %d, want 25", got)
	}
}
