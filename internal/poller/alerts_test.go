package poller

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/api"
	"github.com/dpage/aerly/internal/providers"
	"github.com/dpage/aerly/internal/sse"
	"github.com/dpage/aerly/internal/store"
)

// captureMailer records the messages a poller's email channel would have sent.
type captureMailer struct {
	mu   sync.Mutex
	sent []string
}

func (c *captureMailer) send(_ context.Context, _, _, msg string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sent = append(c.sent, msg)
	return nil
}

func (c *captureMailer) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.sent)
}

// setEstimatedOut writes an estimated departure on the part's flight_details,
// simulating what a resolver/tracker would persist when a delay is published.
func setEstimatedOut(t *testing.T, s *store.Store, partID int64, est time.Time) {
	t.Helper()
	if _, err := s.Pool().Exec(context.Background(),
		`UPDATE flight_details SET estimated_out = $2 WHERE plan_part_id = $1`,
		partID, est); err != nil {
		t.Fatalf("set estimated_out: %v", err)
	}
}

func setStatus(t *testing.T, s *store.Store, partID int64, status string) {
	t.Helper()
	if _, err := s.Pool().Exec(context.Background(),
		`UPDATE flight_details SET flight_status = $2 WHERE plan_part_id = $1`,
		partID, status); err != nil {
		t.Fatalf("set status: %v", err)
	}
}

// setOriginGate writes the departure gate on the part's flight_details,
// simulating what RefreshFlightPartGate persists when the provider reports it.
func setOriginGate(t *testing.T, s *store.Store, partID int64, gate string) {
	t.Helper()
	if _, err := s.Pool().Exec(context.Background(),
		`UPDATE flight_details SET origin_gate = $2 WHERE plan_part_id = $1`,
		partID, gate); err != nil {
		t.Fatalf("set origin_gate: %v", err)
	}
}

// setDestBelt writes the arrival baggage belt on the part's flight_details,
// simulating what RefreshFlightPartBelt persists when the provider reports it.
func setDestBelt(t *testing.T, s *store.Store, partID int64, belt string) {
	t.Helper()
	if _, err := s.Pool().Exec(context.Background(),
		`UPDATE flight_details SET dest_baggage_belt = $2 WHERE plan_part_id = $1`,
		partID, belt); err != nil {
		t.Fatalf("set dest_baggage_belt: %v", err)
	}
}

// alertPoller builds a poller wired with a capture mailer + email enabled.
func alertPoller(t *testing.T) (*Poller, *store.Store, *sse.Hub, *captureMailer) {
	t.Helper()
	p, s, hub := newPoller(t, &mockTracker{}, time.Minute)
	cap := &captureMailer{}
	p.MailFromAddress = "alerts@aerly.test"
	p.SendmailPath = "/bin/true"
	p.PublicURL = "http://localhost:8080"
	p.SendAlertEmail = cap.send
	return p, s, hub, cap
}

// drainAlerts collects alert.created payloads delivered to a subscription until
// a short quiet period. Returns the decoded alerts.
func drainAlerts(t *testing.T, ch <-chan sse.Event) []api.FlightAlertDTO {
	t.Helper()
	var out []api.FlightAlertDTO
	for {
		select {
		case ev := <-ch:
			if ev.Type != "alert.created" {
				continue
			}
			var dto api.NotificationsDTO
			if err := json.Unmarshal(ev.Data, &dto); err != nil {
				t.Fatalf("unmarshal alert: %v", err)
			}
			if dto.Alert != nil {
				out = append(out, *dto.Alert)
			}
		case <-time.After(150 * time.Millisecond):
			return out
		}
	}
}

func TestAlert_DelayBelowThresholdDoesNotAlert(t *testing.T) {
	p, s, hub, cap := alertPoller(t)
	ctx := context.Background()
	owner := seedUser(t, s)
	if err := s.UpsertVerifiedEmail(ctx, owner, "owner@aerly.test"); err != nil {
		t.Fatalf("verify email: %v", err)
	}
	now := time.Now()
	f, err := mkPart(ctx, s, partSeed{
		Ident: "BA100", ScheduledOut: now.Add(time.Hour), ScheduledIn: now.Add(3 * time.Hour),
		OriginIATA: "LHR", DestIATA: "JFK",
	}, owner)
	if err != nil {
		t.Fatalf("mkPart: %v", err)
	}

	ch, unsub := hub.Subscribe(sse.Subscription{ViewerID: owner})
	defer unsub()

	prev := f // no delay yet
	// 10-minute delay: below the 15-minute default threshold.
	setEstimatedOut(t, s, f.ID, f.ScheduledOut.Add(10*time.Minute))
	p.maybeAlert(ctx, prev, f.ID)

	if got := drainAlerts(t, ch); len(got) != 0 {
		t.Fatalf("expected no in-app alert below threshold, got %d", len(got))
	}
	if cap.count() != 0 {
		t.Fatalf("expected no email below threshold, got %d", cap.count())
	}
}

