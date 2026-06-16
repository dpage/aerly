package poller

import (
	"context"
	"errors"
	"log/slog"
	"net/mail"
	"strings"
	"sync"
	"time"

	"github.com/dpage/aerly/internal/mailer"
)

// defaultQuotaAlertCooldown bounds how often the admins are emailed about the
// same upstream provider hitting its rate limit / quota. The poll loop calls
// Track/Resolve for every active flight every tick, so a sustained 429 would
// otherwise fan out one email per flight per tick. One heads-up per provider
// per hour is enough to act on without burying the inbox; the limit usually
// clears well within that window anyway.
const defaultQuotaAlertCooldown = time.Hour

// SuperuserEmailLister is the slice of the store the QuotaNotifier needs: the
// verified addresses of the admins who should hear about quota problems.
// *store.Store satisfies it via SuperuserEmails.
type SuperuserEmailLister interface {
	SuperuserEmails(ctx context.Context) ([]string, error)
}

// QuotaNotifier emails the admins (superusers with a verified address) when an
// upstream data provider rejects a request for rate-limit / quota reasons. It
// is wired as the providers' OnRateLimit hook in main.go, so it fires even
// though the tracker layer (DeadReckoner) swallows the 429 to fall back to
// dead-reckoning.
//
// Notify is safe for concurrent use and self-throttles per provider, so a
// sustained quota exhaustion produces at most one email per Cooldown. The
// whole thing is a no-op until MailFromAddress is configured — matching the
// rest of Aerly's optional outbound-mail flows.
type QuotaNotifier struct {
	Store           SuperuserEmailLister
	MailFromAddress string
	SendmailPath    string
	PublicURL       string
	// Send defaults to mailer.Send; tests override it to capture messages.
	Send func(ctx context.Context, sendmailPath, envelopeSender, message string) error
	// Cooldown is the per-provider quiet period; zero means
	// defaultQuotaAlertCooldown.
	Cooldown time.Duration
	// Now defaults to time.Now; overridable in tests to drive the cooldown.
	Now func() time.Time

	mu       sync.Mutex
	lastSent map[string]time.Time
}

// Notify is the providers.RateLimitReporter hook. provider is a short label
// ("OpenSky", "AeroDataBox"); detail is a human phrase for the alert body. It
// returns immediately: the throttle is checked synchronously and the email, if
// due, is sent on a detached goroutine so a poll (or an Add Flight request)
// never blocks on the sendmail pipe.
func (n *QuotaNotifier) Notify(provider, detail string) {
	if n == nil || n.MailFromAddress == "" || n.Store == nil {
		return
	}
	if !n.due(provider) {
		return
	}
	go n.dispatch(provider, detail)
}

// due reports whether the cooldown for provider has elapsed, stamping the send
// time when it has. Stamping at the gate (rather than after a successful send)
// means a provider with nobody to notify, or a transient send failure, still
// can't spin the loop into a per-tick email storm.
func (n *QuotaNotifier) due(provider string) bool {
	cooldown := n.Cooldown
	if cooldown <= 0 {
		cooldown = defaultQuotaAlertCooldown
	}
	now := n.now()
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.lastSent == nil {
		n.lastSent = map[string]time.Time{}
	}
	if last, ok := n.lastSent[provider]; ok && now.Sub(last) < cooldown {
		return false
	}
	n.lastSent[provider] = now
	return true
}

func (n *QuotaNotifier) now() time.Time {
	if n.Now != nil {
		return n.Now()
	}
	return time.Now()
}

// dispatch resolves the admin recipients and mails each one. Best-effort:
// every failure is logged and swallowed so a mail problem never propagates
// into the caller.
func (n *QuotaNotifier) dispatch(provider, detail string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	recips, err := n.Store.SuperuserEmails(ctx)
	if err != nil {
		slog.Error("quota alert: list superuser emails", "provider", provider, "err", err)
		return
	}
	if len(recips) == 0 {
		slog.Warn("quota alert: provider rate-limited but no admin email on file",
			"provider", provider, "detail", detail)
		return
	}
	if err := validateHeaderAddress(n.MailFromAddress); err != nil {
		slog.Warn("quota alert: invalid MAIL_FROM_ADDRESS, skipping",
			"err", err, "provider", provider)
		return
	}

	send := n.Send
	if send == nil {
		send = mailer.Send
	}
	occurred := n.now()
	slog.Warn("quota alert: provider rate-limited, notifying admins",
		"provider", provider, "detail", detail, "recipients", len(recips))
	for _, to := range recips {
		if err := validateHeaderAddress(to); err != nil {
			slog.Warn("quota alert: skipping invalid recipient", "err", err, "provider", provider)
			continue
		}
		msg := mailer.BuildQuotaAlertEmail(mailer.QuotaAlertInput{
			FromAddr:   n.MailFromAddress,
			ToAddr:     to,
			PublicURL:  n.PublicURL,
			Provider:   provider,
			Detail:     detail,
			OccurredAt: occurred,
		})
		if err := send(ctx, n.SendmailPath, n.MailFromAddress, msg); err != nil {
			slog.Error("quota alert: send email", "to", to, "provider", provider, "err", err)
		}
	}
}

// validateHeaderAddress rejects values that would break RFC822 framing
// (embedded CR/LF can inject extra headers) or that aren't parseable as a mail
// address. MailFromAddress is operator config and recipients come from a
// verified user_emails row, but validating both is cheap insurance against a
// header-injection bug.
func validateHeaderAddress(v string) error {
	if strings.ContainsAny(v, "\r\n") {
		return errors.New("address contains CR/LF")
	}
	if _, err := mail.ParseAddress(v); err != nil {
		return err
	}
	return nil
}
