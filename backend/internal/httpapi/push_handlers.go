package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/trick77/loom/internal/schedule"
	"github.com/trick77/loom/internal/webpush"
	"github.com/trick77/ship/kit/httpx"
)

// PushSender delivers Web Push messages; *webpush.Sender is one.
type PushSender interface {
	PublicKey() string
	Send(ctx context.Context, sub webpush.Subscription, msg webpush.Message) error
}

// maxPushBodyBytes bounds a subscription body: an endpoint URL and two keys.
const maxPushBodyBytes = 8 << 10

// maxPushTitle and maxPushBody cap what a notification shows (runes); the
// lock screen shows less, and the whole message must fit one 4 KiB record.
const (
	maxPushTitle = 120
	maxPushBody  = 300
)

// pushDeviceTimeout bounds one device's delivery. Devices are sent to side by
// side, each on its own clock: one hanging push service must not cost the
// others their notification.
const pushDeviceTimeout = 20 * time.Second

// pushPayload is what the service worker (ui/public/sw.js) shows.
type pushPayload struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	URL   string `json:"url"`
	Tag   string `json:"tag,omitempty"`
}

func (s *server) pushEnabled() bool { return s.push != nil && s.pushStore != nil }

func (s *server) pushUser(w http.ResponseWriter, r *http.Request) (string, bool) {
	user, ok := currentUser(w, r)
	if !ok {
		return "", false
	}
	if !s.pushEnabled() {
		httpx.WriteError(w, http.StatusNotFound, "push notifications are off")
		return "", false
	}
	return user.ID, true
}

func (s *server) handlePushConfig(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.pushUser(w, r); !ok {
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"publicKey": s.push.PublicKey()})
}

// subscriptionBody is PushSubscription.toJSON(). It is decoded leniently: the
// spec types keys as a record, and a browser may add fields.
type subscriptionBody struct {
	Endpoint string            `json:"endpoint"`
	Keys     map[string]string `json:"keys"`
}

