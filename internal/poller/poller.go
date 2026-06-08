// Package poller drives the periodic refresh of active flights via a
// Tracker, persists positions, refreshes the time-derived status, and
// broadcasts updates over the SSE hub. It runs as a goroutine in the same
// process as the HTTP server.
package poller

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/dpage/aerly/internal/api"
	"github.com/dpage/aerly/internal/providers"
	"github.com/dpage/aerly/internal/sse"
	"github.com/dpage/aerly/internal/store"
)

type Poller struct {
	Store    *store.Store
	Tracker  providers.Tracker
	Resolver providers.Resolver // optional; when set, backfills missing metadata
	Hub      *sse.Hub
	Interval time.Duration

	// Email-alert config (spec §9). When MailFromAddress is empty the email
	// channel is skipped (in-app alerts still fire). SendAlertEmail defaults
	// to mailer.Send; tests override it to capture messages.
	MailFromAddress string
	SendmailPath    string
	PublicURL       string
	SendAlertEmail  func(ctx context.Context, sendmailPath, envelopeSender, message string) error
}

// sseAlertEvent builds the user-private alert.created SSE event for a single
// recipient. UserPrivate keeps a superuser show-all subscription from seeing
// another user's alert (same rule as notifications.updated).
func sseAlertEvent(userID int64, payload []byte) sse.Event {
	return sse.Event{
		Type:        "alert.created",
		Data:        payload,
		VisibleTo:   []int64{userID},
		UserPrivate: true,
	}
}

func New(s *store.Store, t providers.Tracker, hub *sse.Hub, interval time.Duration) *Poller {
	if interval <= 0 {
		interval = 60 * time.Second
	}
	return &Poller{Store: s, Tracker: t, Hub: hub, Interval: interval}
}

func (p *Poller) Run(ctx context.Context) {
	slog.Info("poller started", "interval", p.Interval)
	defer slog.Info("poller stopped")

	// Startup sweep: fill any NULL coord columns the latest deploy's
	// airports table can now satisfy, before the main poll loop starts.
	p.Sweep(ctx)

	// Tick immediately on startup so a fresh server doesn't look stale.
	p.tick(ctx)

	mainT := time.NewTicker(p.Interval)
	defer mainT.Stop()
	sweepT := time.NewTicker(sweepInterval)
	defer sweepT.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-mainT.C:
			p.tick(ctx)
		case <-sweepT.C:
			p.Sweep(ctx)
		}
	}
}

// minPollAge returns how long to wait between polls for a given flight.
// Enroute flights are polled at the base interval; all other active statuses
// (Scheduled, etc.) are polled at 5× the base interval since they change
// infrequently before departure.
//
// The stored status only flips Scheduled→Enroute inside refresh() (via
// RefreshFlightPartStatus, which derives it from the schedule), and refresh()
// only runs once this throttle lets the flight through. Keying purely off the
// stored status is therefore a chicken-and-egg trap: a flight that has just
// crossed its scheduled departure is airborne but still stored as Scheduled,
// so the slow 5× cadence would delay the flip to Enroute (and position
// tracking) by up to one slow interval. Treat a flight at/after its scheduled
// departure as enroute for cadence purposes so it's polled promptly.
func (p *Poller) minPollAge(f *store.Flight, now time.Time) time.Duration {
	if f.Status == "Enroute" || !now.Before(f.ScheduledOut) {
		return p.Interval
	}
	return p.Interval * 5
}

func (p *Poller) tick(ctx context.Context) {
	now := time.Now()
	flights, err := p.Store.ActiveFlightParts(ctx, now)
	if err != nil {
		slog.Error("poller: list active flight parts", "err", err)
		return
	}
	for _, f := range flights {
		if ctx.Err() != nil {
			return
		}
		if f.LastPolledAt != nil && now.Sub(*f.LastPolledAt) < p.minPollAge(f, now) {
			continue
		}
		guard("poller.refresh", f.ID, func() { p.refresh(ctx, f, now) })
	}

	// Second pass: flights 30min–12h before departure. Resolve metadata only
	// (gate / airframe / schedule) so an early-published gate — or the real
	// times for a manually-added flight — surface ahead of the tracking window,
	// without polling positions for a plane that isn't airborne yet.
	meta, err := p.Store.FlightPartsNeedingMetadata(ctx, now)
	if err != nil {
		slog.Error("poller: list flight parts needing metadata", "err", err)
		return
	}
	for _, f := range meta {
		if ctx.Err() != nil {
			return
		}
		// Same per-flight cadence as the main loop, so a degenerate flight the
		// resolver can't fix doesn't re-resolve every tick (it stays "needs
		// backfill" forever otherwise).
		if f.LastPolledAt != nil && now.Sub(*f.LastPolledAt) < p.minPollAge(f, now) {
			continue
		}
		guard("poller.refreshMetadata", f.ID, func() { p.refreshMetadata(ctx, f, now) })
	}

	// Upcoming-plan reminders (issue #11) — independent of the status-change
	// alert path above.
	p.remindUpcoming(ctx, now)
}

