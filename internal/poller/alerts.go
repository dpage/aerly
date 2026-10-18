package poller

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dpage/aerly/internal/airports"
	"github.com/dpage/aerly/internal/api"
	"github.com/dpage/aerly/internal/mailer"
	"github.com/dpage/aerly/internal/push"
	"github.com/dpage/aerly/internal/store"
	"github.com/dpage/aerly/internal/timing"
)

// Flight-alert step (spec §9). The poller refresh path calls maybeAlert after
// it has refreshed a part's flight_details. We compare the part's NEW effective
// state against (a) the part's PRE-refresh state, to decide whether a
// meaningful change happened, and (b) a stored per-part dedupe signature, to
// suppress re-sending the same change on the next tick.
//
// "Meaningful" per spec §9 / PRD §6.8:
//   - any transition INTO Cancelled or Diverted always alerts;
//   - a departure delay (estimated_out - scheduled_out) that grows by at least
//     the recipient's alert_prefs.min_delay_min threshold alerts.
//
// Gate changes (README follow-up / AeroDataBox precursor): migration 0014 added
// origin_gate/dest_gate to flight_details, the resolver now surfaces them, and
// the poller persists the live value each tick. When a part's origin_gate or
// dest_gate changes to a NEW non-empty value we always-alert (like
// cancellation; not threshold-gated). A gate appearing where there was none is
// announced as an assignment ("gate_assigned"), and only a known gate moving to
// a different one as a change ("gate"). The gate is folded into the dedupe
// signature so the same gate isn't re-alerted on the next tick.
//
// Terminal (origin_terminal/dest_terminal) is noisier. Only a known terminal
// moving to a different one counts (the first terminal to appear is the
// provider filling the field in), and even then the provider has been seen to
// relabel terminals and to flicker between values, so a change alerts only
// once the new terminal has been reported on two consecutive polls (see
// settleTerminal). Close to departure, and once the flight has gone, a terminal
// change without a gate change at the same end is not alerted at all: by then
// the traveller is at the gate, and the gate is what they act on.
//
// Baggage belt (migration 0027) works almost exactly like gate:
// dest_baggage_belt is the arrival carousel, persisted overwrite-when-non-empty
// each tick, and a change to a new non-empty belt always-alerts and is folded
// into the dedupe signature. The one difference is upstream of this file: the
// poller only persists a belt whilst the flight is within beltWindow of its
// arrival, because the provider will report one many hours before departure.
// The alert path therefore needs no arrival check of its own — a belt it can
// see is one already judged current.

// delaySigBucketMin is the granularity at which a delay is folded into the
// dedupe signature. Two polls reporting the same delay (to the minute) produce
// the same signature and so don't re-alert. We bucket to whole minutes; the
// effective-out time is already minute-resolution in practice.
const delaySigBucketMin = 1

// terminalQuietPeriod is how long before the effective departure terminal
// alerts stop on their own. By then the traveller is airside or boarding, and a
// terminal relabelled by the provider with the gate unchanged sent people
// looking for a move that never happened. A terminal that changes in the same
// poll as the gate at that end is the exception: the gate change corroborates
// it, so it is taken at once and named in the gate alert instead.
const terminalQuietPeriod = 30 * time.Minute

// Terminal ends, used to key the pending terminal changes.
const (
	originEnd = "origin"
	destEnd   = "dest"
)

// pendingTerminalKey identifies one end of one flight part.
type pendingTerminalKey struct {
	partID int64
	end    string
}

// pendingTerminal is a terminal change seen on one poll and waiting for the
// next to confirm it. from is the last stable terminal, the one a confirmed
// change is announced against; to is the new value awaiting confirmation.
type pendingTerminal struct {
	from, to string
}

// terminalTracker holds the unconfirmed terminal changes. It lives in memory
// on the Poller rather than in the database: a restart forgets a change that
// was awaiting confirmation, which loses that one alert, against a migration
// for state that only matters for one poll interval. The poller prunes entries
// for parts that have left every poll set after each complete tick (prune).
type terminalTracker struct {
	mu      sync.Mutex
	pending map[pendingTerminalKey]pendingTerminal
}