func TestAlert_DelayAboveThresholdAlerts(t *testing.T) {
	p, s, hub, cap := alertPoller(t)
	ctx := context.Background()
	owner := seedUser(t, s)
	if err := s.UpsertVerifiedEmail(ctx, owner, "owner@aerly.test"); err != nil {
		t.Fatalf("verify email: %v", err)
	}
	now := time.Now()
	f, err := mkPart(ctx, s, partSeed{
		Ident: "BA200", ScheduledOut: now.Add(time.Hour), ScheduledIn: now.Add(3 * time.Hour),
		OriginIATA: "LHR", DestIATA: "JFK",
	}, owner)
	if err != nil {
		t.Fatalf("mkPart: %v", err)
	}
	ch, unsub := hub.Subscribe(sse.Subscription{ViewerID: owner})
	defer unsub()

	prev := f
	setEstimatedOut(t, s, f.ID, f.ScheduledOut.Add(45*time.Minute))
	p.maybeAlert(ctx, prev, f.ID)

	got := drainAlerts(t, ch)
	if len(got) != 1 {
		t.Fatalf("expected 1 in-app alert, got %d", len(got))
	}
	if got[0].Kind != "delayed" || got[0].Ident != "BA200" {
		t.Fatalf("unexpected alert: %+v", got[0])
	}
	if cap.count() != 1 {
		t.Fatalf("expected 1 email, got %d", cap.count())
	}
}

func TestAlert_CancellationAlwaysAlertsRegardlessOfThreshold(t *testing.T) {
	p, s, hub, cap := alertPoller(t)
	ctx := context.Background()
	owner := seedUser(t, s)
	if err := s.UpsertVerifiedEmail(ctx, owner, "owner@aerly.test"); err != nil {
		t.Fatalf("verify email: %v", err)
	}
	// Crank the owner's threshold way up: cancellation must still alert.
	if err := s.SetAlertPrefs(ctx, store.AlertPrefs{UserID: owner, InApp: true, Email: true, MinDelayMin: 600}); err != nil {
		t.Fatalf("set prefs: %v", err)
	}
	now := time.Now()
	f, err := mkPart(ctx, s, partSeed{
		Ident: "BA300", ScheduledOut: now.Add(time.Hour), ScheduledIn: now.Add(3 * time.Hour),
		OriginIATA: "LHR", DestIATA: "JFK",
	}, owner)
	if err != nil {
		t.Fatalf("mkPart: %v", err)
	}
	ch, unsub := hub.Subscribe(sse.Subscription{ViewerID: owner})
	defer unsub()

	prev := f
	setStatus(t, s, f.ID, "Cancelled")
	p.maybeAlert(ctx, prev, f.ID)

	got := drainAlerts(t, ch)
	if len(got) != 1 || got[0].Kind != "cancelled" {
		t.Fatalf("expected 1 cancelled alert, got %+v", got)
	}
	if cap.count() != 1 {
		t.Fatalf("expected 1 cancellation email, got %d", cap.count())
	}
}