// guard runs fn, recovering from any panic so one poisoned flight row can't
// crash the shared server process (the poller runs in the same process as the
// HTTP server). The panic is logged with the offending flight id.
func guard(where string, id int64, fn func()) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("poller: recovered from panic", "where", where, "id", id, "panic", r)
		}
	}()
	fn()
}

// refreshMetadata resolves a pre-departure flight's gate/airframe/schedule
// (when blank or stale), re-derives its status, and broadcasts the change —
// without any position tracking. The resolver call is gated by the same
// needsBackfill / needsLateRefresh triggers and last_resolved_at throttle as
// the main poll path, so it costs at most one resolve every lateRefreshInterval.
func (p *Poller) refreshMetadata(ctx context.Context, f *store.Flight, now time.Time) {
	if p.Resolver == nil || !(needsBackfill(f) || needsLateRefresh(f, now)) {
		return
	}
	prev := f // pre-resolve, so a newly-published gate is an alertable delta
	if _, err := p.resolveAndUpdate(ctx, f, now); err != nil {
		// Bump last_polled_at even on a failed resolve, so the metadata-pass
		// minPollAge throttle applies — otherwise a flight the resolver can't
		// fix (last_resolved_at stamped, but last_polled_at still nil) is
		// retried every tick.
		if statusErr := p.Store.RefreshFlightPartStatus(ctx, f.ID); statusErr != nil {
			slog.Error("poller: refresh status (metadata pass, resolve error)", "id", f.ID, "err", statusErr)
		}
		return // not-found / transport error already stamped last_resolved_at
	}
	if err := p.Store.RefreshFlightPartStatus(ctx, f.ID); err != nil {
		slog.Error("poller: refresh status (metadata pass)", "id", f.ID, "err", err)
	}
	// A gate/cancellation/delay surfaced ahead of departure is worth an alert
	// just like one found in the active window.
	p.maybeAlert(ctx, prev, f.ID)
	p.publishPartChange(ctx, f.ID)
}

func (p *Poller) refresh(ctx context.Context, f *store.Flight, now time.Time) {
	// Resolver work, two overlapping triggers:
	//   - needsBackfill: airports / airframe are blank (manual add, never
	//     resolved), so we want to fill them in once.
	//   - needsLateRefresh: the flight is close to departure (or enroute)
	//     and last_resolved_at is stale. AeroDataBox only firms up the
	//     operating airframe within ~24h of departure, and airlines swap
	//     metal on the day; without this, we'd keep polling OpenSky for
	//     the airframe that was scheduled at booking time, not the one
	//     actually in the air.
	// last_resolved_at is bumped on every resolve attempt — success,
	// not-found, or transport error — so a doomed lookup doesn't burn
	// quota on every tick.
	//
	// Snapshot BEFORE resolving: a gate (or airframe/schedule) the resolver
	// introduces this tick must be a real prev→cur delta for the alert step,
	// otherwise the change is folded into prev and never alerted.
	prev := f
	if p.Resolver != nil && (needsBackfill(f) || needsLateRefresh(f, now)) {
		if fresh, err := p.resolveAndUpdate(ctx, f, now); err == nil && fresh != nil {
			f = fresh
		}
	}

	pos, err := p.Tracker.Track(ctx, f, now)
	if err != nil {
		slog.Warn("poller: track failed", "flight", f.Ident, "id", f.ID, "err", err)
	}
	if pos != nil {
		if err := p.Store.InsertPartPosition(ctx, *pos); err != nil {
			slog.Error("poller: insert position", "id", f.ID, "err", err)
		} else if !pos.IsEstimated {
			// A real fix just landed. Dead-reckoned estimates always head for
			// the destination, so a fix that arrives off that line leaves a
			// dog-leg in the flown track. Re-lay the preceding estimates onto a
			// smooth great-circle from the nearest solid anchor (origin or last
			// real fix) to this position — a smoothed path beats the kink.
			if err := p.Store.SmoothEstimatedTrack(ctx, f, *pos); err != nil {
				slog.Error("poller: smooth estimated track", "id", f.ID, "err", err)
			}
		}
	}
	// Always refresh the status from the schedule; preserves Cancelled /
	// Diverted, otherwise derives Scheduled / Enroute / Arrived from times.
	if err := p.Store.RefreshFlightPartStatus(ctx, f.ID); err != nil {
		slog.Error("poller: refresh status", "id", f.ID, "err", err)
	}

	// Flight-alert diff step (spec §9): detect a meaningful status/time change
	// and fan out in-app + email alerts to the recipient set, deduped per part.
	p.maybeAlert(ctx, prev, f.ID)

	p.publishPartChange(ctx, f.ID)
}

