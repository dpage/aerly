package emailingest_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/emailingest"
	"github.com/dpage/aerly/internal/planops"
	"github.com/dpage/aerly/internal/providers"
	"github.com/dpage/aerly/internal/store"
	"github.com/dpage/aerly/internal/testsupport"
)

// fakeLLM returns a fixed JSON response and records the docs it received.
type fakeLLM struct {
	resp    string
	err     error
	gotDocs int
}

func (f *fakeLLM) Complete(ctx context.Context, prompt string, docs []emailingest.Document) (string, error) {
	f.gotDocs = len(docs)
	return f.resp, f.err
}

type fakeResolver struct {
	err error
}

func (f fakeResolver) Resolve(ctx context.Context, ident string, date time.Time) (*providers.ResolvedFlight, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &providers.ResolvedFlight{
		Ident:        ident,
		ScheduledOut: date.Add(9 * time.Hour),
		ScheduledIn:  date.Add(13 * time.Hour),
		OriginIATA:   "IST",
		DestIATA:     "LHR",
	}, nil
}

// buildTestSendmail compiles a stub binary that writes stdin to $SENDMAIL_OUT.
// Cached in a sub-test temp dir, returned as the absolute path.
func buildTestSendmail(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "sendmail.go")
	out := filepath.Join(dir, "sendmail")
	code := `package main
import ("io"; "os")
func main() {
	out := os.Getenv("SENDMAIL_OUT")
	if out == "" { io.Copy(io.Discard, os.Stdin); return }
	f, err := os.OpenFile(out, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil { os.Exit(2) }
	defer f.Close()
	io.Copy(f, os.Stdin)
}
`
	if err := os.WriteFile(src, []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "build", "-o", out, src)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build stub sendmail: %v %s", err, b)
	}
	return out
}

type harness struct {
	svc         *emailingest.Service
	sendmailOut string
	maildir     string
	store       *store.Store
}

// newHarness builds a Service wired to a real DB, a fake LLM, a fake resolver,
// and a stub sendmail. Every booking (flights included) flows through the
// planops capture path now that the legacy flight surface is gone. Caller drops
// messages into <maildir>/new/.
func newHarness(t *testing.T, llmResp string, resolverErr error, requireDKIM bool) *harness {
	t.Helper()
	pool := testsupport.NewPool(t)
	s := store.New(pool)

	maildir := t.TempDir()
	sendmailOut := filepath.Join(t.TempDir(), "sent.txt")
	t.Setenv("SENDMAIL_OUT", sendmailOut)

	extractor := emailingest.NewExtractor(&fakeLLM{resp: llmResp}, "test")
	svc := &emailingest.Service{
		Cfg: emailingest.Config{
			MaildirPath:   maildir,
			PollInterval:  50 * time.Millisecond,
			RequireDKIM:   requireDKIM,
			MaxBodyBytes:  1 << 20,
			IngestAddress: "flights@flights.example",
			SendmailPath:  buildTestSendmail(t),
			PublicURL:     "https://flights.example",
		},
		Store:     s,
		Extractor: extractor,
		PlanDeps:  planops.Deps{Store: s, Extractor: extractor, Resolver: fakeResolver{err: resolverErr}},
	}
	if err := svc.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	return &harness{svc: svc, sendmailOut: sendmailOut, maildir: maildir, store: s}
}

// runUntilProcessed runs svc.Run in a goroutine and waits up to timeout for
// the file at maildir/new/name to disappear (success) or land in .failed/.
func (h *harness) runUntilProcessed(t *testing.T, name string, timeout time.Duration) (processedAs string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = h.svc.Run(ctx) }()
	newPath := filepath.Join(h.maildir, "new", name)
	failedPath := filepath.Join(h.maildir, ".failed", name)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(newPath); os.IsNotExist(err) {
			if _, err := os.Stat(failedPath); err == nil {
				return "failed"
			}
			return "removed"
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("file %s never processed within %s", name, timeout)
	return ""
}

