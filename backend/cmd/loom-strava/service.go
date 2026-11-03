package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/trick77/loom/internal/mcpserver"
)

// wantedScopes is what loom asks Strava for: everything the user can read,
// Only Me activities and privacy zones included, and nothing that writes.
var wantedScopes = []string{"read_all", "profile:read_all", "activity:read_all"}

const (
	// refreshEarly refreshes an access token this long before it expires, so
	// no call goes out with one that dies on the way.
	refreshEarly = 5 * time.Minute
	// maxCodeBytes bounds an authorization code; Strava's are 40 hex chars.
	maxCodeBytes = 512
	// maxStateBytes bounds the OAuth state loom passes through.
	maxStateBytes = 512
	// maxResponseBytes bounds one Strava answer: a long ride's streams are a
	// few MB.
	maxResponseBytes = 64 << 20
	// okSaveInterval is how stale the stored "last ok" may get before a
	// successful call writes it: not every call rewrites and fsyncs the file.
	okSaveInterval = 10 * time.Minute
)

var (
	errNotConnected = errors.New("strava is not connected")
	errExpired      = errors.New("strava sign-in expired")
	// errPermission: Strava refused a call right after a fresh token, so a
	// scope is missing (a revoked app fails the refresh itself).
	errPermission = errors.New("strava refused access: a permission is missing")
	// errMalformed: the request cannot be acted on at all.
	errMalformed = errors.New("malformed request")
	// errRejected: Strava refused the authorization code.
	errRejected = errors.New("strava refused the authorization code")
	// errStore: /data could not be written; nothing changed.
	errStore = errors.New("could not store the Strava state")
)

// apiError is a Strava answer outside 2xx.
type apiError struct {
	status int
	path   string
	msg    string
}

func (e *apiError) Error() string {
	if e.msg != "" {
		return fmt.Sprintf("strava %s: %d %s", e.path, e.status, e.msg)
	}
	return fmt.Sprintf("strava %s: %d", e.path, e.status)
}

func isStatus(err error, codes ...int) bool {
	var ae *apiError
	return errors.As(err, &ae) && slices.Contains(codes, ae.status)
}

// summitRefused reports Strava's answer to a Summit-only endpoint without a
// subscription.
func summitRefused(err error) bool {
	return isStatus(err, http.StatusPaymentRequired, http.StatusForbidden)
}

func notFound(err error) bool { return isStatus(err, http.StatusNotFound) }

// rateLimitedError: the app's budget is (nearly) spent until reset.
type rateLimitedError struct{ reset time.Time }

func (e *rateLimitedError) Error() string {
	return "Strava rate limit reached: resets at " + e.reset.UTC().Format("15:04") + " (UTC)"
}

// slot is one user's locks and cache.
type slot struct {
	// tokenMu serializes everything that reads or replaces the user's token
	// pair: a refresh, a connect, a disconnect. Strava kills the old refresh
	// token the moment it hands out a new one, so two refreshes at once
	// would leave one caller holding a dead token.
	tokenMu sync.Mutex
	// dataMu serializes the user's metric file.
	dataMu sync.Mutex
	cache  *userCache
}

type service struct {
	clientID, clientSecret string
	base                   *url.URL
	http                   *http.Client
	store                  fileStore
	now                    func() time.Time
	rate                   *rateBook

	// mu guards st, gen and slots: every user's lookups go through it, so it
	// is never held across a disk write.
	mu    sync.Mutex
	st    state
	gen   uint64 // bumped for every snapshot taken to be saved
	slots map[string]*slot

	// saveMu serializes the writes of state.json; savedGen is the newest
	// snapshot on disk, so an older one never overwrites it.
	saveMu   sync.Mutex
	savedGen uint64
	// beforeSave, when set (tests), runs under saveMu before each write.
	beforeSave func()
}

// snapshotLocked copies the state for a save outside s.mu. Caller holds
// s.mu, right after its change: a later snapshot always contains it.
func (s *service) snapshotLocked() (state, uint64) {
	users := make(map[string]connection, len(s.st.Users))
	for k, v := range s.st.Users {
		users[k] = v
	}
	s.gen++
	return state{Users: users}, s.gen
}

