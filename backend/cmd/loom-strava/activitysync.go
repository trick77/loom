package main

import (
	"context"
	"log/slog"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"time"
)

// The activity store under the user's dataMu: the index is loaded once per
// slot, and every change of it, or of a stored body, happens under the lock
// disconnect deletes them under, so nothing is written back after. Index
// changes are batched (dirty) and written at the end of a sync or a tool
// call: a cold baseline stores dozens of streams.

// athleteOf is the athlete the user's connection reads, 0 when unknown.
func (s *service) athleteOf(user string) int64 {
	c, _ := s.conn(user)
	return c.AthleteID
}

// keepsLocked says whether what was read for athlete may still be stored
// for user: still connected, and still that athlete. Caller holds dataMu.
func (s *service) keepsLocked(user string, athlete int64) bool {
	c, ok := s.conn(user)
	return ok && (athlete == 0 || c.AthleteID == 0 || c.AthleteID == athlete)
}

// indexLocked is the user's index, loaded on first use. Another athlete's
// starts over. Caller holds dataMu.
func (s *service) indexLocked(user string, sl *slot) *activityIndex {
	if sl.index != nil {
		return sl.index
	}
	athlete := s.athleteOf(user)
	x := s.store.loadIndex(user)
	if x != nil && x.Athlete != 0 && athlete != 0 && x.Athlete != athlete {
		// The bodies go with the index: none would be counted or found.
		if err := s.store.deleteActivities(user); err != nil {
			slog.Error("strava: deleting the stored activities failed", "err", err)
		}
		x = nil
	}
	if x == nil {
		x = newIndex(athlete)
	}
	sl.index = x
	return x
}

// flushLocked writes the index when it changed. Caller holds dataMu.
func (s *service) flushLocked(user string, sl *slot) {
	if !sl.indexDirty || sl.index == nil {
		return
	}
	if err := s.store.saveIndex(user, sl.index); err != nil {
		// Only a cost: the list is read from Strava again.
		slog.Error("strava: saving the activity index failed", "err", err)
		return
	}
	sl.indexDirty = false
}

// flushIndex writes what a tool call changed in the index.
func (s *service) flushIndex(user string) {
	sl := s.slot(user)
	sl.dataMu.Lock()
	defer sl.dataMu.Unlock()
	if s.keepsLocked(user, 0) {
		s.flushLocked(user, sl)
	}
}

// deleteStoreLocked removes everything kept for the user's activities and
// metrics. Caller holds dataMu.
func (s *service) deleteStoreLocked(user string, sl *slot) error {
	sl.index, sl.indexDirty, sl.refreshed = nil, false, nil
	if err := s.store.deleteActivities(user); err != nil {
		return err
	}
	return s.store.deleteMetrics(user)
}

// loadBody is a stored answer for one activity. A stored stream's read is
// noted for the eviction order (in memory: written with the next index
// change, not worth a rewrite of its own); a stream the index lost is
// counted again, one gone from disk leaves the count.
func (s *service) loadBody(user string, id int64, kind string) ([]byte, bool) {
	b, ok := s.store.readBody(user, id, kind)
	if kind != kindStreams {
		return b, ok
	}
	sl := s.slot(user)
	sl.dataMu.Lock()
	defer sl.dataMu.Unlock()
	x := s.indexLocked(user, sl)
	key := strconv.FormatInt(id, 10)
	m, known := x.Streams[key]
	p, err := s.store.bodyPath(user, id, kind)
	if err != nil {
		return b, ok
	}
	switch {
	case ok && known:
		m.ReadAt = s.now().UTC()
		x.Streams[key] = m
	case ok:
		if fi, err := os.Stat(p); err == nil {
			x.Streams[key] = streamMeta{Bytes: fi.Size(), ReadAt: s.now().UTC()}
			s.evictStreamsLocked(user, x, key)
			sl.indexDirty = true
		}
	case known:
		if _, err := os.Stat(p); err != nil {
			delete(x.Streams, key)
			sl.indexDirty = true
		}
	}
	return b, ok
}

// keepBody stores one activity's answer, replacing the one before, unless
// the user left or became another athlete since athlete was read.
func (s *service) keepBody(user string, athlete, id int64, kind string, body []byte) {
	sl := s.slot(user)
	sl.dataMu.Lock()
	defer sl.dataMu.Unlock()
	if !s.keepsLocked(user, athlete) {
		return
	}
	n, err := s.store.writeBody(user, id, kind, body)
	if err != nil {
		slog.Error("strava: storing an activity failed", "kind", kind, "err", err)
		return
	}
	if kind != kindStreams {
		return
	}
	x := s.indexLocked(user, sl)
	key := strconv.FormatInt(id, 10)
	x.Streams[key] = streamMeta{Bytes: n, ReadAt: s.now().UTC()}
	s.evictStreamsLocked(user, x, key)
	sl.indexDirty = true
}

