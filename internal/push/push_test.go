package push

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	webpush "github.com/SherClockHolmes/webpush-go"

	"github.com/dpage/aerly/internal/store"
)

// fakeStore is an in-memory subStore that records the bookkeeping calls the
// Sender makes, so tests can assert prune/retry/success behaviour without a DB.
type fakeStore struct {
	mu        sync.Mutex
	subs      map[int64][]store.WebPushSubscription // by user id
	failures  map[int64]int                         // by subscription id
	succeeded map[int64]bool
	deleted   map[int64]bool
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		subs:      map[int64][]store.WebPushSubscription{},
		failures:  map[int64]int{},
		succeeded: map[int64]bool{},
		deleted:   map[int64]bool{},
	}
}

func (f *fakeStore) add(userID int64, sub store.WebPushSubscription) {
	f.subs[userID] = append(f.subs[userID], sub)
}

func (f *fakeStore) WebPushSubscriptionsFor(_ context.Context, userID int64) ([]store.WebPushSubscription, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.subs[userID], nil
}

func (f *fakeStore) DeleteWebPushSubscription(_ context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted[id] = true
	return nil
}

func (f *fakeStore) MarkWebPushSuccess(_ context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.succeeded[id] = true
	f.failures[id] = 0
	return nil
}

func (f *fakeStore) BumpWebPushFailure(_ context.Context, id int64) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failures[id]++
	return f.failures[id], nil
}

// stubResponse builds a minimal *http.Response with the given status.
func stubResponse(status int) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(""))}
}

// senderWith builds an enabled Sender wired to f and a send stub that records
// the payloads it was asked to deliver and returns the responses/errors the
// test supplies, keyed by subscription endpoint.
func senderWith(f *fakeStore, send sendFunc) *Sender {
	s := NewSender(f, "pub", "priv", "mailto:test@example.com")
	s.send = send
	return s
}

func TestSendDisabledIsNoOp(t *testing.T) {
	f := newFakeStore()
	f.add(1, store.WebPushSubscription{ID: 10, Endpoint: "e", UserID: 1})
	called := false
	s := NewSender(f, "", "", "sub") // no keys → disabled
	s.send = func(context.Context, []byte, *webpush.Subscription, *webpush.Options) (*http.Response, error) {
		called = true
		return stubResponse(201), nil
	}
	if s.Enabled() {
		t.Fatal("Enabled() = true with no keys")
	}
	s.Send(context.Background(), []int64{1}, Payload{Title: "x"})
	if called {
		t.Error("send called while disabled")
	}
}

func TestSendEmptyRecipientsIsNoOp(t *testing.T) {
	f := newFakeStore()
	called := false
	s := senderWith(f, func(context.Context, []byte, *webpush.Subscription, *webpush.Options) (*http.Response, error) {
		called = true
		return stubResponse(201), nil
	})
	s.Send(context.Background(), nil, Payload{Title: "x"})
	if called {
		t.Error("send called for empty recipient set")
	}
}

func TestSendSuccessMarksSuccessAndCarriesPayload(t *testing.T) {
	f := newFakeStore()
	f.add(1, store.WebPushSubscription{ID: 10, Endpoint: "https://push/10", P256dh: "k", Auth: "a", UserID: 1})

	var gotMsg []byte
	var gotOpts *webpush.Options
	s := senderWith(f, func(_ context.Context, msg []byte, _ *webpush.Subscription, opts *webpush.Options) (*http.Response, error) {
		gotMsg = msg
		gotOpts = opts
		return stubResponse(201), nil
	})
	s.Send(context.Background(), []int64{1}, Payload{Title: "BA882 delayed", Body: "now 14:00", URL: "/trips/7", Tag: "BA882", Kind: "alert"})

	if !f.succeeded[10] {
		t.Error("subscription 10 not marked successful")
	}
	if !strings.Contains(string(gotMsg), "BA882 delayed") || !strings.Contains(string(gotMsg), "/trips/7") {
		t.Errorf("payload not serialised into message: %s", gotMsg)
	}
	if gotOpts.VAPIDPublicKey != "pub" || gotOpts.Subscriber != "mailto:test@example.com" {
		t.Errorf("VAPID options not passed through: %+v", gotOpts)
	}
}

