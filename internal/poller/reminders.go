package poller

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/dpage/aerly/internal/api"
	"github.com/dpage/aerly/internal/mailer"
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

	// In-app: always (reuses the flight_alerts inbox with kind="reminder").
	p.publishReminder(d, label)

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
			StartTZ:   d.StartTZ,
		})
		if err := send(ctx, p.SendmailPath, p.MailFromAddress, msg); err != nil {
			slog.Error("reminder: send email", "to", d.Email, "part", d.PlanPartID, "err", err)
		}
	}

	if err := p.Store.MarkReminderSent(ctx, d.PlanPartID, d.UserID); err != nil {
		slog.Error("reminder: mark sent", "part", d.PlanPartID, "user", d.UserID, "err", err)
	}
}

// publishReminder persists an in-app alert row (kind="reminder") and pushes the
// user-private alert.created SSE event, reusing the same shape as a flight
// alert so the inbox renders it with no client change beyond nav branching.
func (p *Poller) publishReminder(d store.DueReminder, label string) {
	msg := mailer.PlanReminderSubject(label)
	stored, err := p.Store.InsertFlightAlert(context.Background(), store.FlightAlert{
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
		slog.Error("reminder: persist inbox row", "user", d.UserID, "err", err)
		return
	}
	dto := api.NotificationsDTO{Alert: ptrFlightAlertDTO(api.ToFlightAlertDTO(stored))}
	payload, err := json.Marshal(dto)
	if err != nil {
		slog.Error("reminder: marshal", "err", err)
		return
	}
	p.Hub.Publish(sseAlertEvent(d.UserID, payload))
}
