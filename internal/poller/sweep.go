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
const sweepInterval = flightcoord.Throttle

// Sweep finds every flight with at least one NULL coord column and tries
// to fill the missing legs — first from the embedded airports table
// (free, in-memory), then from the configured Resolver if anything is
// still missing and the row's last_resolved_at throttle allows it. Rows
// whose coords actually changed get republished over SSE so connected
// clients update without a reload.
//
// Per-row failures are logged and isolated; one bad row never aborts
// the sweep.
func (p *Poller) Sweep(ctx context.Context) {
	flights, err := p.Store.FlightPartsWithMissingCoords(ctx)
	if err != nil {
		slog.Error("sweep: list flight parts with missing coords", "err", err)
		return
	}
	if len(flights) == 0 {
		return
	}
	slog.Info("sweep: starting", "candidate_rows", len(flights))
	now := time.Now()
	for _, f := range flights {
		if ctx.Err() != nil {
			return
		}
		p.sweepOne(ctx, f, now)
	}
}

// sweepOne runs the table + resolver passes for a single flight row.
// Extracted so a failure on one row doesn't unwind the whole loop.
// The now parameter feeds the resolver throttle: a recent
// last_resolved_at suppresses repeat API calls for rows that the
// resolver couldn't satisfy last time. The fill logic is shared with the
// post-ingest backfill in flightcoord.Fill.
func (p *Poller) sweepOne(ctx context.Context, f *store.Flight, now time.Time) {
	changed, err := flightcoord.Fill(ctx, p.Store, p.Resolver, f, now)
	if err != nil {
		slog.Error("sweep: backfill", "id", f.ID, "err", err)
		return
	}
	if changed {
		p.publishPartChange(ctx, f.ID)
	}
}
