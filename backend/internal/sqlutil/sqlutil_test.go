package sqlutil

import (
	"testing"
	"time"
)

func TestParseTimeAcceptsSQLiteAndRFC3339FormsInUTC(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want time.Time
	}{
		{"2024-05-06 07:08:09", time.Date(2024, 5, 6, 7, 8, 9, 0, time.UTC)},
		{"2024-05-06T07:08:09Z", time.Date(2024, 5, 6, 7, 8, 9, 0, time.UTC)},
		{"2024-05-06T09:08:09.5+02:00", time.Date(2024, 5, 6, 7, 8, 9, 500_000_000, time.UTC)},
	} {
		got, err := ParseTime(tc.in)
		if err != nil {
			t.Fatalf("ParseTime(%q) error: %v", tc.in, err)
		}
		if !got.Equal(tc.want) || got.Location() != time.UTC {
			t.Fatalf("ParseTime(%q) = %v (%v), want %v UTC", tc.in, got, got.Location(), tc.want)
		}
	}
	if _, err := ParseTime("yesterday"); err == nil {
		t.Fatal("ParseTime(garbage) error = nil, want error")
	}
}

func TestEscapeLike(t *testing.T) {
	if got := EscapeLike(`50%_off\`); got != `50\%\_off\\` {
		t.Fatalf("EscapeLike() = %q", got)
	}
}

func TestNewIDIsOpaqueAndUnique(t *testing.T) {
	a, b := NewID(), NewID()
	if len(a) != 22 || len(b) != 22 {
		t.Fatalf("id length = %d/%d, want 22", len(a), len(b))
	}
	if a == b {
		t.Fatal("two ids collided")
	}
}