const goodMessage = "From: alice@example.com\r\n" +
	"To: flights@flights.example\r\n" +
	"Subject: x\r\n" +
	"Message-ID: <1@x>\r\n" +
	"Authentication-Results: ml; dkim=pass header.d=example.com\r\n" +
	"Content-Type: text/plain\r\n\r\n" +
	"body text"

func writeMessage(t *testing.T, maildir, name, msg string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(maildir, "new", name), []byte(msg), 0o644); err != nil {
		t.Fatal(err)
	}
}

// flightPlanResp renders a planops-schema LLM response with a single flight
// plan. Email bookings now flow through planops.ExtractPlans (the {"plans":...}
// schema) rather than the legacy {"flights":...} schema.
func flightPlanResp(ident, date string) string {
	return `{"plans":[{"type":"flight","title":"` + ident + `","parts":[
		{"type":"flight","confidence":"high","flight":{"ident":"` + ident + `","date":"` + date + `"}}
	]}]}`
}

// flightPlanRespManual renders a flight plan that also carries the email's own
// origin/dest/times, so planops can fall back to them when the resolver has no
// record (the old flightops manual-fallback path).
func flightPlanRespManual(ident, depDate, arrDate string) string {
	return `{"plans":[{"type":"flight","title":"` + ident + `","parts":[
		{"type":"flight","confidence":"high","flight":{"ident":"` + ident + `","date":"` + depDate + `",
		 "origin_iata":"IST","dest_iata":"LHR","depart_time":"22:30","arrive_date":"` + arrDate + `","arrive_time":"01:15"}}
	]}]}`
}

// soloFlightPart returns the single flight plan part visible to viewer, or fails.
func soloFlightPart(t *testing.T, h *harness, viewer int64) *store.PlanPart {
	t.Helper()
	parts, err := h.store.ListVisiblePlanParts(context.Background(), viewer, store.ListVisiblePlanPartsOpts{Type: "flight"})
	if err != nil {
		t.Fatalf("ListVisiblePlanParts: %v", err)
	}
	if len(parts) != 1 {
		t.Fatalf("expected 1 visible flight part, got %d", len(parts))
	}
	return parts[0]
}

func TestIngest_EndToEnd_Success(t *testing.T) {
	h := newHarness(t,
		flightPlanResp("TK1980", time.Now().AddDate(0, 1, 0).Format("2006-01-02")),
		nil, true)
	ctx := context.Background()
	u, _ := h.store.InviteUser(ctx, store.InvitePayload{Username: "alice"})
	if err := h.store.UpsertVerifiedEmail(ctx, u.ID, "alice@example.com"); err != nil {
		t.Fatal(err)
	}

	writeMessage(t, h.maildir, "1", goodMessage)
	state := h.runUntilProcessed(t, "1", 5*time.Second)
	if state != "removed" {
		t.Fatalf("expected file removed, got %s", state)
	}
	// The reply sendmail stub should have received an RFC822 message.
	body, _ := os.ReadFile(h.sendmailOut)
	if !strings.Contains(string(body), "TK1980") {
		t.Errorf("sendmail output missing flight: %s", body)
	}
}

func TestIngest_DKIMFailed_Poison(t *testing.T) {
	h := newHarness(t, `{"flights":[]}`, nil, true)
	ctx := context.Background()
	u, _ := h.store.InviteUser(ctx, store.InvitePayload{Username: "alice"})
	if err := h.store.UpsertVerifiedEmail(ctx, u.ID, "alice@example.com"); err != nil {
		t.Fatal(err)
	}
	// Message has no Authentication-Results → DKIM fail.
	msg := "From: alice@example.com\r\nMessage-ID: <2@x>\r\nContent-Type: text/plain\r\n\r\nbody"
	writeMessage(t, h.maildir, "2", msg)
	state := h.runUntilProcessed(t, "2", 5*time.Second)
	if state != "failed" {
		t.Errorf("expected .failed/, got %s", state)
	}
}