// persist writes snap unless a later snapshot is already on disk, which
// holds its change too. Caller must not hold s.mu.
func (s *service) persist(snap state, gen uint64) error {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	if gen <= s.savedGen {
		return nil
	}
	if s.beforeSave != nil {
		s.beforeSave()
	}
	if err := s.store.save(snap); err != nil {
		return err
	}
	s.savedGen = gen
	return nil
}

// saveNow snapshots and persists, logging a failure: for changes that are
// only a cost when lost (a "last ok" time, an error note).
func (s *service) saveNow() {
	s.mu.Lock()
	snap, gen := s.snapshotLocked()
	s.mu.Unlock()
	if err := s.persist(snap, gen); err != nil {
		slog.Error("strava: saving state failed", "err", err)
	}
}

func newService(cfg config, client *http.Client, store fileStore) (*service, error) {
	st, err := store.load()
	if err != nil {
		return nil, fmt.Errorf("load state: %w", err)
	}
	return &service{
		clientID: cfg.clientID, clientSecret: cfg.clientSecret, base: cfg.base,
		http: client, store: store, now: time.Now, rate: newRateBook(),
		st: st, slots: map[string]*slot{},
	}, nil
}

func (s *service) connections() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.st.Users)
}

func (s *service) slot(user string) *slot {
	s.mu.Lock()
	defer s.mu.Unlock()
	sl, ok := s.slots[user]
	if !ok {
		sl = &slot{cache: newUserCache()}
		s.slots[user] = sl
	}
	return sl
}

// cache is the user's cache. Read under s.mu: a disconnect swaps it.
func (s *service) cache(user string) *userCache {
	sl := s.slot(user)
	s.mu.Lock()
	defer s.mu.Unlock()
	return sl.cache
}

func (s *service) dropCache(user string) {
	sl := s.slot(user)
	s.mu.Lock()
	sl.cache = newUserCache()
	s.mu.Unlock()
}

func (s *service) conn(user string) (connection, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.st.Users[user]
	return c, ok
}

func (s *service) apiURL(path string) string { return s.base.JoinPath("/api/v3", path).String() }

// status is what loom's settings page shows. No token ever leaves.
type status struct {
	State         string      `json:"state"` // never, connected, expired, error
	Athlete       *athleteRef `json:"athlete,omitempty"`
	Scopes        []string    `json:"scopes"`
	MissingScopes []string    `json:"missingScopes"`
	ConnectedAt   time.Time   `json:"connectedAt,omitzero"`
	LastOKAt      time.Time   `json:"lastOkAt,omitzero"`
	LastError     string      `json:"lastError,omitempty"`
}

type athleteRef struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

func (s *service) status(user string) status {
	c, ok := s.conn(user)
	if !ok {
		return status{State: "never", Scopes: []string{}, MissingScopes: []string{}}
	}
	granted := splitScopes(c.Scope)
	out := status{
		Athlete: &athleteRef{ID: c.AthleteID, Name: c.AthleteName},
		Scopes:  granted, MissingScopes: missingScopes(granted),
		ConnectedAt: c.ConnectedAt, LastOKAt: c.LastOKAt, LastError: c.LastError,
	}
	switch {
	case c.Expired:
		out.State = "expired"
	case c.LastError != "":
		out.State = "error"
	default:
		out.State = "connected"
	}
	return out
}

// check makes one cheap Strava call, so the status reflects its answer now.
func (s *service) check(ctx context.Context, user string) status {
	if c, ok := s.conn(user); ok && !c.Expired {
		var a athlete
		if err := s.get(ctx, user, "/athlete", nil, &a); err != nil {
			slog.InfoContext(ctx, "strava check failed", "err", err)
		}
	}
	return s.status(user)
}

