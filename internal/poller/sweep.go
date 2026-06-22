package poller

import (
	"context"
	"log/slog"
	"time"

	"github.com/dpage/aerly/internal/flightcoord"
	"github.com/dpage/aerly/internal/store"
)

// sweepInterval is the cadence of the periodic NULL-coord sweep. The
// embedded airports table only changes between deploys, so a longer
// interval would also be defensible; 4h is a compromise that also lets
// the resolver step inside the sweep eventually self-heal far-future
// flights once their schedule is published, without burning more than
// ~6 API calls per stuck row per day. It mirrors flightcoord.Throttle so
// a row resolved by one sweep tick isn't retried by the next.
//
// It is a package var (not a const) only so the Run-loop test can shorten it to
// drive the sweep ticker branch deterministically; production never reassigns
// it.
var sweepInterval = flightcoord.Throttle

// Sweep finds every flight with at least one NULL coord column and tries
// to fill the missing legs — first from the embedded airports table
// (free, in-memory), then from the configured Resolver if anything is
// still missing and the row's last_resolved_at throttle allows it. Rows
// whose coords actually changed get republished over SSE so connected
// clients update without a reload.
//
// A second pass (sweepProvisional) re-resolves live, unconfirmed flights so
// the airline's published schedule replaces a provisional one as soon as it
// appears; this handles on-table flights the coord pass never visits (an
// off-table flight the coord pass resolves is marked resolved=true by
// flightcoord.Fill and so is excluded from the provisional pass).
//
// The provisional pass is deferred so it runs on every exit path — the normal
// path, the FlightPartsWithMissingCoords error path, and a mid-loop context
// cancellation — without a duplicated call site.
//
// Per-row failures are logged and isolated; one bad row never aborts
// the sweep.
func (p *Poller) Sweep(ctx context.Context) {
	now := time.Now()
	defer p.sweepProvisional(ctx, now)

	flights, err := p.Store.FlightPartsWithMissingCoords(ctx)
	if err != nil {
		slog.Error("sweep: list flight parts with missing coords", "err", err)
		return
	}
	if len(flights) > 0 {
		slog.Info("sweep: starting", "candidate_rows", len(flights))
		for _, f := range flights {
			if ctx.Err() != nil {
				return
			}
			p.sweepOne(ctx, f, now)
		}
	}
}

// sweepOne runs the table + resolver passes for a single flight row.
// Extracted so a failure on one row doesn't unwind the whole loop.
// The now parameter feeds the resolver throttle: a recent
// last_resolved_at suppresses repeat API calls for rows that the
// resolver couldn't satisfy last time. The fill logic is shared with the
// post-ingest backfill in flightcoord.Fill.
func (p *Poller) sweepOne(ctx context.Context, f *store.Flight, now time.Time) {
	changed, err := flightcoord.Fill(ctx, p.Store, p.Resolver, p.AirportResolver, f, now)
	if err != nil {
		slog.Error("sweep: backfill", "id", f.ID, "err", err)
		return
	}
	if changed {
		p.publishPartChange(ctx, f.ID)
	}
}

// provisionalRefreshIntervalFor paces re-resolution of not-yet-confirmed flights
// by time-to-departure, so a far-future flight the airline hasn't scheduled yet
// costs about one resolver call per week rather than one per sweep. Inside 12h
// the near-departure metadata pass takes over, so this only governs the >12h
// range.
func provisionalRefreshIntervalFor(f *store.Flight, now time.Time) time.Duration {
	if f.ScheduledOut.Sub(now) <= 30*24*time.Hour {
		return 24 * time.Hour
	}
	return 7 * 24 * time.Hour
}

// sweepProvisional re-resolves live, unconfirmed flights so the airline's
// published schedule replaces a provisional (email/manual) one as soon as it
// appears — the gap the coord sweep misses for on-table flights. It hands off to
// the metadata pass inside 12h and throttles by time-to-departure to protect
// resolver quota. A successful resolve confirms the flight (resolved=true) and
// freezes its schedule via resolveAndUpdate.
func (p *Poller) sweepProvisional(ctx context.Context, now time.Time) {
	// Bail quietly when there's no resolver, or when the context is already
	// cancelled (a shutdown mid-sweep reaches here via Sweep's defer — don't log
	// a spurious query error for it).
	if p.Resolver == nil || ctx.Err() != nil {
		return
	}
	parts, err := p.Store.ProvisionalFlightParts(ctx)
	if err != nil {
		slog.Error("sweep: list provisional flight parts", "err", err)
		return
	}
	for _, f := range parts {
		if ctx.Err() != nil {
			return
		}
		// Inside 12h the metadata pass owns re-resolution at its ramping cadence.
		if !now.Before(f.ScheduledOut.Add(-lateRefreshWindow)) {
			continue
		}
		// Tiered throttle on last_resolved_at (resolveAndUpdate bumps it on every
		// attempt, success or miss).
		if f.LastResolvedAt != nil && now.Sub(*f.LastResolvedAt) < provisionalRefreshIntervalFor(f, now) {
			continue
		}
		guard("poller.sweepProvisional", f.ID, func() {
			if _, rerr := p.resolveAndUpdate(ctx, f, now); rerr != nil {
				return // miss/transport error already stamped last_resolved_at
			}
			p.publishPartChange(ctx, f.ID)
		})
	}
}
