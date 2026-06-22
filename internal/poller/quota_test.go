package poller

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeSuperusers is a stand-in for the store's SuperuserEmails query.
type fakeSuperusers struct {
	emails []string
	err    error
}

func (f fakeSuperusers) SuperuserEmails(context.Context) ([]string, error) {
	return f.emails, f.err
}

func TestQuotaNotifier_DueThrottlesPerProvider(t *testing.T) {
	now := time.Now()
	n := &QuotaNotifier{
		MailFromAddress: "alerts@aerly.test",
		Cooldown:        time.Hour,
		Now:             func() time.Time { return now },
	}

	if !n.due("OpenSky") {
		t.Fatal("first OpenSky call should be due")
	}
	if n.due("OpenSky") {
		t.Fatal("second OpenSky call within cooldown should be throttled")
	}
	// A different provider has its own window.
	if !n.due("AeroDataBox") {
		t.Fatal("first AeroDataBox call should be due")
	}

	// Advance past the cooldown; OpenSky becomes due again.
	now = now.Add(time.Hour + time.Minute)
	if !n.due("OpenSky") {
		t.Fatal("OpenSky should be due again after the cooldown elapsed")
	}
}

func TestQuotaNotifier_DispatchMailsEveryAdmin(t *testing.T) {
	var (
		mu   sync.Mutex
		sent []string
	)
	n := &QuotaNotifier{
		Store:           fakeSuperusers{emails: []string{"a@aerly.test", "b@aerly.test"}},
		MailFromAddress: "alerts@aerly.test",
		PublicURL:       "http://localhost:8080",
		Send: func(_ context.Context, _, from, msg string) error {
			if from != "alerts@aerly.test" {
				t.Errorf("envelope sender = %q, want alerts@aerly.test", from)
			}
			mu.Lock()
			sent = append(sent, msg)
			mu.Unlock()
			return nil
		},
	}

	n.dispatch("OpenSky", "free-tier rate limit reached")

	mu.Lock()
	defer mu.Unlock()
	if len(sent) != 2 {
		t.Fatalf("sent %d messages, want 2", len(sent))
	}
	toA := strings.Contains(sent[0], "To: a@aerly.test") || strings.Contains(sent[1], "To: a@aerly.test")
	toB := strings.Contains(sent[0], "To: b@aerly.test") || strings.Contains(sent[1], "To: b@aerly.test")
	if !toA || !toB {
		t.Errorf("expected one message per admin; got:\n%s\n%s", sent[0], sent[1])
	}
	for _, m := range sent {
		if !strings.Contains(m, "Aerly hit a rate limit on OpenSky") {
			t.Errorf("message missing subject:\n%s", m)
		}
		if !strings.Contains(m, "free-tier rate limit reached") {
			t.Errorf("message missing detail:\n%s", m)
		}
	}
}

func TestQuotaNotifier_DispatchNoRecipientsIsQuiet(t *testing.T) {
	called := false
	n := &QuotaNotifier{
		Store:           fakeSuperusers{emails: nil},
		MailFromAddress: "alerts@aerly.test",
		Send: func(context.Context, string, string, string) error {
			called = true
			return nil
		},
	}
	n.dispatch("OpenSky", "x") // must not panic, must not send
	if called {
		t.Error("dispatch sent mail with no recipients")
	}
}

func TestQuotaNotifier_NotifyNoopWithoutMailFrom(t *testing.T) {
	called := false
	n := &QuotaNotifier{
		Store: fakeSuperusers{emails: []string{"a@aerly.test"}},
		Send: func(context.Context, string, string, string) error {
			called = true
			return nil
		},
	}
	n.Notify("OpenSky", "x") // MailFromAddress empty → no-op, no goroutine
	if called {
		t.Error("Notify dispatched despite empty MailFromAddress")
	}
	// Nil receiver must also be safe (mirrors the optional-mail pattern).
	var nilN *QuotaNotifier
	nilN.Notify("OpenSky", "x")
}

func TestQuotaNotifier_NotifyNoopWithoutStore(t *testing.T) {
	n := &QuotaNotifier{
		MailFromAddress: "alerts@aerly.test",
		// Store left nil — Notify must short-circuit rather than launch a
		// dispatch goroutine that would dereference a nil Store and panic.
	}
	n.Notify("OpenSky", "x")
	// A nil Store must also leave the cooldown untouched, so a later call once
	// a Store is wired isn't wrongly throttled.
	if _, ok := n.lastSent["OpenSky"]; ok {
		t.Error("Notify stamped the cooldown despite a nil Store")
	}
}

