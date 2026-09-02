package poller

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/dpage/aerly/internal/api"
	"github.com/dpage/aerly/internal/mailer"
	"github.com/dpage/aerly/internal/push"
	"github.com/dpage/aerly/internal/store"
)

// remindUpcoming dispatches upcoming-plan reminders (issue #11). It is a
// restart-safe DB-driven pass: each tick it asks the store for due
// (part, user) reminders, filters them by plan visibility, then sends email +
// an in-app alert and marks the pair sent. It is wholly independent of the
// flight-status-change alert path (maybeAlert) and of alert_prefs.
func (p *Poller) remindUpcoming(ctx context.Context, now time.Time) {
	due, err := p.Store.DueReminders(ctx, now)
	if err != nil {
		slog.Error("reminder: list due", "err", err)
		return
	}
	// Cache the visible-user set per plan so N parts of one plan cost one query.
	visCache := map[int64]map[int64]bool{}
	for _, d := range due {
		if ctx.Err() != nil {
			return
		}
		vis, ok := visCache[d.PlanID]
		if !ok {
			ids, verr := p.Store.VisiblePlanUserIDs(ctx, d.PlanID)
			if verr != nil {
				slog.Error("reminder: visibility", "plan", d.PlanID, "err", verr)
				continue
			}
			vis = make(map[int64]bool, len(ids))
			for _, id := range ids {
				vis[id] = true
			}
			visCache[d.PlanID] = vis
		}
		if !vis[d.UserID] {
			continue // a trip-level opt-in must not leak a plan hidden from this viewer
		}
		d := d
		guard("poller.remind", d.PlanPartID, func() { p.dispatchReminder(ctx, d) })
	}
}

// dispatchReminder sends the email + in-app reminder for one due pair, then
// marks it sent. MarkReminderSent runs last so a crash mid-send re-sends rather
// than silently dropping (mirrors the dedupe-sig ordering in alerts.go).
func (p *Poller) dispatchReminder(ctx context.Context, d store.DueReminder) {
	label := mailer.PlanReminderLabel(d.PlanType, d.PlanTitle, d.Ident)

	// In-app: always (reuses the flight_alerts inbox with kind="reminder"). On a
	// failed insert (or a cancelled tick) bail before marking sent, so the
	// reminder is retried next tick rather than silently dropped.
	if err := p.publishReminder(ctx, d, label); err != nil {
		slog.Error("reminder: persist inbox row", "user", d.UserID, "part", d.PlanPartID, "err", err)
		return
	}

	zone := partZone(d.StartTZ, d.OriginIATA, d.StartLat, d.StartLon)

	// Email: only when mail is configured and the user has a verified address.
	if p.MailFromAddress != "" && d.Email != "" {
		send := p.SendAlertEmail
		if send == nil {
			send = mailer.Send
		}
		msg := mailer.BuildPlanReminderEmail(mailer.PlanReminderInput{
			FromAddr:  p.MailFromAddress,
			ToAddr:    d.Email,
			PublicURL: p.PublicURL,
			TripID:    d.TripID,
			Label:     label,
			StartsAt:  d.StartsAt,
			StartTZ:   zone,
		})
		if err := send(ctx, p.SendmailPath, p.MailFromAddress, msg); err != nil {
			// Identified by user id rather than address: the pair already pins
			// the failure, and the address is the traveller's personal data.
			slog.Error("reminder: send email", "user", d.UserID, "part", d.PlanPartID, "err", err)
		}
	}

	// Marked before the push, not after: the push echoes a reminder the in-app
	// row has already delivered, so a slow push service must not hold the mark
	// open and let a restart in that gap re-send what the traveller has read.
	if err := p.Store.MarkReminderSent(ctx, d.PlanPartID, d.UserID); err != nil {
		slog.Error("reminder: mark sent", "part", d.PlanPartID, "user", d.UserID, "err", err)
	}

	p.pushReminder(ctx, d, label, zone)
}

// pushReminder delivers the reminder to the recipient's subscribed devices as a
// Web Push, gated on their 'reminder' push-kind pref (default on, like every
// other kind). Reaching here means the user opted the trip or plan in, so the
// reminder is wanted; this only decides whether it also reaches their phone.
// Best-effort: a disabled Sender, a user with push off, or one with no
// subscriptions is a silent no-op, and the Sender never blocks this path.
func (p *Poller) pushReminder(ctx context.Context, d store.DueReminder, label, zone string) {
	if p.Push == nil || !p.Push.Enabled() {
		return
	}
	on, err := p.Store.PushKindEnabled(ctx, d.UserID, "reminder")
	if err != nil {
		slog.Error("reminder: push kind pref", "user", d.UserID, "err", err)
		return
	}
	if !on {
		return
	}
	p.Push.Send(ctx, []int64{d.UserID}, push.Payload{
		Title: mailer.PlanReminderSubject(label),
		Body:  "Starts " + mailer.LocalTime(d.StartsAt, zone),
		// Deep-link to the plan's trip; the SW focuses/opens this on click.
		URL: fmt.Sprintf("/trips/%d", d.TripID),
		// One notification per part, so a re-delivery replaces rather than
		// stacks on the one already on screen.
		Tag:  fmt.Sprintf("reminder-%d", d.PlanPartID),
		Kind: "reminder",
	})
}

// publishReminder persists an in-app alert row (kind="reminder") and pushes the
// user-private alert.created SSE event, reusing the same shape as a flight
// alert so the inbox renders it with no client change beyond nav branching.
// publishReminder persists the in-app alert row and pushes the SSE event,
// returning any insert error so the caller can skip marking the reminder sent
// (preserving dedupe/retry). A marshal/publish hiccup after a successful insert
// is logged but not returned — the row is already durable.
func (p *Poller) publishReminder(ctx context.Context, d store.DueReminder, label string) error {
	msg := mailer.PlanReminderSubject(label)
	stored, err := p.Store.InsertFlightAlert(ctx, store.FlightAlert{
		UserID:     d.UserID,
		PlanPartID: d.PlanPartID,
		PlanID:     d.PlanID,
		TripID:     d.TripID,
		Ident:      label, // not a flight ident for non-flights; carries the label
		Kind:       "reminder",
		Status:     d.PlanType,
		Message:    msg,
	})
	if err != nil {
		return err
	}
	dto := api.NotificationsDTO{Alert: ptrFlightAlertDTO(api.ToFlightAlertDTO(stored))}
	payload, err := jsonMarshal(dto)
	if err != nil {
		slog.Error("reminder: marshal", "err", err)
		return nil
	}
	p.Hub.Publish(sseAlertEvent(d.UserID, payload))
	return nil
}