func TestSendGonePrunesImmediately(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusGone} {
		f := newFakeStore()
		f.add(1, store.WebPushSubscription{ID: 10, Endpoint: "https://push/10", UserID: 1})
		s := senderWith(f, func(context.Context, []byte, *webpush.Subscription, *webpush.Options) (*http.Response, error) {
			return stubResponse(status), nil
		})
		s.Send(context.Background(), []int64{1}, Payload{Title: "x"})
		if !f.deleted[10] {
			t.Errorf("status %d: subscription not pruned", status)
		}
		if f.failures[10] != 0 {
			t.Errorf("status %d: gone should not bump failure count", status)
		}
	}
}

func TestSendTransientFailureBumpsButKeeps(t *testing.T) {
	f := newFakeStore()
	f.add(1, store.WebPushSubscription{ID: 10, Endpoint: "https://push/10", UserID: 1})
	s := senderWith(f, func(context.Context, []byte, *webpush.Subscription, *webpush.Options) (*http.Response, error) {
		return stubResponse(http.StatusInternalServerError), nil
	})
	s.Send(context.Background(), []int64{1}, Payload{Title: "x"})
	if f.deleted[10] {
		t.Error("transient 500 should not prune")
	}
	if f.failures[10] != 1 {
		t.Errorf("failure count = %d, want 1", f.failures[10])
	}
}

func TestSendNetworkErrorIsTransient(t *testing.T) {
	f := newFakeStore()
	f.add(1, store.WebPushSubscription{ID: 10, Endpoint: "https://push/10", UserID: 1})
	s := senderWith(f, func(context.Context, []byte, *webpush.Subscription, *webpush.Options) (*http.Response, error) {
		return nil, errors.New("connection refused")
	})
	s.Send(context.Background(), []int64{1}, Payload{Title: "x"})
	if f.deleted[10] {
		t.Error("network error should not prune on first failure")
	}
	if f.failures[10] != 1 {
		t.Errorf("failure count = %d, want 1", f.failures[10])
	}
}

func TestTransientFailurePrunesAtThreshold(t *testing.T) {
	f := newFakeStore()
	// Pre-load the subscription one failure short of the threshold.
	f.failures[10] = maxFailures - 1
	f.add(1, store.WebPushSubscription{ID: 10, Endpoint: "https://push/10", UserID: 1})
	s := senderWith(f, func(context.Context, []byte, *webpush.Subscription, *webpush.Options) (*http.Response, error) {
		return stubResponse(http.StatusInternalServerError), nil
	})
	s.Send(context.Background(), []int64{1}, Payload{Title: "x"})
	if !f.deleted[10] {
		t.Error("subscription should be pruned once it reaches maxFailures")
	}
}

func TestSendFansOutAndDedupesUsers(t *testing.T) {
	f := newFakeStore()
	f.add(1, store.WebPushSubscription{ID: 10, Endpoint: "https://push/10", UserID: 1})
	f.add(1, store.WebPushSubscription{ID: 11, Endpoint: "https://push/11", UserID: 1}) // two devices
	f.add(2, store.WebPushSubscription{ID: 20, Endpoint: "https://push/20", UserID: 2})

	var mu sync.Mutex
	hits := map[string]int{}
	s := senderWith(f, func(_ context.Context, _ []byte, sub *webpush.Subscription, _ *webpush.Options) (*http.Response, error) {
		mu.Lock()
		hits[sub.Endpoint]++
		mu.Unlock()
		return stubResponse(201), nil
	})
	// User 1 listed twice — must not double-send.
	s.Send(context.Background(), []int64{1, 2, 1}, Payload{Title: "x"})

	if len(hits) != 3 {
		t.Fatalf("expected 3 distinct endpoints hit, got %v", hits)
	}
	for ep, n := range hits {
		if n != 1 {
			t.Errorf("endpoint %s hit %d times, want 1", ep, n)
		}
	}
}
