package store

import (
	"context"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/testsupport"
)

// enableDepartureSummaries turns a user's departure-summary preference on.
func enableDepartureSummaries(t *testing.T, s *Store, uid int64) {
	t.Helper()
	if err := s.SetAlertPrefs(context.Background(), AlertPrefs{
		UserID: uid, InApp: true, Email: true, MinDelayMin: 15, DepartureSummary: true,
	}); err != nil {
		t.Fatalf("SetAlertPrefs: %v", err)
	}
}

// TestDueDepartureSummaries_Window: a candidate appears only once `now` has
// reached the four-hour lead point and only while the flight is upcoming.
func TestDueDepartureSummaries_Window(t *testing.T) {
	s := New(testsupport.NewPool(t))
	ctx := context.Background()
	uid := testsupport.InsertUser(t, s.pool, "alice", false, true)
	enableDepartureSummaries(t, s, uid)
	tid := seedTrip(t, s, uid)
	out := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	_, partID := seedCheckinFlightPart(t, s, tid, uid, "BA286", out)

	for _, c := range []struct {
		name string
		now  time.Time
		want int
	}{
		{"a minute early", out.Add(-DepartureSummaryLead - time.Minute), 0},
		{"exactly at the lead point", out.Add(-DepartureSummaryLead), 1},
		{"an hour before departure", out.Add(-time.Hour), 1},
		{"departed", out, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			due, err := s.DueDepartureSummaries(ctx, c.now)
			if err != nil {
				t.Fatalf("DueDepartureSummaries: %v", err)
			}
			if len(due) != c.want {
				t.Fatalf("got %d due, want %d: %+v", len(due), c.want, due)
			}
			if c.want == 1 {
				d := due[0]
				if d.PlanPartID != partID || d.UserID != uid || d.Ident != "BA286" ||
					d.OriginIATA != "LHR" || d.DestIATA != "LIS" || !d.InApp || !d.Email {
					t.Fatalf("due row = %+v", d)
				}
			}
		})
	}
}

// TestDueDepartureSummaries_PreferencesAreIndependent: each notice is gated by
// its own preference, so enabling one never makes the other fire.
func TestDueDepartureSummaries_PreferencesAreIndependent(t *testing.T) {
	s := New(testsupport.NewPool(t))
	ctx := context.Background()
	uid := testsupport.InsertUser(t, s.pool, "alice", false, true)
	tid := seedTrip(t, s, uid)
	out := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	seedCheckinFlightPart(t, s, tid, uid, "BA286", out)
	now := out.Add(-time.Hour)

	// No alert_prefs row at all.
	if due, err := s.DueDepartureSummaries(ctx, now); err != nil || len(due) != 0 {
		t.Fatalf("no prefs row: %d due (err %v)", len(due), err)
	}
	// Check-in on, summary off.
	enableCheckins(t, s, uid)
	if due, err := s.DueDepartureSummaries(ctx, now); err != nil || len(due) != 0 {
		t.Fatalf("checkin only: %d due (err %v)", len(due), err)
	}
	// Summary on, check-in off.
	enableDepartureSummaries(t, s, uid)
	if due, err := s.DueDepartureSummaries(ctx, now); err != nil || len(due) != 1 {
		t.Fatalf("summary on: %d due (err %v)", len(due), err)
	}
	if due, err := s.DueCheckins(ctx, now); err != nil || len(due) != 0 {
		t.Fatalf("checkin off: %d due (err %v)", len(due), err)
	}
}

// TestMarkDepartureSummarySent_Dedupes: marking a pair removes it from the due
// set, is idempotent, and does not touch the check-in sent record.
func TestMarkDepartureSummarySent_Dedupes(t *testing.T) {
	s := New(testsupport.NewPool(t))
	ctx := context.Background()
	uid := testsupport.InsertUser(t, s.pool, "alice", false, true)
	if err := s.SetAlertPrefs(ctx, AlertPrefs{
		UserID: uid, InApp: true, Email: true, MinDelayMin: 15, Checkin: true, DepartureSummary: true,
	}); err != nil {
		t.Fatalf("SetAlertPrefs: %v", err)
	}
	tid := seedTrip(t, s, uid)
	out := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	_, partID := seedCheckinFlightPart(t, s, tid, uid, "BA286", out)
	now := out.Add(-time.Hour)

	if due, _ := s.DueDepartureSummaries(ctx, now); len(due) != 1 {
		t.Fatalf("before marking: %d due, want 1", len(due))
	}
	for i := 0; i < 2; i++ {
		if err := s.MarkDepartureSummarySent(ctx, partID, uid); err != nil {
			t.Fatalf("MarkDepartureSummarySent #%d: %v", i+1, err)
		}
	}
	if due, _ := s.DueDepartureSummaries(ctx, now); len(due) != 0 {
		t.Fatalf("after marking: %d due, want 0", len(due))
	}
	if due, _ := s.DueCheckins(ctx, now); len(due) != 1 {
		t.Fatalf("check-in should be unaffected: %d due, want 1", len(due))
	}
}

// TestDueDepartureSummaries_QueryError: a cancelled context surfaces the query
// error rather than an empty candidate list.
func TestDueDepartureSummaries_QueryError(t *testing.T) {
	s := New(testsupport.NewPool(t))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.DueDepartureSummaries(ctx, time.Now()); err == nil {
		t.Fatal("want an error from a cancelled context")
	}
}