// splitScopes reads a granted scope list. The consent redirect separates with
// commas, the token answer with spaces; a connection stored before either was
// handled is read the same way.
func splitScopes(scope string) []string {
	out := []string{}
	for _, p := range strings.FieldsFunc(scope, func(r rune) bool { return r == ',' || unicode.IsSpace(r) }) {
		if !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	return out
}

func missingScopes(granted []string) []string {
	out := []string{}
	for _, w := range wantedScopes {
		if !slices.Contains(granted, w) {
			out = append(out, w)
		}
	}
	return out
}

// authorizeURL is Strava's consent page for redirectURI. The client id stays
// here; loom only forwards the browser.
func (s *service) authorizeURL(redirectURI, oauthState string) (string, error) {
	u, err := url.Parse(redirectURI)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return "", errMalformed
	}
	if oauthState == "" || len(oauthState) > maxStateBytes {
		return "", errMalformed
	}
	q := url.Values{
		"client_id":       {s.clientID},
		"redirect_uri":    {redirectURI},
		"response_type":   {"code"},
		"approval_prompt": {"force"},
		"scope":           {strings.Join(wantedScopes, ",")},
		"state":           {oauthState},
	}
	return s.base.JoinPath("/oauth/authorize").String() + "?" + q.Encode(), nil
}

// tokenResponse is Strava's answer to both grants. The athlete comes only
// with the code exchange; scope is not documented there, so the consent
// page's own report (loom passes it) is the fallback.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    int64  `json:"expires_at"`
	Scope        string `json:"scope"`
	Athlete      *struct {
		ID        int64  `json:"id"`
		Firstname string `json:"firstname"`
		Lastname  string `json:"lastname"`
	} `json:"athlete"`
}

// postToken calls Strava's token endpoint. status is Strava's HTTP status
// (0 when it was not reached).
func (s *service) postToken(ctx context.Context, form url.Values) (tokenResponse, int, error) {
	form.Set("client_id", s.clientID)
	form.Set("client_secret", s.clientSecret)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.apiURL("/oauth/token"), strings.NewReader(form.Encode()))
	if err != nil {
		return tokenResponse{}, 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.http.Do(req)
	if err != nil {
		return tokenResponse{}, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return tokenResponse{}, resp.StatusCode, err
	}
	if resp.StatusCode != http.StatusOK {
		return tokenResponse{}, resp.StatusCode, &apiError{status: resp.StatusCode, path: "/oauth/token", msg: stravaMessage(body)}
	}
	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return tokenResponse{}, resp.StatusCode, fmt.Errorf("strava token answer: %w", err)
	}
	if tr.AccessToken == "" || tr.RefreshToken == "" {
		return tokenResponse{}, resp.StatusCode, errors.New("strava sent no token")
	}
	return tr, resp.StatusCode, nil
}

// connect exchanges an authorization code for the user's token pair and
// stores it with the granted scope. A different Strava athlete than before
// starts with an empty metric store.
func (s *service) connect(ctx context.Context, user, code, scope string) (status, error) {
	code = strings.TrimSpace(code)
	if code == "" || len(code) > maxCodeBytes || strings.ContainsFunc(code, func(r rune) bool { return r <= ' ' || r > 0x7e }) {
		return status{}, errMalformed
	}
	sl := s.slot(user)
	sl.tokenMu.Lock()
	defer sl.tokenMu.Unlock()
	tr, code2, err := s.postToken(ctx, url.Values{"grant_type": {"authorization_code"}, "code": {code}})
	if code2 == http.StatusBadRequest || code2 == http.StatusUnauthorized {
		return status{}, errRejected
	}
	if err != nil {
		return status{}, err
	}
	granted := tr.Scope
	if granted == "" {
		granted = scope
	}
	next := connection{
		AccessToken: tr.AccessToken, RefreshToken: tr.RefreshToken, ExpiresAt: tr.ExpiresAt,
		Scope: strings.Join(splitScopes(granted), ","), ConnectedAt: s.now().UTC(), LastOKAt: s.now().UTC(),
	}
	s.mu.Lock()
	prev, had := s.st.Users[user]
	// An answer without the athlete keeps the one known; the first tool
	// call learns it from /athlete otherwise (learnAthlete).
	next.AthleteID, next.AthleteName = prev.AthleteID, prev.AthleteName
	if tr.Athlete != nil && tr.Athlete.ID != 0 {
		next.AthleteID = tr.Athlete.ID
		next.AthleteName = strings.TrimSpace(tr.Athlete.Firstname + " " + tr.Athlete.Lastname)
	}
	s.st.Users[user] = next
	snap, gen := s.snapshotLocked()
	s.mu.Unlock()
	if err := s.persist(snap, gen); err != nil {
		// Undone in memory and, best effort, on disk: tokenMu keeps every
		// other change of this user's entry out meanwhile.
		s.mu.Lock()
		if had {
			s.st.Users[user] = prev
		} else {
			delete(s.st.Users, user)
		}
		s.mu.Unlock()
		s.saveNow()
		slog.ErrorContext(ctx, "strava: saving the connection failed", "err", err)
		return status{}, errStore
	}
	// Only a different, known athlete wipes the metrics.
	if had && prev.AthleteID != 0 && next.AthleteID != prev.AthleteID {
		s.deleteMetricsOf(ctx, user)
	}
	s.dropCache(user)
	slog.InfoContext(ctx, "strava connected", "athlete", next.AthleteID)
	return s.status(user), nil
}