func TestIngest_DKIMOff_AcceptsAnyway(t *testing.T) {
	h := newHarness(t,
		flightPlanResp("TK1980", time.Now().AddDate(0, 1, 0).Format("2006-01-02")),
		nil, false)
	ctx := context.Background()
	u, _ := h.store.InviteUser(ctx, store.InvitePayload{Username: "alice"})
	if err := h.store.UpsertVerifiedEmail(ctx, u.ID, "alice@example.com"); err != nil {
		t.Fatal(err)
	}
	msg := "From: alice@example.com\r\nMessage-ID: <3@x>\r\nContent-Type: text/plain\r\n\r\nbody"
	writeMessage(t, h.maildir, "3", msg)
	state := h.runUntilProcessed(t, "3", 5*time.Second)
	if state != "removed" {
		t.Errorf("expected removed (DKIM not required), got %s", state)
	}
}

func TestIngest_UnknownSender_Poison(t *testing.T) {
	h := newHarness(t, `{"flights":[]}`, nil, true)
	writeMessage(t, h.maildir, "4", goodMessage) // From: alice@example.com but no user registered
	state := h.runUntilProcessed(t, "4", 5*time.Second)
	if state != "failed" {
		t.Errorf("expected .failed/, got %s", state)
	}
}

func TestIngest_SelfAddressed_Poison(t *testing.T) {
	h := newHarness(t, `{"flights":[]}`, nil, false)
	msg := "From: flights@flights.example\r\nMessage-ID: <5@x>\r\nContent-Type: text/plain\r\n\r\nbody"
	writeMessage(t, h.maildir, "5", msg)
	state := h.runUntilProcessed(t, "5", 5*time.Second)
	if state != "failed" {
		t.Errorf("expected .failed/, got %s", state)
	}
}

// TestIngest_ResolverError_StillCapturedFromEmail covers the post-Wave-3
// behaviour: planops.enrichFlight falls back to the email's own schedule when
// the resolver errors, so an emailed flight is still committed (as a plan with
// a flight part) rather than reported as a hard failure. The reply lists it
// with the "verify the times" note since the schedule wasn't resolver-confirmed.
func TestIngest_ResolverError_StillCapturedFromEmail(t *testing.T) {
	depDate := time.Now().AddDate(0, 1, 0).Format("2006-01-02")
	h := newHarness(t,
		flightPlanResp("TK1980", depDate),
		errors.New("upstream down"), false)
	ctx := context.Background()
	u, _ := h.store.InviteUser(ctx, store.InvitePayload{Username: "alice"})
	if err := h.store.UpsertVerifiedEmail(ctx, u.ID, "alice@example.com"); err != nil {
		t.Fatal(err)
	}
	writeMessage(t, h.maildir, "6", goodMessage)
	state := h.runUntilProcessed(t, "6", 5*time.Second)
	if state != "removed" {
		t.Errorf("expected file removed, got %s", state)
	}
	body, _ := os.ReadFile(h.sendmailOut)
	if !strings.Contains(string(body), "TK1980") {
		t.Errorf("expected reply to list the captured flight, got: %s", body)
	}
	// The flight landed as a plan part the tracker can see.
	soloFlightPart(t, h, u.ID)
}

