package poller

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/sse"
	"github.com/dpage/aerly/internal/store"
	"github.com/dpage/aerly/internal/timing"
)

// setHomeTZ gives a user a home zone.
func setHomeTZ(t *testing.T, s *store.Store, uid int64, tz string) {
	t.Helper()
	if _, err := s.UpdateUser(context.Background(), uid, store.UpdateUserPayload{HomeTZ: &tz}); err != nil {
		t.Fatalf("set home zone: %v", err)
	}
}

func TestPartBlock(t *testing.T) {
	at := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	lat, lon := 48.2082, 16.3738 // central Vienna
	for _, c := range []struct {
		name             string
		tz, source, iata string
		lat, lon         *float64
		want             timing.Block
	}{
		{"stored and confirmed", "Europe/London", "confirmed", "LHR", nil, nil,
			timing.Block{At: at, TZ: "Europe/London", Source: timing.Confirmed, Place: "London"}},
		{"stored and inferred", "Europe/Vienna", "inferred", "", nil, nil,
			timing.Block{At: at, TZ: "Europe/Vienna", Source: timing.Inferred, Place: "Vienna"}},
		// Review focus 3: a floating part is never moved into the zone its
		// airport or coordinates would give.
		{"floating with an airport", "", "floating", "LHR", &lat, &lon,
			timing.Block{At: at, Source: timing.Floating}},
		{"unknown falls back to the airport", "", "unknown", "LHR", nil, nil,
			timing.Block{At: at, TZ: "Europe/London", Source: timing.Inferred, Place: "London"}},
		{"unknown falls back to the coordinates", "", "unknown", "", &lat, &lon,
			timing.Block{At: at, TZ: "Europe/Vienna", Source: timing.Inferred, Place: "Vienna"}},
		{"unknown with nothing to go on", "", "unknown", "", nil, nil,
			timing.Block{At: at, Source: timing.Unknown}},
	} {
		if got := partBlock(at, c.tz, c.source, c.iata, c.lat, c.lon); got != c.want {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestReminderPushBody(t *testing.T) {
	at := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	london := timing.Block{At: at, TZ: "Europe/London", Source: timing.Confirmed, Place: "London"}
	floating := timing.Block{At: time.Date(2026, 8, 31, 23, 30, 0, 0, time.UTC), Source: timing.Floating}
	unknown := timing.Block{At: at, Source: timing.Unknown}
	for _, c := range []struct {
		name string
		b    timing.Block
		home string
		now  time.Time
		want string
	}{
		{"today, no home", london, "", at.Add(-2 * time.Hour), "Starts 10:00 London (UTC+1)"},
		{"today, home elsewhere", london, "America/New_York", at.Add(-2 * time.Hour),
			"Starts 10:00 London (UTC+1) (05:00 your time)"},
		// 23:30 UTC on the 30th is already the 31st in London.
		{"tomorrow by the UTC date but today in London", london, "", time.Date(2026, 8, 30, 23, 30, 0, 0, time.UTC),
			"Starts 10:00 London (UTC+1)"},
		{"another day", london, "America/New_York", at.Add(-20 * time.Hour),
			"Starts Mon 31 Aug, 10:00 London (UTC+1) (05:00 your time)"},
		{"floating today in UTC", floating, "America/New_York", time.Date(2026, 8, 31, 1, 0, 0, 0, time.UTC),
			"Starts 23:30"},
		{"floating on another UTC day", floating, "Asia/Tokyo", time.Date(2026, 8, 30, 23, 0, 0, 0, time.UTC),
			"Starts Mon 31 Aug, 23:30"},
		// An unknown zone is shown, and its day judged, in the home zone.
		{"unknown, today at home", unknown, "America/New_York", time.Date(2026, 8, 31, 5, 0, 0, 0, time.UTC),
			"Starts 05:00 UTC-4"},
		{"unknown, another day at home", unknown, "America/New_York", time.Date(2026, 8, 30, 3, 0, 0, 0, time.UTC),
			"Starts Mon 31 Aug, 05:00 UTC-4"},
	} {
		if got := reminderPushBody(c.b, c.home, c.now); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// TestRemindUpcoming_FloatingNotShifted is review focus 3 end to end: a
// floating hotel check-in with coordinates in Vienna is quoted as its stored
// digits, with no offset and no second time, on every channel.
func TestRemindUpcoming_FloatingNotShifted(t *testing.T) {
	p, s, _, cap := alertPoller(t)
	fp := &fakePusher{enabled: true}
	p.Push = fp
	ctx := context.Background()
	owner := seedUser(t, s)
	if err := s.UpsertVerifiedEmail(ctx, owner, "alice@example.com"); err != nil {
		t.Fatalf("verify email: %v", err)
	}
	setHomeTZ(t, s, owner, "America/New_York")
	start := time.Date(2026, 8, 31, 15, 0, 0, 0, time.UTC)
	tripID, partID := seedReminderPlan(t, s, owner, "hotel", start)
	if _, err := s.Pool().Exec(ctx, `
		UPDATE plan_parts SET start_tz = '', start_tz_source = 'floating',
			start_lat = 48.2082, start_lon = 16.3738 WHERE id = $1`, partID); err != nil {
		t.Fatalf("make floating: %v", err)
	}
	if _, err := s.Pool().Exec(ctx,
		`UPDATE plans SET title = 'Test Hotel' WHERE id = (SELECT plan_id FROM plan_parts WHERE id = $1)`, partID); err != nil {
		t.Fatalf("title: %v", err)
	}
	if err := s.SetTripReminder(ctx, tripID, owner, 24); err != nil {
		t.Fatalf("SetTripReminder: %v", err)
	}

	p.remindUpcoming(ctx, start.Add(-2*time.Hour))

	if cap.count() != 1 {
		t.Fatalf("want 1 email, got %d", cap.count())
	}
	if !strings.Contains(cap.sent[0], "Test Hotel starts Mon 31 Aug, 15:00.\r\n") {
		t.Errorf("floating start shifted or labelled:\n%s", cap.sent[0])
	}
	for _, bad := range []string{"17:00", "UTC+", "your time"} {
		if strings.Contains(cap.sent[0], bad) {
			t.Errorf("email carries %q:\n%s", bad, cap.sent[0])
		}
	}
	if fp.count() != 1 || fp.payloads[0].Body != "Starts 15:00" {
		t.Errorf("push = %+v, want body %q", fp.payloads, "Starts 15:00")
	}
}

// TestRemindUpcoming_HomeZoneYourTime: a recipient at home in New York gets
// their own time after the airport's, in the email and the push.
func TestRemindUpcoming_HomeZoneYourTime(t *testing.T) {
	p, s, _, cap := alertPoller(t)
	fp := &fakePusher{enabled: true}
	p.Push = fp
	ctx := context.Background()
	owner := seedUser(t, s)
	if err := s.UpsertVerifiedEmail(ctx, owner, "alice@example.com"); err != nil {
		t.Fatalf("verify email: %v", err)
	}
	setHomeTZ(t, s, owner, "America/New_York")
	out := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	f, err := mkPart(ctx, s, partSeed{
		Ident: "XX100", ScheduledOut: out, ScheduledIn: out.Add(4 * time.Hour),
		OriginIATA: "LHR", DestIATA: "IST",
	}, owner)
	if err != nil {
		t.Fatalf("mkPart: %v", err)
	}
	var tripID int64
	if err := s.Pool().QueryRow(ctx,
		`SELECT pl.trip_id FROM plan_parts pp JOIN plans pl ON pl.id = pp.plan_id WHERE pp.id = $1`,
		f.ID).Scan(&tripID); err != nil {
		t.Fatalf("trip lookup: %v", err)
	}
	if err := s.SetTripReminder(ctx, tripID, owner, 24); err != nil {
		t.Fatalf("SetTripReminder: %v", err)
	}

	p.remindUpcoming(ctx, out.Add(-2*time.Hour))

	if cap.count() != 1 ||
		!strings.Contains(cap.sent[0], "Flight XX100 starts Mon 31 Aug, 10:00 London (UTC+1) (05:00 your time).") {
		t.Fatalf("email lacks the your-time suffix:\n%v", cap.sent)
	}
	if want := "Starts 10:00 London (UTC+1) (05:00 your time)"; fp.count() != 1 || fp.payloads[0].Body != want {
		t.Errorf("push = %+v, want body %q", fp.payloads, want)
	}
}

// TestRemindCheckins_HomeZoneYourTime: the check-in reminder email carries
// the recipient's own time too.
func TestRemindCheckins_HomeZoneYourTime(t *testing.T) {
	p, s, _, cap := alertPoller(t)
	ctx := context.Background()
	owner := seedUser(t, s)
	if err := s.UpsertVerifiedEmail(ctx, owner, "alice@example.com"); err != nil {
		t.Fatalf("verify email: %v", err)
	}
	optInToCheckins(t, s, owner)
	setHomeTZ(t, s, owner, "America/New_York")
	out := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	seedCheckinFlight(t, s, owner, "XX100", out)

	p.remindCheckins(ctx, out.Add(-store.CheckinLead))

	if cap.count() != 1 ||
		!strings.Contains(cap.sent[0], "It departs Mon 31 Aug, 10:00 London (UTC+1) (05:00 your time).") {
		t.Fatalf("email lacks the your-time suffix:\n%v", cap.sent)
	}
}

// TestRemindDepartures_HomeZoneYourTime: the departure summary headline, and
// so its push and inbox row, carry the recipient's own time.
func TestRemindDepartures_HomeZoneYourTime(t *testing.T) {
	p, s, cap, _, owner, partID := departureSetup(t)
	ctx := context.Background()
	fp := &fakePusher{enabled: true}
	p.Push = fp
	setHomeTZ(t, s, owner, "America/New_York")
	setEstimatedOut(t, s, partID, depOut.Add(80*time.Minute))

	p.remindDepartures(ctx, depOut.Add(-store.DepartureSummaryLead))

	// 16:20 UTC is 17:20 in London and 12:20 in New York.
	const at = "Thu 15 Oct, 17:20 London (UTC+1) (12:20 your time)."
	if cap.count() != 1 || !strings.Contains(cap.sent[0], at) {
		t.Fatalf("email lacks %q:\n%v", at, cap.sent)
	}
	if fp.count() != 1 || !strings.Contains(fp.payloads[0].Body, at) {
		t.Errorf("push = %+v", fp.payloads)
	}
	alerts, err := s.ListFlightAlerts(ctx, owner, 10)
	if err != nil || len(alerts) != 1 || !strings.HasSuffix(alerts[0].Message, at) {
		t.Errorf("inbox = %+v, %v", alerts, err)
	}
}

// TestDispatchAlert_DelayPerRecipient: the delay detail is written for each
// recipient, so one at home in London gets their own time after Istanbul's
// and one with no home zone gets exactly one time, in the push, the inbox and
// the email alike.
func TestDispatchAlert_DelayPerRecipient(t *testing.T) {
	p, s, hub, cap := alertPoller(t)
	fp := &fakePusher{enabled: true}
	p.Push = fp
	ctx := context.Background()
	home := seedUser(t, s)
	away := seedUser(t, s)
	setHomeTZ(t, s, home, "Europe/London")
	// A January departure, so London is on UTC and Istanbul on UTC+3.
	out := time.Date(2027, 1, 15, 15, 0, 0, 0, time.UTC)
	f, err := mkPart(ctx, s, partSeed{
		Ident: "XX100", ScheduledOut: out, ScheduledIn: out.Add(4 * time.Hour),
		OriginIATA: "IST", DestIATA: "LHR",
	}, home)
	if err != nil {
		t.Fatalf("mkPart: %v", err)
	}
	tp, err := s.TrackerPartRow(ctx, f.ID)
	if err != nil {
		t.Fatalf("TrackerPartRow: %v", err)
	}
	est := out.Add(50 * time.Minute)
	cur := *f
	cur.EstimatedOut = &est
	ch, unsub := hub.Subscribe(sse.Subscription{ViewerID: home})
	defer unsub()

	recips := []store.AlertRecipient{
		{UserID: home, InApp: true, Email: true, EmailAddr: "alice@example.com", HomeTZ: "Europe/London"},
		{UserID: away, InApp: true},
	}
	p.dispatchAlert(ctx, tp, &cur, "delayed", changeDetail("delayed", alertState{}, alertState{}, &cur),
		alertState{delayMin: 50}, recips)

	const withHome = "Your flight XX100 is now delayed — now departing 18:50 Istanbul (UTC+3) (15:50 your time)"
	const bare = "Your flight XX100 is now delayed — now departing 18:50 Istanbul (UTC+3)"
	if fp.count() != 2 || fp.payloads[0].Body != withHome || fp.payloads[1].Body != bare {
		t.Fatalf("push bodies = %+v", fp.payloads)
	}
	if got := drainAlerts(t, ch); len(got) != 1 || got[0].Message != withHome {
		t.Errorf("home recipient's inbox = %+v", got)
	}
	if alerts, _ := s.ListFlightAlerts(ctx, away, 10); len(alerts) != 1 || alerts[0].Message != bare {
		t.Errorf("away recipient's inbox = %+v", alerts)
	}
	if cap.count() != 1 {
		t.Fatalf("want 1 email, got %d", cap.count())
	}
	for _, want := range []string{
		withHome,
		"departing Fri 15 Jan, 18:50 Istanbul (UTC+3) (15:50 your time)",
		// The derived arrival is on London's clock, which is the home zone,
		// so it needs no second time.
		"arriving Fri 15 Jan, ≈19:50 London (UTC).",
	} {
		if !strings.Contains(cap.sent[0], want) {
			t.Errorf("email missing %q:\n%s", want, cap.sent[0])
		}
	}
}
