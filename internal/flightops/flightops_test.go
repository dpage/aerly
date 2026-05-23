package flightops_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/flightops"
	"github.com/dpage/aerly/internal/providers"
	"github.com/dpage/aerly/internal/store"
	"github.com/dpage/aerly/internal/testsupport"
)

type fakeResolver struct {
	out *providers.ResolvedFlight
	err error
}

func (f *fakeResolver) Resolve(ctx context.Context, ident string, date time.Time) (*providers.ResolvedFlight, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.out, nil
}

func ctxAndStore(t *testing.T) (context.Context, *store.Store, int64) {
	t.Helper()
	pool := testsupport.NewPool(t)
	s := store.New(pool)
	uid := testsupport.InsertUser(t, pool, "alice", false, true)
	return context.Background(), s, uid
}

func TestCreate_ResolvesAndCreates(t *testing.T) {
	ctx, s, uid := ctxAndStore(t)
	r := &fakeResolver{out: &providers.ResolvedFlight{
		Ident:        "TK1980",
		ScheduledOut: time.Date(2026, 6, 12, 9, 30, 0, 0, time.UTC),
		ScheduledIn:  time.Date(2026, 6, 12, 13, 0, 0, 0, time.UTC),
		OriginIATA:   "IST",
		DestIATA:     "LHR",
	}}

	f, err := flightops.Create(ctx, flightops.Deps{Store: s, Resolver: r}, uid, "TK1980", "2026-06-12")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if f.Ident != "TK1980" {
		t.Errorf("ident = %q, want TK1980", f.Ident)
	}
	if f.CreatedBy == nil || *f.CreatedBy != uid {
		t.Errorf("CreatedBy = %v, want %d", f.CreatedBy, uid)
	}
	pmap, _ := s.PassengersByFlight(ctx, []int64{f.ID})
	if len(pmap[f.ID]) != 1 || pmap[f.ID][0] != uid {
		t.Errorf("passengers = %+v, want exactly [%d]", pmap[f.ID], uid)
	}
}

// TestCreate_PreservesCallerIdent guards the booking-vs-operating-carrier
// case: when the resolver canonicalises EZY2823 (marketing/ICAO) to U22823
// (operating carrier IATA), we should still store what the caller asked for
// so the user sees the ident that's on their booking. Whitespace is
// stripped and the ident is upper-cased to give a stable canonical form
// regardless of upstream extractor formatting.
func TestCreate_PreservesCallerIdent(t *testing.T) {
	ctx, s, uid := ctxAndStore(t)
	r := &fakeResolver{out: &providers.ResolvedFlight{
		Ident:        "U22823", // resolver swaps marketing → operating carrier
		ScheduledOut: time.Date(2027, 1, 15, 10, 25, 0, 0, time.UTC),
		ScheduledIn:  time.Date(2027, 1, 15, 16, 20, 0, 0, time.UTC),
		OriginIATA:   "BRS",
		DestIATA:     "SID",
	}}

	f, err := flightops.Create(ctx, flightops.Deps{Store: s, Resolver: r}, uid, "ezy 2823", "2027-01-15")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if f.Ident != "EZY2823" {
		t.Errorf("ident = %q, want EZY2823 (caller form, canonicalised)", f.Ident)
	}
}

func TestCreate_BadDateRejected(t *testing.T) {
	ctx, s, uid := ctxAndStore(t)
	_, err := flightops.Create(ctx,
		flightops.Deps{Store: s, Resolver: &fakeResolver{}},
		uid, "TK1980", "12-06-2026")
	if err == nil {
		t.Fatal("expected error for non-YYYY-MM-DD date")
	}
	if !strings.Contains(err.Error(), "YYYY-MM-DD") {
		t.Errorf("err message = %q", err.Error())
	}
}

func TestCreate_ResolverError(t *testing.T) {
	ctx, s, uid := ctxAndStore(t)
	_, err := flightops.Create(ctx,
		flightops.Deps{Store: s, Resolver: &fakeResolver{err: errors.New("upstream nope")}},
		uid, "TK1980", "2026-06-12")
	if err == nil {
		t.Fatal("expected error when resolver fails")
	}
	if !strings.Contains(err.Error(), "upstream nope") {
		t.Errorf("err did not wrap resolver error: %v", err)
	}
}

func TestCreate_NoResolverConfigured(t *testing.T) {
	ctx, s, uid := ctxAndStore(t)
	_, err := flightops.Create(ctx, flightops.Deps{Store: s, Resolver: nil}, uid, "TK1980", "2026-06-12")
	if err == nil {
		t.Fatal("expected error when no resolver is configured")
	}
}

func TestCreate_NilStore(t *testing.T) {
	_, err := flightops.Create(context.Background(),
		flightops.Deps{Store: nil, Resolver: &fakeResolver{}},
		1, "TK1980", "2026-06-12")
	if err == nil {
		t.Fatal("expected error when Store is nil")
	}
}

func TestCreate_StoreRejectsBadResolverOutput(t *testing.T) {
	ctx, s, uid := ctxAndStore(t)
	// scheduled_in not after scheduled_out → store rejects.
	r := &fakeResolver{out: &providers.ResolvedFlight{
		Ident:        "TK1980",
		ScheduledOut: time.Date(2026, 6, 12, 13, 0, 0, 0, time.UTC),
		ScheduledIn:  time.Date(2026, 6, 12, 9, 30, 0, 0, time.UTC),
		OriginIATA:   "IST",
		DestIATA:     "LHR",
	}}
	if _, err := flightops.Create(ctx, flightops.Deps{Store: s, Resolver: r}, uid, "TK1980", "2026-06-12"); err == nil {
		t.Error("expected error from store on out-of-order times")
	}
}