func (s *service) deleteMetricsOf(ctx context.Context, user string) {
	sl := s.slot(user)
	sl.dataMu.Lock()
	defer sl.dataMu.Unlock()
	if err := s.store.deleteMetrics(user); err != nil {
		slog.ErrorContext(ctx, "strava: deleting the previous athlete's metrics failed", "err", err)
	}
}

// learnAthlete records the athlete /athlete answered for user's token when
// the connection does not know it (a token answer without one) or knows
// another: /athlete over the user's own token is the authority. Another
// athlete's metrics go.
func (s *service) learnAthlete(ctx context.Context, user string, a athlete) {
	if a.ID == 0 {
		return
	}
	s.mu.Lock()
	cur, ok := s.st.Users[user]
	if !ok || cur.AthleteID == a.ID {
		s.mu.Unlock()
		return
	}
	old := cur.AthleteID
	cur.AthleteID = a.ID
	if cur.AthleteName == "" || old != 0 {
		cur.AthleteName = a.name()
	}
	s.st.Users[user] = cur
	snap, gen := s.snapshotLocked()
	s.mu.Unlock()
	if err := s.persist(snap, gen); err != nil {
		slog.ErrorContext(ctx, "strava: saving state failed", "err", err)
	}
	sl := s.slot(user)
	sl.dataMu.Lock()
	defer sl.dataMu.Unlock()
	if m, _ := s.store.loadMetrics(user); old != 0 || (m.Athlete != 0 && m.Athlete != a.ID) {
		if err := s.store.deleteMetrics(user); err != nil {
			slog.ErrorContext(ctx, "strava: deleting another athlete's metrics failed", "err", err)
		}
	}
}

// disconnect revokes the user's token at Strava and deletes everything kept
// for the user: the entry, the metric file, the cache. Strava failing to
// revoke does not keep the data here (the API agreement wants it gone).
func (s *service) disconnect(ctx context.Context, user string) (status, error) {
	sl := s.slot(user)
	sl.tokenMu.Lock()
	defer sl.tokenMu.Unlock()
	c, ok := s.conn(user)
	if ok {
		if err := s.revoke(ctx, c); err != nil {
			slog.WarnContext(ctx, "strava: revoking the token failed; deleting it here anyway", "err", err)
		}
	}
	// The metric file and the entry go under dataMu together: an analysis
	// saving metrics checks the connection under the same lock, so it can
	// never write the file back between the two.
	sl.dataMu.Lock()
	defer sl.dataMu.Unlock()
	if err := s.store.deleteMetrics(user); err != nil {
		slog.ErrorContext(ctx, "strava: deleting metrics failed", "err", err)
		return status{}, errStore
	}
	if ok {
		s.mu.Lock()
		cur := s.st.Users[user]
		delete(s.st.Users, user)
		snap, gen := s.snapshotLocked()
		s.mu.Unlock()
		if err := s.persist(snap, gen); err != nil {
			s.mu.Lock()
			s.st.Users[user] = cur
			s.mu.Unlock()
			s.saveNow()
			slog.ErrorContext(ctx, "strava: saving the disconnect failed", "err", err)
			return status{}, errStore
		}
	}
	s.dropCache(user)
	return s.status(user), nil
}