func (s *server) handlePushSubscribe(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.pushUser(w, r)
	if !ok {
		return
	}
	var body subscriptionBody
	if !httpx.DecodeJSON(w, r, &body, maxPushBodyBytes) {
		return
	}
	sub := webpush.Subscription{Endpoint: body.Endpoint, P256dh: body.Keys["p256dh"], Auth: body.Keys["auth"]}
	if err := sub.Validate(); err != nil {
		httpx.WriteError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	err := s.pushStore.Save(r.Context(), userID, sub, truncateRunes(r.UserAgent(), 200), s.push.PublicKey())
	switch {
	case errors.Is(err, webpush.ErrTaken):
		httpx.WriteError(w, http.StatusConflict, "this endpoint belongs to another account")
	case err != nil:
		serverError(w, r, err, "push store failed")
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *server) handlePushUnsubscribe(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.pushUser(w, r)
	if !ok {
		return
	}
	var body struct {
		Endpoint string `json:"endpoint"`
	}
	if !httpx.DecodeJSON(w, r, &body, maxPushBodyBytes) {
		return
	}
	if _, err := s.pushStore.Delete(r.Context(), userID, body.Endpoint); err != nil {
		serverError(w, r, err, "push store failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handlePushTest pushes the caller's own text to the caller's own device: the
// browser words it in the UI language and proves the whole path end to end.
func (s *server) handlePushTest(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.pushUser(w, r)
	if !ok {
		return
	}
	var body struct {
		Endpoint string `json:"endpoint"`
		Title    string `json:"title"`
		Body     string `json:"body"`
	}
	if !httpx.DecodeJSON(w, r, &body, maxPushBodyBytes) {
		return
	}
	subs, err := s.pushStore.List(r.Context(), userID)
	if err != nil {
		serverError(w, r, err, "push store failed")
		return
	}
	for _, sub := range subs {
		if sub.Endpoint != body.Endpoint {
			continue
		}
		err := s.pushTo(r.Context(), userID, sub, pushPayload{Title: body.Title, Body: body.Body, URL: "/"})
		switch {
		case errors.Is(err, webpush.ErrGone):
			httpx.WriteError(w, http.StatusGone, "the push service no longer knows this device")
		case err != nil:
			slog.Warn("test push failed", "err", err)
			httpx.WriteError(w, http.StatusBadGateway, "the push service refused the message")
		default:
			w.WriteHeader(http.StatusNoContent)
		}
		return
	}
	httpx.WriteError(w, http.StatusNotFound, "this device is not subscribed")
}

// pushTo sends one payload to one device: a gone subscription is dropped, an
// accepted one marked. A row made with an older VAPID key (a rotated session
// secret) cannot be delivered and counts as gone; the browser subscribes anew
// on its next visit.
func (s *server) pushTo(ctx context.Context, userID string, sub webpush.Stored, p pushPayload) error {
	err := webpush.ErrGone
	if sub.VAPIDKey == s.push.PublicKey() {
		p.Title, p.Body = truncateRunes(p.Title, maxPushTitle), truncateRunes(p.Body, maxPushBody)
		payload, merr := json.Marshal(p)
		if merr != nil {
			return merr
		}
		err = s.push.Send(ctx, sub.Subscription, webpush.Message{Payload: payload})
	}
	store := context.WithoutCancel(ctx)
	switch {
	case errors.Is(err, webpush.ErrGone):
		slog.Info("dropping gone push subscription", "stale_key", sub.VAPIDKey != s.push.PublicKey())
		if _, derr := s.pushStore.Delete(store, userID, sub.Endpoint); derr != nil {
			slog.Warn("drop gone push subscription failed", "err", derr)
		}
	case err == nil:
		if merr := s.pushStore.MarkSent(store, userID, sub.Endpoint, s.clock()); merr != nil {
			slog.Warn("mark push sent failed", "err", merr)
		}
	}
	return err
}

// pushNotification sends an inbox item to all of the user's devices, off the
// caller's path: a slow push service must not hold up a scheduled run.
func (s *server) pushNotification(ctx context.Context, userID string, n schedule.Notification) {
	if !s.pushEnabled() {
		return
	}
	p := pushPayload{Title: n.Title, Body: n.Body, URL: pushURL(n)}
	if n.ScheduleID != nil {
		// One schedule's newer run replaces its older one on the lock screen.
		p.Tag = "schedule-" + *n.ScheduleID
	}
	s.background.Spawn(context.WithoutCancel(ctx), "push:"+userID, func(ctx context.Context) {
		subs, err := s.pushStore.List(ctx, userID)
		if err != nil {
			slog.Warn("list push subscriptions failed", "err", err)
			return
		}
		var wg sync.WaitGroup
		for _, sub := range subs {
			wg.Go(func() {
				ctx, cancel := context.WithTimeout(ctx, pushDeviceTimeout)
				defer cancel()
				if err := s.pushTo(ctx, userID, sub, p); err != nil && !errors.Is(err, webpush.ErrGone) {
					slog.Warn("push notification failed", "err", err)
				}
			})
		}
		wg.Wait()
	})
}

// pushURL is the page a tap on the notification opens, the same one the inbox
// opens: a disabled schedule's page (to enable it again), else the run's
// thread, else the schedule.
func pushURL(n schedule.Notification) string {
	switch {
	case n.Kind == schedule.KindScheduleDisabled && n.ScheduleID != nil:
		return "/schedules/" + url.PathEscape(*n.ScheduleID)
	case n.ThreadID != nil:
		return "/thread/" + url.PathEscape(*n.ThreadID)
	case n.ScheduleID != nil:
		return "/schedules/" + url.PathEscape(*n.ScheduleID)
	default:
		return "/"
	}
}

// truncateRunes cuts s to at most n runes, marking a cut with an ellipsis.
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
