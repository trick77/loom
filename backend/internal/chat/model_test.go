package chat

import "testing"

func TestMemoryLengthCaps(t *testing.T) {
	// User memory carries the richer two-layer digest (work/personal context, top
	// of mind, brief history), so its budget is larger than project memory's.
	if MaxUserMemoryLength != 6000 {
		t.Fatalf("MaxUserMemoryLength = %d, want 6000", MaxUserMemoryLength)
	}
	if MaxProjectMemoryLength != 3000 {
		t.Fatalf("MaxProjectMemoryLength = %d, want 3000", MaxProjectMemoryLength)
	}
}

func TestUserDirectiveBudgetCaps(t *testing.T) {
	if MaxUserDirectivesTotalLength != 1000 {
		t.Fatalf("MaxUserDirectivesTotalLength = %d, want 1000", MaxUserDirectivesTotalLength)
	}
	if MaxUserDirectiveLength != 1000 {
		t.Fatalf("MaxUserDirectiveLength = %d, want 1000", MaxUserDirectiveLength)
	}
}
