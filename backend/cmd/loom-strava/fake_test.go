package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func jsonUnmarshal(s string, into any) error { return json.Unmarshal([]byte(s), into) }

const (
	testToken    = "test-service-token-0123456789abcdef"
	testClientID = "4242"
	testSecret   = "client-secret"
	userA        = "user-a"
	userB        = "user-b"
)

// fakeStrava is Strava's API, OAuth and consent endpoints over httptest,
// with every athlete's data kept apart by the token it is read with.
type fakeStrava struct {
	t   *testing.T
	srv *httptest.Server

	mu       sync.Mutex
	now      time.Time
	codes    map[string]int64 // authorization code -> athlete
	access   map[string]tokenInfo
	refresh  map[string]int64 // valid refresh tokens
	dead     map[string]bool  // rotated refresh tokens: Strava rejects them
	serial   int
	scope    string // the scope the token answer reports ("" = none)
	calls    map[string]int
	seen     []string // "athlete:path" of every API call
	revoked  []string
	revokeOK bool
	// revokeMissing: /oauth/revoke answers 404, so the legacy
	// /oauth/deauthorize must take over.
	revokeMissing bool
	deauthorized  []string
	// noAthlete: the code exchange answers without the athlete.
	noAthlete bool

	athletes   map[int64]map[string]any
	zones      map[int64]any
	activities map[int64][]map[string]any // per athlete
	streams    map[int64]map[string]any   // per activity
	summit     map[int64]bool

	// rate headers: limit and the usage the next answer reports.
	rateLimit15, rateLimitDay int
	rateUsed15, rateUsedDay   int
	noRateHeaders             bool
	tooMany                   bool
	failStreams               map[int64]bool
	slowToken                 time.Duration
	onAPI                     func(r *http.Request, bearer string)
}

type tokenInfo struct {
	athlete int64
	expires time.Time
}

func newFakeStrava(t *testing.T) *fakeStrava {
	f := &fakeStrava{
		t: t, now: time.Date(2026, 11, 3, 10, 2, 0, 0, time.UTC),
		codes: map[string]int64{}, access: map[string]tokenInfo{}, refresh: map[string]int64{}, dead: map[string]bool{},
		// As Strava answered in production: spaces, unlike the consent
		// redirect's commas.
		scope: "activity:read_all profile:read_all read read_all", calls: map[string]int{}, revokeOK: true,
		athletes: map[int64]map[string]any{}, zones: map[int64]any{}, activities: map[int64][]map[string]any{},
		streams: map[int64]map[string]any{}, summit: map[int64]bool{}, failStreams: map[int64]bool{},
		rateLimit15: 100, rateLimitDay: 1000,
	}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeStrava) clock() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *fakeStrava) advance(d time.Duration) {
	f.mu.Lock()
	f.now = f.now.Add(d)
	f.mu.Unlock()
}

func (f *fakeStrava) count(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[path]
}