// revoke ends the user's authorization at Strava: /oauth/revoke (Basic auth,
// the documented endpoint since 2026-06-01), or, where Strava does not have
// it (404, 405), the legacy /oauth/deauthorize with the access token.
func (s *service) revoke(ctx context.Context, c connection) error {
	tok := c.RefreshToken
	if tok == "" {
		tok = c.AccessToken
	}
	err := s.postOAuth(ctx, "/oauth/revoke", url.Values{"token": {tok}}, true)
	if isStatus(err, http.StatusNotFound, http.StatusMethodNotAllowed) && c.AccessToken != "" {
		return s.postOAuth(ctx, "/oauth/deauthorize", url.Values{"access_token": {c.AccessToken}}, false)
	}
	return err
}

func (s *service) postOAuth(ctx context.Context, path string, form url.Values, basic bool) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.base.JoinPath(path).String(), strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	if basic {
		req.SetBasicAuth(s.clientID, s.clientSecret)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return &apiError{status: resp.StatusCode, path: path, msg: stravaMessage(body)}
	}
	return nil
}

// accessToken is the user's access token, refreshed first when it is about
// to expire.
func (s *service) accessToken(ctx context.Context, user string) (string, error) {
	sl := s.slot(user)
	sl.tokenMu.Lock()
	defer sl.tokenMu.Unlock()
	c, ok := s.conn(user)
	switch {
	case !ok:
		return "", errNotConnected
	case c.Expired:
		return "", errExpired
	case time.Unix(c.ExpiresAt, 0).After(s.now().Add(refreshEarly)):
		return c.AccessToken, nil
	}
	return s.refreshLocked(ctx, user, c)
}

// refreshAfter401 refreshes once after Strava refused stale, unless another
// call already did.
func (s *service) refreshAfter401(ctx context.Context, user, stale string) error {
	sl := s.slot(user)
	sl.tokenMu.Lock()
	defer sl.tokenMu.Unlock()
	c, ok := s.conn(user)
	switch {
	case !ok:
		return errNotConnected
	case c.Expired:
		return errExpired
	case c.AccessToken != stale:
		return nil
	}
	_, err := s.refreshLocked(ctx, user, c)
	return err
}

// refreshLocked trades c's refresh token for a new pair and persists it
// before anyone uses it: the old refresh token is dead the moment Strava
// answers, so a crash between answer and save would lose the connection.
// Caller holds the user's tokenMu.
func (s *service) refreshLocked(ctx context.Context, user string, c connection) (string, error) {
	tr, code, err := s.postToken(ctx, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {c.RefreshToken}})
	if code == http.StatusBadRequest || code == http.StatusUnauthorized {
		s.markExpired(user, c.RefreshToken, err)
		return "", errExpired
	}
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	cur, ok := s.st.Users[user]
	if !ok || cur.RefreshToken != c.RefreshToken {
		s.mu.Unlock()
		return "", errNotConnected
	}
	cur.AccessToken, cur.RefreshToken, cur.ExpiresAt = tr.AccessToken, tr.RefreshToken, tr.ExpiresAt
	s.st.Users[user] = cur
	snap, gen := s.snapshotLocked()
	s.mu.Unlock()
	// Waited for, under tokenMu but not s.mu: this user's calls wait for the
	// disk, nobody else's.
	if err := s.persist(snap, gen); err != nil {
		// Kept in memory regardless: the old pair is already dead, so this
		// one is the only working connection until the disk takes it.
		slog.ErrorContext(ctx, "strava: saving the refreshed token failed", "err", err)
	}
	return tr.AccessToken, nil
}

func (s *service) markExpired(user, refreshToken string, cause error) {
	s.mu.Lock()
	cur, ok := s.st.Users[user]
	if !ok || cur.RefreshToken != refreshToken {
		s.mu.Unlock()
		return
	}
	cur.Expired = true
	cur.LastError = "Strava refused the refresh token"
	if cause != nil {
		cur.LastError = cause.Error()
	}
	s.st.Users[user] = cur
	snap, gen := s.snapshotLocked()
	s.mu.Unlock()
	if err := s.persist(snap, gen); err != nil {
		slog.Error("strava: saving state failed", "err", err)
	}
}

