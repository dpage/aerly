package flightcoord

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/providers"
	"github.com/dpage/aerly/internal/store"
)

// fakeBackfiller records the BackfillFlightPart payload and counts airframe
// (throttle) bumps, standing in for *store.Store without a database.
type fakeBackfiller struct {
	backfilled *store.BackfillPayload
	airframe   int
	resolved   int
	startLabel string
	endLabel   string
}

func (f *fakeBackfiller) BackfillFlightPart(_ context.Context, _ int64, in store.BackfillPayload) error {
	cp := in
	f.backfilled = &cp
	return nil
}

func (f *fakeBackfiller) RefreshFlightPartAirframe(_ context.Context, _ int64, _, _ string) error {
	f.airframe++
	return nil
}

func (f *fakeBackfiller) MarkFlightPartResolved(_ context.Context, _ int64, _, originLabel, _, destLabel string) error {
	f.resolved++
	f.startLabel, f.endLabel = originLabel, destLabel
	return nil
}

// fakeResolver returns a fixed result (or error) and counts calls.
type fakeResolver struct {
	rf    *providers.ResolvedFlight
	err   error
	calls int
}

func (r *fakeResolver) Resolve(_ context.Context, _ string, _ time.Time) (*providers.ResolvedFlight, error) {
	r.calls++
	if r.err != nil {
		return nil, r.err
	}
	c := *r.rf
	return &c, nil
}

// fakeAirportResolver returns a fixed airport per IATA code (or an error) and
// counts calls, standing in for the date-free airport lookup.
type fakeAirportResolver struct {
	byCode map[string]*providers.Airport
	err    error
	calls  int
}

func (r *fakeAirportResolver) ResolveAirport(_ context.Context, iata string) (*providers.Airport, error) {
	r.calls++
	if r.err != nil {
		return nil, r.err
	}
	ap, ok := r.byCode[iata]
	if !ok {
		return nil, providers.ErrAirportNotFound
	}
	c := *ap
	return &c, nil
}

func ptr(f float64) *float64 { return &f }

// BRS (Bristol) is in the embedded airports table; ZZZ is not.
func TestFill_TableFillsKnownLeg_NoResolverNeeded(t *testing.T) {
	st := &fakeBackfiller{}
	r := &fakeResolver{} // present, but should stay untouched
	f := &store.Flight{ID: 1, Ident: "EZY1", OriginIATA: "BRS", DestIATA: "BRS"}

	changed, err := Fill(context.Background(), st, r, nil, f, time.Now())
	if err != nil {
		t.Fatalf("Fill: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true (both legs table-fillable)")
	}
	if r.calls != 0 {
		t.Errorf("resolver should not be called when the table satisfies every leg; calls=%d", r.calls)
	}
	if st.backfilled == nil || st.backfilled.OriginLat == 0 {
		t.Errorf("expected table coords in backfill payload, got %+v", st.backfilled)
	}
}

func TestFill_ResolverFillsOffTableLeg(t *testing.T) {
	st := &fakeBackfiller{}
	r := &fakeResolver{rf: &providers.ResolvedFlight{
		OriginIATA: "BRS", DestIATA: "ZZZ", DestLat: 12.34, DestLon: -56.78,
		ICAO24: "abc123",
	}}
	f := &store.Flight{ID: 2, Ident: "FR9226", OriginIATA: "BRS", DestIATA: "ZZZ"}

	changed, err := Fill(context.Background(), st, r, nil, f, time.Now())
	if err != nil {
		t.Fatalf("Fill: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true")
	}
	if r.calls != 1 {
		t.Errorf("resolver calls=%d, want 1", r.calls)
	}
	if st.airframe != 1 {
		t.Errorf("expected last_resolved_at bump (airframe refresh), got %d", st.airframe)
	}
	if st.backfilled == nil || st.backfilled.DestLat != 12.34 {
		t.Errorf("expected resolver dest coords, got %+v", st.backfilled)
	}
	// A successful resolve flips resolved and upgrades the labels: the table leg
	// (BRS) gets its airport name, the off-table leg with no provider name falls
	// back to the bare code.
	if st.resolved != 1 {
		t.Errorf("expected MarkFlightPartResolved called once, got %d", st.resolved)
	}
	if !strings.Contains(st.startLabel, "(BRS)") {
		t.Errorf("origin label should be friendly Name (BRS), got %q", st.startLabel)
	}
	if st.endLabel != "ZZZ" {
		t.Errorf("off-table dest with no provider name should be bare code, got %q", st.endLabel)
	}
}

