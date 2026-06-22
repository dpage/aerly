package poller

import (
	"context"
	"encoding/json"
	"errors"
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

// TestSnapshotAndSignature exercises the pure snapshot/effectiveOut/signature
// helpers directly: the delay-clamp (negative effective-out → 0), the
// estimated-out fallback when there's no actual time, and the belt/terminal
// components of the dedupe signature.
func TestSnapshotAndSignature(t *testing.T) {
	out := time.Date(2026, 6, 3, 10, 0, 0, 0, time.UTC)

	// No estimated/actual departure → no delay reported.
	noTimes := &store.Flight{Status: "Scheduled", ScheduledOut: out}
	if st := snapshot(noTimes); st.hasDelay {
		t.Errorf("a flight with only a scheduled time should report no delay: %+v", st)
	}

	// Estimated-out earlier than scheduled → delay clamps to 0 (not negative).
	early := out.Add(-20 * time.Minute)
	earlyF := &store.Flight{Status: "Scheduled", ScheduledOut: out, EstimatedOut: &early}
	if st := snapshot(earlyF); !st.hasDelay || st.delayMin != 0 {
		t.Errorf("an early estimate should clamp the delay to 0, got %+v", st)
	}

	// Actual-out wins over estimated-out for effectiveOut.
	act := out.Add(40 * time.Minute)
	est := out.Add(10 * time.Minute)
	bothF := &store.Flight{Status: "Enroute", ScheduledOut: out, ActualOut: &act, EstimatedOut: &est}
	if eff := effectiveOut(bothF); eff == nil || !eff.Equal(act) {
		t.Errorf("effectiveOut should prefer actual_out, got %v", eff)
	}
	if st := snapshot(bothF); st.delayMin != 40 {
		t.Errorf("delay from actual_out = %d, want 40", st.delayMin)
	}

	// Belt and destination-terminal both fold into the signature.
	full := alertState{
		status: "Scheduled", destBelt: "7", destTerminal: "2", originTerminal: "1",
		originGate: "A1", destGate: "B2", hasDelay: true, delayMin: 30,
	}
	sig := alertSignature(full)
	for _, want := range []string{"delay:30", "ogate:A1", "dgate:B2", "belt:7", "oterm:1", "dterm:2"} {
		if !strings.Contains(sig, want) {
			t.Errorf("signature %q missing %q", sig, want)
		}
	}
}

// TestChangeDetail exercises the changeDetail phrasing branches that the
// end-to-end alert tests don't reach: a delay with no effective-out time, an
// arrival-gate change, and an arrival-terminal change.
func TestChangeDetail(t *testing.T) {
	out := time.Date(2026, 6, 3, 10, 0, 0, 0, time.UTC)
	noEff := &store.Flight{ScheduledOut: out} // no estimated/actual → no eff time
	if got := changeDetail("delayed", alertState{}, alertState{}, noEff); got != "now delayed" {
		t.Errorf("delayed with no eff time = %q, want 'now delayed'", got)
	}

	// Arrival gate moved (origin unchanged) → arrival phrasing.
	prev := alertState{}
	cur := alertState{destGate: "C3"}
	if got := changeDetail("gate", prev, cur, noEff); got != "now arrives at gate C3" {
		t.Errorf("dest-gate detail = %q", got)
	}

	// Arrival terminal moved (origin unchanged) → arrival phrasing.
	curT := alertState{destTerminal: "4"}
	if got := changeDetail("terminal", prev, curT, noEff); got != "now arrives at terminal 4" {
		t.Errorf("dest-terminal detail = %q", got)
	}
}

// TestAlert_DedupeSigMatchSuppressesReAlert covers the stored-signature match
// branch (151-153): a change whose kind is non-empty but whose signature equals
// the last one we stamped is suppressed. We alert once (stamping the sig), then
// replay the SAME pre-state so changeKind is still "delayed" but the signature
// is unchanged.
func TestAlert_DedupeSigMatchSuppressesReAlert(t *testing.T) {
	p, s, hub, _ := alertPoller(t)
	ctx := context.Background()
	owner := seedUser(t, s)
	now := time.Now()
	f, err := mkPart(ctx, s, partSeed{
		Ident: "BA700", ScheduledOut: now.Add(time.Hour), ScheduledIn: now.Add(3 * time.Hour),
		OriginIATA: "LHR", DestIATA: "JFK",
	}, owner)
	if err != nil {
		t.Fatalf("mkPart: %v", err)
	}
	ch, unsub := hub.Subscribe(sse.Subscription{ViewerID: owner})
	defer unsub()

	prev := f // no delay
	setEstimatedOut(t, s, f.ID, f.ScheduledOut.Add(45*time.Minute))
	p.maybeAlert(ctx, prev, f.ID) // alerts + stamps sig "delay:45"
	if got := drainAlerts(t, ch); len(got) != 1 {
		t.Fatalf("first delay: expected 1 alert, got %d", len(got))
	}

	// Replay with the same no-delay prev: changeKind is "delayed" again, but the
	// stored signature already equals the current one → suppressed.
	p.maybeAlert(ctx, prev, f.ID)
	if got := drainAlerts(t, ch); len(got) != 0 {
		t.Fatalf("sig-match replay should be suppressed, got %d", len(got))
	}
}

// TestAlert_LookupErrorsAreSwallowed covers the early-return error branches in
// maybeAlert (FlightPartByID, dedupe-sig read, TrackerPartRow,
// AlertRecipientsWithPrefs) and the SetFlightPartAlertSig stamp error, all via a
// cancelled context: maybeAlert must log and bail without panicking and without
// emitting an alert.
func TestAlert_LookupErrorsAreSwallowed(t *testing.T) {
	p, s, hub, cap := alertPoller(t)
	owner := seedUser(t, s)
	now := time.Now()
	f, err := mkPart(context.Background(), s, partSeed{
		Ident: "BA800", ScheduledOut: now.Add(time.Hour), ScheduledIn: now.Add(3 * time.Hour),
		OriginIATA: "LHR", DestIATA: "JFK",
	}, owner)
	if err != nil {
		t.Fatalf("mkPart: %v", err)
	}
	ch, unsub := hub.Subscribe(sse.Subscription{ViewerID: owner})
	defer unsub()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // FlightPartByID errors → maybeAlert returns at the first guard

	p.maybeAlert(ctx, f, f.ID) // must not panic

	if got := drainAlerts(t, ch); len(got) != 0 {
		t.Fatalf("a cancelled context must not emit an alert, got %d", len(got))
	}
	if cap.count() != 0 {
		t.Fatalf("a cancelled context must not send email, got %d", cap.count())
	}
}

// TestPublishAlert_InsertErrorSkipsBroadcast covers the InsertFlightAlert error
// branch in publishAlert: a cancelled context fails the persist, so no SSE event
// is pushed (no orphan event without a backing row).
func TestPublishAlert_InsertErrorSkipsBroadcast(t *testing.T) {
	p, s, hub, _ := alertPoller(t)
	owner := seedUser(t, s)
	now := time.Now()
	f, err := mkPart(context.Background(), s, partSeed{
		Ident: "BA810", ScheduledOut: now.Add(time.Hour), ScheduledIn: now.Add(3 * time.Hour),
		OriginIATA: "LHR", DestIATA: "JFK",
	}, owner)
	if err != nil {
		t.Fatalf("mkPart: %v", err)
	}
	tp, err := s.TrackerPartRow(context.Background(), f.ID)
	if err != nil {
		t.Fatalf("tracker row: %v", err)
	}
	ch, unsub := hub.Subscribe(sse.Subscription{ViewerID: owner})
	defer unsub()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p.publishAlert(ctx, owner, tp, "BA810", "delayed", "now delayed")

	select {
	case ev := <-ch:
		t.Errorf("no SSE event expected when the insert fails, got %s", ev.Type)
	case <-time.After(150 * time.Millisecond):
	}
}

// TestPublishAlert_MarshalErrorSkipsBroadcast covers the marshal-failure branch
// in publishAlert: the row persists but the SSE encode fails, so no event is
// pushed.
func TestPublishAlert_MarshalErrorSkipsBroadcast(t *testing.T) {
	p, s, hub, _ := alertPoller(t)
	ctx := context.Background()
	owner := seedUser(t, s)
	now := time.Now()
	f, err := mkPart(ctx, s, partSeed{
		Ident: "BA820", ScheduledOut: now.Add(time.Hour), ScheduledIn: now.Add(3 * time.Hour),
		OriginIATA: "LHR", DestIATA: "JFK",
	}, owner)
	if err != nil {
		t.Fatalf("mkPart: %v", err)
	}
	tp, err := s.TrackerPartRow(ctx, f.ID)
	if err != nil {
		t.Fatalf("tracker row: %v", err)
	}
	ch, unsub := hub.Subscribe(sse.Subscription{ViewerID: owner})
	defer unsub()

	failMarshal(t)
	p.publishAlert(ctx, owner, tp, "BA820", "delayed", "now delayed")

	select {
	case ev := <-ch:
		t.Errorf("no SSE event expected when marshal fails, got %s", ev.Type)
	case <-time.After(150 * time.Millisecond):
	}
	// The backing row was still persisted before the marshal step.
	rows, _ := s.ListFlightAlerts(ctx, owner, 10)
	if len(rows) != 1 {
		t.Fatalf("expected the alert row to persist before the marshal failure, got %d", len(rows))
	}
}

// TestPushAlert_KindPrefErrorSwallowed covers the PushKindEnabled error branch
// in pushAlert: a cancelled context fails the pref lookup, so pushAlert logs and
// returns without sending (and without panicking).
func TestPushAlert_KindPrefErrorSwallowed(t *testing.T) {
	p, s, _, _ := alertPoller(t)
	fp := &fakePusher{enabled: true}
	p.Push = fp
	owner := seedUser(t, s)
	now := time.Now()
	f, err := mkPart(context.Background(), s, partSeed{
		Ident: "BA830", ScheduledOut: now.Add(time.Hour), ScheduledIn: now.Add(3 * time.Hour),
		OriginIATA: "LHR", DestIATA: "JFK",
	}, owner)
	if err != nil {
		t.Fatalf("mkPart: %v", err)
	}
	tp, err := s.TrackerPartRow(context.Background(), f.ID)
	if err != nil {
		t.Fatalf("tracker row: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // PushKindEnabled errors → pushAlert returns before Send
	p.pushAlert(ctx, owner, tp, "BA830", "now delayed")

	if fp.count() != 0 {
		t.Fatalf("no push expected when the kind-pref lookup fails, got %d", fp.count())
	}
}

// TestSendAlertEmail_Branches covers sendAlertEmail's MailFrom-empty early
// return, the send==nil fall-back to mailer.Send, and the send-error log path.
func TestSendAlertEmail_Branches(t *testing.T) {
	ctx := context.Background()

	// (a) No MailFromAddress configured → early return, nothing sent.
	p1, _, _ := newPoller(t, &mockTracker{}, time.Minute)
	p1.sendAlertEmail(ctx, "to@aerly.test", "BA900", "delayed", "now delayed") // no-op

	// (b) Send==nil → defaults to mailer.Send with a no-op sendmail (/bin/true).
	p2, _, _ := newPoller(t, &mockTracker{}, time.Minute)
	p2.MailFromAddress = "alerts@aerly.test"
	p2.SendmailPath = "/bin/true"
	p2.PublicURL = "http://localhost:8080"
	p2.sendAlertEmail(ctx, "to@aerly.test", "BA901", "cancelled", "") // default mailer

	// (c) A failing Send is logged and swallowed (no panic, no propagation).
	p3, _, _ := newPoller(t, &mockTracker{}, time.Minute)
	p3.MailFromAddress = "alerts@aerly.test"
	p3.SendmailPath = "/bin/true"
	p3.SendAlertEmail = func(context.Context, string, string, string) error {
		return errors.New("sendmail pipe broke")
	}
	p3.sendAlertEmail(ctx, "to@aerly.test", "BA902", "gate", "now departs gate B32")
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