func TestIngest_ResolverUnscheduled_ManualFallback(t *testing.T) {
	// LLM extracts a flight part with full manual details. Resolver reports the
	// flight is unscheduled. planops.enrichFlight falls back to the email's own
	// schedule, so the flight is captured as a plan with origin/dest from the
	// email, and the reply tells the user to verify the times.
	depDate := time.Now().AddDate(0, 1, 0).Format("2006-01-02")
	arrDate := time.Now().AddDate(0, 1, 0).AddDate(0, 0, 1).Format("2006-01-02")
	h := newHarness(t, flightPlanRespManual("TK1980", depDate, arrDate), providers.ErrFlightUnscheduled, false)
	ctx := context.Background()
	u, _ := h.store.InviteUser(ctx, store.InvitePayload{Username: "alice"})
	if err := h.store.UpsertVerifiedEmail(ctx, u.ID, "alice@example.com"); err != nil {
		t.Fatal(err)
	}
	writeMessage(t, h.maildir, "10", goodMessage)
	state := h.runUntilProcessed(t, "10", 5*time.Second)
	if state != "removed" {
		t.Fatalf("expected removed, got %s", state)
	}
	body, _ := os.ReadFile(h.sendmailOut)
	bs := string(body)
	if !strings.Contains(bs, "TK1980 on "+depDate+" (from the email") {
		t.Errorf("expected manual-fallback note in reply, got:\n%s", bs)
	}
	if !strings.Contains(bs, "please check the departure and arrival times") {
		t.Errorf("expected manual trailer in reply, got:\n%s", bs)
	}
	// The flight should be a plan part attached to alice, with the email's IATAs.
	part := soloFlightPart(t, h, u.ID)
	fd, err := h.store.FlightDetailFor(ctx, part.ID)
	if err != nil {
		t.Fatalf("FlightDetailFor: %v", err)
	}
	if fd.Ident != "TK1980" || fd.OriginIATA != "IST" || fd.DestIATA != "LHR" {
		t.Errorf("flight detail wrong: %+v", fd)
	}
}

func TestIngest_ResolverUnscheduled_NoManualDetails_StillCaptured(t *testing.T) {
	// Resolver fails AND the LLM didn't extract manual details. Under the
	// plan model the flight is still captured (with a placeholder schedule from
	// the email's date) rather than discarded — the user can correct it in the
	// UI. This replaces the legacy "hard failure" behaviour.
	depDate := time.Now().AddDate(0, 1, 0).Format("2006-01-02")
	h := newHarness(t, flightPlanResp("TK1980", depDate), providers.ErrFlightUnscheduled, false)
	ctx := context.Background()
	u, _ := h.store.InviteUser(ctx, store.InvitePayload{Username: "alice"})
	if err := h.store.UpsertVerifiedEmail(ctx, u.ID, "alice@example.com"); err != nil {
		t.Fatal(err)
	}
	writeMessage(t, h.maildir, "11", goodMessage)
	state := h.runUntilProcessed(t, "11", 5*time.Second)
	if state != "removed" {
		t.Fatalf("expected removed, got %s", state)
	}
	body, _ := os.ReadFile(h.sendmailOut)
	if !strings.Contains(string(body), "TK1980") {
		t.Errorf("expected reply to list the captured flight, got:\n%s", body)
	}
	soloFlightPart(t, h, u.ID)
}

func TestIngest_MalformedMessage_Poison(t *testing.T) {
	h := newHarness(t, `{"flights":[]}`, nil, false)
	writeMessage(t, h.maildir, "7", "not an email at all")
	state := h.runUntilProcessed(t, "7", 5*time.Second)
	if state != "failed" {
		t.Errorf("expected .failed/, got %s", state)
	}
}

// The legacy SSE-on-insert tests (TestIngest_PublishesSSEOnInsert /
// TestIngest_ManualFallback_PublishesSSE) were removed in Wave 3: email ingest
// now creates plans, and — like the rest of the plan model — does not broadcast
// per-row SSE events on creation. Connected clients pick up new plans on their
// next trip refresh, exactly as for interactive plan creation.