func TestAlert_ViewerWithoutOptInGetsNothing(t *testing.T) {
	p, s, hub, cap := alertPoller(t)
	ctx := context.Background()
	owner := seedUser(t, s)
	viewer := seedUser(t, s)
	if err := s.UpsertVerifiedEmail(ctx, viewer, "viewer@aerly.test"); err != nil {
		t.Fatalf("verify email: %v", err)
	}
	now := time.Now()
	f, err := mkPart(ctx, s, partSeed{
		Ident: "BA400", ScheduledOut: now.Add(time.Hour), ScheduledIn: now.Add(3 * time.Hour),
		OriginIATA: "LHR", DestIATA: "JFK",
	}, owner)
	if err != nil {
		t.Fatalf("mkPart: %v", err)
	}
	// viewer is a trip member but NOT a passenger and did NOT opt in.
	tp, err := s.TrackerPartRow(ctx, f.ID)
	if err != nil {
		t.Fatalf("tracker row: %v", err)
	}
	if _, err := s.Pool().Exec(ctx,
		`INSERT INTO trip_members (trip_id, user_id, role) VALUES ($1, $2, 'viewer')`,
		tp.TripID, viewer); err != nil {
		t.Fatalf("add viewer: %v", err)
	}

	ch, unsub := hub.Subscribe(sse.Subscription{ViewerID: viewer})
	defer unsub()

	prev := f
	setStatus(t, s, f.ID, "Cancelled")
	p.maybeAlert(ctx, prev, f.ID)

	if got := drainAlerts(t, ch); len(got) != 0 {
		t.Fatalf("non-opted-in viewer got %d in-app alerts", len(got))
	}
	// The only verified email is the viewer's; owner has none → no email at all.
	if cap.count() != 0 {
		t.Fatalf("non-opted-in viewer got email, count=%d", cap.count())
	}

	// After opt-in, the viewer DOES get alerted on the next (distinct) change.
	if err := s.AddPlanAlertOptin(ctx, tp.PlanID, viewer); err != nil {
		t.Fatalf("opt in: %v", err)
	}
	prev2, err := s.FlightPartByID(ctx, f.ID)
	if err != nil {
		t.Fatalf("refetch: %v", err)
	}
	setStatus(t, s, f.ID, "Diverted")
	p.maybeAlert(ctx, prev2, f.ID)

	got := drainAlerts(t, ch)
	if len(got) != 1 || got[0].Kind != "diverted" {
		t.Fatalf("opted-in viewer expected 1 diverted alert, got %+v", got)
	}
	if cap.count() != 1 {
		t.Fatalf("opted-in viewer expected 1 email, got %d", cap.count())
	}
}

func TestAlert_DedupeSuppressesRepeatOfSameChange(t *testing.T) {
	p, s, hub, cap := alertPoller(t)
	ctx := context.Background()
	owner := seedUser(t, s)
	if err := s.UpsertVerifiedEmail(ctx, owner, "owner@aerly.test"); err != nil {
		t.Fatalf("verify email: %v", err)
	}
	now := time.Now()
	f, err := mkPart(ctx, s, partSeed{
		Ident: "BA500", ScheduledOut: now.Add(time.Hour), ScheduledIn: now.Add(3 * time.Hour),
		OriginIATA: "LHR", DestIATA: "JFK",
	}, owner)
	if err != nil {
		t.Fatalf("mkPart: %v", err)
	}
	ch, unsub := hub.Subscribe(sse.Subscription{ViewerID: owner})
	defer unsub()

	// First tick: a 45-minute delay alerts.
	prev := f
	setEstimatedOut(t, s, f.ID, f.ScheduledOut.Add(45*time.Minute))
	p.maybeAlert(ctx, prev, f.ID)
	if got := drainAlerts(t, ch); len(got) != 1 {
		t.Fatalf("first delay: expected 1 alert, got %d", len(got))
	}
	if cap.count() != 1 {
		t.Fatalf("first delay: expected 1 email, got %d", cap.count())
	}

	// Second tick: same 45-minute delay still present. The pre-state now also
	// carries the delay, and the dedupe signature is unchanged → no re-alert.
	prev2, err := s.FlightPartByID(ctx, f.ID)
	if err != nil {
		t.Fatalf("refetch: %v", err)
	}
	p.maybeAlert(ctx, prev2, f.ID)
	if got := drainAlerts(t, ch); len(got) != 0 {
		t.Fatalf("repeat delay: expected no alert, got %d", len(got))
	}
	if cap.count() != 1 {
		t.Fatalf("repeat delay: expected still 1 email, got %d", cap.count())
	}
}

