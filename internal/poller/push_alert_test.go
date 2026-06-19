package poller

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/push"
	"github.com/dpage/aerly/internal/sse"
	"github.com/dpage/aerly/internal/store"
)

// fakePusher records the push sends the poller makes so the alert tests can
// assert the push channel without real VAPID/crypto/network.
type fakePusher struct {
	mu       sync.Mutex
	enabled  bool
	payloads []push.Payload
	users    [][]int64
}

func (f *fakePusher) Enabled() bool { return f.enabled }

func (f *fakePusher) Send(_ context.Context, userIDs []int64, p push.Payload) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.payloads = append(f.payloads, p)
	f.users = append(f.users, userIDs)
}

func (f *fakePusher) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.payloads)
}

// pushAlertSetup wires an alert poller with a fake pusher and seeds a delayed
// flight that's over the threshold, returning everything a test needs.
func pushAlertSetup(t *testing.T) (*Poller, *store.Store, *fakePusher, int64, *store.Flight) {
	t.Helper()
	p, s, _, _ := alertPoller(t)
	fp := &fakePusher{enabled: true}
	p.Push = fp
	ctx := context.Background()
	owner := seedUser(t, s)
	now := time.Now()
	f, err := mkPart(ctx, s, partSeed{
		Ident: "BA882", ScheduledOut: now.Add(time.Hour), ScheduledIn: now.Add(3 * time.Hour),
		OriginIATA: "LHR", DestIATA: "JFK",
	}, owner)
	if err != nil {
		t.Fatalf("mkPart: %v", err)
	}
	return p, s, fp, owner, f
}

func TestPushAlert_DeliveredWhenKindEnabled(t *testing.T) {
	p, s, fp, _, f := pushAlertSetup(t)
	ctx := context.Background()

	prev := f
	setEstimatedOut(t, s, f.ID, f.ScheduledOut.Add(45*time.Minute))
	p.maybeAlert(ctx, prev, f.ID)

	if fp.count() != 1 {
		t.Fatalf("expected 1 push, got %d", fp.count())
	}
	got := fp.payloads[0]
	if got.Kind != "alert" {
		t.Errorf("payload kind = %q, want alert", got.Kind)
	}
	if got.Tag != "alert-BA882" {
		t.Errorf("payload tag = %q, want alert-BA882", got.Tag)
	}
	if got.Body == "" || got.URL == "" {
		t.Errorf("payload missing body/url: %+v", got)
	}
}

func TestPushAlert_SkippedWhenKindDisabled(t *testing.T) {
	p, s, fp, owner, f := pushAlertSetup(t)
	ctx := context.Background()
	if err := s.SetPushKindPref(ctx, owner, "alert", false); err != nil {
		t.Fatalf("disable alert push: %v", err)
	}

	prev := f
	setEstimatedOut(t, s, f.ID, f.ScheduledOut.Add(45*time.Minute))
	p.maybeAlert(ctx, prev, f.ID)

	if fp.count() != 0 {
		t.Fatalf("expected no push when kind disabled, got %d", fp.count())
	}
}

func TestPushAlert_SkippedBelowThreshold(t *testing.T) {
	p, s, fp, _, f := pushAlertSetup(t)
	ctx := context.Background()

	prev := f
	// 10-minute delay: below the 15-minute default threshold, so no alert fires
	// at all — and therefore no push.
	setEstimatedOut(t, s, f.ID, f.ScheduledOut.Add(10*time.Minute))
	p.maybeAlert(ctx, prev, f.ID)

	if fp.count() != 0 {
		t.Fatalf("expected no push below threshold, got %d", fp.count())
	}
}

func TestPushAlert_NoSenderIsNoOp(t *testing.T) {
	// A poller with no Push set must not panic and must still deliver in-app.
	p, s, hub, _ := alertPoller(t)
	ctx := context.Background()
	owner := seedUser(t, s)
	now := time.Now()
	f, err := mkPart(ctx, s, partSeed{
		Ident: "BA400", ScheduledOut: now.Add(time.Hour), ScheduledIn: now.Add(3 * time.Hour),
		OriginIATA: "LHR", DestIATA: "JFK",
	}, owner)
	if err != nil {
		t.Fatalf("mkPart: %v", err)
	}
	ch, unsub := hub.Subscribe(sse.Subscription{ViewerID: owner})
	defer unsub()

	prev := f
	setEstimatedOut(t, s, f.ID, f.ScheduledOut.Add(45*time.Minute))
	p.maybeAlert(ctx, prev, f.ID) // p.Push is nil

	if got := drainAlerts(t, ch); len(got) != 1 {
		t.Fatalf("expected in-app alert to still fire with no pusher, got %d", len(got))
	}
}

func TestPushAlert_DisabledSenderIsNoOp(t *testing.T) {
	p, s, _, _ := alertPoller(t)
	fp := &fakePusher{enabled: false} // sender present but not configured
	p.Push = fp
	ctx := context.Background()
	owner := seedUser(t, s)
	now := time.Now()
	f, err := mkPart(ctx, s, partSeed{
		Ident: "BA500", ScheduledOut: now.Add(time.Hour), ScheduledIn: now.Add(3 * time.Hour),
		OriginIATA: "LHR", DestIATA: "JFK",
	}, owner)
	if err != nil {
		t.Fatalf("mkPart: %v", err)
	}

	prev := f
	setEstimatedOut(t, s, f.ID, f.ScheduledOut.Add(45*time.Minute))
	p.maybeAlert(ctx, prev, f.ID)

	if fp.count() != 0 {
		t.Fatalf("disabled sender should not be asked to send, got %d", fp.count())
	}
}