func TestFill_ResolveErrorDoesNotMarkResolved(t *testing.T) {
	st := &fakeBackfiller{}
	r := &fakeResolver{err: errors.New("not found")}
	f := &store.Flight{ID: 7, Ident: "FR9226", OriginIATA: "BRS", OriginLat: ptr(51.0), DestIATA: "ZZZ"}
	if _, err := Fill(context.Background(), st, r, nil, f, time.Now()); err != nil {
		t.Fatalf("Fill: %v", err)
	}
	if st.resolved != 0 {
		t.Errorf("a failed resolve must not mark the flight resolved, got %d", st.resolved)
	}
}

// A leg the table already satisfied must keep its table coords even when the
// resolver returns a (wrong) value for it.
func TestFill_DoesNotClobberTableLegWithResolver(t *testing.T) {
	st := &fakeBackfiller{}
	r := &fakeResolver{rf: &providers.ResolvedFlight{
		OriginIATA: "BRS", OriginLat: 99.0, OriginLon: 99.0, // wrong on purpose
		DestIATA: "ZZZ", DestLat: 12.34, DestLon: -56.78,
	}}
	// Origin BRS is table-known; only dest ZZZ needs the resolver.
	f := &store.Flight{ID: 3, Ident: "FR9226", OriginIATA: "BRS", DestIATA: "ZZZ"}

	if _, err := Fill(context.Background(), st, r, nil, f, time.Now()); err != nil {
		t.Fatalf("Fill: %v", err)
	}
	if st.backfilled == nil {
		t.Fatal("expected a backfill payload")
	}
	if st.backfilled.OriginLat == 99.0 {
		t.Errorf("origin should keep the BRS table value, not the resolver's 99.0")
	}
	if st.backfilled.DestLat != 12.34 {
		t.Errorf("dest should take the resolver value, got %v", st.backfilled.DestLat)
	}
}

func TestFill_ThrottleBlocksResolver(t *testing.T) {
	st := &fakeBackfiller{}
	r := &fakeResolver{rf: &providers.ResolvedFlight{DestIATA: "ZZZ", DestLat: 1, DestLon: 2}}
	recent := time.Now()
	// Origin already has coords; only the off-table dest needs resolving, but
	// the row was resolved just now so the throttle blocks the lookup.
	f := &store.Flight{ID: 4, Ident: "FR9226", OriginIATA: "BRS", OriginLat: ptr(51.0), DestIATA: "ZZZ", LastResolvedAt: &recent}

	changed, err := Fill(context.Background(), st, r, nil, f, recent.Add(time.Minute))
	if err != nil {
		t.Fatalf("Fill: %v", err)
	}
	if r.calls != 0 {
		t.Errorf("throttled row should not call the resolver; calls=%d", r.calls)
	}
	if changed || st.backfilled != nil {
		t.Errorf("nothing should change when the only missing leg is throttled")
	}
}

func TestFill_NoResolverConfiguredOffTableLegStaysNull(t *testing.T) {
	st := &fakeBackfiller{}
	f := &store.Flight{ID: 5, Ident: "FR9226", OriginIATA: "ZZZ", DestIATA: "QQQ"}

	changed, err := Fill(context.Background(), st, nil, nil, f, time.Now())
	if err != nil {
		t.Fatalf("Fill: %v", err)
	}
	if changed || st.backfilled != nil {
		t.Errorf("no resolver + off-table legs: nothing to write, got changed=%v", changed)
	}
}

func TestFill_ResolveErrorBumpsThrottleButLeavesCoords(t *testing.T) {
	st := &fakeBackfiller{}
	r := &fakeResolver{err: errors.New("not found")}
	// Origin already filled; only the off-table dest is left for the resolver,
	// which fails — so no coords change, but the throttle is still bumped.
	f := &store.Flight{ID: 6, Ident: "FR9226", OriginIATA: "BRS", OriginLat: ptr(51.0), DestIATA: "ZZZ"}

	changed, err := Fill(context.Background(), st, r, nil, f, time.Now())
	if err != nil {
		t.Fatalf("Fill: %v", err)
	}
	if changed || st.backfilled != nil {
		t.Errorf("resolve error: dest should stay NULL, got changed=%v", changed)
	}
	if st.airframe != 1 {
		t.Errorf("throttle should still be bumped on resolve error, got %d", st.airframe)
	}
	if r.calls != 1 {
		t.Errorf("resolver calls=%d, want 1", r.calls)
	}
}