// TestAlert_SameTerminalDoesNotReAlert mirrors the gate/belt dedupe coverage: a
// terminal assignment alerts once, and the same terminal on the next tick does
// not re-alert.
func TestAlert_SameTerminalDoesNotReAlert(t *testing.T) {
	p, s, hub, _ := alertPoller(t)
	ctx := context.Background()
	owner := seedUser(t, s)
	if err := s.UpsertVerifiedEmail(ctx, owner, "owner@aerly.test"); err != nil {
		t.Fatalf("verify email: %v", err)
	}
	now := time.Now()
	f, err := mkPart(ctx, s, partSeed{
		Ident: "BA600", ScheduledOut: now.Add(time.Hour), ScheduledIn: now.Add(3 * time.Hour),
		OriginIATA: "LHR", DestIATA: "JFK",
	}, owner)
	if err != nil {
		t.Fatalf("mkPart: %v", err)
	}
	ch, unsub := hub.Subscribe(sse.Subscription{ViewerID: owner})
	defer unsub()

	// First tick: a departure terminal is assigned → one terminal alert.
	prev := f
	if _, err := s.Pool().Exec(ctx,
		`UPDATE flight_details SET origin_terminal = '5' WHERE plan_part_id = $1`, f.ID); err != nil {
		t.Fatalf("set terminal: %v", err)
	}
	p.maybeAlert(ctx, prev, f.ID)
	if got := drainAlerts(t, ch); len(got) != 1 || got[0].Kind != "terminal" {
		t.Fatalf("first terminal: expected 1 terminal alert, got %+v", got)
	}

	// Second tick: same terminal still present → no re-alert.
	prev2, err := s.FlightPartByID(ctx, f.ID)
	if err != nil {
		t.Fatalf("refetch: %v", err)
	}
	p.maybeAlert(ctx, prev2, f.ID)
	if got := drainAlerts(t, ch); len(got) != 0 {
		t.Fatalf("repeat terminal: expected no alert, got %d", len(got))
	}
}

// gateAlertHarness sets up a poller whose resolver assigns a departure gate,
// plus an owner with a verified email and gate alerts enabled. Returns the
// owner so the caller seeds a flight at whatever pre-departure offset it needs.
func gateAlertHarness(t *testing.T) (*Poller, *store.Store, *sse.Hub, *captureMailer, int64) {
	t.Helper()
	p, s, hub, cap := alertPoller(t)
	p.Resolver = &fakeResolver{rf: &providers.ResolvedFlight{
		Ident: "BA286", OriginIATA: "LHR", DestIATA: "SFO", OriginGate: "B32",
	}}
	ctx := context.Background()
	owner := seedUser(t, s)
	if err := s.UpsertVerifiedEmail(ctx, owner, "owner@aerly.test"); err != nil {
		t.Fatalf("verify email: %v", err)
	}
	if err := s.SetAlertPrefs(ctx, store.AlertPrefs{UserID: owner, InApp: true, Email: true, MinDelayMin: 600}); err != nil {
		t.Fatalf("set prefs: %v", err)
	}
	return p, s, hub, cap, owner
}

// TestTickAlertsOnGateAssignedInActiveWindow: a flight already inside the 30-min
// tracking window whose resolver introduces a gate must alert (+ email). Guards
// the prev-snapshot timing in refresh(): prev must be captured BEFORE the
// resolve that sets the gate, or there's no delta to alert on.
func TestTickAlertsOnGateAssignedInActiveWindow(t *testing.T) {
	p, s, hub, cap, owner := gateAlertHarness(t)
	ctx := context.Background()
	now := time.Now()
	// Departs in 20 min → within the active window; no gate / airframe yet.
	if _, err := mkPart(ctx, s, partSeed{
		Ident: "BA286", ScheduledOut: now.Add(20 * time.Minute), ScheduledIn: now.Add(3 * time.Hour),
	}, owner); err != nil {
		t.Fatalf("mkPart: %v", err)
	}
	ch, unsub := hub.Subscribe(sse.Subscription{ViewerID: owner})
	defer unsub()

	p.tick(ctx)

	got := drainAlerts(t, ch)
	if len(got) != 1 || got[0].Kind != "gate" {
		t.Fatalf("expected 1 gate alert from the active-window resolve, got %+v", got)
	}
	if cap.count() != 1 {
		t.Fatalf("expected 1 gate-change email, got %d", cap.count())
	}
}