// alertState is the minimal snapshot of a flight part used both to detect a
// meaningful change and to build the dedupe signature.
type alertState struct {
	status         string
	delayMin       int  // departure delay = revised off-block - scheduled_out, clamped >= 0
	hasDelay       bool // false until the provider publishes a revised departure
	terminalDV     bool // status is Cancelled or Diverted
	departed       bool // the aircraft has left: wheels-off observed, or Enroute/Arrived
	originGate     string
	destGate       string
	destBelt       string // arrival baggage belt
	originTerminal string
	destTerminal   string

	// Set by evaluateAlert, never by snapshot: the terminal at that end changed
	// alongside its gate inside the quiet period, so the gate alert names it.
	originTerminalWithGate bool
	destTerminalWithGate   bool
}

func snapshot(f *store.Flight) alertState {
	st := alertState{
		status:         f.Status,
		originGate:     f.OriginGate,
		destGate:       f.DestGate,
		destBelt:       f.DestBaggageBelt,
		originTerminal: f.OriginTerminal,
		destTerminal:   f.DestTerminal,
	}
	st.terminalDV = f.Status == "Cancelled" || f.Status == "Diverted"
	st.departed = f.ActualOut != nil || f.Status == "Enroute" || f.Status == "Arrived"
	// The delay is the off-block estimate's alone, never wheels-off's: that
	// runs a taxi-time later, and counting it would fire a phantom "now
	// delayed" alert to everyone watching just as the aircraft left.
	if t := timing.Effective(f.Times()); t.Revised {
		st.delayMin, st.hasDelay = max(0, int(t.Departure.Delay/time.Minute)), true
	}
	return st
}

// alertSignature is the per-part dedupe key for a state. Cancellation/diversion
// key on status alone; a delay keys on the bucketed delay so the same delay
// isn't re-sent, but a deeper delay produces a new signature and re-alerts. The
// gate is always folded in (a change to a new gate produces a new signature and
// so re-alerts, while the same gate value dedupes). Components are joined so a
// part that both slips and changes gate carries both in its signature.
func alertSignature(st alertState) string {
	var parts []string
	switch {
	case st.terminalDV:
		parts = append(parts, "status:"+st.status)
	case st.hasDelay:
		parts = append(parts, fmt.Sprintf("delay:%d", (st.delayMin/delaySigBucketMin)*delaySigBucketMin))
	}
	if st.originGate != "" {
		parts = append(parts, "ogate:"+st.originGate)
	}
	if st.destGate != "" {
		parts = append(parts, "dgate:"+st.destGate)
	}
	if st.destBelt != "" {
		parts = append(parts, "belt:"+st.destBelt)
	}
	if st.originTerminal != "" {
		parts = append(parts, "oterm:"+st.originTerminal)
	}
	if st.destTerminal != "" {
		parts = append(parts, "dterm:"+st.destTerminal)
	}
	return strings.Join(parts, "|")
}

// signatureCarryingDelay is alertSignature(st), except that a state with no
// published delay (and no cancellation or diversion, which key on status
// instead) carries forward the delay in last, the signature last stamped. A
// provider withdrawing its estimate has not cancelled the delay it announced,
// and without the carry an alert stamped whilst the estimate was missing (a
// gate, say) would drop the delay from the signature, so the same estimate
// restored later would be announced a second time.
func signatureCarryingDelay(st alertState, last string) string {
	if !st.hasDelay && !st.terminalDV {
		for _, part := range strings.Split(last, "|") {
			if v, found := strings.CutPrefix(part, "delay:"); found {
				if n, err := strconv.Atoi(v); err == nil {
					st.hasDelay, st.delayMin = true, n
				}
			}
		}
	}
	return alertSignature(st)
}