// evictStreamsLocked drops the least recently read streams past
// maxStreamBytes, never keep.
func (s *service) evictStreamsLocked(user string, x *activityIndex, keep string) {
	for x.streamBytes() > maxStreamBytes {
		oldest := ""
		for k, m := range x.Streams {
			if k != keep && (oldest == "" || m.ReadAt.Before(x.Streams[oldest].ReadAt)) {
				oldest = k
			}
		}
		if oldest == "" {
			return
		}
		if id, err := strconv.ParseInt(oldest, 10, 64); err == nil {
			s.store.removeBodies(user, id, kindStreams)
		}
		delete(x.Streams, oldest)
	}
}

// noteActivity puts a summary read with a detail into the index: what was
// read last is what the list shows.
func (s *service) noteActivity(user string, athlete int64, a summaryActivity) {
	sl := s.slot(user)
	sl.dataMu.Lock()
	defer sl.dataMu.Unlock()
	if !s.keepsLocked(user, athlete) {
		return
	}
	x := s.indexLocked(user, sl)
	if x.Synced.IsZero() {
		// No list read yet: the backfill brings it, and an index with one
		// old activity would read the whole time since again.
		return
	}
	key := strconv.FormatInt(a.ID, 10)
	old, had := x.Activities[key]
	if had && reflect.DeepEqual(old, a) {
		return
	}
	if !had {
		x.Seen[key] = s.now().UTC() // new to the list: maybe just uploaded
	}
	x.Activities[key] = a
	sl.indexDirty = true // written at the end of the tool call
}

// discardBody removes a stored answer a newer read did not replace (one
// not stored, as not settled): the old one must not come back.
func (s *service) discardBody(user string, id int64, kind string) {
	sl := s.slot(user)
	sl.dataMu.Lock()
	defer sl.dataMu.Unlock()
	s.store.removeBodies(user, id, kind)
}

// dropLocked forgets activities Strava no longer has: stored files, index
// rows, memory and metrics. Caller holds dataMu.
func (s *service) dropLocked(user string, sl *slot, ids []int64) {
	if len(ids) == 0 {
		return
	}
	x := s.indexLocked(user, sl)
	uc := s.cache(user)
	var m metricFile
	var mErr error
	loaded, metrics := false, false
	touched := map[string]bool{} // sports of dropped entries: their zones move
	for _, id := range ids {
		key := strconv.FormatInt(id, 10)
		uc.detail.remove(kindDetail + "/" + key)
		uc.detail.remove(kindZones + "/" + key)
		uc.streams.remove(kindStreams + "/" + key)
		_, listed := x.Activities[key]
		_, streamed := x.Streams[key]
		if !listed && !streamed && !s.storedAny(user, id) {
			continue // never kept (a 404 for a mistyped id): nothing to touch
		}
		s.store.removeBodies(user, id, kindDetail, kindStreams, kindZones)
		delete(x.Activities, key)
		delete(x.Streams, key)
		delete(x.Seen, key)
		sl.indexDirty = true
		if !loaded {
			m, mErr = s.store.loadMetrics(user)
			loaded = true
		}
		if sm, ok := m.Activities[key]; ok && mErr == nil {
			touched[sm.Sport] = true
			delete(m.Activities, key)
			metrics = true
		}
	}
	if metrics {
		// A deleted ride may have set the max or the LTHR.
		recalcHRZones(&m, touched, nil, nil, s.now())
		if err := s.store.saveMetrics(user, m); err != nil {
			slog.Error("strava: saving metrics failed", "err", err)
		}
	}
}

// storedAny says whether any answer of the activity is on disk.
func (s *service) storedAny(user string, id int64) bool {
	for _, k := range []string{kindDetail, kindStreams, kindZones} {
		if p, err := s.store.bodyPath(user, id, k); err == nil {
			if _, err := os.Stat(p); err == nil {
				return true
			}
		}
	}
	return false
}

// dropActivity forgets an activity Strava answered 404 for.
func (s *service) dropActivity(user string, id int64) {
	sl := s.slot(user)
	sl.dataMu.Lock()
	defer sl.dataMu.Unlock()
	s.dropLocked(user, sl, []int64{id})
	s.flushLocked(user, sl)
}

// forget drops what was derived from an activity's streams (the stored
// streams, zones and metrics), so the next analysis reads them again: after
// a refresh of the detail alone.
func (s *service) forget(user string, id int64) {
	sl := s.slot(user)
	sl.dataMu.Lock()
	defer sl.dataMu.Unlock()
	key := strconv.FormatInt(id, 10)
	s.store.removeBodies(user, id, kindStreams, kindZones)
	x := s.indexLocked(user, sl)
	if _, ok := x.Streams[key]; ok {
		delete(x.Streams, key)
		sl.indexDirty = true
		s.flushLocked(user, sl)
	}
	uc := s.cache(user)
	uc.streams.remove(kindStreams + "/" + key)
	uc.detail.remove(kindZones + "/" + key)
	if m, err := s.store.loadMetrics(user); err == nil {
		if _, ok := m.Activities[key]; ok {
			delete(m.Activities, key)
			if err := s.store.saveMetrics(user, m); err != nil {
				slog.Error("strava: saving metrics failed", "err", err)
			}
		}
	}
}