// TestTickAlertsOnGateAssignedAheadOfDeparture: a gate the resolver supplies
// during the pre-departure metadata pass (30min–12h out) must also alert (+
// email) — refreshMetadata previously set the gate silently, so Magnus's gate
// landed with no notification.
func TestTickAlertsOnGateAssignedAheadOfDeparture(t *testing.T) {
	p, s, hub, cap, owner := gateAlertHarness(t)
	ctx := context.Background()
	now := time.Now()
	// Departs in 2h → outside the active window, inside the 12h metadata band.
	if _, err := mkPart(ctx, s, partSeed{
		Ident: "BA286", ScheduledOut: now.Add(2 * time.Hour), ScheduledIn: now.Add(5 * time.Hour),
	}, owner); err != nil {
		t.Fatalf("mkPart: %v", err)
	}
	ch, unsub := hub.Subscribe(sse.Subscription{ViewerID: owner})
	defer unsub()

	p.tick(ctx)

	got := drainAlerts(t, ch)
	if len(got) != 1 || got[0].Kind != "gate" {
		t.Fatalf("expected 1 gate alert from the metadata pass, got %+v", got)
	}
	if cap.count() != 1 {
		t.Fatalf("expected 1 gate-change email, got %d", cap.count())
	}
}

func TestAlert_GateChangeAlwaysAlertsRegardlessOfThreshold(t *testing.T) {
	p, s, hub, cap := alertPoller(t)
	ctx := context.Background()
	owner := seedUser(t, s)
	if err := s.UpsertVerifiedEmail(ctx, owner, "owner@aerly.test"); err != nil {
		t.Fatalf("verify email: %v", err)
	}
	// Crank the delay threshold way up: a gate change must still alert.
	if err := s.SetAlertPrefs(ctx, store.AlertPrefs{UserID: owner, InApp: true, Email: true, MinDelayMin: 600}); err != nil {
		t.Fatalf("set prefs: %v", err)
	}
	now := time.Now()
	f, err := mkPart(ctx, s, partSeed{
		Ident: "BA286", ScheduledOut: now.Add(time.Hour), ScheduledIn: now.Add(3 * time.Hour),
		OriginIATA: "LHR", DestIATA: "SFO",
	}, owner)
	if err != nil {
		t.Fatalf("mkPart: %v", err)
	}
	ch, unsub := hub.Subscribe(sse.Subscription{ViewerID: owner})
	defer unsub()

	prev := f // no gate yet
	setOriginGate(t, s, f.ID, "B32")
	p.maybeAlert(ctx, prev, f.ID)

	got := drainAlerts(t, ch)
	if len(got) != 1 || got[0].Kind != "gate" {
		t.Fatalf("expected 1 gate alert, got %+v", got)
	}
	if !strings.Contains(got[0].Message, "B32") || !strings.Contains(got[0].Message, "Gate change") {
		t.Fatalf("gate message missing detail: %q", got[0].Message)
	}
	if cap.count() != 1 {
		t.Fatalf("expected 1 gate-change email, got %d", cap.count())
	}
}

// A newly-published arrival baggage belt fires an in-app + email alert, exactly
// like a gate change.
func TestAlert_BeltChangeNotifies(t *testing.T) {
	p, s, hub, cap := alertPoller(t)
	ctx := context.Background()
	owner := seedUser(t, s)
	if err := s.UpsertVerifiedEmail(ctx, owner, "owner@aerly.test"); err != nil {
		t.Fatalf("verify email: %v", err)
	}
	// In-app + email on; crank the delay threshold so only the belt can alert.
	if err := s.SetAlertPrefs(ctx, store.AlertPrefs{UserID: owner, InApp: true, Email: true, MinDelayMin: 600}); err != nil {
		t.Fatalf("set prefs: %v", err)
	}
	now := time.Now()
	f, err := mkPart(ctx, s, partSeed{
		Ident: "BA286", ScheduledOut: now.Add(time.Hour), ScheduledIn: now.Add(3 * time.Hour),
		OriginIATA: "LHR", DestIATA: "SFO",
	}, owner)
	if err != nil {
		t.Fatalf("mkPart: %v", err)
	}
	ch, unsub := hub.Subscribe(sse.Subscription{ViewerID: owner})
	defer unsub()

	prev := f // no belt yet
	setDestBelt(t, s, f.ID, "34")
	p.maybeAlert(ctx, prev, f.ID)

	got := drainAlerts(t, ch)
	if len(got) != 1 || got[0].Kind != "belt" {
		t.Fatalf("expected 1 belt alert, got %+v", got)
	}
	if !strings.Contains(got[0].Message, "34") || !strings.Contains(got[0].Message, "Baggage belt") {
		t.Fatalf("belt message missing detail: %q", got[0].Message)
	}
	if cap.count() != 1 {
		t.Fatalf("expected 1 belt-change email, got %d", cap.count())
	}

	// Same belt on the next tick must not re-alert (dedupe signature).
	prev2, err := s.FlightPartByID(ctx, f.ID)
	if err != nil {
		t.Fatalf("refetch: %v", err)
	}
	p.maybeAlert(ctx, prev2, f.ID)
	if extra := drainAlerts(t, ch); len(extra) != 0 {
		t.Fatalf("same belt re-alerted: %+v", extra)
	}
}

