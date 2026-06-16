package poller

import (
	"context"
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
