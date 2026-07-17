package emailingest_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/emailingest"
	"github.com/dpage/aerly/internal/geocode"
	"github.com/dpage/aerly/internal/store"
)

// goodMessageNoDKIM builds a minimal valid message from the given sender with a
// unique Message-ID. DKIM headers are omitted; callers use it with harnesses
// constructed with requireDKIM=false.
func goodMessageNoDKIM(from, msgID string) string {
	return "From: " + from + "\r\n" +
		"To: flights@flights.example\r\n" +
		"Subject: x\r\n" +
		"Message-ID: " + msgID + "\r\n" +
		"Content-Type: text/plain\r\n\r\n" +
		"body text"
}

func aliceInvite() store.InvitePayload { return store.InvitePayload{Username: "alice"} }

// TestRun_EnsureDirsError verifies Run surfaces an EnsureDirs failure rather
// than spinning. A regular file standing where the Maildir should be makes
// MkdirAll fail.
func TestRun_EnsureDirsError(t *testing.T) {
	dir := t.TempDir()
	bogus := filepath.Join(dir, "afile")
	if err := os.WriteFile(bogus, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := &emailingest.Service{Cfg: emailingest.Config{MaildirPath: bogus}}
	if err := svc.Run(context.Background()); err == nil {
		t.Error("expected Run to fail when the Maildir path can't be created")
	}
}

func TestEnsureDirs_Error(t *testing.T) {
	dir := t.TempDir()
	bogus := filepath.Join(dir, "afile")
	if err := os.WriteFile(bogus, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := &emailingest.Service{Cfg: emailingest.Config{MaildirPath: bogus}}
	if err := svc.EnsureDirs(); err == nil {
		t.Error("expected EnsureDirs to fail under a non-directory path")
	}
}

// TestRun_DefaultInterval covers the PollInterval<=0 default branch: Run starts,
// drains once, and exits cleanly when the context is cancelled.
func TestRun_DefaultInterval(t *testing.T) {
	dir := t.TempDir()
	svc := &emailingest.Service{Cfg: emailingest.Config{MaildirPath: dir, PollInterval: 0}}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := svc.Run(ctx); err == nil {
		t.Error("expected context error when Run is cancelled")
	}
	// The default-interval ticker subdirs were created by EnsureDirs.
	if _, err := os.Stat(filepath.Join(dir, "new")); err != nil {
		t.Errorf("new/ not created: %v", err)
	}
}

// TestRun_DrainReadDirError makes new/ unreadable (0o000) after EnsureDirs so
// drainNew's ReadDir errors on each tick; EnsureDirs' MkdirAll is a no-op on the
// already-existing dir (it doesn't need to read it), and Run keeps looping
// rather than crashing.
func TestRun_DrainReadDirError(t *testing.T) {
	dir := t.TempDir()
	svc := &emailingest.Service{Cfg: emailingest.Config{MaildirPath: dir, PollInterval: 20 * time.Millisecond}}
	if err := svc.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	newDir := filepath.Join(dir, "new")
	if err := os.Chmod(newDir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(newDir, 0o755) })
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()
	if err := svc.Run(ctx); err == nil {
		t.Error("expected context cancellation error")
	}
}

// TestDrainSkipsSubdirectories drops a sub-directory into new/ and confirms the
// drain loop skips it (the e.IsDir() branch) without trying to process it.
func TestDrainSkipsSubdirectories(t *testing.T) {
	h := newHarness(t, `{"plans":[]}`, nil, false)
	subdir := filepath.Join(h.maildir, "new", "a-subdir")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A real message alongside the sub-dir is still processed; the sub-dir is
	// left untouched.
	ctx := context.Background()
	u, _ := h.store.InviteUser(ctx, aliceInvite())
	if err := h.store.UpsertVerifiedEmail(ctx, u.ID, "alice@example.com"); err != nil {
		t.Fatal(err)
	}
	writeMessage(t, h.maildir, "subskip", goodMessageNoDKIM("alice@example.com", "<sub@x>"))
	if state := h.runUntilProcessed(t, "subskip", 5*time.Second); state != "removed" {
		t.Fatalf("expected message removed, got %s", state)
	}
	if _, err := os.Stat(subdir); err != nil {
		t.Errorf("sub-directory should be left in place: %v", err)
	}
}

// TestProcessOne_ReadError drops an unreadable file into new/ and confirms it is
// treated as transient (left in new/ for a later retry) rather than removed or
// quarantined.
func TestProcessOne_ReadError(t *testing.T) {
	h := newHarness(t, `{"plans":[]}`, nil, false)
	name := "unreadable"
	path := filepath.Join(h.maildir, "new", name)
	if err := os.WriteFile(path, []byte("From: a@example.com\r\n\r\nbody"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_ = h.svc.Run(ctx)
	// Transient: still in new/, not removed, not in .failed/.
	if _, err := os.Stat(path); err != nil {
		t.Errorf("unreadable file should remain in new/ as transient: %v", err)
	}
	if _, err := os.Stat(filepath.Join(h.maildir, ".failed", name)); err == nil {
		t.Error("unreadable file should not be quarantined as poison")
	}
}

// panicLLM panics on every Complete call, used to drive the processOneSafely
// panic-recovery branch: a message whose extraction blows up must be quarantined
// as poison rather than crashing the shared ingest goroutine.
type panicLLM struct{}

func (panicLLM) Complete(context.Context, string, []emailingest.Document) (string, error) {
	panic("synthetic extractor panic")
}

func TestProcessOneSafely_PanicRecovery(t *testing.T) {
	h := newHarness(t, `{"plans":[]}`, nil, false)
	// Swap in an extractor whose LLM panics so capturePlans → Propose panics.
	ex := emailingest.NewExtractor(panicLLM{}, "test")
	h.svc.Extractor = ex
	h.svc.PlanDeps.Extractor = ex

	ctx := context.Background()
	u, _ := h.store.InviteUser(ctx, aliceInvite())
	if err := h.store.UpsertVerifiedEmail(ctx, u.ID, "alice@example.com"); err != nil {
		t.Fatal(err)
	}
	writeMessage(t, h.maildir, "panic", goodMessageNoDKIM("alice@example.com", "<panic@x>"))
	if state := h.runUntilProcessed(t, "panic", 5*time.Second); state != "failed" {
		t.Errorf("expected a panicking message to be quarantined as poison, got %s", state)
	}
}

// TestIngest_MissingFrom_Poison covers the parsed.From=="" rejection: a message
// with no From header can't be tied to a verified sender and is quarantined.
func TestIngest_MissingFrom_Poison(t *testing.T) {
	h := newHarness(t, `{"plans":[]}`, nil, false)
	msg := "To: flights@flights.example\r\nMessage-ID: <nofrom@x>\r\nContent-Type: text/plain\r\n\r\nbody"
	writeMessage(t, h.maildir, "nofrom", msg)
	if state := h.runUntilProcessed(t, "nofrom", 5*time.Second); state != "failed" {
		t.Errorf("expected .failed/, got %s", state)
	}
}

// TestIngest_NothingFound covers the status="nothing_found" branch: the LLM
// returns no plans, so nothing is added or failed; the message is still
// processed (removed) and a reply is sent.
func TestIngest_NothingFound(t *testing.T) {
	h := newHarness(t, `{"plans":[]}`, nil, false)
	ctx := context.Background()
	u, _ := h.store.InviteUser(ctx, aliceInvite())
	if err := h.store.UpsertVerifiedEmail(ctx, u.ID, "alice@example.com"); err != nil {
		t.Fatal(err)
	}
	writeMessage(t, h.maildir, "nothing", goodMessageNoDKIM("alice@example.com", "<nf@x>"))
	if state := h.runUntilProcessed(t, "nothing", 5*time.Second); state != "removed" {
		t.Errorf("expected removed, got %s", state)
	}
}

// TestIngest_ProposeError_Transient covers the capturePlans propose-failure
// branch (treated as transient): a malformed LLM response makes ExtractPlans
// error inside Propose, so the message is retried (left in new/).
func TestIngest_ProposeError_Transient(t *testing.T) {
	h := newHarness(t, "this is not json", nil, false)
	ctx := context.Background()
	u, _ := h.store.InviteUser(ctx, aliceInvite())
	if err := h.store.UpsertVerifiedEmail(ctx, u.ID, "alice@example.com"); err != nil {
		t.Fatal(err)
	}
	name := "proposeerr"
	writeMessage(t, h.maildir, name, goodMessageNoDKIM("alice@example.com", "<pe@x>"))
	runCtx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	_ = h.svc.Run(runCtx)
	if _, err := os.Stat(filepath.Join(h.maildir, "new", name)); err != nil {
		t.Errorf("propose failure should leave the message in new/ (transient): %v", err)
	}
}

// TestIngest_SendReplyError_StillProcessed covers the Send-failure branch in
// processOne: even when the reply can't be sent, the message is considered
// processed (removed) rather than looped on.
func TestIngest_SendReplyError_StillProcessed(t *testing.T) {
	h := newHarness(t, `{"plans":[]}`, nil, false)
	// Point sendmail at a path that doesn't exist so Send fails.
	h.svc.Cfg.SendmailPath = filepath.Join(t.TempDir(), "no-such-sendmail")
	ctx := context.Background()
	u, _ := h.store.InviteUser(ctx, aliceInvite())
	if err := h.store.UpsertVerifiedEmail(ctx, u.ID, "alice@example.com"); err != nil {
		t.Fatal(err)
	}
	writeMessage(t, h.maildir, "sendfail", goodMessageNoDKIM("alice@example.com", "<sf@x>"))
	if state := h.runUntilProcessed(t, "sendfail", 5*time.Second); state != "removed" {
		t.Errorf("expected removed despite a send failure, got %s", state)
	}
}

// TestIngest_CreateTripFallbackTitle covers createTripForPlan's title-based
// naming fallback: a non-flight plan with a title (no destination to name the
// trip after) names the auto-created trip from its title.
func TestIngest_CreateTripFallbackTitle(t *testing.T) {
	llmResp := `{"plans":[{"type":"dining","title":"Dinner at Noma","parts":[
		{"type":"dining","confidence":"high","start_date":"2026-06-12","dining":{"reservation_name":"Test User"}}
	]}]}`
	h := newHarness(t, llmResp, nil, false)
	ctx := context.Background()
	u, _ := h.store.InviteUser(ctx, aliceInvite())
	if err := h.store.UpsertVerifiedEmail(ctx, u.ID, "alice@example.com"); err != nil {
		t.Fatal(err)
	}
	writeMessage(t, h.maildir, "dinetrip", goodMessageNoDKIM("alice@example.com", "<dt@x>"))
	if state := h.runUntilProcessed(t, "dinetrip", 5*time.Second); state != "removed" {
		t.Fatalf("expected removed, got %s", state)
	}
	trips, err := h.store.ListTrips(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(trips) != 1 {
		t.Fatalf("expected 1 auto-created trip, got %d", len(trips))
	}
	if trips[0].Name != "Dinner at Noma" {
		t.Errorf("trip name = %q, want the plan title", trips[0].Name)
	}
}

// TestIngest_CreateTripGenericFallback covers createTripForPlan's final
// fallback: a non-flight plan with no title (so no destination and no title to
// name the trip after) lands under the generic "Trip from email" name.
func TestIngest_CreateTripGenericFallback(t *testing.T) {
	llmResp := `{"plans":[{"type":"dining","parts":[
		{"type":"dining","confidence":"high","start_date":"2026-06-12","dining":{"reservation_name":"Test User"}}
	]}]}`
	h := newHarness(t, llmResp, nil, false)
	ctx := context.Background()
	u, _ := h.store.InviteUser(ctx, aliceInvite())
	if err := h.store.UpsertVerifiedEmail(ctx, u.ID, "alice@example.com"); err != nil {
		t.Fatal(err)
	}
	writeMessage(t, h.maildir, "generictrip", goodMessageNoDKIM("alice@example.com", "<gt@x>"))
	if state := h.runUntilProcessed(t, "generictrip", 5*time.Second); state != "removed" {
		t.Fatalf("expected removed, got %s", state)
	}
	trips, err := h.store.ListTrips(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(trips) != 1 || trips[0].Name != "Trip from email" {
		t.Fatalf("trips = %+v, want one named 'Trip from email'", trips)
	}
}

// pdfMultipartMessage wraps a base64 PDF attachment in a multipart message from
// the given sender, exercising the capturePlans document-conversion loop.
func pdfMultipartMessage(from, msgID string) string {
	// "%PDF-1.4\n" base64-encoded.
	return "From: " + from + "\r\n" +
		"To: flights@flights.example\r\n" +
		"Subject: booking\r\n" +
		"Message-ID: " + msgID + "\r\n" +
		"Content-Type: multipart/mixed; boundary=BB\r\n\r\n" +
		"--BB\r\n" +
		"Content-Type: text/plain\r\n\r\n" +
		"see attached\r\n" +
		"--BB\r\n" +
		"Content-Type: application/pdf\r\n" +
		"Content-Transfer-Encoding: base64\r\n\r\n" +
		"JVBERi0xLjQK\r\n" +
		"--BB--\r\n"
}

// TestIngest_WithPDFAttachment covers the capturePlans docs-conversion loop:
// the PDF is forwarded to the extractor as a planops.Document.
func TestIngest_WithPDFAttachment(t *testing.T) {
	llmResp := `{"plans":[{"type":"dining","title":"Dinner","parts":[
		{"type":"dining","confidence":"high","start_date":"2026-06-12","dining":{"reservation_name":"Test User"}}
	]}]}`
	h := newHarness(t, llmResp, nil, false)
	ctx := context.Background()
	u, _ := h.store.InviteUser(ctx, aliceInvite())
	if err := h.store.UpsertVerifiedEmail(ctx, u.ID, "alice@example.com"); err != nil {
		t.Fatal(err)
	}
	writeMessage(t, h.maildir, "withpdf", pdfMultipartMessage("alice@example.com", "<pdf@x>"))
	if state := h.runUntilProcessed(t, "withpdf", 5*time.Second); state != "removed" {
		t.Fatalf("expected removed, got %s", state)
	}
}

// failingGeocoder errors on every lookup so the capturePlans geocode-failure
// branch (non-fatal) is exercised: the plan still commits and the message is
// processed.
type failingGeocoder struct{}

// Candidates errors on every lookup, exercising the Resolver's failed-lookup
// branch (log + no pin, not a crash).
func (failingGeocoder) Candidates(context.Context, geocode.Query) ([]geocode.Candidate, error) {
	return nil, errContext("geocode down")
}

func (failingGeocoder) Geocode(context.Context, string, string) (float64, float64, bool, error) {
	return 0, 0, false, errContext("geocode down")
}
func (failingGeocoder) GeocodeCountry(context.Context, string) (string, bool, error) {
	return "", false, nil
}
func (failingGeocoder) ReversePlace(context.Context, float64, float64) (string, string, bool, error) {
	return "", "", false, nil
}
func (failingGeocoder) ReverseCountry(context.Context, float64, float64) (string, bool, error) {
	return "", false, nil
}

type errContext string

func (e errContext) Error() string { return string(e) }

func TestIngest_GeocodeFailureNonFatal(t *testing.T) {
	llmResp := `{"plans":[{"type":"hotel","title":"Plaza","confirmation_ref":"H1","parts":[
		{"type":"hotel","confidence":"high","start_date":"2026-06-12","end_date":"2026-06-15","start_address":"1 Main St","hotel":{"property_name":"Plaza","address":"1 Main St"}}
	]}]}`
	h := newHarness(t, llmResp, nil, false)
	h.svc.GeoResolver = geoResolver(failingGeocoder{})
	ctx := context.Background()
	u, _ := h.store.InviteUser(ctx, aliceInvite())
	if err := h.store.UpsertVerifiedEmail(ctx, u.ID, "alice@example.com"); err != nil {
		t.Fatal(err)
	}
	writeMessage(t, h.maildir, "geofail", goodMessageNoDKIM("alice@example.com", "<gf@x>"))
	if state := h.runUntilProcessed(t, "geofail", 5*time.Second); state != "removed" {
		t.Fatalf("expected removed despite geocode failure, got %s", state)
	}
	// The plan was still committed.
	trips, _ := h.store.ListTrips(ctx, u.ID)
	if len(trips) != 1 {
		t.Fatalf("expected the hotel plan to commit, trips = %d", len(trips))
	}
}