// When a gate and a belt are first published on the SAME tick, gate wins: the
// changeKind precedence (gate before belt) means the single alert leads with
// the gate. This locks that ordering against silent drift.
func TestAlert_GateBeforeBeltOnSameTick(t *testing.T) {
	p, s, hub, cap := alertPoller(t)
	ctx := context.Background()
	owner := seedUser(t, s)
	if err := s.UpsertVerifiedEmail(ctx, owner, "owner@aerly.test"); err != nil {
		t.Fatalf("verify email: %v", err)
	}
	// Crank the delay threshold so only gate/belt can alert.
	if err := s.SetAlertPrefs(ctx, store.AlertPrefs{UserID: owner, InApp: true, Email: true, MinDelayMin: 600}); err != nil {
		t.Fatalf("set prefs: %v", err)
	}
	now := time.Now()
	f, err := mkPart(ctx, s, partSeed{
		Ident: "BA286", ScheduledOut: now.Add(time.Hour), ScheduledIn: now.Add(3 * time.Hour),
		OriginIATA: "LHR", DestIATA: "SFO",
	}, owner)
	if err != nil {
		t.Fatalf("mkPart: %v", err)
	}
	ch, unsub := hub.Subscribe(sse.Subscription{ViewerID: owner})
	defer unsub()

	prev := f // neither gate nor belt yet
	setOriginGate(t, s, f.ID, "B32")
	setDestBelt(t, s, f.ID, "34")
	p.maybeAlert(ctx, prev, f.ID)

	got := drainAlerts(t, ch)
	if len(got) != 1 || got[0].Kind != "gate" {
		t.Fatalf("expected a single gate alert (gate wins over belt), got %+v", got)
	}
	if !strings.Contains(got[0].Message, "B32") {
		t.Fatalf("gate alert missing gate detail: %q", got[0].Message)
	}
	if cap.count() != 1 {
		t.Fatalf("expected 1 email, got %d", cap.count())
	}
}

func TestAlert_GateChangePersistsInboxRow(t *testing.T) {
	p, s, _, _ := alertPoller(t)
	ctx := context.Background()
	owner := seedUser(t, s)
	// In-app on; crank the delay threshold so only the gate change can alert.
	if err := s.SetAlertPrefs(ctx, store.AlertPrefs{UserID: owner, InApp: true, Email: false, MinDelayMin: 600}); err != nil {
		t.Fatalf("set prefs: %v", err)
	}
	now := time.Now()
	f, err := mkPart(ctx, s, partSeed{
		Ident: "BA286", ScheduledOut: now.Add(time.Hour), ScheduledIn: now.Add(3 * time.Hour),
		OriginIATA: "LHR", DestIATA: "JFK",
	}, owner)
	if err != nil {
		t.Fatalf("mkPart: %v", err)
	}

	prev := f // no gate yet
	setOriginGate(t, s, f.ID, "B32")
	p.maybeAlert(ctx, prev, f.ID)

	rows, err := s.ListFlightAlerts(ctx, owner, 50)
	if err != nil || len(rows) != 1 {
		t.Fatalf("ListFlightAlerts = %d, %v", len(rows), err)
	}
	if rows[0].Kind != "gate" || rows[0].PlanPartID != f.ID {
		t.Errorf("persisted alert wrong: %+v", rows[0])
	}
	if !strings.Contains(rows[0].Message, "B32") {
		t.Errorf("message missing gate: %q", rows[0].Message)
	}
}