// mayRefresh says whether a refresh of id goes to Strava: not within
// refreshDebounce of the last one that succeeded (markRefreshed).
func (s *service) mayRefresh(user string, id int64) bool {
	sl := s.slot(user)
	sl.dataMu.Lock()
	defer sl.dataMu.Unlock()
	now := s.now()
	for k, at := range sl.refreshed {
		if now.Sub(at) >= refreshDebounce {
			delete(sl.refreshed, k)
		}
	}
	_, recent := sl.refreshed[id]
	return !recent
}

func (s *service) markRefreshed(user string, id int64) {
	sl := s.slot(user)
	sl.dataMu.Lock()
	defer sl.dataMu.Unlock()
	if sl.refreshed == nil {
		sl.refreshed = map[int64]time.Time{}
	}
	sl.refreshed[id] = s.now()
}

// settled says an activity's detail is final: it ended settleTime ago, and
// a list read did not bring it as new within settleTime (a ride synced from
// the watch a day late is matched to segments only after its upload).
func (s *service) settled(user string, a summaryActivity) bool {
	now := s.now()
	if a.utcStart().Add(time.Duration(a.ElapsedTime)*time.Second + settleTime).After(now) {
		return false
	}
	sl := s.slot(user)
	sl.dataMu.Lock()
	defer sl.dataMu.Unlock()
	x := s.indexLocked(user, sl)
	key := strconv.FormatInt(a.ID, 10)
	if seen, ok := x.Seen[key]; ok {
		return now.Sub(seen) >= settleTime
	}
	// Not listed although the list reaches back to it: uploaded since the
	// last list read, so maybe minutes ago.
	_, listed := x.Activities[key]
	reached := x.Complete || (x.Cursor != 0 && a.utcStart().Unix() >= x.Cursor)
	return listed || x.Synced.IsZero() || !reached
}

// listActivities is the user's activities between after and before (unix
// seconds; 0 = open), newest first, from the index after a sync that reads
// at most pages backfill pages. more says older ones are not read yet and
// the window reaches back to them.
func (s *service) listActivities(ctx context.Context, user string, after, before int64, pages int) ([]summaryActivity, bool, error) {
	err := s.syncIndex(ctx, user, after, pages)
	sl := s.slot(user)
	sl.dataMu.Lock()
	x := s.indexLocked(user, sl)
	list := x.between(after, before)
	more := !x.Complete && (after == 0 || x.Cursor == 0 || x.Cursor > after)
	never := x.Synced.IsZero()
	sl.dataMu.Unlock()
	if err != nil {
		if never {
			return nil, false, err
		}
		slog.WarnContext(ctx, "strava: list sync failed, answering from the store", "err", err)
	}
	return list, more, nil
}

// indexed is how many activities the user's index holds.
func (s *service) indexed(user string) int {
	sl := s.slot(user)
	sl.dataMu.Lock()
	defer sl.dataMu.Unlock()
	x := s.indexLocked(user, sl)
	if x.Synced.IsZero() {
		return 0
	}
	return len(x.Activities)
}

// syncIndex brings the user's index up to date for a window from need
// (unix seconds, 0 = all history). The head (the window before the newest
// activity) is read again at most once per listTTL; the backfill (newest to
// oldest, resumable) goes on until it covers need, at most pages pages and
// while the budget allows. A head that fails does not stop the backfill.
func (s *service) syncIndex(ctx context.Context, user string, need int64, pages int) error {
	sl := s.slot(user)
	// One sync per user; a caller whose time runs out waiting answers from
	// the index as it is.
	select {
	case sl.syncCh <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-sl.syncCh }()
	defer s.flushIndex(user)

	athlete := s.athleteOf(user)
	sl.dataMu.Lock()
	x := s.indexLocked(user, sl)
	synced, complete, cursor := x.Synced, x.Complete, x.Cursor
	var after int64
	if n := x.newest(); !n.IsZero() {
		after = n.Add(-headWindow).Unix()
	}
	sl.dataMu.Unlock()

	now := s.now()
	var headErr error
	if !synced.IsZero() && now.Sub(synced) >= listTTL {
		got, done, err := s.readHead(ctx, user, after)
		if !s.mergeHead(user, athlete, x, got, done, after) {
			return nil
		}
		if headErr = err; err == nil {
			s.markSynced(user, athlete, x, now)
		}
	}
	for page := 0; !complete && (need == 0 || cursor == 0 || cursor > need) && page < pages; page++ {
		if page > 0 && s.spent(ctx) != "" {
			break
		}
		q := url.Values{"per_page": {strconv.Itoa(activitiesPerPage)}, "page": {"1"}}
		if cursor > 0 {
			// One second on: activities sharing the cursor's second (a ride
			// uploaded twice) are not lost at a page border.
			q.Set("before", strconv.FormatInt(cursor+1, 10))
		}
		var got []summaryActivity
		if err := s.get(ctx, user, "/athlete/activities", q, &got); err != nil {
			return err
		}
		var ok bool
		if cursor, complete, ok = s.mergeBackfill(user, athlete, x, got); !ok {
			return nil
		}
		if synced.IsZero() {
			synced = now
			s.markSynced(user, athlete, x, now)
		}
	}
	return headErr
}