// publishPartChange rebuilds the convergence payload for a flight part that
// just refreshed and broadcasts it over the hub, scoped to the part's plan
// visibility set (spec §4). Replaces the old flight.updated broadcast; the
// payload is the locked TrackerPartDTO and the event type is plan_part.updated.
// Shared by refresh and the coord sweep.
func (p *Poller) publishPartChange(ctx context.Context, partID int64) {
	// The poller is a trusted server-side actor and must be able to refetch any
	// active part regardless of viewer — per-recipient visibility is applied
	// below via VisiblePlanUserIDs on the broadcast. So we use the unscoped row
	// fetch, not the viewer-gated TrackerPartByID. The part may have been
	// deleted by a concurrent edit between the status write and here;
	// ErrNotFound is the benign "row gone" case, so we just skip the broadcast
	// (mirrors the old FlightByID refetch guard).
	tp, err := p.Store.TrackerPartRow(ctx, partID)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			slog.Error("poller: refetch part", "id", partID, "err", err)
		}
		return
	}
	latest, _ := p.Store.LatestPartPositions(ctx, []int64{partID})
	dto := api.TrackerPartDTO{
		PlanPartID:   tp.PlanPartID,
		PlanID:       tp.PlanID,
		TripID:       tp.TripID,
		OwnerID:      tp.OwnerID,
		Title:        tp.Title,
		Status:       tp.Status,
		EffectiveAt:  tp.EffectiveAt,
		Ident:        tp.Ident,
		DestIATA:     tp.DestIATA,
		LastPolledAt: tp.LastPolledAt,
	}
	if pos := latest[partID]; pos != nil {
		pd := api.ToPositionDTO(pos)
		dto.LatestPosition = &pd
	}
	// Carry the flown track so the FE polyline grows live with the plane.
	// Without this the track only ever reflects the last full HTTP fetch, so
	// a moving plane trails no path between page loads. Best-effort: a track
	// lookup failure shouldn't suppress the position/status broadcast.
	if tracks, err := p.Store.PartTracks(ctx, []int64{partID}, 0); err != nil {
		slog.Warn("poller: load track for broadcast", "id", partID, "err", err)
	} else if track := tracks[partID]; len(track) > 0 {
		dto.Track = make([]api.PositionDTO, len(track))
		for i, pos := range track {
			dto.Track[i] = api.ToPositionDTO(pos)
		}
	}
	payload, err := json.Marshal(dto)
	if err != nil {
		slog.Error("poller: marshal dto", "err", err)
		return
	}
	visible, err := p.Store.VisiblePlanUserIDs(ctx, tp.PlanID)
	if err != nil {
		slog.Warn("poller: visibility lookup failed", "plan_id", tp.PlanID, "err", err)
	}
	p.Hub.Publish(sse.Event{Type: "plan_part.updated", Data: payload, VisibleTo: visible})
}

// needsBackfill is true when the resolver could meaningfully fill in at least
// one of the metadata fields the rest of the system needs. A degenerate
// schedule (arrival not after departure) counts: it means a flight was added
// with just a number + date and the real times never arrived, so we keep
// asking the resolver until it supplies them — otherwise a flight that already
// has its airframe/airports would never re-resolve to fix the times.
func needsBackfill(f *store.Flight) bool {
	return f.OriginIATA == "" || f.DestIATA == "" || f.ICAO24 == nil ||
		!f.ScheduledIn.After(f.ScheduledOut)
}

// lateRefreshWindow is how close to scheduled departure we start re-asking
// the resolver about the operating airframe. AeroDataBox doesn't reliably
// publish modeS / callSign until ~24h out, but airlines also swap metal
// closer in than that, so the cheap thing is to keep poking from here.
const lateRefreshWindow = 12 * time.Hour

// lateRefreshInterval throttles how often we re-resolve while inside the
// window — covers the "every tick for an enroute flight" case. AeroDataBox
// BASIC tier allows a few hundred calls/day; one call per active flight
// per ~4h is well under that.
const lateRefreshInterval = 4 * time.Hour