// maybeAlert is invoked by refresh after the part's flight_details have been
// refreshed. prev is the part's pre-refresh carrier; partID identifies the
// part; reported says whether this poll carried a fresh report from the
// provider (a tracking tick whose resolve was throttled or failed only re-reads
// what is already stored, so it must not count towards confirming a terminal
// change). It re-fetches the post-refresh state, decides whether a meaningful
// change occurred, dedupes against the stored signature, and fans out in-app +
// email alerts to the recipient set filtered by each user's alert_prefs.
func (p *Poller) maybeAlert(ctx context.Context, prev *store.Flight, partID int64, reported bool) {
	cur, err := p.Store.FlightPartByID(ctx, partID)
	if err != nil {
		// Benign: the part may have been deleted concurrently. Anything else
		// is logged by the caller's own paths; here we just skip.
		return
	}
	kind, detail, sigSt := p.evaluateAlert(partID, prev, cur, reported, time.Now())
	if kind == "" {
		return
	}

	// Dedupe: don't re-send a change whose signature matches the last one we
	// alerted on for this part.
	last, ok, serr := p.Store.FlightPartAlertSig(ctx, partID)
	if serr != nil {
		slog.Error("alert: read dedupe sig", "id", partID, "err", serr)
		return
	}
	sig := signatureCarryingDelay(sigSt, last)
	if ok && last == sig {
		return
	}

	// Resolve the trip/plan context (for the SSE payload + visibility) and the
	// recipient set with prefs.
	tp, err := p.Store.TrackerPartRow(ctx, partID)
	if err != nil {
		slog.Error("alert: tracker row", "id", partID, "err", err)
		return
	}
	recips, err := p.Store.AlertRecipientsWithPrefs(ctx, tp.PlanID)
	if err != nil {
		slog.Error("alert: recipients", "plan_id", tp.PlanID, "err", err)
		return
	}

	p.dispatchAlert(ctx, tp, cur, kind, detail, sigSt, recips)

	// Stamp the dedupe signature only after we've attempted delivery so a mid-
	// flight crash re-alerts rather than silently swallowing the change.
	if err := p.Store.SetFlightPartAlertSig(ctx, partID, sig); err != nil {
		slog.Error("alert: stamp dedupe sig", "id", partID, "err", err)
	}
}

// evaluateAlert decides what, if anything, the refresh prev → cur should
// announce at now (reported as for maybeAlert): the alert kind ("" for nothing), its detail phrase, and the
// state whose signature dedupes it. It also advances the pending terminal
// changes for partID, so it must be called exactly once per poll of a part.
//
// The terminals are settled before classifying. prev comes from the store, so
// by the poll that confirms a terminal change the new value is already in both
// snapshots; settling puts the last stable terminal back into prev so that
// changeKind sees the change, and keeps an unconfirmed terminal out of both prev
// and the signature, so that it neither alerts early nor gets stamped into a
// signature that would then swallow the confirmed alert.
func (p *Poller) evaluateAlert(partID int64, prev, cur *store.Flight, reported bool, now time.Time) (kind, detail string, sigSt alertState) {
	prevSt := snapshot(prev)
	curSt := snapshot(cur)
	sigSt = curSt

	quiet := curSt.departed || inQuietPeriod(cur, now)
	prevSt.originTerminal, sigSt.originTerminal, curSt.originTerminalWithGate = p.terminals.settle(partID, originEnd,
		prevSt.originTerminal, curSt.originTerminal, reported, quiet, gateChanged(prevSt.originGate, curSt.originGate))
	prevSt.destTerminal, sigSt.destTerminal, curSt.destTerminalWithGate = p.terminals.settle(partID, destEnd,
		prevSt.destTerminal, curSt.destTerminal, reported, quiet, gateChanged(prevSt.destGate, curSt.destGate))

	kind = changeKind(prevSt, curSt)
	if kind == "" {
		return "", "", sigSt
	}
	return kind, changeDetail(kind, prevSt, curSt, cur), sigSt
}