// TestIngest_PlanCapture_AutoCreatesTrip exercises the rewired Service planops
// path: an email with a non-flight booking and no matching trip auto-creates a
// trip and commits the plan against it (surfaced, not silently dropped).
func TestIngest_PlanCapture_AutoCreatesTrip(t *testing.T) {
	// The LLM returns a hotel-only plan. Extract (flights schema) finds no
	// flights; ExtractPlans (plans schema) finds the hotel.
	llmResp := `{"plans":[{"type":"hotel","title":"Hotel Plaza","confirmation_ref":"H1","parts":[
		{"type":"hotel","confidence":"high","start_date":"2026-06-12","end_date":"2026-06-15","hotel":{"property_name":"Hotel Plaza","address":"1 Main St"}}
	]}]}`
	h := newHarness(t, llmResp, nil, false)
	ctx := context.Background()
	u, _ := h.store.InviteUser(ctx, store.InvitePayload{Username: "alice"})
	if err := h.store.UpsertVerifiedEmail(ctx, u.ID, "alice@example.com"); err != nil {
		t.Fatal(err)
	}
	// No pre-existing trips.
	writeMessage(t, h.maildir, "30", goodMessage)
	if state := h.runUntilProcessed(t, "30", 5*time.Second); state != "removed" {
		t.Fatalf("expected removed, got %s", state)
	}
	trips, err := h.store.ListTrips(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(trips) != 1 {
		t.Fatalf("expected 1 auto-created trip, got %d", len(trips))
	}
	plans, err := h.store.PlansByTrip(ctx, trips[0].ID)
	if err != nil || len(plans) != 1 {
		t.Fatalf("PlansByTrip = %d, %v", len(plans), err)
	}
	if plans[0].Type != "hotel" || plans[0].Source != "email" {
		t.Errorf("plan = %+v, want hotel/email", plans[0])
	}
}

// TestIngest_PlanCapture_AttachesToExistingTrip verifies the date-proximity
// selection attaches the ingested plan to an overlapping existing trip rather
// than creating a new one.
func TestIngest_PlanCapture_AttachesToExistingTrip(t *testing.T) {
	llmResp := `{"plans":[{"type":"hotel","title":"Hotel Plaza","confirmation_ref":"H1","parts":[
		{"type":"hotel","confidence":"high","start_date":"2026-06-12","end_date":"2026-06-15","hotel":{"property_name":"Hotel Plaza"}}
	]}]}`
	h := newHarness(t, llmResp, nil, false)
	ctx := context.Background()
	u, _ := h.store.InviteUser(ctx, store.InvitePayload{Username: "alice"})
	if err := h.store.UpsertVerifiedEmail(ctx, u.ID, "alice@example.com"); err != nil {
		t.Fatal(err)
	}
	// Pre-existing trip spanning the hotel's dates (via a flight plan part).
	trip, err := h.store.CreateTrip(ctx, store.CreateTripPayload{Name: "Existing"}, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	out := time.Date(2026, 6, 11, 9, 0, 0, 0, time.UTC)
	in := time.Date(2026, 6, 16, 17, 0, 0, 0, time.UTC)
	if _, err := h.store.CreatePlan(ctx, store.CreatePlanPayload{
		TripID: trip.ID, Type: "flight", Title: "BA1",
		Parts: []store.CreatePlanPartPayload{{
			StartsAt: out, EndsAt: &in,
			Flight: &store.FlightDetail{Ident: "BA1", ScheduledOut: out, ScheduledIn: in, OriginIATA: "LHR", DestIATA: "JFK"},
		}},
	}, u.ID); err != nil {
		t.Fatal(err)
	}

	writeMessage(t, h.maildir, "31", goodMessage)
	if state := h.runUntilProcessed(t, "31", 5*time.Second); state != "removed" {
		t.Fatalf("expected removed, got %s", state)
	}
	trips, err := h.store.ListTrips(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(trips) != 1 {
		t.Fatalf("expected the hotel to attach to the existing trip (1 trip), got %d", len(trips))
	}
	plans, _ := h.store.PlansByTrip(ctx, trip.ID)
	var hotels int
	for _, p := range plans {
		if p.Type == "hotel" {
			hotels++
		}
	}
	if hotels != 1 {
		t.Errorf("expected 1 hotel plan on existing trip, got %d", hotels)
	}
}
