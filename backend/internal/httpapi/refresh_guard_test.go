package httpapi

import "testing"

func TestInflightKeysAcquireIsExclusiveUntilReleased(t *testing.T) {
	var keys inflightKeys

	release, ok := keys.tryAcquire("memory:user:u1")
	if !ok {
		t.Fatal("first tryAcquire() = false, want true")
	}
	if _, ok := keys.tryAcquire("memory:user:u1"); ok {
		t.Fatal("second tryAcquire() on a held key = true, want false")
	}
	if other, ok := keys.tryAcquire("memory:user:u2"); !ok {
		t.Fatal("tryAcquire() on a different key = false, want true")
	} else {
		other()
	}
	release()
	if again, ok := keys.tryAcquire("memory:user:u1"); !ok {
		t.Fatal("tryAcquire() after release = false, want true")
	} else {
		again()
	}
}
