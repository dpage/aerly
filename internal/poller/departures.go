package poller

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/dpage/aerly/internal/airports"
	"github.com/dpage/aerly/internal/api"
	"github.com/dpage/aerly/internal/mailer"
	"github.com/dpage/aerly/internal/push"
	"github.com/dpage/aerly/internal/store"
	"github.com/dpage/aerly/internal/timing"
	"github.com/dpage/aerly/internal/weather"
)

// forecastDays is how many days of destination weather a departure summary
// carries, starting from the arrival date.
const forecastDays = 5

// forecastTimeout bounds each forecast fetch so a slow provider cannot stall
// the poll loop; the summary then goes out without weather.
// A variable only so a test can shorten it.
var forecastTimeout = 5 * time.Second

// remindDepartures dispatches flight departure summaries (issue #130), four
// hours before each leg. It is the check-in reminder pass (remindCheckins)
// with different content: the same due query shape, the same visibility
// filter and the same once-per-(leg, user) marking, but each summary is built
// from the leg's live status, gate and the destination forecast at dispatch.
func (p *Poller) remindDepartures(ctx context.Context, now time.Time) {
	due, err := p.Store.DueDepartureSummaries(ctx, now)
	if err != nil {
		slog.Error("departure: list due", "err", err)
		return
	}
	// Cache the visible-user set per plan so several legs of one plan cost one
	// query, as the check-in pass does.
	visCache := map[int64]map[int64]bool{}
	for _, d := range due {
		if ctx.Err() != nil {
			return
		}
		vis, ok := visCache[d.PlanID]
		if !ok {
			ids, verr := p.Store.VisiblePlanUserIDs(ctx, d.PlanID)
			if verr != nil {
				slog.Error("departure: visibility", "plan", d.PlanID, "err", verr)
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
		guard("poller.departure", d.PlanPartID, func() { p.dispatchDeparture(ctx, d) })
	}
}

// departureInput builds the summary from the leg's live flight data and its
// plan's supplier contact. ok is false when either lookup fails, in which case
// the caller skips the pair unmarked so the next tick tries again. Weather is
// fetched here too but never fails the build: without it the summary simply
// leaves the forecast out.
func (p *Poller) departureInput(ctx context.Context, d store.DueDepartureSummary) (mailer.DepartureSummaryInput, bool) {
	f, err := p.Store.FlightPartByID(ctx, d.PlanPartID)
	if err != nil {
		slog.Error("departure: load flight", "part", d.PlanPartID, "err", err)
		return mailer.DepartureSummaryInput{}, false
	}
	plan, err := p.Store.PlanByID(ctx, d.PlanID)
	if err != nil {
		slog.Error("departure: load plan", "plan", d.PlanID, "err", err)
		return mailer.DepartureSummaryInput{}, false
	}

	t := timing.Effective(f.Times())
	departs, arrives := t.Departure.At, arrivalAt(t)

	destLabel := f.DestIATA
	if city, ok := airports.LookupCity(f.DestIATA); ok {
		destLabel = city
	}
	destTZ := airportZone(f.DestIATA, f.DestLat, f.DestLon)

	route := ""
	if f.OriginIATA != "" && f.DestIATA != "" {
		route = f.OriginIATA + " → " + f.DestIATA
	}

	// The forecast starts on the day the traveller lands, in the destination's
	// own calendar; a leg with no known arrival uses its departure instead.
	landing := departs
	if arrives != nil {
		landing = *arrives
	}

	return mailer.DepartureSummaryInput{
		FromAddr:       p.MailFromAddress,
		ToAddr:         d.EmailAddr,
		PublicURL:      p.PublicURL,
		TripID:         d.TripID,
		Ident:          f.Ident,
		Route:          route,
		DestLabel:      destLabel,
		DestIATA:       f.DestIATA,
		ScheduledOut:   f.ScheduledOut,
		DepartsAt:      departs,
		DepartureDelay: t.Departure.Delay,
		OriginTZ:       partZone(d.StartTZ, f.OriginIATA, f.OriginLat, f.OriginLon),
		ArrivesAt:      arrives,
		DestTZ:         destTZ,
		Terminal:       f.OriginTerminal,
		Gate:           f.OriginGate,
		SupplierName:   plan.SupplierName,
		ContactPhone:   plan.ContactPhone,
		Forecast:       p.departureForecast(ctx, f, destTZ, landing),
		ForecastPlace:  destLabel,
	}, true
}

// arrivalAt is the effective arrival time, or nil when none is known.
func arrivalAt(t timing.Times) *time.Time {
	if t.Arrival == nil {
		return nil
	}
	return &t.Arrival.At
}

// departureForecast fetches the destination forecast for forecastDays days
// from the landing date in the destination zone. It returns nil, and the
// summary goes out without weather, when no forecaster is wired, the
// destination's coordinates are unknown, or the provider fails.
func (p *Poller) departureForecast(ctx context.Context, f *store.Flight, destTZ string, landing time.Time) []mailer.ForecastDay {
	if p.Weather == nil {
		return nil
	}
	// The airport table first, as for the zone, then whatever coordinate the
	// row carries for an airport the table doesn't name.
	lat, lon, ok := airports.Lookup(f.DestIATA)
	if !ok {
		if f.DestLat == nil || f.DestLon == nil {
			return nil
		}
		lat, lon = *f.DestLat, *f.DestLon
	}
	local := inZone(landing, destTZ)
	from := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
	fctx, cancel := context.WithTimeout(ctx, forecastTimeout)
	defer cancel()
	days, err := p.Weather.Forecast(fctx, lat, lon, destTZ, from, forecastDays)
	if err != nil {
		slog.Warn("departure: forecast", "part", f.ID, "err", err)
		return nil
	}
	out := make([]mailer.ForecastDay, 0, len(days))
	for _, day := range days {
		// A civil date at UTC midnight: the mailer formats it in the Date's own
		// location, so UTC keeps the day from shifting.
		date, perr := time.Parse("2006-01-02", day.Date)
		if perr != nil {
			slog.Warn("departure: forecast date", "part", f.ID, "date", day.Date, "err", perr)
			continue
		}
		text, emoji := weather.Describe(day.Code)
		out = append(out, mailer.ForecastDay{
			Date:      date,
			Text:      text,
			Emoji:     emoji,
			MaxC:      day.MaxC,
			MinC:      day.MinC,
			PrecipPct: day.PrecipPct,
		})
	}
	return out
}

// dispatchDeparture delivers one due departure summary on the channels the
// user has enabled, then marks it sent. The retry and mark-sent ordering is
// dispatchCheckin's, for the same reasons; see the comments there.
func (p *Poller) dispatchDeparture(ctx context.Context, d store.DueDepartureSummary) {
	in, ok := p.departureInput(ctx, d)
	if !ok {
		return
	}
	delivered := false

	if d.InApp {
		if err := p.publishDeparture(ctx, d, in); err != nil {
			slog.Error("departure: persist inbox row", "user", d.UserID, "part", d.PlanPartID, "err", err)
			return
		}
		delivered = true
	}

	emailable := d.Email && p.MailFromAddress != "" && d.EmailAddr != ""
	if emailable {
		send := p.SendAlertEmail
		if send == nil {
			send = mailer.Send
		}
		if err := send(ctx, p.SendmailPath, p.MailFromAddress, mailer.BuildDepartureSummaryEmail(in)); err != nil {
			// Identified by user id, not address, as in dispatchCheckin.
			slog.Error("departure: send email", "user", d.UserID, "part", d.PlanPartID, "err", err)
		} else {
			delivered = true
		}
	}

	// An email-only recipient whose send failed stays unmarked and retries
	// until departure, when the pair stops being due.
	if !delivered && emailable {
		return
	}

	// Marked before the best-effort push, and a failed mark skips the push
	// that the retry would otherwise repeat.
	if err := p.Store.MarkDepartureSummarySent(ctx, d.PlanPartID, d.UserID); err != nil {
		slog.Error("departure: mark sent", "part", d.PlanPartID, "user", d.UserID, "err", err)
		return
	}

	p.pushDeparture(ctx, d, in)
}

// pushDeparture sends the summary to the recipient's subscribed devices,
// gated on their 'departure' push-kind pref (default on). Best-effort and
// silent when push is off, exactly as pushCheckin.
func (p *Poller) pushDeparture(ctx context.Context, d store.DueDepartureSummary, in mailer.DepartureSummaryInput) {
	if p.Push == nil || !p.Push.Enabled() {
		return
	}
	on, err := p.Store.PushKindEnabled(ctx, d.UserID, "departure")
	if err != nil {
		slog.Error("departure: push kind pref", "user", d.UserID, "err", err)
		return
	}
	if !on {
		return
	}
	body := mailer.DepartureSummaryHeadline(in)
	if w := mailer.DepartureSummaryWeatherLine(in); w != "" {
		body += " " + w
	}
	p.Push.Send(ctx, []int64{d.UserID}, push.Payload{
		Title: mailer.DepartureSummarySubject(in.Ident, in.DestLabel),
		Body:  body,
		URL:   fmt.Sprintf("/trips/%d", d.TripID),
		Tag:   "departure-" + in.Ident,
		Kind:  "departure",
	})
}

// publishDeparture persists the in-app row (kind and status "departure", the
// headline as its message) and pushes the user-private alert.created SSE
// event. As with publishCheckin, a marshal hiccup after the insert is logged
// rather than returned, since the row is durable.
func (p *Poller) publishDeparture(ctx context.Context, d store.DueDepartureSummary, in mailer.DepartureSummaryInput) error {
	stored, err := p.Store.InsertFlightAlert(ctx, store.FlightAlert{
		UserID:     d.UserID,
		PlanPartID: d.PlanPartID,
		PlanID:     d.PlanID,
		TripID:     d.TripID,
		Ident:      in.Ident,
		Kind:       "departure",
		Status:     "departure",
		Message:    mailer.DepartureSummaryHeadline(in),
	})
	if err != nil {
		return err
	}
	dto := api.NotificationsDTO{Alert: ptrFlightAlertDTO(api.ToFlightAlertDTO(stored))}
	payload, err := jsonMarshal(dto)
	if err != nil {
		slog.Error("departure: marshal", "err", err)
		return nil
	}
	p.Hub.Publish(sseAlertEvent(d.UserID, payload))
	return nil
}