// An imported flight whose date is outside the provider's ±180-day window
// (the flight lookup returns ErrFlightNotFound) still gets its off-table
// airport plotted via the date-free airport lookup.
func TestFill_AirportFallbackResolvesOutOfWindowOffTableLeg(t *testing.T) {
	st := &fakeBackfiller{}
	// Wrap the sentinel like the real resolver does, so the fallback's
	// errors.Is check is exercised rather than direct equality.
	r := &fakeResolver{err: fmt.Errorf("outside provider window: %w", providers.ErrFlightNotFound)}
	ar := &fakeAirportResolver{byCode: map[string]*providers.Airport{
		"ZZZ": {IATA: "ZZZ", Name: "Off-table Airport", Lat: 50.345, Lon: 30.8947},
	}}
	// Origin BRS is table-known; dest ZZZ is off-table and the flight itself is
	// unresolvable (old import) — so only the airport lookup can fill it.
	f := &store.Flight{ID: 10, Ident: "PS786", OriginIATA: "BRS", DestIATA: "ZZZ"}

	changed, err := Fill(context.Background(), st, r, ar, f, time.Now())
	if err != nil {
		t.Fatalf("Fill: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true (airport fallback fills ZZZ)")
	}
	if ar.calls != 1 {
		t.Errorf("airport resolver calls=%d, want 1 (dest only)", ar.calls)
	}
	if st.backfilled == nil || st.backfilled.DestLat != 50.345 || st.backfilled.DestLon != 30.8947 {
		t.Errorf("expected ZZZ coords from airport lookup, got %+v", st.backfilled)
	}
	// The airport name upgrades the bare label, and the part is marked resolved.
	if st.resolved != 1 {
		t.Errorf("expected MarkFlightPartResolved called once, got %d", st.resolved)
	}
	if st.endLabel != "Off-table Airport (ZZZ)" {
		t.Errorf("dest label should use the airport name, got %q", st.endLabel)
	}
	if st.airframe != 1 {
		t.Errorf("throttle should be bumped, got %d", st.airframe)
	}
}

// The airport fallback must NOT fire when the flight lookup fails with a
// transient (non-not-found) error — the airport endpoint would likely hit the
// same throttle/outage, so we save the call for the next sweep.
func TestFill_AirportFallbackSkippedOnTransientFlightError(t *testing.T) {
	st := &fakeBackfiller{}
	r := &fakeResolver{err: errors.New("aerodatabox rate limit")}
	ar := &fakeAirportResolver{byCode: map[string]*providers.Airport{
		"ZZZ": {IATA: "ZZZ", Name: "Off-table Airport", Lat: 50.345, Lon: 30.8947},
	}}
	f := &store.Flight{ID: 11, Ident: "PS786", OriginIATA: "BRS", OriginLat: ptr(51.0), DestIATA: "ZZZ"}

	changed, err := Fill(context.Background(), st, r, ar, f, time.Now())
	if err != nil {
		t.Fatalf("Fill: %v", err)
	}
	if ar.calls != 0 {
		t.Errorf("airport resolver should be skipped on a transient flight error; calls=%d", ar.calls)
	}
	if changed || st.backfilled != nil {
		t.Errorf("nothing should be filled, got changed=%v", changed)
	}
}

// When the flight lookup succeeds and fills the off-table leg itself, the
// airport fallback is unnecessary and must not be called.
func TestFill_AirportFallbackNotCalledWhenFlightFillsLeg(t *testing.T) {
	st := &fakeBackfiller{}
	r := &fakeResolver{rf: &providers.ResolvedFlight{
		OriginIATA: "BRS", DestIATA: "ZZZ", DestLat: 50.345, DestLon: 30.8947,
		DestName: "Off-table Airport",
	}}
	ar := &fakeAirportResolver{byCode: map[string]*providers.Airport{}}
	f := &store.Flight{ID: 12, Ident: "PS786", OriginIATA: "BRS", DestIATA: "ZZZ"}

	if _, err := Fill(context.Background(), st, r, ar, f, time.Now()); err != nil {
		t.Fatalf("Fill: %v", err)
	}
	if ar.calls != 0 {
		t.Errorf("airport resolver should not be called when the flight lookup fills the leg; calls=%d", ar.calls)
	}
	if st.backfilled == nil || st.backfilled.DestLat != 50.345 {
		t.Errorf("expected flight-lookup dest coords, got %+v", st.backfilled)
	}
}

// With no flight resolver configured at all, the airport lookup alone still
// backfills an off-table leg.
func TestFill_AirportFallbackWithoutFlightResolver(t *testing.T) {
	st := &fakeBackfiller{}
	ar := &fakeAirportResolver{byCode: map[string]*providers.Airport{
		"ZZZ": {IATA: "ZZZ", Name: "Off-table Airport", Lat: 50.345, Lon: 30.8947},
	}}
	f := &store.Flight{ID: 13, Ident: "PS786", OriginIATA: "BRS", DestIATA: "ZZZ"}

	changed, err := Fill(context.Background(), st, nil, ar, f, time.Now())
	if err != nil {
		t.Fatalf("Fill: %v", err)
	}
	if !changed || st.backfilled == nil || st.backfilled.DestLat != 50.345 {
		t.Errorf("airport lookup should fill ZZZ with no flight resolver, got %+v", st.backfilled)
	}
	if ar.calls != 1 {
		t.Errorf("airport resolver calls=%d, want 1", ar.calls)
	}
}
