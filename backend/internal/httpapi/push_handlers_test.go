package httpapi

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/trick77/loom/internal/schedule"
	"github.com/trick77/loom/internal/webpush"
)

// fakePush records what loom pushed; gone endpoints answer ErrGone.
type fakePush struct {
	mu   sync.Mutex
	sent []pushSent
	gone map[string]bool
	err  error
	done chan struct{}
}

type pushSent struct {
	endpoint string
	payload  pushPayload
}

func (f *fakePush) PublicKey() string { return "BPUBLIC" }

func (f *fakePush) Send(_ context.Context, sub webpush.Subscription, msg webpush.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	var p pushPayload
	_ = json.Unmarshal(msg.Payload, &p)
	f.sent = append(f.sent, pushSent{endpoint: sub.Endpoint, payload: p})
	if f.done != nil {
		defer func() { f.done <- struct{}{} }()
	}
	if f.gone[sub.Endpoint] {
		return webpush.ErrGone
	}
	return f.err
}

func (f *fakePush) sentTo() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []string{}
	for _, s := range f.sent {
		out = append(out, s.endpoint)
	}
	return out
}

// browserKeys is a valid p256dh/auth pair.
func browserKeys(t *testing.T) (string, string) {
	t.Helper()
	key, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	auth := make([]byte, 16)
	_, _ = rand.Read(auth)
	return base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()), base64.RawURLEncoding.EncodeToString(auth)
}

func newPushFixture(t *testing.T, push *fakePush) *scheduleFixture {
	t.Helper()
	f := newScheduleFixture(t)
	f.deps.Push = push
	f.deps.PushStore = webpush.NewStore(f.db)
	f.handler = newAuthenticatedServer(t, f.deps)
	return f
}

func subscribeBody(t *testing.T, endpoint string) string {
	p256dh, auth := browserKeys(t)
	return fmt.Sprintf(`{"endpoint":%q,"expirationTime":null,"applicationServerKey":"BPUBLIC","keys":{"p256dh":%q,"auth":%q}}`, endpoint, p256dh, auth)
}

func TestPushConfigIsOffWithoutASender(t *testing.T) {
	f := newScheduleFixture(t)
	if rec := f.do(t, http.MethodGet, "/api/push", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("GET /api/push without push = %d", rec.Code)
	}
	var me meResponse
	_ = json.Unmarshal(f.do(t, http.MethodGet, "/api/me", "").Body.Bytes(), &me)
	if me.Push {
		t.Error("me.push true without a sender")
	}
}

