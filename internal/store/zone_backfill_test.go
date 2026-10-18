package store

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/testsupport"
)

// seedZoneTrip inserts a synthetic trip with one hotel part (London
// coordinates, floating start and end) and one flight part (XX100, zoned
// start, floating end, dismissed copy alongside). Returns ids.
func seedZoneTrip(t *testing.T, s *Store) (tripID, hotelPart, flightPart, dismissedPart int64) {
	t.Helper()
	ctx := context.Background()
	userID := testsupport.InsertUser(t, s.Pool(), fmt.Sprintf("zbuser-%d", time.Now().UnixNano()), false, true)
	mustScan := func(q string, args ...any) int64 {
		var id int64
		if err := s.Pool().QueryRow(ctx, q, args...).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	tripID = mustScan(`INSERT INTO trips (name, created_by) VALUES ('Test Trip', $1) RETURNING id`, userID)
	hotelPlan := mustScan(`INSERT INTO plans (trip_id, type, created_by) VALUES ($1, 'hotel', $2) RETURNING id`, tripID, userID)
	flightPlan := mustScan(`INSERT INTO plans (trip_id, type, created_by) VALUES ($1, 'flight', $2) RETURNING id`, tripID, userID)
	hotelPart = mustScan(`
		INSERT INTO plan_parts (plan_id, starts_at, ends_at, start_label, status, start_lat, start_lon,
			start_tz, start_tz_source, end_tz, end_tz_source)
		VALUES ($1, '2026-07-01 15:00+00', '2026-07-03 11:00+00', 'Test Hotel', 'confirmed',
			51.5, -0.12, '', 'floating', '', 'floating') RETURNING id`, hotelPlan)
	flightPart = mustScan(`
		INSERT INTO plan_parts (plan_id, starts_at, ends_at, start_label, end_label, status,
			start_tz, start_tz_source, end_tz, end_tz_source)
		VALUES ($1, '2026-07-04 08:00+00', '2026-07-04 10:00+00', 'XXA', 'XXB', 'confirmed',
			'Europe/London', 'confirmed', '', 'floating') RETURNING id`, flightPlan)
	if _, err := s.Pool().Exec(ctx, `
		INSERT INTO flight_details (plan_part_id, ident, scheduled_out, scheduled_in, origin_iata, dest_iata,
			flight_status, resolved)
		VALUES ($1, 'XX100', '2026-07-04 08:00+00', '2026-07-04 10:00+00', 'LHR', 'CDG', 'Scheduled', false)`,
		flightPart); err != nil {
		t.Fatal(err)
	}
	dismissedPart = mustScan(`
		INSERT INTO plan_parts (plan_id, starts_at, ends_at, status, dismissed_at,
			start_tz, start_tz_source, end_tz, end_tz_source)
		VALUES ($1, '2026-07-05 08:00+00', '2026-07-05 10:00+00', 'confirmed', now(),
			'Europe/Paris', 'confirmed', '', 'floating') RETURNING id`, flightPlan)
	return
}

func TestFloatingEnds(t *testing.T) {
	pool := testsupport.NewPool(t)
	if pool == nil {
		return
	}
	s := New(pool)
	ctx := context.Background()
	tripID, hotel, flight, dismissed := seedZoneTrip(t, s)

	ends, err := s.FloatingEnds(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]FloatingEnd{}
	for _, e := range ends {
		if e.TripID == tripID {
			got[fmt.Sprintf("%d/%s", e.PartID, e.End)] = e
		}
		if e.PartID == dismissed {
			t.Errorf("dismissed part returned: %+v", e)
		}
	}
	if len(got) != 3 {
		t.Fatalf("got %d ends: %+v", len(got), got)
	}
	hs := got[fmt.Sprintf("%d/start", hotel)]
	if hs.Lat == nil || *hs.Lat != 51.5 || hs.PlanType != "hotel" || hs.IATA != "" {
		t.Errorf("hotel start = %+v", hs)
	}
	he := got[fmt.Sprintf("%d/end", hotel)] // falls back to the start's coordinates
	if he.Lat == nil || *he.Lat != 51.5 || he.Lon == nil || *he.Lon != -0.12 {
		t.Errorf("hotel end = %+v", he)
	}
	fe := got[fmt.Sprintf("%d/end", flight)]
	if fe.IATA != "CDG" || fe.PlanType != "flight" || fe.Lat != nil {
		t.Errorf("flight end = %+v", fe)
	}
	if _, ok := got[fmt.Sprintf("%d/start", flight)]; ok {
		t.Error("zoned flight start returned")
	}
}

func TestTripZonedEnds(t *testing.T) {
	pool := testsupport.NewPool(t)
	if pool == nil {
		return
	}
	s := New(pool)
	tripID, _, _, _ := seedZoneTrip(t, s)
	z, err := s.TripZonedEnds(context.Background(), tripID)
	if err != nil {
		t.Fatal(err)
	}
	// Only the flight's zoned start counts: the dismissed part is excluded and
	// the hotel and flight ends are floating.
	if len(z) != 1 || z[0].TZ != "Europe/London" || z[0].Dwell != 2*time.Hour {
		t.Fatalf("zoned = %+v", z)
	}
	// An end end carries zero dwell.
	if _, err := s.Pool().Exec(context.Background(),
		`UPDATE plan_parts SET end_tz = 'Europe/Paris', end_tz_source = 'confirmed' WHERE end_tz_source = 'floating' AND dismissed_at IS NULL`); err != nil {
		t.Fatal(err)
	}
	z, err = s.TripZonedEnds(context.Background(), tripID)
	if err != nil {
		t.Fatal(err)
	}
	var zeroDwellEnds int
	for _, e := range z {
		if e.TZ == "Europe/Paris" && e.Dwell == 0 {
			zeroDwellEnds++
		}
	}
	if len(z) != 3 || zeroDwellEnds != 2 {
		t.Fatalf("zoned = %+v", z)
	}
}

func TestApplyZoneFixes(t *testing.T) {
	pool := testsupport.NewPool(t)
	if pool == nil {
		return
	}
	s := New(pool)
	ctx := context.Background()
	_, hotel, flight, _ := seedZoneTrip(t, s)
	read := func(id int64, end string) (string, string, time.Time) {
		var tz, src string
		var at time.Time
		q := `SELECT start_tz, start_tz_source, starts_at FROM plan_parts WHERE id = $1`
		if end == "end" {
			q = `SELECT end_tz, end_tz_source, ends_at FROM plan_parts WHERE id = $1`
		}
		if err := s.Pool().QueryRow(ctx, q, id).Scan(&tz, &src, &at); err != nil {
			t.Fatal(err)
		}
		return tz, src, at
	}
	at1 := time.Date(2026, 7, 1, 14, 0, 0, 0, time.UTC)
	at2 := time.Date(2026, 7, 3, 10, 0, 0, 0, time.UTC)

	t.Run("applies and skips changed sources", func(t *testing.T) {
		// Make the hotel end no longer floating; its fix must be skipped.
		if _, err := s.Pool().Exec(ctx,
			`UPDATE plan_parts SET end_tz = 'Europe/Paris', end_tz_source = 'confirmed' WHERE id = $1`, hotel); err != nil {
			t.Fatal(err)
		}
		err := s.ApplyZoneFixes(ctx, []ZoneFix{
			{PartID: hotel, End: "start", TZ: "Europe/London", Source: "confirmed", At: at1},
			{PartID: hotel, End: "end", TZ: "Europe/London", Source: "confirmed", At: at2},
			{PartID: flight, End: "end", TZ: "Europe/Paris", Source: "", At: at2},
		})
		if err != nil {
			t.Fatal(err)
		}
		if tz, src, at := read(hotel, "start"); tz != "Europe/London" || src != "confirmed" || !at.Equal(at1) {
			t.Errorf("hotel start = %q %q %v", tz, src, at)
		}
		if tz, src, at := read(hotel, "end"); tz != "Europe/Paris" || src != "confirmed" || at.Equal(at2) {
			t.Errorf("hotel end was overwritten: %q %q %v", tz, src, at)
		}
		if tz, src, _ := read(flight, "end"); tz != "Europe/Paris" || src != "confirmed" {
			t.Errorf("flight end = %q %q", tz, src)
		}
	})

	t.Run("a validation error applies nothing", func(t *testing.T) {
		pool2 := testsupport.NewPool(t)
		s2 := New(pool2)
		_, h2, f2, _ := seedZoneTrip(t, s2)
		err := s2.ApplyZoneFixes(ctx, []ZoneFix{
			{PartID: h2, End: "start", TZ: "Europe/London", Source: "confirmed", At: at1},
			{PartID: f2, End: "end", TZ: "Mars/Olympus", Source: "confirmed", At: at2},
		})
		if !errors.Is(err, ErrInvalidZone) {
			t.Fatalf("err = %v", err)
		}
		var src string
		if err := s2.Pool().QueryRow(ctx, `SELECT start_tz_source FROM plan_parts WHERE id = $1`, h2).Scan(&src); err != nil {
			t.Fatal(err)
		}
		if src != "floating" {
			t.Errorf("first fix applied despite the error: %q", src)
		}
	})

	t.Run("unknown end", func(t *testing.T) {
		if err := s.ApplyZoneFixes(ctx, []ZoneFix{{PartID: 1, End: "middle"}}); err == nil {
			t.Fatal("want error")
		}
	})

	t.Run("database error rolls back", func(t *testing.T) {
		cctx, cancel := context.WithCancel(ctx)
		cancel()
		if err := s.ApplyZoneFixes(cctx, []ZoneFix{{PartID: 1, End: "start", TZ: "Europe/London", Source: "confirmed", At: at1}}); err == nil {
			t.Fatal("want error on cancelled context")
		}
	})
}

func TestZoneBackfillQueriesFailOnCancelledContext(t *testing.T) {
	pool := testsupport.NewPool(t)
	if pool == nil {
		return
	}
	s := New(pool)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.FloatingEnds(ctx); err == nil {
		t.Error("FloatingEnds: want error")
	}
	if _, err := s.TripZonedEnds(ctx, 1); err == nil {
		t.Error("TripZonedEnds: want error")
	}
}
