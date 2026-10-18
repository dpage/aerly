// Command backfill-part-zones is a re-runnable job that gives floating part
// ends (stored as local wall-clock digits with no zone) a zone, and re-anchors
// their instants in it. A zone from the end's own coordinates or airport is
// recorded as confirmed; one borrowed from the nearest zoned end in the trip,
// or from the trip's dominant zone, is recorded as inferred. Anything with no
// evidence is left floating.
//
// It defaults to a dry run so a bare invocation changes nothing:
//
//	go run ./cmd/backfill-part-zones          # list what it would change
//	go run ./cmd/backfill-part-zones -n=false # apply
//
// The output carries only part ids, ends, zones, sources and instants.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"sort"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"github.com/dpage/aerly/internal/config"
	"github.com/dpage/aerly/internal/db"
	"github.com/dpage/aerly/internal/store"
	"github.com/dpage/aerly/internal/timing"
	"github.com/dpage/aerly/migrations"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))
	if err := run(); err != nil {
		slog.Error("backfill failed", "err", err)
		os.Exit(1)
	}
}

func run() error {
	dryRun := flag.Bool("n", true, "dry run: list the changes, make none (use -n=false to apply)")
	flag.Parse()

	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Warn(".env present but failed to parse", "err", err)
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := db.Migrate(ctx, pool, migrations.FS); err != nil {
		return err
	}
	s := store.New(pool)

	return process(ctx, s, *dryRun)
}

// zoneStore is the part of the store the job needs.
type zoneStore interface {
	FloatingEnds(context.Context) ([]store.FloatingEnd, error)
	TripZonedEnds(context.Context, int64) ([]store.ZonedEnd, error)
	ApplyZoneFixes(context.Context, []store.ZoneFix) error
}

// process plans the fixes and, unless dryRun, applies them.
func process(ctx context.Context, s zoneStore, dryRun bool) error {
	ends, err := s.FloatingEnds(ctx)
	if err != nil {
		return err
	}
	slog.Info("backfill: starting", "floating_ends", len(ends), "dry_run", dryRun)

	fixes, rep, err := plan(ends, func(tripID int64) ([]store.ZonedEnd, error) {
		return s.TripZonedEnds(ctx, tripID)
	})
	if err != nil {
		return err
	}
	for i, f := range fixes {
		slog.Info("backfill: fix", "part", f.PartID, "end", f.End, "zone", f.TZ,
			"source", f.Source, "old", rep.Old[i].Format(time.RFC3339),
			"new", f.At.Format(time.RFC3339))
	}
	if !dryRun {
		if err := s.ApplyZoneFixes(ctx, fixes); err != nil {
			return err
		}
	}
	slog.Info("backfill: done", "confirmed", rep.Confirmed, "inferred", rep.Inferred,
		"left floating", rep.Floating, "dry_run", dryRun)
	return nil
}

// report totals the outcomes of a plan. Old holds each fix's instant before
// the change, in the same order as the fixes.
type report struct {
	Confirmed, Inferred, Floating int
	Old                           []time.Time
}

// plan decides a fix for every floating end it can. It is pure apart from the
// trip callback, which supplies a trip's already-zoned ends (called once per
// trip).
func plan(ends []store.FloatingEnd, trip func(int64) ([]store.ZonedEnd, error)) ([]store.ZoneFix, report, error) {
	var fixes []store.ZoneFix
	var rep report
	cache := map[int64][]store.ZonedEnd{}
	for _, e := range ends {
		zoned, ok := cache[e.TripID]
		if !ok {
			var err error
			if zoned, err = trip(e.TripID); err != nil {
				return nil, report{}, err
			}
			cache[e.TripID] = zoned
		}
		tz, src := timing.Infer(timing.Hints{
			Lat: e.Lat, Lon: e.Lon, IATA: e.IATA,
			NeighbourTZ: nearest(zoned, e.At), TripTZ: dominant(zoned),
		})
		at, ok := timing.Anchor(e.At, tz)
		if tz == "" || !ok {
			rep.Floating++
			continue
		}
		if src == timing.Confirmed {
			rep.Confirmed++
		} else {
			rep.Inferred++
		}
		fixes = append(fixes, store.ZoneFix{PartID: e.PartID, End: e.End, TZ: tz, Source: string(src), At: at})
		rep.Old = append(rep.Old, e.At)
	}
	return fixes, rep, nil
}

// nearest is the zone of the end closest in time to at; ties go to the
// earlier end. at is wall-clock digits, so the distance is off by at most a
// zone offset, which is fine for choosing a neighbour.
func nearest(zoned []store.ZonedEnd, at time.Time) string {
	best, bestD := "", time.Duration(0)
	var bestAt time.Time
	for _, z := range zoned {
		d := z.At.Sub(at)
		if d < 0 {
			d = -d
		}
		if best == "" || d < bestD || (d == bestD && z.At.Before(bestAt)) {
			best, bestD, bestAt = z.TZ, d, z.At
		}
	}
	return best
}

// dominant is the zone with the largest summed dwell; ties go to the
// alphabetically first zone so the result is deterministic.
func dominant(zoned []store.ZonedEnd) string {
	total := map[string]time.Duration{}
	for _, z := range zoned {
		total[z.TZ] += z.Dwell
	}
	zones := make([]string, 0, len(total))
	for tz := range total {
		zones = append(zones, tz)
	}
	sort.Strings(zones)
	best := ""
	for _, tz := range zones {
		if best == "" || total[tz] > total[best] {
			best = tz
		}
	}
	return best
}
