package chat

import "testing"

func TestMemoryLengthCaps(t *testing.T) {
	if MaxUserMemoryLength != 3000 {
		t.Fatalf("MaxUserMemoryLength = %d, want 3000", MaxUserMemoryLength)
	}
	if MaxProjectMemoryLength != 3000 {
		t.Fatalf("MaxProjectMemoryLength = %d, want 3000", MaxProjectMemoryLength)
	}
}