// readHead reads the activities after after (0 = all, newest first); done
// says every page was read.
func (s *service) readHead(ctx context.Context, user string, after int64) ([]summaryActivity, bool, error) {
	var out []summaryActivity
	for page := 1; page <= maxHeadPages; page++ {
		q := url.Values{"per_page": {strconv.Itoa(activitiesPerPage)}, "page": {strconv.Itoa(page)}}
		if after > 0 {
			q.Set("after", strconv.FormatInt(after, 10))
		}
		var got []summaryActivity
		if err := s.get(ctx, user, "/athlete/activities", q, &got); err != nil {
			return out, false, err
		}
		out = append(out, got...)
		if len(got) < activitiesPerPage {
			return out, true, nil
		}
	}
	return out, false, nil
}

// syncTargetLocked says whether a sync's reads may still go into the index
// it started on: the user still connected to that athlete, and the index not
// deleted (and maybe begun anew) meanwhile. Caller holds dataMu.
func (s *service) syncTargetLocked(user string, sl *slot, athlete int64, x *activityIndex) bool {
	return s.keepsLocked(user, athlete) && sl.index == x
}

// mergeHead puts the head into the index; an activity new to it is noted as
// seen now (settled). Only a head read to its end says what is gone: then
// every indexed activity of the window it lacks goes. false: the index is
// no longer the sync's.
func (s *service) mergeHead(user string, athlete int64, x *activityIndex, got []summaryActivity, done bool, after int64) bool {
	sl := s.slot(user)
	sl.dataMu.Lock()
	defer sl.dataMu.Unlock()
	if !s.syncTargetLocked(user, sl, athlete, x) {
		return false
	}
	now := s.now().UTC()
	for k, at := range x.Seen {
		if now.Sub(at) >= settleTime {
			delete(x.Seen, k)
		}
	}
	seen := map[string]bool{}
	for _, a := range got {
		key := strconv.FormatInt(a.ID, 10)
		seen[key] = true
		if _, had := x.Activities[key]; !had {
			x.Seen[key] = now
		}
		x.Activities[key] = a
	}
	if done {
		var gone []int64
		for key, a := range x.Activities {
			if !seen[key] && a.utcStart().Unix() > after {
				gone = append(gone, a.ID)
			}
		}
		s.dropLocked(user, sl, gone)
	}
	sl.indexDirty = true
	return true
}

// mergeBackfill puts one backfill page into the index and moves the cursor
// to its oldest activity. complete: the page was the last one. ok is false
// when the index is no longer the sync's.
func (s *service) mergeBackfill(user string, athlete int64, x *activityIndex, got []summaryActivity) (cursor int64, complete, ok bool) {
	sl := s.slot(user)
	sl.dataMu.Lock()
	defer sl.dataMu.Unlock()
	if !s.syncTargetLocked(user, sl, athlete, x) {
		return 0, true, false
	}
	prev := x.Cursor
	for _, a := range got {
		x.Activities[strconv.FormatInt(a.ID, 10)] = a
		if st := a.utcStart().Unix(); x.Cursor == 0 || st < x.Cursor {
			x.Cursor = st
		}
	}
	switch {
	case len(got) < activitiesPerPage:
		x.Complete = true
	case prev != 0 && x.Cursor >= prev:
		// A full page all in the cursor's second: step past it rather
		// than read the same page forever.
		x.Cursor = prev - 1
	}
	if x.Athlete == 0 {
		x.Athlete = athlete
	}
	sl.indexDirty = true
	return x.Cursor, x.Complete, true
}

func (s *service) markSynced(user string, athlete int64, x *activityIndex, now time.Time) {
	sl := s.slot(user)
	sl.dataMu.Lock()
	defer sl.dataMu.Unlock()
	if !s.syncTargetLocked(user, sl, athlete, x) {
		return
	}
	x.Synced = now.UTC()
	sl.indexDirty = true
}