func (f *fakeStrava) issue(athlete int64) (string, string, int64) {
	f.serial++
	acc := fmt.Sprintf("acc-%d-%d", athlete, f.serial)
	ref := fmt.Sprintf("ref-%d-%d", athlete, f.serial)
	exp := f.now.Add(6 * time.Hour)
	f.access[acc] = tokenInfo{athlete: athlete, expires: exp}
	f.refresh[ref] = athlete
	return acc, ref, exp.Unix()
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (f *fakeStrava) serve(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/api/v3/oauth/token" && r.Method == http.MethodPost:
		f.token(w, r)
	case r.URL.Path == "/oauth/revoke" && r.Method == http.MethodPost:
		f.mu.Lock()
		missing := f.revokeMissing
		f.mu.Unlock()
		if missing {
			http.NotFound(w, r)
			return
		}
		id, secret, ok := r.BasicAuth()
		_ = r.ParseForm()
		f.mu.Lock()
		f.revoked = append(f.revoked, r.PostForm.Get("token"))
		okRevoke := f.revokeOK
		f.mu.Unlock()
		if !ok || id != testClientID || secret != testSecret {
			writeJSON(w, 401, map[string]string{"message": "bad client"})
			return
		}
		if !okRevoke {
			writeJSON(w, 500, map[string]string{"message": "boom"})
			return
		}
		writeJSON(w, 200, map[string]any{})
	case r.URL.Path == "/oauth/deauthorize" && r.Method == http.MethodPost:
		_ = r.ParseForm()
		f.mu.Lock()
		f.deauthorized = append(f.deauthorized, r.PostForm.Get("access_token"))
		_, basic := r.Header["Authorization"]
		f.mu.Unlock()
		if basic {
			writeJSON(w, 400, map[string]string{"message": "deauthorize takes no client auth"})
			return
		}
		writeJSON(w, 200, map[string]any{"access_token": r.PostForm.Get("access_token")})
	case strings.HasPrefix(r.URL.Path, "/api/v3/"):
		f.api(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeStrava) token(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	if d := f.slowToken; d > 0 {
		time.Sleep(d)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls["token:"+r.PostForm.Get("grant_type")]++
	if r.PostForm.Get("client_id") != testClientID || r.PostForm.Get("client_secret") != testSecret {
		writeJSON(w, 401, map[string]string{"message": "Authorization Error"})
		return
	}
	switch r.PostForm.Get("grant_type") {
	case "authorization_code":
		athlete, ok := f.codes[r.PostForm.Get("code")]
		if !ok {
			writeJSON(w, 400, map[string]any{"message": "Bad Request", "errors": []map[string]string{{"resource": "AuthorizationCode", "field": "code", "code": "invalid"}}})
			return
		}
		delete(f.codes, r.PostForm.Get("code"))
		acc, ref, exp := f.issue(athlete)
		a := f.athletes[athlete]
		resp := map[string]any{"token_type": "Bearer", "access_token": acc, "refresh_token": ref, "expires_at": exp}
		if !f.noAthlete {
			resp["athlete"] = map[string]any{"id": athlete, "firstname": a["firstname"], "lastname": a["lastname"]}
		}
		if f.scope != "" {
			resp["scope"] = f.scope
		}
		writeJSON(w, 200, resp)
	case "refresh_token":
		ref := r.PostForm.Get("refresh_token")
		athlete, ok := f.refresh[ref]
		if !ok || f.dead[ref] {
			writeJSON(w, 400, map[string]any{"message": "Bad Request", "errors": []map[string]string{{"resource": "RefreshToken", "field": "refresh_token", "code": "invalid"}}})
			return
		}
		// Rotation: the old refresh token dies the moment a new one exists.
		delete(f.refresh, ref)
		f.dead[ref] = true
		acc, nref, exp := f.issue(athlete)
		writeJSON(w, 200, map[string]any{"token_type": "Bearer", "access_token": acc, "refresh_token": nref, "expires_at": exp})
	default:
		writeJSON(w, 400, map[string]string{"message": "bad grant"})
	}
}

func (f *fakeStrava) rateHeaders(w http.ResponseWriter) {
	if f.noRateHeaders {
		return
	}
	f.rateUsed15++
	f.rateUsedDay++
	w.Header().Set("X-ReadRateLimit-Limit", fmt.Sprintf("%d,%d", f.rateLimit15, f.rateLimitDay))
	w.Header().Set("X-ReadRateLimit-Usage", fmt.Sprintf("%d,%d", f.rateUsed15, f.rateUsedDay))
	w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d,%d", 2*f.rateLimit15, 2*f.rateLimitDay))
	w.Header().Set("X-RateLimit-Usage", fmt.Sprintf("%d,%d", f.rateUsed15, f.rateUsedDay))
}

func (f *fakeStrava) api(w http.ResponseWriter, r *http.Request) {
	bearer := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if f.onAPI != nil {
		f.onAPI(r, bearer)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	path := strings.TrimPrefix(r.URL.Path, "/api/v3")
	f.calls[path]++
	if f.tooMany {
		w.Header().Set("X-ReadRateLimit-Limit", fmt.Sprintf("%d,%d", f.rateLimit15, f.rateLimitDay))
		w.Header().Set("X-ReadRateLimit-Usage", fmt.Sprintf("%d,%d", f.rateLimit15+1, f.rateUsedDay))
		writeJSON(w, 429, map[string]string{"message": "Rate Limit Exceeded"})
		return
	}
	f.rateHeaders(w)
	tok, ok := f.access[bearer]
	if !ok || !f.now.Before(tok.expires) {
		writeJSON(w, 401, map[string]any{"message": "Authorization Error", "errors": []map[string]string{{"resource": "Athlete", "field": "access_token", "code": "invalid"}}})
		return
	}
	me := tok.athlete
	f.seen = append(f.seen, fmt.Sprintf("%d:%s", me, path))
	parts := strings.Split(strings.Trim(path, "/"), "/")
	q := r.URL.Query()
	switch {
	case path == "/athlete":
		writeJSON(w, 200, f.athletes[me])
	case path == "/athlete/zones":
		writeJSON(w, 200, f.zones[me])
	case len(parts) == 3 && parts[0] == "athletes" && parts[2] == "stats":
		if parts[1] != strconv.FormatInt(me, 10) {
			writeJSON(w, 403, map[string]string{"message": "not you"})
			return
		}
		writeJSON(w, 200, map[string]any{"biggest_ride_distance": 180000.0, "biggest_climb_elevation_gain": 1200.0,
			"recent_ride_totals": map[string]any{"count": 8, "distance": 412000.0, "moving_time": 52000.0, "elevation_gain": 4100.0},
			"ytd_ride_totals":    map[string]any{"count": 140, "distance": 6100000.0, "moving_time": 790000.0, "elevation_gain": 61000.0},
			"all_ride_totals":    map[string]any{"count": 900, "distance": 41000000.0, "moving_time": 5000000.0, "elevation_gain": 400000.0}})
	case path == "/athlete/activities":
		f.list(w, me, q)
	case len(parts) == 2 && parts[0] == "activities":
		a := f.find(me, parts[1])
		if a == nil {
			writeJSON(w, 404, map[string]string{"message": "Record Not Found"})
			return
		}
		writeJSON(w, 200, a)
	case len(parts) == 3 && parts[0] == "activities" && parts[2] == "streams":
		a := f.find(me, parts[1])
		id, _ := strconv.ParseInt(parts[1], 10, 64)
		if f.failStreams[id] {
			writeJSON(w, 500, map[string]string{"message": "boom"})
			return
		}
		st, ok := f.streams[id]
		if a == nil || !ok {
			writeJSON(w, 404, map[string]string{"message": "Record Not Found"})
			return
		}
		writeJSON(w, 200, st)
	case len(parts) == 3 && parts[0] == "activities" && parts[2] == "zones":
		if !f.summit[me] {
			writeJSON(w, 402, map[string]string{"message": "Payment Required"})
			return
		}
		writeJSON(w, 200, []map[string]any{{"type": "heartrate", "distribution_buckets": []map[string]any{
			{"min": 0, "max": 123, "time": 100}, {"min": 123, "max": 153, "time": 200}, {"min": 153, "max": 169, "time": 300},
			{"min": 169, "max": 184, "time": 50}, {"min": 184, "max": -1, "time": 10}}}})
	case len(parts) == 2 && parts[0] == "gear":
		writeJSON(w, 200, map[string]any{"id": parts[1], "name": "Canyon Ultimate", "brand_name": "Canyon", "model_name": "Ultimate CF SLX",
			"frame_type": 3, "distance": 12345000.0})
	case len(parts) == 3 && parts[0] == "athletes" && parts[2] == "routes":
		writeJSON(w, 200, []map[string]any{{"id": 3141592653589793, "id_str": "3141592653589793238", "name": "Albis loop", "distance": 74300.0,
			"elevation_gain": 880.0, "type": 1, "estimated_moving_time": 9000.0, "starred": true}})
	case len(parts) == 2 && parts[0] == "routes":
		owner := me
		if parts[1] == "999" {
			owner = me + 1
		}
		writeJSON(w, 200, map[string]any{"id": 1, "id_str": parts[1], "name": "Albis loop", "distance": 74300.0, "elevation_gain": 880.0,
			"athlete": map[string]any{"id": owner}, "segments": []map[string]any{{"id": 77, "name": "Albis climb", "distance": 6400.0, "average_grade": 6.1}}})
	case path == "/segments/starred":
		writeJSON(w, 200, []map[string]any{{"id": 77, "name": "Albis climb", "activity_type": "Ride", "distance": 6400.0, "average_grade": 6.1,
			"athlete_pr_effort": map[string]any{"pr_elapsed_time": 1355, "pr_date": "2026-11-01", "effort_count": 12}}})
	case len(parts) == 2 && parts[0] == "segments":
		writeJSON(w, 200, map[string]any{"id": 77, "name": "Albis climb", "activity_type": "Ride", "distance": 6400.0, "average_grade": 6.1,
			"maximum_grade": 11.0, "elevation_high": 900.0, "elevation_low": 510.0,
			"athlete_segment_stats": map[string]any{"pr_elapsed_time": 1355, "pr_date": "2026-11-01", "effort_count": 12}})
	case path == "/segment_efforts":
		if !f.summit[me] {
			writeJSON(w, 402, map[string]string{"message": "Payment Required"})
			return
		}
		writeJSON(w, 200, []map[string]any{
			{"id": 1, "elapsed_time": 1400, "start_date_local": "2026-10-12T09:40:00Z", "average_watts": 250.0, "pr_rank": 2, "activity": map[string]any{"id": 2002}},
			{"id": 2, "elapsed_time": 1355, "start_date_local": "2026-11-01T09:40:00Z", "average_watts": 259.0, "pr_rank": 1, "activity": map[string]any{"id": 2001}},
		})
	default:
		writeJSON(w, 404, map[string]string{"message": "Record Not Found"})
	}
}

func (f *fakeStrava) find(me int64, id string) map[string]any {
	for _, a := range f.activities[me] {
		if fmt.Sprint(a["id"]) == id {
			return a
		}
	}
	return nil
}

func (f *fakeStrava) list(w http.ResponseWriter, me int64, q url.Values) {
	after, _ := strconv.ParseInt(q.Get("after"), 10, 64)
	before, _ := strconv.ParseInt(q.Get("before"), 10, 64)
	per, _ := strconv.Atoi(q.Get("per_page"))
	page, _ := strconv.Atoi(q.Get("page"))
	if per <= 0 {
		per = 30
	}
	if page <= 0 {
		page = 1
	}
	var out []map[string]any
	for _, a := range f.activities[me] {
		t, _ := time.Parse(time.RFC3339, a["start_date"].(string))
		if (after > 0 && t.Unix() <= after) || (before > 0 && t.Unix() >= before) {
			continue
		}
		out = append(out, a)
	}
	// Strava's order: newest first, oldest first when after is given.
	slices.SortStableFunc(out, func(x, y map[string]any) int {
		c := strings.Compare(y["start_date"].(string), x["start_date"].(string))
		if after > 0 {
			return -c
		}
		return c
	})
	lo := min((page-1)*per, len(out))
	hi := min(lo+per, len(out))
	writeJSON(w, 200, out[lo:hi])
}

// addAthlete registers an athlete and an authorization code for it.
func (f *fakeStrava) addAthlete(id int64, first, code string, ftp float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.athletes[id] = map[string]any{"id": id, "firstname": first, "lastname": "Test", "city": "Zurich", "country": "Switzerland",
		"measurement_preference": "meters", "ftp": ftp, "weight": 74.0,
		"bikes": []map[string]any{{"id": fmt.Sprintf("b%d", id), "name": first + "'s bike", "primary": true, "distance": 12345000.0}},
		"shoes": []map[string]any{{"id": fmt.Sprintf("g%d", id), "name": first + "'s shoes", "distance": 800000.0}}}
	f.zones[id] = map[string]any{
		"heart_rate": map[string]any{"custom_zones": true, "zones": []map[string]any{{"min": 0, "max": 123}, {"min": 123, "max": 153},
			{"min": 153, "max": 169}, {"min": 169, "max": 184}, {"min": 184, "max": -1}}},
	}
	f.codes[code] = id
}

// addActivity adds a ride for athlete starting at local time start (UTC+1),
// with streams when st is not nil.
func (f *fakeStrava) addActivity(athlete, id int64, name, sport string, start time.Time, st *streamSet, extra map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a := map[string]any{"id": id, "athlete": map[string]any{"id": athlete}, "name": name, "sport_type": sport, "type": sport,
		"start_date": start.Add(-time.Hour).UTC().Format(time.RFC3339), "start_date_local": start.Format("2006-01-02T15:04:05Z"),
		"distance": 10000.0, "moving_time": 1500, "elapsed_time": 1600, "total_elevation_gain": 120.0, "average_speed": 6.6,
		"has_heartrate": true, "average_heartrate": 140.0, "max_heartrate": 170.0, "average_watts": 220.0,
		"weighted_average_watts": 230.0, "device_watts": true, "kilojoules": 330.0, "gear_id": fmt.Sprintf("b%d", athlete),
		"start_latlng": []float64{47.3, 8.5}, "end_latlng": []float64{47.39, 8.5}, "kudos_count": 3, "comment_count": 1,
		"laps": []map[string]any{}, "device_name": "Edge 840"}
	for k, v := range extra {
		a[k] = v
	}
	f.activities[athlete] = append(f.activities[athlete], a)
	if st != nil {
		b, _ := json.Marshal(streamJSON(*st))
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		f.streams[id] = m
	}
}

// streamJSON renders a streamSet the way Strava sends it (key_by_type).
func streamJSON(s streamSet) map[string]any {
	out := map[string]any{}
	put := func(k string, p *struct{ Data floats }) {
		if p != nil {
			out[k] = map[string]any{"data": []float64(p.Data), "series_type": "time", "resolution": "high"}
		}
	}
	put("time", s.Time)
	put("distance", s.Distance)
	put("altitude", s.Altitude)
	put("velocity_smooth", s.Velocity)
	put("heartrate", s.HR)
	put("cadence", s.Cadence)
	put("watts", s.Watts)
	put("temp", s.Temp)
	put("grade_smooth", s.Grade)
	if s.LatLng != nil {
		out["latlng"] = map[string]any{"data": s.LatLng.Data}
	}
	if s.Moving != nil {
		out["moving"] = map[string]any{"data": s.Moving.Data}
	}
	return out
}

// newTestService is a service over the fake, its clock the fake's.
func newTestService(t *testing.T, f *fakeStrava) (*service, http.Handler, string) {
	t.Helper()
	dir := t.TempDir()
	base, _ := url.Parse(f.srv.URL)
	cfg := config{token: testToken, clientID: testClientID, clientSecret: testSecret, data: dir, base: base}
	svc, err := newService(cfg, f.srv.Client(), fileStore{dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	svc.now = f.clock
	return svc, newServer(cfg, svc, "test"), dir
}

func request(t *testing.T, h http.Handler, method, path, user, body string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	if user != "" {
		req.Header.Set("X-Loom-User", user)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

type toolResult struct {
	Text    string
	Sources []map[string]any
	Files   []struct {
		URI, MimeType, Text string
	}
	IsError bool
}

// callTool makes a legacy-protocol tools/call for user.
func callTool(t *testing.T, h http.Handler, user, name, args string) toolResult {
	t.Helper()
	body := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":%q,"arguments":%s}}`, name, args)
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	if user != "" {
		req.Header.Set("X-Loom-User", user)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var r struct {
		Result struct {
			Content []struct {
				Type     string `json:"type"`
				Text     string `json:"text"`
				Resource struct {
					URI      string `json:"uri"`
					MimeType string `json:"mimeType"`
					Text     string `json:"text"`
				} `json:"resource"`
			} `json:"content"`
			StructuredContent struct {
				Sources []map[string]any `json:"sources"`
			} `json:"structuredContent"`
			IsError bool `json:"isError"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &r); err != nil || len(r.Result.Content) == 0 {
		t.Fatalf("%s: %d %s", name, rec.Code, rec.Body.String())
	}
	out := toolResult{Text: r.Result.Content[0].Text, Sources: r.Result.StructuredContent.Sources, IsError: r.Result.IsError}
	for _, c := range r.Result.Content[1:] {
		out.Files = append(out.Files, struct{ URI, MimeType, Text string }{c.Resource.URI, c.Resource.MimeType, c.Resource.Text})
	}
	return out
}

func (r toolResult) file(name string) string {
	for _, f := range r.Files {
		if f.URI == "file:///"+name {
			return f.Text
		}
	}
	return ""
}

// connect walks a user through PUT /connection with the athlete's code.
func connect(t *testing.T, h http.Handler, user, code string) map[string]any {
	t.Helper()
	status, body := request(t, h, http.MethodPut, "/connection", user, fmt.Sprintf(`{"code":%q,"scope":"read,activity:read_all"}`, code))
	if status != 200 {
		t.Fatalf("connect %s: %d %v", user, status, body)
	}
	return body
}