// inQuietPeriod reports whether now is within terminalQuietPeriod of the
// flight's effective departure (the revised off-block time the delay is
// measured from, else the timetable), or past it.
func inQuietPeriod(f *store.Flight, now time.Time) bool {
	// Not Departure.At: that becomes wheels-off once observed, a taxi later than the off-block time this window runs from.
	dep := f.ScheduledOut.Add(timing.Effective(f.Times()).Departure.Delay)
	return !now.Before(dep.Add(-terminalQuietPeriod))
}

// settle advances the pending terminal change for one end of a part and
// returns the previous terminal changeKind should compare against, the
// terminal to fold into the dedupe signature, and whether the gate alert should
// name the terminal (withGate). gateMoved says whether the gate at this end
// changed (assigned or moved) in this poll.
//
//   - in the quiet period with no gate change at this end, any pending change
//     is dropped and the new terminal accepted silently;
//   - in the quiet period with a gate change at this end, a terminal that
//     differs from the stable one (the pending change's from, else prev) is
//     taken at once, corroborated by the gate change, and named in the gate
//     alert; the terminal goes into the signature so it is not alerted again;
//   - a poll with no fresh provider report (reported false) leaves any pending
//     change as it is and alerts nothing, since all it saw is what the last
//     report already stored;
//   - a pending change reported again is confirmed: it is announced against
//     the stable terminal it moved from;
//   - a pending change that reverts to the stable terminal is dropped;
//   - a pending change that moves on again waits for the new value instead;
//   - a fresh change (known terminal → a different known one) starts pending.
//
// Whilst a change is pending, both returns hold the stable terminal for the
// signature and the current one for the comparison, so nothing alerts and an
// alert for something else on the same poll stamps the stable terminal.
func (tt *terminalTracker) settle(partID int64, end, prev, cur string, reported, quiet, gateMoved bool) (cmpPrev, sigTerm string, withGate bool) {
	key := pendingTerminalKey{partID: partID, end: end}
	tt.mu.Lock()
	defer tt.mu.Unlock()
	pend, ok := tt.pending[key]
	switch {
	case quiet && !gateMoved:
		delete(tt.pending, key)
		return cur, cur, false
	case !reported && ok:
		return cur, pend.from, false
	case !reported:
		return cur, cur, false
	case quiet:
		stable := prev
		if ok {
			stable = pend.from
		}
		delete(tt.pending, key)
		if terminalChanged(stable, cur) {
			return stable, cur, true
		}
		return cur, cur, false
	case ok && cur == pend.to:
		delete(tt.pending, key)
		return pend.from, cur, false
	case ok && cur == pend.from:
		delete(tt.pending, key)
		return cur, cur, false
	case ok && cur == "":
		// The provider dropped the field; keep waiting on what we had.
		return cur, pend.from, false
	case ok:
		tt.pending[key] = pendingTerminal{from: pend.from, to: cur}
		return cur, pend.from, false
	case terminalChanged(prev, cur):
		if tt.pending == nil {
			tt.pending = make(map[pendingTerminalKey]pendingTerminal)
		}
		tt.pending[key] = pendingTerminal{from: prev, to: cur}
		return cur, prev, false
	default:
		return prev, cur, false
	}
}

// prune drops the pending terminal changes for every part not in keep. The
// poller calls it after a tick whose poll queries all succeeded, with the union
// of the parts they listed, so a part that leaves every poll set (dismissed,
// deleted, or landed and past the post-arrival window) with a change still
// pending does not hold its entry for the life of the process.
func (tt *terminalTracker) prune(keep map[int64]struct{}) {
	tt.mu.Lock()
	defer tt.mu.Unlock()
	for key := range tt.pending {
		if _, ok := keep[key.partID]; !ok {
			delete(tt.pending, key)
		}
	}
}