func TestPushSubscribeAndUnsubscribe(t *testing.T) {
	f := newPushFixture(t, &fakePush{})
	rec := f.do(t, http.MethodGet, "/api/push", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"publicKey":"BPUBLIC"`) {
		t.Fatalf("config = %d %s", rec.Code, rec.Body)
	}
	var me meResponse
	_ = json.Unmarshal(f.do(t, http.MethodGet, "/api/me", "").Body.Bytes(), &me)
	if !me.Push {
		t.Error("me.push false with a sender")
	}

	endpoint := "https://web.push.apple.com/QGx1c3Q"
	if rec := f.do(t, http.MethodPut, "/api/push/subscription", subscribeBody(t, endpoint)); rec.Code != http.StatusNoContent {
		t.Fatalf("subscribe = %d %s", rec.Code, rec.Body)
	}
	list, _ := webpush.NewStore(f.db).List(context.Background(), testUser.ID)
	if len(list) != 1 || list[0].Endpoint != endpoint {
		t.Fatalf("stored %+v", list)
	}
	if rec := f.do(t, http.MethodDelete, "/api/push/subscription", fmt.Sprintf(`{"endpoint":%q}`, endpoint)); rec.Code != http.StatusNoContent {
		t.Fatalf("unsubscribe = %d %s", rec.Code, rec.Body)
	}
	if list, _ := webpush.NewStore(f.db).List(context.Background(), testUser.ID); len(list) != 0 {
		t.Fatalf("still stored %+v", list)
	}
}

func TestPushSubscribeRefusesBadSubscriptions(t *testing.T) {
	f := newPushFixture(t, &fakePush{})
	p256dh, auth := browserKeys(t)
	for name, body := range map[string]string{
		"internal host": subscribeBody(t, "https://10.0.0.1/push"),
		"http":          subscribeBody(t, "http://web.push.apple.com/x"),
		"bad p256dh":    fmt.Sprintf(`{"endpoint":"https://web.push.apple.com/x","keys":{"p256dh":"AAAA","auth":%q}}`, auth),
		"short auth":    fmt.Sprintf(`{"endpoint":"https://web.push.apple.com/x","keys":{"p256dh":%q,"auth":"AAAA"}}`, p256dh),
		"no key":        fmt.Sprintf(`{"endpoint":"https://web.push.apple.com/x","keys":{"p256dh":%q,"auth":%q}}`, p256dh, auth),
	} {
		if rec := f.do(t, http.MethodPut, "/api/push/subscription", body); rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body)
		}
	}
	if rec := f.do(t, http.MethodPut, "/api/push/subscription", "{"); rec.Code != http.StatusBadRequest {
		t.Errorf("malformed body: %d", rec.Code)
	}
}

func TestPushSubscribeToleratesExtraFieldsAndRefusesATakenEndpoint(t *testing.T) {
	f := newPushFixture(t, &fakePush{})
	p256dh, auth := browserKeys(t)
	body := fmt.Sprintf(`{"endpoint":"https://web.push.apple.com/x","expirationTime":null,"vendor":1,"applicationServerKey":"BPUBLIC","keys":{"p256dh":%q,"auth":%q,"future":"y"}}`, p256dh, auth)
	if rec := f.do(t, http.MethodPut, "/api/push/subscription", body); rec.Code != http.StatusNoContent {
		t.Fatalf("subscribe with extra fields = %d %s", rec.Code, rec.Body)
	}
	if err := webpush.NewStore(f.db).Save(context.Background(), f.otherUID, webpush.Subscription{Endpoint: "https://web.push.apple.com/theirs", P256dh: "p", Auth: "a"}, "", "BPUBLIC"); err != nil {
		t.Fatal(err)
	}
	if rec := f.do(t, http.MethodPut, "/api/push/subscription", subscribeBody(t, "https://web.push.apple.com/theirs")); rec.Code != http.StatusConflict {
		t.Fatalf("taking another user's endpoint = %d %s", rec.Code, rec.Body)
	}
}

// A browser that renewed with an old key (a worker resubscribing from its
// stale options) is recorded with that key, so loom drops it instead of
// sending into a 403 forever.
func TestPushSubscribeRecordsTheKeyTheBrowserUsed(t *testing.T) {
	push := &fakePush{}
	f := newPushFixture(t, push)
	p256dh, auth := browserKeys(t)
	endpoint := "https://web.push.apple.com/old-key"
	body := fmt.Sprintf(`{"endpoint":%q,"applicationServerKey":"BOLDKEY","keys":{"p256dh":%q,"auth":%q}}`, endpoint, p256dh, auth)
	if rec := f.do(t, http.MethodPut, "/api/push/subscription", body); rec.Code != http.StatusNoContent {
		t.Fatalf("subscribe = %d %s", rec.Code, rec.Body)
	}
	if rec := f.do(t, http.MethodPost, "/api/push/test", fmt.Sprintf(`{"endpoint":%q,"title":"t","body":"b"}`, endpoint)); rec.Code != http.StatusGone {
		t.Fatalf("test to an old-key row = %d %s", rec.Code, rec.Body)
	}
	if got := push.sentTo(); len(got) != 0 {
		t.Errorf("sent to %v", got)
	}
}

func TestPushTestGoesToThisDeviceOnly(t *testing.T) {
	push := &fakePush{}
	f := newPushFixture(t, push)
	mine, other := "https://web.push.apple.com/mine", "https://fcm.googleapis.com/fcm/send/other"
	f.do(t, http.MethodPut, "/api/push/subscription", subscribeBody(t, mine))
	f.do(t, http.MethodPut, "/api/push/subscription", subscribeBody(t, other))

	rec := f.do(t, http.MethodPost, "/api/push/test", fmt.Sprintf(`{"endpoint":%q,"title":"Test","body":"Works"}`, mine))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("test = %d %s", rec.Code, rec.Body)
	}
	if got := push.sentTo(); len(got) != 1 || got[0] != mine {
		t.Fatalf("sent to %v", got)
	}
	if p := push.sent[0].payload; p.Title != "Test" || p.Body != "Works" || p.URL != "/" {
		t.Errorf("payload %+v", p)
	}
	// Another user's endpoint is not this user's device.
	if err := webpush.NewStore(f.db).Save(context.Background(), f.otherUID, webpush.Subscription{Endpoint: "https://web.push.apple.com/theirs", P256dh: "p", Auth: "a"}, "", "BPUBLIC"); err != nil {
		t.Fatal(err)
	}
	if rec := f.do(t, http.MethodPost, "/api/push/test", `{"endpoint":"https://web.push.apple.com/theirs","title":"x","body":"y"}`); rec.Code != http.StatusNotFound {
		t.Errorf("other user's endpoint = %d", rec.Code)
	}
}

func TestPushTestReportsAFailedDelivery(t *testing.T) {
	push := &fakePush{err: fmt.Errorf("push service answered 403: BadJwtToken")}
	f := newPushFixture(t, push)
	endpoint := "https://web.push.apple.com/mine"
	f.do(t, http.MethodPut, "/api/push/subscription", subscribeBody(t, endpoint))
	if rec := f.do(t, http.MethodPost, "/api/push/test", fmt.Sprintf(`{"endpoint":%q,"title":"t","body":"b"}`, endpoint)); rec.Code != http.StatusBadGateway {
		t.Fatalf("failed test = %d %s", rec.Code, rec.Body)
	}
	push.gone = map[string]bool{endpoint: true}
	if rec := f.do(t, http.MethodPost, "/api/push/test", fmt.Sprintf(`{"endpoint":%q,"title":"t","body":"b"}`, endpoint)); rec.Code != http.StatusGone {
		t.Fatalf("gone test = %d %s", rec.Code, rec.Body)
	}
	if list, _ := webpush.NewStore(f.db).List(context.Background(), testUser.ID); len(list) != 0 {
		t.Errorf("gone subscription kept: %+v", list)
	}
}

func TestNotifyPushesToEveryDeviceAndDropsGoneOnes(t *testing.T) {
	push := &fakePush{done: make(chan struct{}, 4)}
	f := newPushFixture(t, push)
	st := webpush.NewStore(f.db)
	ctx := context.Background()
	phone, laptop := "https://web.push.apple.com/phone", "https://fcm.googleapis.com/fcm/send/laptop"
	for _, e := range []string{phone, laptop} {
		if err := st.Save(ctx, testUser.ID, webpush.Subscription{Endpoint: e, P256dh: "p", Auth: "a"}, "", "BPUBLIC"); err != nil {
			t.Fatal(err)
		}
	}
	_ = st.Save(ctx, f.otherUID, webpush.Subscription{Endpoint: "https://web.push.apple.com/theirs", P256dh: "p", Auth: "a"}, "", "BPUBLIC")
	// Subscribed before the session secret was rotated: never sent, dropped.
	stale := "https://web.push.apple.com/stale"
	_ = st.Save(ctx, testUser.ID, webpush.Subscription{Endpoint: stale, P256dh: "p", Auth: "a"}, "", "BOLDKEY")
	push.gone = map[string]bool{laptop: true}

	s := newServer(f.deps)
	sid, tid := "sch_1", "thr_1"
	s.notify(ctx, testUser.ID, schedule.Notification{
		Kind: schedule.KindScheduleRun, ScheduleID: &sid, ThreadID: &tid,
		Title: "Cinema scout", Body: strings.Repeat("é", 1000),
	})
	for range 2 {
		select {
		case <-push.done:
		case <-time.After(5 * time.Second):
			t.Fatalf("pushes did not arrive: %v", push.sentTo())
		}
	}
	got := push.sentTo()
	if len(got) != 2 || slices.Contains(got, stale) {
		t.Fatalf("sent to %v", got)
	}
	p := push.sent[0].payload
	if p.Title != "Cinema scout" || p.URL != "/thread/thr_1" || p.Tag != "schedule-sch_1" {
		t.Errorf("payload %+v", p)
	}
	if n := len([]rune(p.Body)); n > maxPushBody {
		t.Errorf("body is %d runes, cap %d", n, maxPushBody)
	}
	// The gone laptop is dropped once the fan-out ends.
	deadline := time.Now().Add(5 * time.Second)
	for {
		list, _ := st.List(ctx, testUser.ID)
		if len(list) == 1 && list[0].Endpoint == phone && list[0].LastSuccessAt != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("subscriptions after fan-out: %+v", list)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestPushURLForANotification(t *testing.T) {
	sid, tid := "s 1", "t/1"
	for _, c := range []struct {
		n    schedule.Notification
		want string
	}{
		{schedule.Notification{ThreadID: &tid, ScheduleID: &sid}, "/thread/t%2F1"},
		// Like the inbox: a disabled schedule opens where it can be enabled.
		{schedule.Notification{Kind: schedule.KindScheduleDisabled, ThreadID: &tid, ScheduleID: &sid}, "/schedules/s%201"},
		{schedule.Notification{ScheduleID: &sid}, "/schedules/s%201"},
		{schedule.Notification{}, "/"},
	} {
		if got := pushURL(c.n); got != c.want {
			t.Errorf("pushURL = %q, want %q", got, c.want)
		}
	}
}
