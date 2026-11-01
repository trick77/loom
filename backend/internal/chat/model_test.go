package chat

import "testing"

func TestMemoryLengthCaps(t *testing.T) {
	if MaxProjectMemoryLength != 3000 {
		t.Fatalf("MaxProjectMemoryLength = %d, want 3000", MaxProjectMemoryLength)
	}
}

func TestUserDirectiveBudgetCaps(t *testing.T) {
	if MaxUserDirectivesTotalLength != 3000 {
		t.Fatalf("MaxUserDirectivesTotalLength = %d, want 3000", MaxUserDirectivesTotalLength)
	}
	if MaxUserDirectiveLength != 1000 {
		t.Fatalf("MaxUserDirectiveLength = %d, want 1000", MaxUserDirectiveLength)
	}
}