// changeKind classifies the transition prev → cur into an alert kind, or ""
// when nothing alert-worthy happened. Cancellation/diversion are always
// alert-worthy on entry. A delay is alert-worthy when it grew vs the previous
// snapshot (threshold filtering is per-recipient and happens in dispatchAlert).
func changeKind(prev, cur alertState) string {
	// Cancellation / diversion: alert on entry into the terminal state.
	if cur.status == "Cancelled" && prev.status != "Cancelled" {
		return "cancelled"
	}
	if cur.status == "Diverted" && prev.status != "Diverted" {
		return "diverted"
	}
	// Gate change: a known departure or arrival gate that moved to a
	// different one. Always-alert (like cancellation), ahead of a delay so a
	// flight that both slips and re-gates leads with the actionable gate. A
	// gate going empty (provider dropped the field) is not a change worth
	// alerting. A move at either end leads over an assignment at the other,
	// since a move is what sends someone the wrong way.
	if gateMoved(prev.originGate, cur.originGate) || gateMoved(prev.destGate, cur.destGate) {
		return "gate"
	}
	// Gate assigned: a gate appearing where there was none. Still always-alert
	// (it is what a traveller is waiting for), but not called a change.
	if gateAssigned(prev.originGate, cur.originGate) || gateAssigned(prev.destGate, cur.destGate) {
		return "gate_assigned"
	}
	// Baggage belt: the arrival belt became a NEW non-empty value. Always-alert
	// like gate (the belt is published near arrival; a reassignment is exactly
	// what travellers want pushed). Same new-non-empty rule as gateChanged.
	if gateChanged(prev.destBelt, cur.destBelt) {
		return "belt"
	}
	// Terminal change: a known departure or arrival terminal that moved to a
	// different one. Always-alert like gate/belt — a terminal reassignment can
	// mean a different building and more time needed. Unlike a gate, a
	// terminal first appearing is not news: the provider fills it in when it
	// first resolves the flight, and announcing that as a "terminal change"
	// read as a reassignment that never happened. By the time prev reaches
	// here evaluateAlert has already held back an unconfirmed change and one
	// inside the quiet period. (A terminal that changed with its gate inside
	// the quiet period does reach here, but the gate alert above outranks it
	// and names the terminal in its detail.)
	if terminalChanged(prev.originTerminal, cur.originTerminal) || terminalChanged(prev.destTerminal, cur.destTerminal) {
		return "terminal"
	}
	// Delay: a (deeper) departure delay. We treat any increase in the measured
	// delay as a candidate; the min_delay_min threshold is applied per
	// recipient so a 5-minute slip below everyone's threshold is suppressed
	// there, and dedupe stops the same delay re-firing.
	//
	// Not once the aircraft has left, though: a departure delay is news only to
	// someone still waiting to board. AeroDataBox has been seen to move the
	// revised departure to the takeoff time as the flight leaves, so without
	// this an on-time pushback followed by a long taxi alerted as "now
	// delayed" just after the aircraft was airborne.
	if cur.hasDelay && !cur.departed && cur.delayMin > prev.delayMin && cur.delayMin > 0 {
		return "delayed"
	}
	return ""
}

// gateChanged reports whether cur is a new, non-empty gate distinct from prev.
// An unchanged gate, or a gate that disappeared (cur == ""), is not a change.
func gateChanged(prev, cur string) bool {
	return cur != "" && cur != prev
}

// gateMoved reports whether a known gate moved to a different known one.
func gateMoved(prev, cur string) bool {
	return prev != "" && gateChanged(prev, cur)
}

// gateAssigned reports whether a gate appeared where there was none.
func gateAssigned(prev, cur string) bool {
	return prev == "" && cur != ""
}

// terminalChanged reports whether a known terminal moved to a different known
// one. A terminal first being filled in (prev == "") or disappearing (cur ==
// "") is not a change.
func terminalChanged(prev, cur string) bool {
	return prev != "" && gateChanged(prev, cur)
}

