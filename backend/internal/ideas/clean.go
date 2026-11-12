package ideas

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"
	"unicode"
)

// Field caps in runes, enforced on every write path (REST and the idea
// tools): an idea is a note, not a document.
const (
	MaxTitleRunes   = 80
	MaxReasonRunes  = 500
	MaxExcerptRunes = 1200
)

// dateLayout is the 'YYYY-MM-DD' of due_on and remind_on: a civil date in
// the user's zone, no time.
const dateLayout = "2006-01-02"

// Breaks unicode.IsControl does not see (unicode.IsSpace does).
const (
	lineSeparator      = rune(0x2028)
	paragraphSeparator = rune(0x2029)
	nextLine           = rune(0x0085)
)

// ellipsis ends a cut field.
const ellipsis = string(rune(0x2026))

// CleanLine makes text one line of at most max runes: every space and break
// (unicode.IsSpace, which covers U+2028, U+2029 and U+0085) becomes one
// space, other control and bidi override characters go. The idea tools print
// title and reason as data lines: a newline in one must not start a line of
// its own.
func CleanLine(s string, max int) string {
	var b strings.Builder
	space := false
	for _, r := range s {
		switch {
		case unicode.IsSpace(r):
			space = b.Len() > 0
			continue
		case dropRune(r):
			continue
		}
		if space {
			b.WriteByte(' ')
			space = false
		}
		b.WriteRune(r)
	}
	return truncate(b.String(), max)
}

// CleanText keeps line breaks (an excerpt is a passage) but drops the other
// control and bidi override characters and trims the ends.
func CleanText(s string, max int) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\r' || r == lineSeparator || r == paragraphSeparator || r == nextLine:
			b.WriteByte('\n')
		case r == '\n' || r == '\t' || !dropRune(r):
			b.WriteRune(r)
		}
	}
	return truncate(strings.TrimSpace(b.String()), max)
}

// dropRune is a control character or an invisible bidi override, which can
// make shown text read differently from what is stored.
func dropRune(r rune) bool {
	if unicode.IsControl(r) {
		return true
	}
	return unicode.Is(unicode.Bidi_Control, r) || r == 0xFEFF // BOM, zero-width no-break space
}

// truncate cuts s to max runes, the last one an ellipsis when it cut.
func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return strings.TrimRightFunc(string(r[:max-1]), unicode.IsSpace) + ellipsis
}

// ParseDate reads a 'YYYY-MM-DD' date.
func ParseDate(s string) (time.Time, error) {
	return time.Parse(dateLayout, strings.TrimSpace(s))
}

// FormatDate writes t's civil date as 'YYYY-MM-DD'.
func FormatDate(t time.Time) string {
	return t.Format(dateLayout)
}

// DefaultRemindOn is the follow-up date for a due date, computed by the
// server, never a model: a week before; when that is not after today, the
// day before; when that is not after today either, today. A due date that
// has passed gets none (ok false): the idea reads "due date passed".
func DefaultRemindOn(due, today time.Time) (time.Time, bool) {
	if due.Before(today) {
		return time.Time{}, false
	}
	for _, days := range []int{7, 1} {
		if d := due.AddDate(0, 0, -days); d.After(today) {
			return d, true
		}
	}
	return today, true
}

// OptionalDate is a date field with three states: absent (Set false: keep,
// or the server's default), null (Set, Value nil: none) and a date.
type OptionalDate struct {
	Set   bool
	Value *string
}

// UnmarshalJSON runs only for a key that is present, so it marks Set.
func (d *OptionalDate) UnmarshalJSON(raw []byte) error {
	d.Set = true
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		d.Value = nil
		return nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return err
	}
	d.Value = &s
	return nil
}

// DateOf is a set date, as an OptionalDate.
func DateOf(s string) OptionalDate {
	return OptionalDate{Set: true, Value: &s}
}