// A terminal reassignment surfaced near departure raises a terminal alert.
func TestAlertOnTerminalChange(t *testing.T) {
	tr := &mockTracker{}
	p, s, _ := newPoller(t, tr, time.Minute)
	ctx := context.Background()
	uid := seedUser(t, s)
	if err := s.UpsertVerifiedEmail(ctx, uid, "owner@aerly.test"); err != nil {
		t.Fatalf("verify email: %v", err)
	}
	if err := s.SetAlertPrefs(ctx, store.AlertPrefs{UserID: uid, InApp: true, Email: true, MinDelayMin: 600}); err != nil {
		t.Fatalf("set prefs: %v", err)
	}
	cap := &captureMailer{}
	p.MailFromAddress = "alerts@aerly.test"
	p.SendmailPath = "/bin/true"
	p.PublicURL = "http://localhost:8080"
	p.SendAlertEmail = cap.send

	now := time.Now()
	// 6h out, already confirmed with terminal "5"; provider now reports "3".
	f, err := mkPart(ctx, s, partSeed{
		Ident: "BA286", ScheduledOut: now.Add(6 * time.Hour), ScheduledIn: now.Add(8 * time.Hour),
		OriginIATA: "LHR", DestIATA: "JFK",
	}, uid)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// Seed a known prior terminal and confirm the flight so the schedule path is
	// inert and the only delta is the terminal.
	if _, err := s.Pool().Exec(ctx,
		`UPDATE flight_details SET origin_terminal = '5', resolved = true WHERE plan_part_id = $1`, f.ID); err != nil {
		t.Fatalf("seed terminal: %v", err)
	}
	p.Resolver = &fakeResolver{rf: &providers.ResolvedFlight{
		Ident: "BA286", OriginIATA: "LHR", DestIATA: "JFK", OriginTerminal: "3",
	}}

	hub := p.Hub
	ch, unsub := hub.Subscribe(sse.Subscription{ViewerID: uid})
	defer unsub()

	p.tick(ctx)

	got := drainAlerts(t, ch)
	if len(got) != 1 || got[0].Kind != "terminal" {
		t.Fatalf("expected 1 terminal alert, got %+v", got)
	}
	if cap.count() != 1 {
		t.Fatalf("expected 1 terminal-change email, got %d", cap.count())
	}
}

func TestAlert_SameGateDoesNotReAlert(t *testing.T) {
	p, s, hub, cap := alertPoller(t)
	ctx := context.Background()
	owner := seedUser(t, s)
	if err := s.UpsertVerifiedEmail(ctx, owner, "owner@aerly.test"); err != nil {
		t.Fatalf("verify email: %v", err)
	}
	now := time.Now()
	f, err := mkPart(ctx, s, partSeed{
		Ident: "BA286", ScheduledOut: now.Add(time.Hour), ScheduledIn: now.Add(3 * time.Hour),
		OriginIATA: "LHR", DestIATA: "SFO",
	}, owner)
	if err != nil {
		t.Fatalf("mkPart: %v", err)
	}
	ch, unsub := hub.Subscribe(sse.Subscription{ViewerID: owner})
	defer unsub()

	// First tick: gate assigned → alert.
	prev := f
	setOriginGate(t, s, f.ID, "B32")
	p.maybeAlert(ctx, prev, f.ID)
	if got := drainAlerts(t, ch); len(got) != 1 {
		t.Fatalf("first gate: expected 1 alert, got %d", len(got))
	}
	if cap.count() != 1 {
		t.Fatalf("first gate: expected 1 email, got %d", cap.count())
	}

	// Second tick: gate unchanged (still B32). Dedupe signature unchanged →
	// no re-alert.
	prev2, err := s.FlightPartByID(ctx, f.ID)
	if err != nil {
		t.Fatalf("refetch: %v", err)
	}
	p.maybeAlert(ctx, prev2, f.ID)
	if got := drainAlerts(t, ch); len(got) != 0 {
		t.Fatalf("same gate: expected no re-alert, got %d", len(got))
	}
	if cap.count() != 1 {
		t.Fatalf("same gate: expected still 1 email, got %d", cap.count())
	}

	// Third tick: gate moves to a NEW value → alerts again.
	prev3, err := s.FlightPartByID(ctx, f.ID)
	if err != nil {
		t.Fatalf("refetch: %v", err)
	}
	setOriginGate(t, s, f.ID, "B40")
	p.maybeAlert(ctx, prev3, f.ID)
	if got := drainAlerts(t, ch); len(got) != 1 || got[0].Kind != "gate" {
		t.Fatalf("new gate: expected 1 gate re-alert, got %+v", got)
	}
}