// TestQuotaNotifier_NotifyDispatchesWhenDue drives the happy Notify path: a due
// provider must launch the detached dispatch goroutine, which resolves the
// recipients and mails them. We wait on the captured send rather than sleeping.
func TestQuotaNotifier_NotifyDispatchesWhenDue(t *testing.T) {
	got := make(chan string, 1)
	n := &QuotaNotifier{
		Store:           fakeSuperusers{emails: []string{"admin@aerly.test"}},
		MailFromAddress: "alerts@aerly.test",
		PublicURL:       "http://localhost:8080",
		Send: func(_ context.Context, _, _, msg string) error {
			got <- msg
			return nil
		},
	}

	n.Notify("AeroDataBox", "monthly quota exhausted")

	select {
	case msg := <-got:
		if !strings.Contains(msg, "AeroDataBox") {
			t.Errorf("dispatched message missing provider:\n%s", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Notify did not dispatch a quota email for a due provider")
	}
	// A second call within the (default) cooldown is throttled: no further send.
	n.Notify("AeroDataBox", "still exhausted")
	select {
	case <-got:
		t.Error("second Notify within the cooldown should have been throttled")
	case <-time.After(150 * time.Millisecond):
	}
}

// TestQuotaNotifier_DueDefaultCooldown covers the cooldown<=0 branch in due():
// with no explicit Cooldown, defaultQuotaAlertCooldown applies, so a second call
// moments later is throttled but one an hour-and-change later is due again.
func TestQuotaNotifier_DueDefaultCooldown(t *testing.T) {
	now := time.Now()
	n := &QuotaNotifier{
		MailFromAddress: "alerts@aerly.test",
		// Cooldown deliberately left zero → defaultQuotaAlertCooldown (1h).
		Now: func() time.Time { return now },
	}
	if !n.due("OpenSky") {
		t.Fatal("first call should be due under the default cooldown")
	}
	if n.due("OpenSky") {
		t.Fatal("second immediate call should be throttled under the default cooldown")
	}
	now = now.Add(defaultQuotaAlertCooldown + time.Minute)
	if !n.due("OpenSky") {
		t.Fatal("call past the default cooldown should be due again")
	}
}

// TestQuotaNotifier_DispatchSuperuserEmailsError covers the list-failed branch:
// when SuperuserEmails errors, dispatch logs and bails without sending.
func TestQuotaNotifier_DispatchSuperuserEmailsError(t *testing.T) {
	called := false
	n := &QuotaNotifier{
		Store:           fakeSuperusers{err: errors.New("db down")},
		MailFromAddress: "alerts@aerly.test",
		Send: func(context.Context, string, string, string) error {
			called = true
			return nil
		},
	}
	n.dispatch("OpenSky", "x")
	if called {
		t.Error("dispatch sent mail despite the recipient lookup failing")
	}
}

// TestQuotaNotifier_DispatchInvalidMailFromSkips covers the validateHeaderAddress
// guard on the envelope sender: a MAIL_FROM with embedded CR/LF is rejected
// before any recipient is mailed.
func TestQuotaNotifier_DispatchInvalidMailFromSkips(t *testing.T) {
	called := false
	n := &QuotaNotifier{
		Store:           fakeSuperusers{emails: []string{"admin@aerly.test"}},
		MailFromAddress: "alerts@aerly.test\r\nBcc: evil@aerly.test",
		Send: func(context.Context, string, string, string) error {
			called = true
			return nil
		},
	}
	n.dispatch("OpenSky", "x")
	if called {
		t.Error("dispatch sent mail despite an invalid MAIL_FROM_ADDRESS")
	}
}

// TestQuotaNotifier_DispatchSkipsInvalidRecipient covers the per-recipient
// validateHeaderAddress guard and the send-error branch: a header-injecting
// recipient is skipped, and a Send that errors for the valid one is logged and
// swallowed (dispatch still returns cleanly).
func TestQuotaNotifier_DispatchSkipsInvalidRecipient(t *testing.T) {
	var (
		mu   sync.Mutex
		sent []string
	)
	n := &QuotaNotifier{
		Store: fakeSuperusers{emails: []string{
			"bad@aerly.test\r\nBcc: evil@aerly.test", // invalid → skipped
			"good@aerly.test",                        // valid → attempted (errors)
		}},
		MailFromAddress: "alerts@aerly.test",
		Send: func(_ context.Context, _, _, msg string) error {
			mu.Lock()
			sent = append(sent, msg)
			mu.Unlock()
			return errors.New("sendmail pipe broke")
		},
	}
	n.dispatch("OpenSky", "x")
	mu.Lock()
	defer mu.Unlock()
	if len(sent) != 1 {
		t.Fatalf("expected exactly one send attempt (invalid recipient skipped), got %d", len(sent))
	}
	if !strings.Contains(sent[0], "To: good@aerly.test") {
		t.Errorf("the send attempt should target the valid recipient:\n%s", sent[0])
	}
}

// TestQuotaNotifier_DispatchDefaultSendNoMailFrom: with Send left nil, dispatch
// falls back to mailer.Send. We can't capture a real sendmail invocation, but a
// recipient-lookup that yields nobody exercises the Send==nil default-resolution
// only when there is someone to mail; to drive the default-send line without a
// live sendmail we point SendmailPath at /bin/true (a sendmail that accepts and
// discards), so mailer.Send runs end-to-end without error.
func TestQuotaNotifier_DispatchDefaultSendNoMailFrom(t *testing.T) {
	n := &QuotaNotifier{
		Store:           fakeSuperusers{emails: []string{"admin@aerly.test"}},
		MailFromAddress: "alerts@aerly.test",
		SendmailPath:    "/bin/true", // a no-op sendmail: accepts on stdin, exits 0
		PublicURL:       "http://localhost:8080",
		// Send left nil → dispatch must default to mailer.Send.
	}
	n.dispatch("OpenSky", "x") // must not panic and must reach the default send
}

func TestValidateHeaderAddress(t *testing.T) {
	if err := validateHeaderAddress("ok@aerly.test"); err != nil {
		t.Errorf("a plain address should validate: %v", err)
	}
	if err := validateHeaderAddress("a@aerly.test\r\nBcc: x@aerly.test"); err == nil {
		t.Error("an address with CR/LF should be rejected (header injection)")
	}
	if err := validateHeaderAddress("not-an-address"); err == nil {
		t.Error("an unparseable address should be rejected")
	}
}
