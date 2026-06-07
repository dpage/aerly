package flightcoord

import (
	"context"
	"errors"
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

func ptr(f float64) *float64 { return &f }

// BRS (Bristol) is in the embedded airports table; ZZZ is not.
func TestFill_TableFillsKnownLeg_NoResolverNeeded(t *testing.T) {
	st := &fakeBackfiller{}
	r := &fakeResolver{} // present, but should stay untouched
	f := &store.Flight{ID: 1, Ident: "EZY1", OriginIATA: "BRS", DestIATA: "BRS"}

	changed, err := Fill(context.Background(), st, r, f, time.Now())
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

	changed, err := Fill(context.Background(), st, r, f, time.Now())
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
	if _, err := Fill(context.Background(), st, r, f, time.Now()); err != nil {
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

	if _, err := Fill(context.Background(), st, r, f, time.Now()); err != nil {
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

	changed, err := Fill(context.Background(), st, r, f, recent.Add(time.Minute))
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

	changed, err := Fill(context.Background(), st, nil, f, time.Now())
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

	changed, err := Fill(context.Background(), st, r, f, time.Now())
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
