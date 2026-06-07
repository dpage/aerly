// Command backfill-flight-labels is a one-time, re-runnable job that re-resolves
// existing flights to (a) upgrade their bare IATA place labels to the friendly
// "Name (CODE)" form and (b) set flight_details.resolved correctly (see the
// flight-route-labels-and-editing design).
//
// It is quota-aware: AeroDataBox's monthly quota, when exhausted, otherwise
// silently breaks off-table resolution, so the job STOPS cleanly the moment it
// sees a quota error, logs how far it got, and exits 0. Because it only walks
// unresolved flights, simply re-running it later resumes where it stopped.
//
// Usage (reads the same .env / config as the server):
//
//	go run ./cmd/backfill-flight-labels          # resolve every unresolved flight
//	go run ./cmd/backfill-flight-labels -n        # dry run: list what it would do
//	go run ./cmd/backfill-flight-labels -throttle 2s
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"github.com/dpage/aerly/internal/airports"
	"github.com/dpage/aerly/internal/config"
	"github.com/dpage/aerly/internal/db"
	"github.com/dpage/aerly/internal/flightcoord"
	"github.com/dpage/aerly/internal/providers"
	"github.com/dpage/aerly/internal/store"
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
	dryRun := flag.Bool("n", false, "dry run: list flights that would be re-resolved, make no changes")
	throttle := flag.Duration("throttle", 1200*time.Millisecond, "minimum gap between resolver calls")
	flag.Parse()

	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Warn(".env present but failed to parse", "err", err)
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.AeroDataBoxKey == "" {
		return errors.New("AERODATABOX_RAPIDAPI_KEY is required to re-resolve flights")
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
	resolver := providers.NewAeroDataBox(cfg.AeroDataBoxKey)

	rows, err := s.ListUnresolvedFlightParts(ctx)
	if err != nil {
		return err
	}
	slog.Info("backfill: starting", "unresolved_flights", len(rows), "dry_run", *dryRun)

	var resolved, relabelled, failed int
	for i, r := range rows {
		if ctx.Err() != nil {
			slog.Info("backfill: interrupted", "processed", i)
			break
		}
		if *dryRun {
			slog.Info("backfill: would re-resolve", "part", r.PartID, "ident", r.Ident,
				"route", r.OriginIATA+"→"+r.DestIATA)
			continue
		}
		if i > 0 {
			sleep(ctx, *throttle)
		}

		rf, rerr := resolver.Resolve(ctx, r.Ident, r.ScheduledOut)
		if rerr != nil {
			if isQuotaExhausted(rerr) {
				slog.Warn("backfill: monthly quota exhausted — stopping; re-run later to resume",
					"processed", i, "resolved", resolved, "relabelled", relabelled, "err", rerr)
				break
			}
			// Not found / transient: still upgrade table-airport labels for free,
			// leaving the flight unresolved so a future run retries it.
			if tableRelabel(ctx, s, r) {
				relabelled++
			} else {
				failed++
			}
			slog.Info("backfill: not resolved", "part", r.PartID, "ident", r.Ident, "err", rerr)
			continue
		}

		up := flightcoord.RouteUpdateFromResolved(rf)
		if err := s.UpdateFlightPartRoute(ctx, r.PartID, up); err != nil {
			return err
		}
		resolved++
		slog.Info("backfill: resolved", "part", r.PartID, "ident", r.Ident,
			"route", rf.OriginIATA+"→"+rf.DestIATA)
	}

	slog.Info("backfill: done", "resolved", resolved, "relabelled", relabelled, "failed", failed)
	return nil
}

// tableRelabel upgrades a part's bare IATA labels using only the embedded
// airports table (no provider call). Returns whether either leg is on-table (so
// the label actually changes). The resolved flag is left false.
func tableRelabel(ctx context.Context, s *store.Store, r store.FlightPartRow) bool {
	_, originOnTable := airports.LookupCity(r.OriginIATA)
	_, destOnTable := airports.LookupCity(r.DestIATA)
	if !originOnTable && !destOnTable {
		return false
	}
	originLabel := airports.Label(r.OriginIATA, "")
	destLabel := airports.Label(r.DestIATA, "")
	up := store.FlightRouteUpdate{StartLabel: &originLabel, EndLabel: &destLabel}
	if err := s.UpdateFlightPartRoute(ctx, r.PartID, up); err != nil {
		slog.Error("backfill: table relabel", "part", r.PartID, "err", err)
		return false
	}
	return true
}

// isQuotaExhausted reports whether the resolver error is the monthly-quota wall
// (vs a transient per-second throttle, which the resolver already retries). The
// upstream's quota message contains "quota"; per-second throttles do not.
func isQuotaExhausted(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "quota")
}

func sleep(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}
