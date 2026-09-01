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

// remindCheckins dispatches flight check-in reminders (issue #119). It is a
// restart-safe DB-driven pass in the same shape as remindUpcoming: each tick it
// asks the store for due (flight part, user) pairs, filters them by plan
// visibility, sends on whichever channels the user has on, and marks the pair
// sent. It is independent of both the status-change alert path (maybeAlert) and
// the upcoming-plan reminders, and fires at most once per flight per user.
func (p *Poller) remindCheckins(ctx context.Context, now time.Time) {
	due, err := p.Store.DueCheckins(ctx, now)
	if err != nil {
		slog.Error("checkin: list due", "err", err)
		return
	}
	// Cache the visible-user set per plan so several legs of one plan cost one
	// query, as the reminder pass does.
	visCache := map[int64]map[int64]bool{}
	for _, d := range due {
		if ctx.Err() != nil {
			return
		}
		vis, ok := visCache[d.PlanID]
		if !ok {
			ids, verr := p.Store.VisiblePlanUserIDs(ctx, d.PlanID)
			if verr != nil {
				slog.Error("checkin: visibility", "plan", d.PlanID, "err", verr)
				continue
			}
			vis = make(map[int64]bool, len(ids))
			for _, id := range ids {
				vis[id] = true
			}
			visCache[d.PlanID] = vis
		}
		if !vis[d.UserID] {
			continue // a stale opt-in must not reach someone the plan is now hidden from
		}
		d := d
		guard("poller.checkin", d.PlanPartID, func() { p.dispatchCheckin(ctx, d) })
	}
}

// dispatchCheckin delivers one due check-in reminder on the channels the user
// has enabled, then marks it sent. MarkCheckinSent runs last so a crash
// mid-send re-sends rather than silently dropping, mirroring the reminder and
// alert paths.
func (p *Poller) dispatchCheckin(ctx context.Context, d store.DueCheckin) {
	// delivered tracks whether anything actually reached the traveller, so a
	// recipient whose only channel failed isn't marked as reminded.
	delivered := false

	// In-app: reuses the flight_alerts inbox with kind="checkin". A failed
	// insert (or a cancelled tick) bails before marking sent, so the reminder
	// is retried next tick rather than silently dropped.
	if d.InApp {
		if err := p.publishCheckin(ctx, d); err != nil {
			slog.Error("checkin: persist inbox row", "user", d.UserID, "part", d.PlanPartID, "err", err)
			return
		}
		delivered = true
	}

	// Email: only when mail is configured and the user has a verified address.
	emailable := d.Email && p.MailFromAddress != "" && d.EmailAddr != ""
	if emailable {
		send := p.SendAlertEmail
		if send == nil {
			send = mailer.Send
		}
		msg := mailer.BuildCheckinReminderEmail(mailer.CheckinReminderInput{
			FromAddr:  p.MailFromAddress,
			ToAddr:    d.EmailAddr,
			PublicURL: p.PublicURL,
			TripID:    d.TripID,
			Ident:     d.Ident,
			Route:     checkinRoute(d),
			StartsAt:  d.StartsAt,
			StartTZ:   partZone(d.StartTZ, d.OriginIATA, d.StartLat, d.StartLon),
		})
		if err := send(ctx, p.SendmailPath, p.MailFromAddress, msg); err != nil {
			// The recipient is identified by user id rather than address: the
			// pair already pins the failure, and the address is the traveller's
			// personal data, which has no business in an error log.
			slog.Error("checkin: send email", "user", d.UserID, "part", d.PlanPartID, "err", err)
		} else {
			delivered = true
		}
	}

	p.pushCheckin(ctx, d)

	// An email-only recipient whose send failed has had nothing at all, so
	// leave the pair unmarked and let the next tick try again. The retry is
	// bounded by the check-in window itself — once the flight departs the pair
	// stops being due — so a permanently bad address costs a day of attempts
	// rather than forever. A recipient who got the in-app reminder is marked
	// regardless: re-sending to catch a flaky sendmail would duplicate the row
	// they have already seen. Nothing attempted at all (every channel off) is
	// also marked, since there is nothing to retry.
	if !delivered && emailable {
		return
	}

	if err := p.Store.MarkCheckinSent(ctx, d.PlanPartID, d.UserID); err != nil {
		slog.Error("checkin: mark sent", "part", d.PlanPartID, "user", d.UserID, "err", err)
	}
}

// pushCheckin delivers the reminder to the recipient's subscribed devices as a
// Web Push, gated on their 'checkin' push-kind pref (default on, like the other
// kinds: granting push permission opts a user into every kind and they toggle
// individual ones off). Reaching here means the reminder is due and the user
// asked for check-in reminders at all, so it is worth pushing. Best-effort: a
// disabled Sender, a user with push off, or one with no subscriptions is a
// silent no-op, and the Sender itself never blocks or errors out of this path.
// Mirrors the change-alert path's pushAlert.
func (p *Poller) pushCheckin(ctx context.Context, d store.DueCheckin) {
	if p.Push == nil || !p.Push.Enabled() {
		return
	}
	on, err := p.Store.PushKindEnabled(ctx, d.UserID, "checkin")
	if err != nil {
		slog.Error("checkin: push kind pref", "user", d.UserID, "err", err)
		return
	}
	if !on {
		return
	}
	body := "Online check-in opens in five minutes"
	if route := checkinRoute(d); route != "" {
		body += " (" + route + ")"
	}
	p.Push.Send(ctx, []int64{d.UserID}, push.Payload{
		Title: "Check-in opens soon: " + d.Ident,
		Body:  body,
		// Deep-link to the flight's trip; the SW focuses/opens this on click.
		URL: fmt.Sprintf("/trips/%d", d.TripID),
		// One notification per flight; there is only ever one, but the tag
		// keeps a re-delivery from stacking.
		Tag:  "checkin-" + d.Ident,
		Kind: "checkin",
	})
}

// publishCheckin persists the in-app alert row (kind="checkin") and pushes the
// user-private alert.created SSE event, reusing the same shape as a flight
// alert so the inbox renders it with no client change. A marshal/publish hiccup
// after a successful insert is logged but not returned — the row is durable.
func (p *Poller) publishCheckin(ctx context.Context, d store.DueCheckin) error {
	stored, err := p.Store.InsertFlightAlert(ctx, store.FlightAlert{
		UserID:     d.UserID,
		PlanPartID: d.PlanPartID,
		PlanID:     d.PlanID,
		TripID:     d.TripID,
		Ident:      d.Ident,
		Kind:       "checkin",
		Status:     "checkin",
		Message:    mailer.CheckinReminderSubject(d.Ident),
	})
	if err != nil {
		return err
	}
	dto := api.NotificationsDTO{Alert: ptrFlightAlertDTO(api.ToFlightAlertDTO(stored))}
	payload, err := jsonMarshal(dto)
	if err != nil {
		slog.Error("checkin: marshal", "err", err)
		return nil
	}
	p.Hub.Publish(sseAlertEvent(d.UserID, payload))
	return nil
}

// checkinRoute renders the leg as "LHR → LIS" so a traveller with two flights
// the same day can tell which one just opened. "" when either end is unknown.
func checkinRoute(d store.DueCheckin) string {
	if d.OriginIATA == "" || d.DestIATA == "" {
		return ""
	}
	return d.OriginIATA + " → " + d.DestIATA
}
