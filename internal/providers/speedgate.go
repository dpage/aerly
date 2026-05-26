package providers

import (
	"context"
	"log/slog"
	"time"

	"github.com/dpage/aerly/internal/geo"
	"github.com/dpage/aerly/internal/store"
)

// SpeedGate wraps an inner Tracker and drops a real fix whose implied
// ground speed from the most recent stored position exceeds MaxSpeedKt.
// It's defence against icao24 collisions and stale-airframe lookups in
// upstream providers like OpenSky, where a query for one flight's
// airframe occasionally returns a state vector for a completely
// different aircraft — producing a giant "teleport" in the rendered
// polyline.
//
// Rejected fixes are returned as (nil, nil) so a downstream wrapper
// (typically [DeadReckoner]) can fall back to extrapolation. Estimated
// fixes from the inner tracker pass through untouched — they're
// hypotheses, not measurements, and gating them would defeat the
// fallback the dead-reckoner just produced.
type SpeedGate struct {
	Inner      Tracker
	Anchor     LatestPositionFetcher
	MaxSpeedKt float64
}

// LatestPositionFetcher returns the most recent position recorded for a
// flight, regardless of whether it was real or dead-reckoned. The
// dead-reckoner's anchor stays roughly on the planned route during
// coverage gaps, so even an all-estimated history gives the gate a
// usable comparison point. [store.Store.LatestPosition] satisfies it.
type LatestPositionFetcher interface {
	LatestPosition(ctx context.Context, flightID int64) (*store.Position, error)
}

// NewSpeedGate returns a SpeedGate with a default threshold of 1000 kt —
// comfortably above the ground speed of any commercial airliner even
// with a strong jet-stream tailwind (~700-750 kt is the realistic
// upper bound), and orders of magnitude below the implied speed of an
// icao24-collision teleport.
func NewSpeedGate(inner Tracker, anchor LatestPositionFetcher) *SpeedGate {
	return &SpeedGate{
		Inner:      inner,
		Anchor:     anchor,
		MaxSpeedKt: 1000,
	}
}

func (g *SpeedGate) Track(ctx context.Context, f *store.Flight, now time.Time) (*store.Position, error) {
	pos, err := g.Inner.Track(ctx, f, now)
	if err != nil || pos == nil || pos.IsEstimated {
		return pos, err
	}
	if g.Anchor == nil {
		return pos, nil
	}
	anchor, fetchErr := g.Anchor.LatestPosition(ctx, f.ID)
	if fetchErr != nil || anchor == nil {
		return pos, nil
	}
	dt := pos.Ts.Sub(anchor.Ts).Hours()
	if dt <= 0 {
		return pos, nil
	}
	distNM := geo.HaversineNM(anchor.Lat, anchor.Lon, pos.Lat, pos.Lon)
	impliedKt := distNM / dt
	if impliedKt > g.MaxSpeedKt {
		slog.Warn("speedgate: implausible fix dropped",
			"flight_id", f.ID, "ident", f.Ident,
			"from_lat", anchor.Lat, "from_lon", anchor.Lon,
			"to_lat", pos.Lat, "to_lon", pos.Lon,
			"dt_hours", dt, "implied_kt", impliedKt,
			"max_kt", g.MaxSpeedKt)
		return nil, nil //nolint:nilnil // intentional: let DeadReckoner fall back
	}
	return pos, nil
}
