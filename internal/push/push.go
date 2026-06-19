// Package push delivers Web Push notifications to users' devices, complementing
// the in-app/SSE and email channels by reaching a user when Aerly is closed.
//
// A Sender wraps the VAPID key pair and a subscription store. Each notification
// origin (the poller for flight alerts, the share handler for shares) calls
// Send with the recipient user IDs and a Payload; the Sender loads those users'
// device subscriptions, signs and POSTs the encrypted push to each browser's
// push service, and prunes subscriptions the service reports as dead.
//
// Sending is best-effort: it never blocks or fails the originating action.
// When no VAPID keys are configured the whole package is dormant (Enabled
// reports false and Send is a no-op), so a deployment without push keys behaves
// exactly as before.
package push

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"

	webpush "github.com/SherClockHolmes/webpush-go"

	"github.com/dpage/aerly/internal/store"
)

// maxFailures is how many consecutive transient failures a subscription may
// accrue before it is pruned. A 404/410 prunes immediately (the subscription is
// permanently gone); transient 429/5xx errors only prune once they persist, so
// a brief push-service outage doesn't lose every subscription.
const maxFailures = 10

// pushTTL is the seconds a push service should retain an undeliverable message
// before dropping it. A day is plenty for a flight alert: useful if the phone
// is briefly offline, stale beyond that.
const pushTTL = 24 * 60 * 60

// maxConcurrent bounds in-flight push requests per Send call so a large
// recipient set can't open an unbounded number of sockets.
const maxConcurrent = 8

// Payload is the JSON body delivered to the service worker's push handler. The
// service worker renders Title/Body as the OS notification and deep-links to
// URL on click; Tag collapses repeated notifications for the same subject (e.g.
// successive updates to one flight); Kind is informational for the client.
type Payload struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	URL   string `json:"url,omitempty"`
	Tag   string `json:"tag,omitempty"`
	Kind  string `json:"kind,omitempty"`
}

// subStore is the slice of the store the Sender needs, narrowed to an interface
// so tests can substitute a fake without a database.
type subStore interface {
	WebPushSubscriptionsFor(ctx context.Context, userID int64) ([]store.WebPushSubscription, error)
	DeleteWebPushSubscription(ctx context.Context, id int64) error
	MarkWebPushSuccess(ctx context.Context, id int64) error
	BumpWebPushFailure(ctx context.Context, id int64) (int, error)
}

// sendFunc matches webpush.SendNotificationWithContext. It's a field on Sender
// so tests can stand in for the real (crypto + network) send and assert the
// Sender's own logic — success/prune/retry bookkeeping — in isolation.
type sendFunc func(ctx context.Context, message []byte, s *webpush.Subscription, opts *webpush.Options) (*http.Response, error)

// Sender delivers push notifications using a VAPID key pair.
type Sender struct {
	store      subStore
	publicKey  string
	privateKey string
	subject    string
	send       sendFunc
}

// NewSender builds a Sender. When publicKey/privateKey are empty the Sender is
// disabled and Send is a no-op (see Enabled).
func NewSender(st subStore, publicKey, privateKey, subject string) *Sender {
	return &Sender{
		store:      st,
		publicKey:  publicKey,
		privateKey: privateKey,
		subject:    subject,
		send:       webpush.SendNotificationWithContext,
	}
}

// Enabled reports whether push can actually be sent: a Sender exists and both
// halves of the VAPID key pair are present.
func (s *Sender) Enabled() bool {
	return s != nil && s.publicKey != "" && s.privateKey != ""
}

// Send pushes payload p to every device of every listed user. It is
// best-effort: errors are logged, never returned, so a failed push never blocks
// the originating flight alert or share. A disabled Sender or empty recipient
// set is a silent no-op.
func (s *Sender) Send(ctx context.Context, userIDs []int64, p Payload) {
	if !s.Enabled() || len(userIDs) == 0 {
		return
	}
	msg, err := json.Marshal(p)
	if err != nil {
		slog.Error("push: marshal payload", "err", err)
		return
	}

	// Collect every target subscription across the recipient users first, then
	// fan the sends out under a bounded worker pool.
	var subs []store.WebPushSubscription
	seen := make(map[int64]bool, len(userIDs))
	for _, uid := range userIDs {
		if seen[uid] {
			continue
		}
		seen[uid] = true
		us, err := s.store.WebPushSubscriptionsFor(ctx, uid)
		if err != nil {
			slog.Error("push: load subscriptions", "user", uid, "err", err)
			continue
		}
		subs = append(subs, us...)
	}
	if len(subs) == 0 {
		return
	}

	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup
	for _, sub := range subs {
		wg.Add(1)
		sem <- struct{}{}
		go func(sub store.WebPushSubscription) {
			defer wg.Done()
			defer func() { <-sem }()
			s.sendOne(ctx, msg, sub)
		}(sub)
	}
	wg.Wait()
}

// sendOne delivers one message to one subscription and reconciles the result
// with the store: success clears failure state; a permanently-gone subscription
// (404/410) is pruned immediately; any other error is a transient failure that
// prunes only after maxFailures consecutive occurrences.
func (s *Sender) sendOne(ctx context.Context, msg []byte, sub store.WebPushSubscription) {
	resp, err := s.send(ctx, msg, &webpush.Subscription{
		Endpoint: sub.Endpoint,
		Keys:     webpush.Keys{P256dh: sub.P256dh, Auth: sub.Auth},
	}, &webpush.Options{
		Subscriber:      s.subject,
		VAPIDPublicKey:  s.publicKey,
		VAPIDPrivateKey: s.privateKey,
		TTL:             pushTTL,
		Urgency:         webpush.UrgencyHigh,
	})
	if err != nil {
		// Network/encoding error before we got a response: treat as transient.
		s.fail(ctx, sub.ID)
		return
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		if err := s.store.MarkWebPushSuccess(ctx, sub.ID); err != nil {
			slog.Error("push: mark success", "id", sub.ID, "err", err)
		}
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		// The push service says this subscription is permanently gone.
		if err := s.store.DeleteWebPushSubscription(ctx, sub.ID); err != nil {
			slog.Error("push: prune dead subscription", "id", sub.ID, "err", err)
		}
	default:
		slog.Warn("push: transient send failure", "id", sub.ID, "status", resp.StatusCode)
		s.fail(ctx, sub.ID)
	}
}

// fail records a transient failure and prunes the subscription once it has
// failed maxFailures times in a row.
func (s *Sender) fail(ctx context.Context, id int64) {
	n, err := s.store.BumpWebPushFailure(ctx, id)
	if err != nil {
		slog.Error("push: bump failure", "id", id, "err", err)
		return
	}
	if n >= maxFailures {
		if err := s.store.DeleteWebPushSubscription(ctx, id); err != nil {
			slog.Error("push: prune flaky subscription", "id", id, "err", err)
		}
	}
}