// record notes what a call made with token learned about the connection. A
// call that raced a token change says nothing about the new pair.
func (s *service) record(user, token string, err error) {
	s.mu.Lock()
	if !s.recordLocked(user, token, err) {
		s.mu.Unlock()
		return
	}
	snap, gen := s.snapshotLocked()
	s.mu.Unlock()
	if err := s.persist(snap, gen); err != nil {
		slog.Error("strava: saving state failed", "err", err)
	}
}

// recordLocked applies record's change and says whether it is worth a
// write. Caller holds s.mu.
func (s *service) recordLocked(user, token string, err error) bool {
	cur, ok := s.st.Users[user]
	if !ok || cur.AccessToken != token {
		return false
	}
	switch {
	case err == nil:
		before := cur
		cur.LastOKAt, cur.LastError = s.now().UTC(), ""
		s.st.Users[user] = cur
		// Only "last ok" moved: kept in memory, written now and then.
		return before.LastError != "" || cur.LastOKAt.Sub(before.LastOKAt) >= okSaveInterval
	case errors.Is(err, errPermission), isStatus(err, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable):
		cur.LastError = err.Error()
		s.st.Users[user] = cur
		return true
	default:
		// A missing activity, a Summit refusal, the rate limit, a timeout:
		// nothing about the connection.
		return false
	}
}

// get reads one Strava API path for user into into. It refuses without a
// call when the rate budget is nearly spent, and retries once with a fresh
// token after a 401.
func (s *service) get(ctx context.Context, user, path string, q url.Values, into any) error {
	for attempt := 0; ; attempt++ {
		if reset, ok := s.rate.blocked(s.now()); ok {
			return &rateLimitedError{reset: reset}
		}
		tok, err := s.accessToken(ctx, user)
		if err != nil {
			return err
		}
		code, body, err := s.do(ctx, path, q, tok)
		if err == nil && code == http.StatusUnauthorized && attempt == 0 {
			if err := s.refreshAfter401(ctx, user, tok); err != nil {
				return err
			}
			continue
		}
		switch {
		case err != nil:
		case code == http.StatusUnauthorized:
			err = errPermission
		case code == http.StatusTooManyRequests:
			reset, _ := s.rate.blocked(s.now())
			err = &rateLimitedError{reset: reset}
		case code/100 != 2:
			err = &apiError{status: code, path: path, msg: stravaMessage(body)}
		}
		s.record(user, tok, err)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(body, into); err != nil {
			return fmt.Errorf("strava %s: %w", path, err)
		}
		return nil
	}
}

func (s *service) do(ctx context.Context, path string, q url.Values, tok string) (int, []byte, error) {
	u := s.apiURL(path)
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Accept", "application/json")
	resp, err := s.http.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusTooManyRequests {
		s.rate.exhaust(resp.Header, s.now())
	} else {
		s.rate.observe(resp.Header, s.now())
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	return resp.StatusCode, body, err
}

// stravaMessage is the short reason in a Strava error body.
func stravaMessage(body []byte) string {
	var e struct {
		Message string `json:"message"`
		Errors  []struct {
			Field string `json:"field"`
			Code  string `json:"code"`
		} `json:"errors"`
	}
	if json.Unmarshal(body, &e) != nil {
		return ""
	}
	msg := e.Message
	for _, x := range e.Errors {
		msg += fmt.Sprintf(" (%s %s)", x.Field, x.Code)
	}
	return mcpserver.OneLine(msg, 200)
}

// transient reports a failure that is the call's, not the connection's.
func transient(err error) bool {
	var ne net.Error
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
		(errors.As(err, &ne) && ne.Timeout())
}

// cached reads path through c: a hit costs no Strava call.
func cached[T any](ctx context.Context, s *service, user string, c *lru, key, path string, q url.Values) (T, error) {
	if v, ok := c.get(key, s.now()); ok {
		if t, ok := v.(T); ok {
			return t, nil
		}
	}
	var out T
	if err := s.get(ctx, user, path, q, &out); err != nil {
		return out, err
	}
	c.put(key, out, s.now())
	return out, nil
}