// changeDetail builds the human phrase appended to the headline. For a delay it
// names the airline's revised departure; for a gate change it names the new gate
// (departure preferred when both moved) and for an assignment the gate assigned,
// adding that end's terminal when it changed alongside the gate inside the
// quiet period; cancellation/diversion stand alone.
func changeDetail(kind string, prev, cur alertState, f *store.Flight) string {
	switch kind {
	case "delayed":
		if t := timing.Effective(f.Times()); t.Revised {
			eff := f.ScheduledOut.Add(t.Departure.Delay)
			// Quote the revised departure on the departure airport's clock,
			// which is the one the traveller is reading (issue #117); UTC only
			// where the airport's zone can't be resolved at all.
			zone := airportZone(f.OriginIATA, f.OriginLat, f.OriginLon)
			return "now departing " + inZone(eff, zone).Format("15:04 MST")
		}
		return "now delayed"
	case "diverted":
		return "diverted to a different airport"
	case "gate":
		if gateMoved(prev.originGate, cur.originGate) {
			return "now departs gate " + cur.originGate + withTerminal(cur.originTerminalWithGate, cur.originTerminal)
		}
		if gateMoved(prev.destGate, cur.destGate) {
			return "now arrives at gate " + cur.destGate + withTerminal(cur.destTerminalWithGate, cur.destTerminal)
		}
		return "gate changed"
	case "gate_assigned":
		if gateAssigned(prev.originGate, cur.originGate) {
			return "departs gate " + cur.originGate + withTerminal(cur.originTerminalWithGate, cur.originTerminal)
		}
		if gateAssigned(prev.destGate, cur.destGate) {
			return "arrives at gate " + cur.destGate + withTerminal(cur.destTerminalWithGate, cur.destTerminal)
		}
		return "gate assigned"
	case "belt":
		return "bags on belt " + cur.destBelt
	case "terminal":
		if terminalChanged(prev.originTerminal, cur.originTerminal) {
			return "now departs terminal " + cur.originTerminal
		}
		if terminalChanged(prev.destTerminal, cur.destTerminal) {
			return "now arrives at terminal " + cur.destTerminal
		}
		return "terminal changed"
	default:
		return ""
	}
}

// withTerminal is the ", terminal N" suffix a gate alert carries when the
// terminal at that end changed alongside the gate, else "".
func withTerminal(changed bool, terminal string) string {
	if !changed {
		return ""
	}
	return ", terminal " + terminal
}

// alertMessage is the one-liner carried in the in-app SSE payload.
func alertMessage(ident, kind, detail string) string {
	subj := mailer.FlightAlertSubject(ident, kind)
	if detail != "" {
		return subj + " — " + detail
	}
	return subj
}

// dispatchAlert fans the change out to every recipient, filtered by each user's
// alert_prefs: in-app via a per-user notifications.updated-style SSE event, and
// email via the mailer when an address is on file and email alerts are on. For
// a delay, the recipient's min_delay_min threshold gates BOTH channels.
func (p *Poller) dispatchAlert(
	ctx context.Context,
	tp *store.TrackerPart,
	cur *store.Flight,
	kind, detail string,
	st alertState,
	recips []store.AlertRecipient,
) {
	msg := alertMessage(cur.Ident, kind, detail)
	always := kind == "cancelled" || kind == "diverted" || kind == "gate" || kind == "gate_assigned" || kind == "belt" || kind == "terminal"

	for _, r := range recips {
		// Threshold filter for delays only; cancellations/diversions always
		// alert (PRD §6.8).
		if !always && st.delayMin < r.MinDelayMin {
			continue
		}
		if r.InApp {
			p.publishAlert(ctx, r.UserID, tp, cur.Ident, kind, msg)
		}
		if r.Email && r.EmailAddr != "" {
			p.sendAlertEmail(ctx, r.EmailAddr, cur, kind, detail)
		}
		p.pushAlert(ctx, r.UserID, tp, cur.Ident, msg)
	}
}