// needsLateRefresh is true when the flight is in (or close to) its active
// window and we haven't asked the resolver recently. It complements
// needsBackfill: backfill cares about *which fields are empty*, this
// cares about *how stale the data is*.
func needsLateRefresh(f *store.Flight, now time.Time) bool {
	if now.Before(f.ScheduledOut.Add(-lateRefreshWindow)) {
		return false
	}
	if f.Status == "Arrived" || f.Status == "Cancelled" || f.Status == "Diverted" {
		return false
	}
	if f.LastResolvedAt == nil {
		return true
	}
	return now.Sub(*f.LastResolvedAt) >= lateRefreshInterval
}

// resolveAndUpdate calls the Resolver and persists the result through both
// the empty-fill path (BackfillFlightPart, which protects user-typed values)
// and the day-of overwrite path (RefreshFlightPartAirframe, which catches
// airframe swaps and bumps last_resolved_at). On error or not-found we
// still bump last_resolved_at via an empty Refresh so the next tick
// throttles instead of retrying immediately.
func (p *Poller) resolveAndUpdate(ctx context.Context, f *store.Flight, now time.Time) (*store.Flight, error) {
	rf, err := p.Resolver.Resolve(ctx, f.Ident, f.ScheduledOut)
	if err != nil {
		if !errors.Is(err, providers.ErrFlightNotFound) {
			slog.Warn("poller: resolve failed",
				"ident", f.Ident, "id", f.ID, "err", err)
		}
		if touchErr := p.Store.RefreshFlightPartAirframe(ctx, f.ID, "", ""); touchErr != nil {
			slog.Error("poller: stamp last_resolved_at failed", "id", f.ID, "err", touchErr)
		}
		return nil, err
	}
	if err := p.Store.BackfillFlightPart(ctx, f.ID, store.BackfillPayload{
		OriginIATA: rf.OriginIATA, OriginLat: rf.OriginLat, OriginLon: rf.OriginLon,
		DestIATA: rf.DestIATA, DestLat: rf.DestLat, DestLon: rf.DestLon,
		ICAO24: rf.ICAO24, Callsign: rf.Callsign,
		Notes:          rf.Notes,
		OriginTerminal: rf.OriginTerminal, DestTerminal: rf.DestTerminal,
		AircraftType: rf.AircraftType,
	}); err != nil {
		slog.Error("poller: backfill write failed", "id", f.ID, "err", err)
		return nil, err
	}
	if err := p.Store.RefreshFlightPartAirframe(ctx, f.ID, rf.ICAO24, rf.Callsign); err != nil {
		slog.Error("poller: refresh airframe failed", "id", f.ID, "err", err)
		return nil, err
	}
	// Gate is updatable (a change is what the gate-change alert detects), so it
	// goes through the overwrite-when-non-empty path rather than the
	// only-fill-empty backfill above.
	if err := p.Store.RefreshFlightPartGate(ctx, f.ID, rf.OriginGate, rf.DestGate); err != nil {
		slog.Error("poller: refresh gate failed", "id", f.ID, "err", err)
		return nil, err
	}
	// Arrival baggage belt is updatable like gate (a change is what the
	// belt-change alert detects), so it takes the same overwrite-when-non-empty
	// path rather than the only-fill-empty backfill above.
	if err := p.Store.RefreshFlightPartBelt(ctx, f.ID, rf.DestBaggageBelt); err != nil {
		slog.Error("poller: refresh belt failed", "id", f.ID, "err", err)
		return nil, err
	}
	// Fill the schedule from the resolver when the stored one is degenerate (a
	// manual add with just a number + date leaves scheduled_in == scheduled_out).
	// The store guards on scheduled_in <= scheduled_out, so a real user-entered
	// schedule is never overwritten. Best-effort: a failure here doesn't abort
	// the airframe/gate work already persisted above.
	if !rf.ScheduledOut.IsZero() && rf.ScheduledIn.After(rf.ScheduledOut) {
		if err := p.Store.RefreshFlightPartSchedule(ctx, f.ID, rf.ScheduledOut, rf.ScheduledIn); err != nil {
			slog.Error("poller: refresh schedule failed", "id", f.ID, "err", err)
		}
	}
	slog.Info("poller: resolved",
		"ident", f.Ident, "id", f.ID,
		"origin", rf.OriginIATA, "dest", rf.DestIATA,
		"icao24", rf.ICAO24, "callsign", rf.Callsign)
	return p.Store.FlightPartByID(ctx, f.ID)
}
