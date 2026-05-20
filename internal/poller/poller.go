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

	"github.com/dpage/flight-tracker/internal/api"
	"github.com/dpage/flight-tracker/internal/providers"
	"github.com/dpage/flight-tracker/internal/sse"
	"github.com/dpage/flight-tracker/internal/store"
)

type Poller struct {
	Store    *store.Store
	Tracker  providers.Tracker
	Resolver providers.Resolver // optional; when set, backfills missing metadata
	Hub      *sse.Hub
	Interval time.Duration
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

	// Tick immediately on startup so a fresh server doesn't look stale.
	p.tick(ctx)

	t := time.NewTicker(p.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			p.tick(ctx)
		}
	}
}

// minPollAge returns how long to wait between polls for a given flight.
// Enroute flights are polled at the base interval; all other active statuses
// (Scheduled, etc.) are polled at 5× the base interval since they change
// infrequently before departure.
func (p *Poller) minPollAge(status string) time.Duration {
	if status == "Enroute" {
		return p.Interval
	}
	return p.Interval * 5
}

func (p *Poller) tick(ctx context.Context) {
	now := time.Now()
	flights, err := p.Store.ActiveFlights(ctx, now)
	if err != nil {
		slog.Error("poller: list active flights", "err", err)
		return
	}
	for _, f := range flights {
		if ctx.Err() != nil {
			return
		}
		if f.LastPolledAt != nil && now.Sub(*f.LastPolledAt) < p.minPollAge(f.Status) {
			continue
		}
		p.refresh(ctx, f, now)
	}
}

func (p *Poller) refresh(ctx context.Context, f *store.Flight, now time.Time) {
	// Opportunistic metadata backfill: when a Resolver is configured and the
	// flight is missing airport / airframe data (typically because it was
	// added manually with the full form), try to fetch it once. Only the
	// empty fields are written, so user-typed values are never clobbered.
	// The resolver's cache short-circuits subsequent attempts for a known-
	// missing flight, so this can't burn quota on repeated polls.
	if p.Resolver != nil && needsBackfill(f) {
		if fresh, err := p.backfillMetadata(ctx, f); err == nil && fresh != nil {
			f = fresh
		}
	}

	pos, err := p.Tracker.Track(ctx, f, now)
	if err != nil {
		slog.Warn("poller: track failed", "flight", f.Ident, "id", f.ID, "err", err)
	}
	if pos != nil {
		if err := p.Store.InsertPosition(ctx, *pos); err != nil {
			slog.Error("poller: insert position", "id", f.ID, "err", err)
		}
	}
	// Always refresh the status from the schedule; preserves Cancelled /
	// Diverted, otherwise derives Scheduled / Enroute / Arrived from times.
	if err := p.Store.RefreshFlightStatus(ctx, f.ID); err != nil {
		slog.Error("poller: refresh status", "id", f.ID, "err", err)
	}

	fresh, err := p.Store.FlightByID(ctx, f.ID)
	if err != nil {
		slog.Error("poller: refetch flight", "id", f.ID, "err", err)
		return
	}
	pmap, _ := p.Store.PassengersByFlight(ctx, []int64{f.ID})
	smap, _ := p.Store.SharedUserIDsByFlight(ctx, []int64{f.ID})
	latest, _ := p.Store.LatestPositions(ctx, []int64{f.ID})
	tracks, _ := p.Store.RecentTracks(ctx, []int64{f.ID}, 200)
	dto := api.ToFlightDTO(fresh, pmap[f.ID], smap[f.ID], latest[f.ID], tracks[f.ID])
	payload, err := json.Marshal(dto)
	if err != nil {
		slog.Error("poller: marshal dto", "err", err)
		return
	}
	// Scope the broadcast to viewers who can see this flight. Public
	// flights publish with empty VisibleTo (the hub's broadcast path).
	var visible []int64
	if !fresh.IsPublic {
		visible, err = p.Store.VisibleUserIDs(ctx, fresh.ID)
		if err != nil {
			slog.Warn("poller: visibility lookup failed", "id", fresh.ID, "err", err)
		}
	}
	p.Hub.Publish(sse.Event{Type: "flight.updated", Data: payload, VisibleTo: visible})
}

// needsBackfill is true when the resolver could meaningfully fill in at
// least one of the metadata fields that the rest of the system needs.
func needsBackfill(f *store.Flight) bool {
	return f.OriginIATA == "" || f.DestIATA == "" || f.ICAO24 == nil
}

// backfillMetadata calls the Resolver for the flight's ident + departure
// date and writes whichever fields are currently empty. Returns the
// refetched Flight on success so the caller can continue with up-to-date
// state, or nil + error otherwise.
func (p *Poller) backfillMetadata(ctx context.Context, f *store.Flight) (*store.Flight, error) {
	rf, err := p.Resolver.Resolve(ctx, f.Ident, f.ScheduledOut)
	if err != nil {
		if !errors.Is(err, providers.ErrFlightNotFound) {
			slog.Warn("poller: backfill resolve failed",
				"ident", f.Ident, "id", f.ID, "err", err)
		}
		return nil, err
	}
	if err := p.Store.BackfillFlight(ctx, f.ID, store.BackfillPayload{
		OriginIATA: rf.OriginIATA, OriginLat: rf.OriginLat, OriginLon: rf.OriginLon,
		DestIATA: rf.DestIATA, DestLat: rf.DestLat, DestLon: rf.DestLon,
		ICAO24: rf.ICAO24,
		Notes:  rf.Notes,
	}); err != nil {
		slog.Error("poller: backfill write failed", "id", f.ID, "err", err)
		return nil, err
	}
	slog.Info("poller: backfilled metadata",
		"ident", f.Ident, "id", f.ID,
		"origin", rf.OriginIATA, "dest", rf.DestIATA, "icao24", rf.ICAO24)
	return p.Store.FlightByID(ctx, f.ID)
}