// pushAlert delivers the change to the recipient's subscribed devices as a Web
// Push, gated on the user's 'alert' push-kind pref (default on). Threshold and
// dedupe filtering have already happened in dispatchAlert, so reaching here
// means the alert is worth pushing. Best-effort: a disabled Sender, a user with
// push off, or one with no subscriptions is a silent no-op, and the Sender
// itself never blocks or errors out of this path.
func (p *Poller) pushAlert(ctx context.Context, userID int64, tp *store.TrackerPart, ident, msg string) {
	if p.Push == nil || !p.Push.Enabled() {
		return
	}
	on, err := p.Store.PushKindEnabled(ctx, userID, "alert")
	if err != nil {
		slog.Error("alert: push kind pref", "user", userID, "err", err)
		return
	}
	if !on {
		return
	}
	p.Push.Send(ctx, []int64{userID}, push.Payload{
		Title: "Aerly flight alert",
		Body:  msg,
		// Deep-link to the flight's trip; the SW focuses/opens this on click.
		URL: fmt.Sprintf("/trips/%d", tp.TripID),
		// Collapse successive updates to the same flight into one notification.
		Tag:  "alert-" + ident,
		Kind: "alert",
	})
}

// publishAlert persists the alert for the recipient, then pushes a single-user,
// user-private alert.created SSE event carrying the stored row (with id +
// created_at). The payload reuses the open-shape NotificationsDTO with the Alert
// field set, so clients reading only friend_requests_pending ignore it safely.
// Persistence is best-effort: a failed insert is logged and we skip the push for
// that recipient (no orphan SSE without a backing row).
func (p *Poller) publishAlert(ctx context.Context, userID int64, tp *store.TrackerPart, ident, kind, msg string) {
	stored, err := p.Store.InsertFlightAlert(ctx, store.FlightAlert{
		UserID:     userID,
		PlanPartID: tp.PlanPartID,
		PlanID:     tp.PlanID,
		TripID:     tp.TripID,
		Ident:      ident,
		Kind:       kind,
		Status:     tp.Status,
		Message:    msg,
	})
	if err != nil {
		slog.Error("alert: persist inbox row", "user", userID, "err", err)
		return
	}
	dto := api.NotificationsDTO{Alert: ptrFlightAlertDTO(api.ToFlightAlertDTO(stored))}
	payload, err := jsonMarshal(dto)
	if err != nil {
		slog.Error("alert: marshal", "err", err)
		return
	}
	p.Hub.Publish(sseAlertEvent(userID, payload))
}

func ptrFlightAlertDTO(d api.FlightAlertDTO) *api.FlightAlertDTO { return &d }

// sendAlertEmail dispatches the templated flight-change email. Best-effort:
// failures are logged and never block the poll loop. Skipped when no sender or
// MailFrom is configured (e.g. dev without a sendmail pipe). The email names the
// route and times of f so a traveller with several flights booked can tell
// which one changed (issue #144); each time is quoted on its own airport's
// clock, as the delay detail already is.
func (p *Poller) sendAlertEmail(ctx context.Context, to string, f *store.Flight, kind, detail string) {
	if p.MailFromAddress == "" {
		return
	}
	send := p.SendAlertEmail
	if send == nil {
		send = mailer.Send
	}
	t := timing.Effective(f.Times())
	msg := mailer.BuildFlightAlertEmail(mailer.FlightAlertInput{
		FromAddr:  p.MailFromAddress,
		ToAddr:    to,
		PublicURL: p.PublicURL,
		Ident:     f.Ident,
		Kind:      kind,
		Detail:    detail,
		Origin:    airports.Label(f.OriginIATA, ""),
		Dest:      airports.Label(f.DestIATA, ""),
		DepartsAt: t.Departure.At,
		OriginTZ:  airportZone(f.OriginIATA, f.OriginLat, f.OriginLon),
		ArrivesAt: arrivalAt(t),
		DestTZ:    airportZone(f.DestIATA, f.DestLat, f.DestLon),
	})
	if err := send(ctx, p.SendmailPath, p.MailFromAddress, msg); err != nil {
		slog.Error("alert: send email", "to", to, "ident", f.Ident, "err", err)
	}
}
