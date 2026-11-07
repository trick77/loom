package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// restart is a fresh process over the same /data.
func restart(t *testing.T, f *fakeStrava, svc *service, dir string) (*service, http.Handler) {
	t.Helper()
	svc2, err := newService(config{clientID: testClientID, clientSecret: testSecret, base: svc.base}, f.srv.Client(), fileStore{dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	svc2.now = f.clock
	return svc2, newServer(config{token: testToken}, svc2, "test")
}

func storedFile(dir string, id int64, kind string) string {
	return filepath.Join(dir, activitiesDir, userA, fmt.Sprintf("%d.%s.json.gz", id, kind))
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// setField changes one of athlete 1's activities on Strava.
func (f *fakeStrava) setField(id int64, key string, v any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, a := range f.activities[1] {
		if a["id"] == id {
			a[key] = v
		}
	}
}

// deleteActivity deletes one of athlete 1's activities on Strava.
func (f *fakeStrava) deleteActivity(id int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var keep []map[string]any
	for _, a := range f.activities[1] {
		if a["id"] != id {
			keep = append(keep, a)
		}
	}
	f.activities[1] = keep
}

// After a restart an analysis asked before costs no Strava read of the
// activity, its streams or the list; the store is per user and private.
func TestStore_restartReadsTheStore(t *testing.T) {
	f, svc, dir := analysisFixture(t)
	h := newServer(config{token: testToken}, svc, "test")
	if r := callTool(t, h, userA, "activity_analysis", `{"id":1001}`); r.IsError {
		t.Fatal(r.Text)
	}
	for _, p := range []string{storedFile(dir, 1001, kindDetail), storedFile(dir, 1001, kindStreams), storedFile(dir, 1003, kindStreams),
		filepath.Join(dir, activitiesDir, userA, indexFile)} {
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatalf("not stored: %s", p)
		}
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("%s mode %v", p, fi.Mode().Perm())
		}
	}
	paths := []string{"/activities/1001", "/activities/1001/streams", "/activities/1003/streams", "/athlete/activities"}
	before := map[string]int{}
	for _, p := range paths {
		before[p] = f.count(p)
	}
	_, h2 := restart(t, f, svc, dir)
	r := callTool(t, h2, userA, "activity_analysis", `{"id":1001}`)
	if r.IsError || strings.Contains(r.Text, "partial") || !strings.Contains(r.Text, "compared with 3 rides") {
		t.Fatalf("after restart: %s", r.Text)
	}
	for _, p := range paths {
		if f.count(p) != before[p] {
			t.Errorf("%s read again after a restart: %d -> %d", p, before[p], f.count(p))
		}
	}
	// The 5-minute head: past it, one list read and still no activity read.
	f.advance(6 * time.Minute)
	if r := callTool(t, h2, userA, "list_activities", `{}`); r.IsError || !strings.Contains(r.Text, "Albis loop") {
		t.Fatalf("list: %s", r.Text)
	}
	if f.count("/athlete/activities") != before["/athlete/activities"]+1 {
		t.Errorf("list calls %d, want one head read", f.count("/athlete/activities")-before["/athlete/activities"])
	}
}

// A detail is stored only once settled; streams at once.
func TestStore_youngDetailWaits(t *testing.T) {
	f := newFakeStrava(t)
	f.addAthlete(1, "Anna", "code-a", 250)
	ride := synthRide(false)
	// 09:35-10:00 UTC (10:35 local); the fake's clock is 10:02.
	f.addActivity(1, 7, "Just now", "Ride", time.Date(2026, 11, 3, 10, 35, 0, 0, time.UTC), &ride, map[string]any{"elapsed_time": 1500})
	svc, h, dir := newTestService(t, f)
	connect(t, h, userA, "code-a")
	if r := callTool(t, h, userA, "activity_analysis", `{"id":7}`); r.IsError {
		t.Fatal(r.Text)
	}
	if exists(storedFile(dir, 7, kindDetail)) || !exists(storedFile(dir, 7, kindStreams)) {
		t.Errorf("young: detail stored %v, streams stored %v", exists(storedFile(dir, 7, kindDetail)), exists(storedFile(dir, 7, kindStreams)))
	}
	f.advance(25 * time.Hour) // memory cache gone, detail settled
	_, h2 := restart(t, f, svc, dir)
	if r := callTool(t, h2, userA, "get_activity", `{"id":7}`); r.IsError {
		t.Fatal(r.Text)
	}
	if !exists(storedFile(dir, 7, kindDetail)) {
		t.Error("a settled detail was not stored")
	}
}

// refresh reads the activity again, replaces the stored copy and the list
// row, drops what came from the old streams; not twice in 10 minutes.
func TestStore_refresh(t *testing.T) {
	f, svc, dir := analysisFixture(t)
	h := newServer(config{token: testToken}, svc, "test")
	if r := callTool(t, h, userA, "activity_analysis", `{"id":1001}`); r.IsError {
		t.Fatal(r.Text)
	}
	f.setField(1001, "name", "Albis loop (cropped)")
	details := f.count("/activities/1001")
	if r := callTool(t, h, userA, "get_activity", `{"id":1001}`); strings.Contains(r.Text, "cropped") || f.count("/activities/1001") != details {
		t.Fatalf("a plain read went to Strava: %s", r.Text)
	}
	streams := f.count("/activities/1001/streams")
	r := callTool(t, h, userA, "get_activity", `{"id":1001,"refresh":true}`)
	if !strings.Contains(r.Text, "Albis loop (cropped)") || f.count("/activities/1001") != details+1 {
		t.Fatalf("refresh: %s", r.Text)
	}
	// A recent activity: its streams file is read again, not the old one.
	if f.count("/activities/1001/streams") != streams+1 || r.file("strava_1001_streams.csv") == "" {
		t.Error("the old streams survived a refresh")
	}
	if m, _ := svc.store.loadMetrics(userA); m.Activities["1001"].V != 0 {
		t.Error("the old metrics survived a refresh")
	}
	if r := callTool(t, h, userA, "list_activities", `{}`); !strings.Contains(r.Text, "Albis loop (cropped)") {
		t.Errorf("list keeps the old name:\n%s", r.Text)
	}
	// Debounced: the store answers.
	callTool(t, h, userA, "get_activity", `{"id":1001,"refresh":true}`)
	if f.count("/activities/1001") != details+1 {
		t.Error("a second refresh within 10 minutes went to Strava")
	}
	f.advance(11 * time.Minute)
	streams = f.count("/activities/1001/streams")
	if r := callTool(t, h, userA, "activity_analysis", `{"id":1001,"refresh":true}`); r.IsError || !strings.Contains(r.Text, "cropped") {
		t.Fatalf("analysis refresh: %s", r.Text)
	}
	if f.count("/activities/1001") != details+2 || f.count("/activities/1001/streams") != streams+1 {
		t.Error("analysis refresh did not read detail and streams")
	}
	if m, _ := svc.store.loadMetrics(userA); m.Activities["1001"].V == 0 {
		t.Error("analysis refresh stored no metrics")
	}
	// Gone on Strava: a refresh drops what was kept.
	f.deleteActivity(1001)
	f.advance(11 * time.Minute)
	if r := callTool(t, h, userA, "get_activity", `{"id":1001,"refresh":true}`); !r.IsError {
		t.Fatalf("deleted activity: %s", r.Text)
	}
	if exists(storedFile(dir, 1001, kindDetail)) || exists(storedFile(dir, 1001, kindStreams)) {
		t.Error("a deleted activity's files stayed")
	}
	if r := callTool(t, h, userA, "list_activities", `{}`); strings.Contains(r.Text, "id 1001") {
		t.Errorf("a deleted activity is still listed:\n%s", r.Text)
	}
}

// The head brings uploads and drops deletions within its window; older
// rows stay untouched.
func TestStore_headSync(t *testing.T) {
	f, svc, dir := analysisFixture(t)
	h := newServer(config{token: testToken}, svc, "test")
	if r := callTool(t, h, userA, "activity_analysis", `{"id":1002}`); r.IsError {
		t.Fatal(r.Text)
	}
	f.addActivity(1, 1009, "Fresh upload", "Ride", local(2026, 11, 3, 8), nil, nil)
	f.deleteActivity(1002)
	f.deleteActivity(1006) // 2026-09-01: before the head's 30 days
	r := callTool(t, h, userA, "list_activities", `{}`)
	if strings.Contains(r.Text, "Fresh upload") {
		t.Errorf("the head was read within 5 minutes:\n%s", r.Text)
	}
	f.advance(6 * time.Minute)
	r = callTool(t, h, userA, "list_activities", `{}`)
	if !strings.Contains(r.Text, "Fresh upload") || strings.Contains(r.Text, "id 1002") || !strings.Contains(r.Text, "id 1006") {
		t.Errorf("after the head:\n%s", r.Text)
	}
	if exists(storedFile(dir, 1002, kindDetail)) || exists(storedFile(dir, 1002, kindStreams)) {
		t.Error("a deleted activity's files stayed")
	}
	// Nor served from memory or kept in the metrics.
	if r := callTool(t, h, userA, "get_activity", `{"id":1002}`); !r.IsError {
		t.Errorf("a deleted activity served from memory: %.200s", r.Text)
	}
	if m, _ := svc.store.loadMetrics(userA); m.Activities["1002"].V != 0 {
		t.Error("a deleted activity's metrics stayed")
	}
}

// Under a tight budget the backfill stops after its first page and says
// so; the next call reads on.
func TestStore_backfillResumes(t *testing.T) {
	f := newFakeStrava(t)
	f.addAthlete(1, "Anna", "code-a", 250)
	start := local(2024, 1, 1, 8)
	for i := range 450 {
		f.addActivity(1, int64(20000+i), fmt.Sprintf("Ride %d", i), "Ride", start.Add(time.Duration(i)*24*time.Hour), nil, nil)
	}
	_, h, _ := newTestService(t, f)
	connect(t, h, userA, "code-a")
	f.mu.Lock()
	f.rateLimit15 = 10
	f.rateUsed15 = 4 // /athlete makes it 5 of 10: the first page only
	f.mu.Unlock()
	r := callTool(t, h, userA, "list_activities", `{"after":"2024-01-01","limit":500}`)
	if !strings.Contains(r.Text, "newest first: 200;") || !strings.Contains(r.Text, "older activities are still being read") {
		t.Fatalf("first:\n%.400s", r.Text)
	}
	f.mu.Lock()
	f.rateUsed15 = 0
	f.mu.Unlock()
	f.advance(time.Minute)
	r = callTool(t, h, userA, "list_activities", `{"after":"2024-01-01","limit":500}`)
	if !strings.Contains(r.Text, "newest first: 450\n") {
		t.Fatalf("resumed:\n%.400s", r.Text)
	}
	if n := f.count("/athlete/activities"); n != 3 {
		t.Errorf("list calls = %d, want 3 (200 + 200 + 50)", n)
	}
}

// Disconnect deletes the store; another athlete starts with an empty one.
func TestStore_goneWithTheConnection(t *testing.T) {
	_, svc, dir := analysisFixture(t)
	h := newServer(config{token: testToken}, svc, "test")
	if r := callTool(t, h, userA, "activity_analysis", `{"id":1001}`); r.IsError {
		t.Fatal(r.Text)
	}
	if code, _ := request(t, h, http.MethodDelete, "/connection", userA, ""); code != 200 {
		t.Fatal(code)
	}
	if exists(filepath.Join(dir, activitiesDir, userA)) {
		t.Error("the activity store survived a disconnect")
	}
}

func TestStore_reconnectToAnotherAthlete(t *testing.T) {
	f, svc, dir := analysisFixture(t)
	h := newServer(config{token: testToken}, svc, "test")
	if r := callTool(t, h, userA, "get_activity", `{"id":1001}`); r.IsError {
		t.Fatal(r.Text)
	}
	if !exists(storedFile(dir, 1001, kindDetail)) {
		t.Fatal("not stored")
	}
	f.addAthlete(2, "Ben", "code-b", 300)
	connect(t, h, userA, "code-b")
	if exists(filepath.Join(dir, activitiesDir, userA)) {
		t.Error("athlete 1's store survived a reconnect to athlete 2")
	}
}

// Past maxStreamBytes the least recently read streams go, never the one
// just written.
func TestStore_streamCap(t *testing.T) {
	f, svc, dir := analysisFixture(t)
	h := newServer(config{token: testToken}, svc, "test")
	old := maxStreamBytes
	t.Cleanup(func() { maxStreamBytes = old })
	if r := callTool(t, h, userA, "activity_analysis", `{"id":1001}`); r.IsError {
		t.Fatal(r.Text)
	}
	sl := svc.slot(userA)
	sl.dataMu.Lock()
	x := svc.indexLocked(userA, sl)
	total, n := x.streamBytes(), len(x.Streams)
	sl.dataMu.Unlock()
	if n < 3 || total == 0 {
		t.Fatalf("streams indexed: %d, %d bytes", n, total)
	}
	maxStreamBytes = total // one more write evicts
	f.advance(time.Minute)
	svc.loadBody(userA, 1001, kindStreams) // 1001 read last
	ride := synthRide(false)
	f.addActivity(1, 1010, "New", "Ride", local(2026, 10, 30, 9), &ride, nil)
	if _, err := svc.streams(t.Context(), userA, 1010, false); err != nil {
		t.Fatal(err)
	}
	if !exists(storedFile(dir, 1010, kindStreams)) || !exists(storedFile(dir, 1001, kindStreams)) {
		t.Error("evicted the new or the recently read streams")
	}
	sl.dataMu.Lock()
	over := x.streamBytes() > maxStreamBytes
	sl.dataMu.Unlock()
	if over {
		t.Error("still over the cap")
	}
}

// Calls of one user at once neither lose index rows nor race.
func TestStore_concurrentCalls(t *testing.T) {
	_, svc, _ := analysisFixture(t)
	h := newServer(config{token: testToken}, svc, "test")
	var wg sync.WaitGroup
	for _, args := range []string{`{"id":1001}`, `{"id":1002}`, `{"id":1003}`} {
		wg.Add(2)
		go func() {
			defer wg.Done()
			callTool(t, h, userA, "activity_analysis", args)
		}()
		go func() {
			defer wg.Done()
			callTool(t, h, userA, "list_activities", `{}`)
		}()
	}
	wg.Wait()
	idx := svc.store.loadIndex(userA)
	if idx == nil || len(idx.Activities) != 8 || !idx.Complete {
		t.Fatalf("index after concurrent calls: %+v", idx)
	}
}

func TestStore_unreadableFilesAreMissing(t *testing.T) {
	dir := t.TempDir()
	st := fileStore{dir: dir}
	if _, err := st.writeBody(userA, 1, kindDetail, []byte(`{"id":1}`)); err != nil {
		t.Fatal(err)
	}
	if b, ok := st.readBody(userA, 1, kindDetail); !ok || string(b) != `{"id":1}` {
		t.Fatalf("round trip: %q %v", b, ok)
	}
	p := filepath.Join(dir, activitiesDir, userA, "1.detail.json.gz")
	if err := os.WriteFile(p, []byte("not gzip"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := st.readBody(userA, 1, kindDetail); ok || exists(p) {
		t.Error("an unreadable body was served or kept")
	}
	if err := os.WriteFile(filepath.Join(dir, activitiesDir, userA, indexFile), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if st.loadIndex(userA) != nil {
		t.Error("an unreadable index was used")
	}
	if _, err := st.writeBody("../x", 1, kindDetail, nil); err == nil {
		t.Error("a bad user id reached the disk")
	}
}

// A window the backfill has not reached is not "none"; an analysis reads
// only as much history as its window needs.
func TestStore_windowAwareBackfill(t *testing.T) {
	f := newFakeStrava(t)
	f.addAthlete(1, "Anna", "code-a", 250)
	start := local(2024, 1, 1, 8)
	ride := synthRide(false)
	for i := range 450 {
		var st *streamSet
		if i == 449 {
			st = &ride
		}
		f.addActivity(1, int64(20000+i), fmt.Sprintf("Ride %d", i), "Ride", start.Add(time.Duration(i)*24*time.Hour), st, nil)
	}
	_, h, _ := newTestService(t, f)
	connect(t, h, userA, "code-a")
	if r := callTool(t, h, userA, "activity_analysis", `{"id":20449}`); r.IsError || !strings.Contains(r.Text, "compared with") {
		t.Fatalf("analysis: %s", r.Text)
	}
	if n := f.count("/athlete/activities"); n != 1 {
		t.Errorf("list calls = %d, want the one page the 6-week window needs", n)
	}
	f.mu.Lock()
	f.rateLimit15 = 10
	f.rateUsed15 = 5 // /athlete is cached: the first page makes 6 of 10, no second
	f.mu.Unlock()
	f.advance(time.Minute)
	r := callTool(t, h, userA, "list_activities", `{"after":"2024-01-01","before":"2024-02-01"}`)
	if !strings.Contains(r.Text, "No Strava activities read yet") || strings.Contains(r.Text, "No Strava activities match") {
		t.Errorf("unread window:\n%s", r.Text)
	}
}

// A full page of activities in one second does not stop the backfill.
func TestStore_backfillPastOneSecond(t *testing.T) {
	f := newFakeStrava(t)
	f.addAthlete(1, "Anna", "code-a", 250)
	at := local(2026, 10, 1, 8)
	for i := range activitiesPerPage {
		f.addActivity(1, int64(30000+i), "Same second", "Ride", at, nil, nil)
	}
	f.addActivity(1, 29999, "Day before", "Ride", at.Add(-24*time.Hour), nil, nil)
	svc, h, _ := newTestService(t, f)
	connect(t, h, userA, "code-a")
	if r := callTool(t, h, userA, "list_activities", `{"limit":500}`); !strings.Contains(r.Text, "newest first: 201\n") {
		t.Fatalf("list:\n%.300s", r.Text)
	}
	if idx := svc.store.loadIndex(userA); idx == nil || !idx.Complete || len(idx.Activities) != 201 {
		t.Errorf("index: %+v", idx)
	}
}

// A failed refresh does not count against the 10 minutes.
func TestStore_failedRefreshRetries(t *testing.T) {
	f, svc, _ := analysisFixture(t)
	h := newServer(config{token: testToken}, svc, "test")
	if r := callTool(t, h, userA, "activity_analysis", `{"id":1001}`); r.IsError {
		t.Fatal(r.Text)
	}
	f.mu.Lock()
	f.failStreams[1001] = true
	f.mu.Unlock()
	if r := callTool(t, h, userA, "activity_analysis", `{"id":1001,"refresh":true}`); !r.IsError {
		t.Fatalf("refresh with failing streams: %s", r.Text)
	}
	f.mu.Lock()
	f.failStreams[1001] = false
	f.mu.Unlock()
	f.advance(2 * time.Minute) // within 10 minutes of the failed try
	f.setField(1001, "name", "Renamed")
	if r := callTool(t, h, userA, "activity_analysis", `{"id":1001,"refresh":true}`); !strings.Contains(r.Text, "Renamed") {
		t.Errorf("retry after a failed refresh: %.300s", r.Text)
	}
}

// A ride the head brings as new is stored only an hour after that, even
// when it ended long ago (synced late from the watch).
func TestStore_lateUploadWaits(t *testing.T) {
	f, svc, dir := analysisFixture(t)
	h := newServer(config{token: testToken}, svc, "test")
	if r := callTool(t, h, userA, "list_activities", `{}`); r.IsError {
		t.Fatal(r.Text)
	}
	f.addActivity(1, 1011, "Synced late", "Ride", local(2026, 11, 2, 18), nil, nil)
	f.advance(6 * time.Minute)
	if r := callTool(t, h, userA, "list_activities", `{}`); !strings.Contains(r.Text, "Synced late") {
		t.Fatalf("head: %s", r.Text)
	}
	if r := callTool(t, h, userA, "get_activity", `{"id":1011}`); r.IsError {
		t.Fatal(r.Text)
	}
	if exists(storedFile(dir, 1011, kindDetail)) {
		t.Error("a just-uploaded detail was stored")
	}
	f.advance(25 * time.Hour)
	if r := callTool(t, h, userA, "get_activity", `{"id":1011}`); r.IsError {
		t.Fatal(r.Text)
	}
	if !exists(storedFile(dir, 1011, kindDetail)) {
		t.Error("the settled detail was not stored")
	}
}

// Nothing stored expires: a year on, after a restart, detail, streams and
// list still come from the store.
func TestStore_keptForGood(t *testing.T) {
	f, svc, dir := analysisFixture(t)
	h := newServer(config{token: testToken}, svc, "test")
	if r := callTool(t, h, userA, "activity_analysis", `{"id":1001}`); r.IsError {
		t.Fatal(r.Text)
	}
	if r := callTool(t, h, userA, "list_activities", `{}`); r.IsError {
		t.Fatal(r.Text)
	}
	listed := len(svc.store.loadIndex(userA).Activities)
	details, streams := f.count("/activities/1001"), f.count("/activities/1001/streams")
	f.advance(365 * 24 * time.Hour)
	svc2, _ := restart(t, f, svc, dir)
	if _, err := svc2.activity(t.Context(), userA, 1001, false); err != nil {
		t.Fatal(err)
	}
	if _, err := svc2.streams(t.Context(), userA, 1001, false); err != nil {
		t.Fatal(err)
	}
	if n := f.count("/activities/1001") - details; n != 0 {
		t.Errorf("a year-old detail was read again: %d calls", n)
	}
	if n := f.count("/activities/1001/streams") - streams; n != 0 {
		t.Errorf("year-old streams were read again: %d calls", n)
	}
	sl := svc2.slot(userA)
	sl.dataMu.Lock()
	n := len(svc2.indexLocked(userA, sl).Activities)
	sl.dataMu.Unlock()
	if n != listed {
		t.Errorf("a year-old index lost activities: %d of %d listed", n, listed)
	}
}

// An index written before the created field went still loads.
func TestStore_indexWithCreatedLoads(t *testing.T) {
	dir := t.TempDir()
	st := fileStore{dir: dir}
	body := fmt.Sprintf(`{"v":%d,"athlete":1,"created":"2026-11-03T10:00:00Z","complete":true,"activities":{"7":{"id":7}}}`, indexVersion)
	z, err := gzipBytes([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, activitiesDir, userA), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, activitiesDir, userA, indexFile), z, 0o600); err != nil {
		t.Fatal(err)
	}
	if x := st.loadIndex(userA); x == nil || !x.Complete || len(x.Activities) != 1 {
		t.Errorf("legacy index: %+v", x)
	}
}

// A sync whose index was deleted meanwhile (disconnect, reconnect) writes
// nothing into the new one.
func TestStore_syncIntoAStaleIndex(t *testing.T) {
	_, svc, _ := analysisFixture(t)
	sl := svc.slot(userA)
	sl.dataMu.Lock()
	old := svc.indexLocked(userA, sl)
	if err := svc.deleteStoreLocked(userA, sl); err != nil {
		t.Fatal(err)
	}
	sl.dataMu.Unlock()
	if _, _, ok := svc.mergeBackfill(userA, 1, old, []summaryActivity{{ID: 1, StartDate: "2020-01-01T00:00:00Z"}}); ok {
		t.Error("a backfill page went into a deleted index")
	}
	svc.markSynced(userA, 1, old, time.Now())
	sl.dataMu.Lock()
	fresh := svc.indexLocked(userA, sl)
	sl.dataMu.Unlock()
	if fresh == old || fresh.Cursor != 0 || !fresh.Synced.IsZero() {
		t.Errorf("the new index took the old sync's state: %+v", fresh)
	}
}

// A stored stream the index lost is counted again when read.
func TestStore_lostStreamCountedAgain(t *testing.T) {
	_, svc, dir := analysisFixture(t)
	h := newServer(config{token: testToken}, svc, "test")
	if r := callTool(t, h, userA, "activity_analysis", `{"id":1001}`); r.IsError {
		t.Fatal(r.Text)
	}
	if err := os.Remove(filepath.Join(dir, activitiesDir, userA, indexFile)); err != nil {
		t.Fatal(err)
	}
	svc.slot(userA).index = nil
	svc.loadBody(userA, 1001, kindStreams)
	sl := svc.slot(userA)
	sl.dataMu.Lock()
	m := svc.indexLocked(userA, sl).Streams["1001"]
	sl.dataMu.Unlock()
	if fi, err := os.Stat(storedFile(dir, 1001, kindStreams)); err != nil || m.Bytes != fi.Size() {
		t.Errorf("lost stream counted as %d bytes", m.Bytes)
	}
}

// A refreshed detail not stored (just listed as new) takes the old stored
// copy with it.
func TestStore_unsettledRefreshDropsTheOldCopy(t *testing.T) {
	f, svc, dir := analysisFixture(t)
	h := newServer(config{token: testToken}, svc, "test")
	if r := callTool(t, h, userA, "get_activity", `{"id":1001}`); r.IsError {
		t.Fatal(r.Text)
	}
	if r := callTool(t, h, userA, "list_activities", `{}`); r.IsError {
		t.Fatal(r.Text)
	}
	sl := svc.slot(userA)
	sl.dataMu.Lock()
	svc.indexLocked(userA, sl).Seen["1001"] = f.clock()
	sl.dataMu.Unlock()
	if r := callTool(t, h, userA, "get_activity", `{"id":1001,"refresh":true}`); r.IsError {
		t.Fatal(r.Text)
	}
	if exists(storedFile(dir, 1001, kindDetail)) {
		t.Error("the old detail stayed behind an unsettled refresh")
	}
}

// A body that will not open stays; only a corrupt one goes.
func TestStore_unopenableBodyStays(t *testing.T) {
	dir := t.TempDir()
	st := fileStore{dir: dir}
	if _, err := st.writeBody(userA, 1, kindDetail, []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, activitiesDir, userA, "1.detail.json.gz")
	if err := os.Chmod(p, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(p, 0o600) })
	if _, ok := st.readBody(userA, 1, kindDetail); ok {
		t.Skip("running with permission to read anything")
	}
	if !exists(p) {
		t.Error("a body that would not open was deleted")
	}
}
