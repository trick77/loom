package main

import (
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Strava limits the app, not the user: every connected user spends the same
// budget, so the bookkeeping is app-wide. Its defaults for a new app's reads,
// used until a response says otherwise.
const (
	defaultRead15   = 100
	defaultReadDay  = 1000
	defaultAll15    = 200
	defaultAllDay   = 2000
	refuseFraction  = 0.9
	window15Minutes = 15
)

// usage is one limit pair as Strava last reported it: the 15-minute window
// and the day, both in UTC.
type usage struct {
	limit15, limitDay int
	used15, usedDay   int
	at                time.Time
}

// current is the usage counted in now's windows: a report from an earlier
// window says nothing about this one, Strava reset it.
func (u usage) current(now time.Time) (used15, usedDay int) {
	if !u.at.IsZero() && quarterStart(u.at).Equal(quarterStart(now)) {
		used15 = u.used15
	}
	if !u.at.IsZero() && dayStart(u.at).Equal(dayStart(now)) {
		usedDay = u.usedDay
	}
	return used15, usedDay
}

type rateBook struct {
	mu   sync.Mutex
	read usage // X-ReadRateLimit-*: GETs
	all  usage // X-RateLimit-*: every request
}

func newRateBook() *rateBook {
	return &rateBook{
		read: usage{limit15: defaultRead15, limitDay: defaultReadDay},
		all:  usage{limit15: defaultAll15, limitDay: defaultAllDay},
	}
}

// observe records a response's rate headers. A response without them (a
// proxy's error page) still spent one request: counted by hand.
func (b *rateBook) observe(h http.Header, now time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	update(&b.read, h.Get("X-ReadRateLimit-Limit"), h.Get("X-ReadRateLimit-Usage"), now)
	update(&b.all, h.Get("X-RateLimit-Limit"), h.Get("X-RateLimit-Usage"), now)
}

func update(u *usage, limit, used string, now time.Time) {
	if l15, lDay, ok := pair(limit); ok && l15 > 0 && lDay > 0 {
		u.limit15, u.limitDay = l15, lDay
	}
	if u15, uDay, ok := pair(used); ok {
		u.used15, u.usedDay, u.at = u15, uDay, now
		return
	}
	c15, cDay := u.current(now)
	u.used15, u.usedDay, u.at = c15+1, cDay+1, now
}

// exhaust records a 429: Strava says the budget is gone, whatever the
// counts said.
func (b *rateBook) exhaust(h http.Header, now time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	update(&b.read, h.Get("X-ReadRateLimit-Limit"), h.Get("X-ReadRateLimit-Usage"), now)
	update(&b.all, h.Get("X-RateLimit-Limit"), h.Get("X-RateLimit-Usage"), now)
	for _, u := range []*usage{&b.read, &b.all} {
		c15, cDay := u.current(now)
		if cDay >= u.limitDay {
			continue // the day is out: blocked reports the day's reset
		}
		u.used15, u.usedDay, u.at = max(c15, u.limit15), cDay, now
	}
}

// blocked reports whether calls must stop, above refuseFraction of either
// window, and when the binding window resets.
func (b *rateBook) blocked(now time.Time) (time.Time, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	var reset time.Time
	for _, u := range []usage{b.read, b.all} {
		c15, cDay := u.current(now)
		if float64(cDay) >= refuseFraction*float64(u.limitDay) {
			if r := dayStart(now).AddDate(0, 0, 1); r.After(reset) {
				reset = r
			}
		} else if float64(c15) >= refuseFraction*float64(u.limit15) {
			if r := quarterStart(now).Add(window15Minutes * time.Minute); r.After(reset) {
				reset = r
			}
		}
	}
	return reset, !reset.IsZero()
}

// fraction15 is how much of the 15-minute read budget is spent now.
func (b *rateBook) fraction15(now time.Time) float64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	c15, _ := b.read.current(now)
	a15, _ := b.all.current(now)
	return max(float64(c15)/float64(b.read.limit15), float64(a15)/float64(b.all.limit15))
}

// pair parses Strava's "15-minute,daily" header value.
func pair(v string) (int, int, bool) {
	a, b, ok := strings.Cut(v, ",")
	if !ok {
		return 0, 0, false
	}
	x, err1 := strconv.Atoi(strings.TrimSpace(a))
	y, err2 := strconv.Atoi(strings.TrimSpace(b))
	if err1 != nil || err2 != nil || x < 0 || y < 0 {
		return 0, 0, false
	}
	return x, y, true
}

// quarterStart is the start of t's 15-minute window: Strava resets at :00,
// :15, :30 and :45 UTC.
func quarterStart(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute()/window15Minutes*window15Minutes, 0, 0, time.UTC)
}

// dayStart is midnight UTC of t's day, when Strava resets the daily count.
func dayStart(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}
