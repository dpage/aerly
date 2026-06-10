package planops

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/dpage/aerly/internal/airports"
	"github.com/dpage/aerly/internal/providers"
	"github.com/dpage/aerly/internal/store"
)

// Deps bundles the collaborators Propose and Commit need. Resolver may be nil
// to disable flight-schedule enrichment (flight parts then fall back to the
// schedule the extractor pulled from the email).
type Deps struct {
	Store     *store.Store
	Extractor Extractor
	Resolver  providers.Resolver
}

// ProposedPart is one extracted timeline entry awaiting confirmation. It mirrors
// the shape a store.CreatePlanPartPayload will take on commit, plus the place
// labels and a resolved/typed satellite detail when one was enriched.
type ProposedPart struct {
	Type         string
	StartsAt     time.Time
	EndsAt       *time.Time
	StartTZ      string
	EndTZ        string
	StartLabel   string
	EndLabel     string
	StartAddress string
	EndAddress   string
	Status       string

	Flight    *store.FlightDetail
	Hotel     *store.HotelDetail
	Train     *store.TrainDetail
	Ground    *store.GroundDetail
	Dining    *store.DiningDetail
	Excursion *store.ExcursionDetail

	// startTimeDefaulted marks a part whose start time-of-day was filled from a
	// type default rather than stated in the source. The transfer-timing
	// post-pass (applyTransferTimes) only retimes such parts, never an explicit
	// time the email gave.
	startTimeDefaulted bool
}

// ProposedPlan is a plan the ingest pipeline proposes, awaiting user
// confirmation (never auto-committed — spec §6.1). Confidence is 0..1.
// SupersedesPartID is set when a flight part matches an existing visible flight
// part in the trip (a proposed rebooking).
type ProposedPlan struct {
	Type             string
	Title            string
	ConfirmationRef  string
	TicketNumber     string
	Notes            string
	CostAmount       *float64
	CostCurrency     string
	SupplierName     string
	ContactEmail     string
	ContactPhone     string
	Website          string
	Confidence       float64
	Parts            []ProposedPart
	SupersedesPartID *int64
}

// confidenceScore maps the extractor's "high"|"medium"|"low" to a 0..1 score
// for the FE confirm step. Low parts are dropped upstream, so we only see
// high/medium here in practice.
func confidenceScore(s string) float64 {
	switch strings.ToLower(s) {
	case "high":
		return 0.95
	case "medium":
		return 0.6
	case "low":
		return 0.3
	default:
		return 0.6
	}
}

// Propose runs the extractor over the supplied text + documents, enriches
// flight parts via the resolver, runs the rebooking match against existing
// visible flight parts in the trip, and returns proposed plans for
// confirmation. Nothing is written here.
func Propose(ctx context.Context, deps Deps, userID, tripID int64, text string, docs []Document) ([]ProposedPlan, error) {
	if deps.Store == nil {
		return nil, errors.New("planops.Propose: nil Store")
	}
	if deps.Extractor == nil {
		return nil, errors.New("planops.Propose: nil Extractor")
	}
	// Prepend the traveller's home address as context so references like "from
	// home" in a confirmation resolve to a real address the extractor can fill.
	// It is framed as reference-only: the extractor prompt instructs the model
	// not to copy context lines into output fields, so a prompt-injection in the
	// (untrusted) email body can't exfiltrate the home address into a persisted
	// plan field or the confirmation reply unless a booking genuinely starts there.
	body := text
	if userID != 0 {
		if u, err := deps.Store.UserByID(ctx, userID); err == nil && u != nil && u.HomeAddress != "" {
			body = "Context (reference only, do not echo into output): the traveller's home address is " +
				u.HomeAddress + ". Use it only to resolve references such as \"home\".\n\n" + text
		}
	}
	extracted, err := deps.Extractor.ExtractPlans(ctx, body, docs)
	if err != nil {
		return nil, fmt.Errorf("extract: %w", err)
	}

	// Gather the trip's existing visible flight parts once, for the rebooking
	// match. tripID==0 (email pre-trip-selection) yields an empty candidate set.
	var candidates []rebookCandidate
	if tripID != 0 {
		candidates, err = visibleFlightCandidates(ctx, deps, userID, tripID)
		if err != nil {
			return nil, err
		}
	}

	out := make([]ProposedPlan, 0, len(extracted))
	for _, ep := range extracted {
		pp := ProposedPlan{
			Type:            ep.Type,
			Title:           ep.Title,
			ConfirmationRef: ep.ConfirmationRef,
			TicketNumber:    ep.TicketNumber,
			CostAmount:      ep.CostAmount,
			CostCurrency:    ep.CostCurrency,
			SupplierName:    ep.SupplierName,
			ContactEmail:    ep.ContactEmail,
			ContactPhone:    ep.ContactPhone,
			Website:         ep.Website,
		}
		minConf := 1.0
		for _, part := range ep.Parts {
			converted, conf := proposePart(ctx, deps, part)
			pp.Parts = append(pp.Parts, converted)
			if conf < minConf {
				minConf = conf
			}
		}
		if len(pp.Parts) == 0 {
			continue
		}
		pp.Confidence = minConf
		// Rebooking match: only for single-flight plans (a rebooking replaces
		// one flight leg). Match against the trip's existing flight parts.
		if ep.Type == "flight" && len(pp.Parts) == 1 && pp.Parts[0].Flight != nil {
			if m := matchRebooking(ep.ConfirmationRef, pp.Parts[0].Flight, candidates); m != nil {
				id := m.partID
				pp.SupersedesPartID = &id
				if m.confidence > pp.Confidence {
					// A PNR match is high-confidence regardless of extraction.
					pp.Confidence = m.confidence
				}
			}
		}
		out = append(out, pp)
	}
	// Fold linkable-type proposals (flight/train/ground) that share a
	// confirmation reference into one multi-part booking — the LLM (and the .ics
	// importer) sometimes split a single PNR across several plans (issue #12).
	out = groupByConfirmationRef(out)
	// Retime airport↔accommodation transfers whose time was defaulted off the
	// flanking flight in the same batch (e.g. a holiday confirmation that names
	// the flight times but not the transfer time — spec §10).
	applyTransferTimes(out)
	return out, nil
}

// groupByConfirmationRef folds linkable-type proposals that share a non-empty,
// case-insensitive confirmation_ref into a single multi-part proposal, parts
// ordered by start. The LLM and importers sometimes split one booking (PNR)
// across several plans; this re-groups them so the user confirms one booking.
// Proposals with an empty ref, or of a non-linkable type, pass through untouched
// and keep their original relative order. A merged plan is no longer a
// single-flight rebooking candidate, so its SupersedesPartID is cleared.
func groupByConfirmationRef(plans []ProposedPlan) []ProposedPlan {
	out := make([]ProposedPlan, 0, len(plans))
	byKey := map[string]int{} // group key -> index into out
	for _, p := range plans {
		ref := strings.ToUpper(strings.TrimSpace(p.ConfirmationRef))
		if ref == "" || !store.LinkableType(p.Type) {
			out = append(out, p)
			continue
		}
		key := p.Type + "\x00" + ref
		if i, ok := byKey[key]; ok {
			out[i].Parts = append(out[i].Parts, p.Parts...)
			if p.Confidence < out[i].Confidence {
				out[i].Confidence = p.Confidence
			}
			// Fill ticket/cost from a later fragment when the primary lacked them,
			// so a booking split across plans doesn't lose the metadata it carried.
			if out[i].TicketNumber == "" {
				out[i].TicketNumber = p.TicketNumber
			}
			if out[i].CostAmount == nil && p.CostAmount != nil {
				v := *p.CostAmount
				out[i].CostAmount = &v
			}
			// Backfill the currency independently of the amount: the primary may
			// have an amount but a blank (non-ISO, dropped) currency a later
			// fragment supplies.
			if out[i].CostCurrency == "" {
				out[i].CostCurrency = p.CostCurrency
			}
			// Same booking, same supplier — fill any contact detail a later
			// fragment carried that the primary lacked.
			if out[i].SupplierName == "" {
				out[i].SupplierName = p.SupplierName
			}
			if out[i].ContactEmail == "" {
				out[i].ContactEmail = p.ContactEmail
			}
			if out[i].ContactPhone == "" {
				out[i].ContactPhone = p.ContactPhone
			}
			if out[i].Website == "" {
				out[i].Website = p.Website
			}
			out[i].SupersedesPartID = nil
			continue
		}
		byKey[key] = len(out)
		out = append(out, p)
	}
	for i := range out {
		if len(out[i].Parts) > 1 {
			parts := out[i].Parts
			sort.SliceStable(parts, func(a, b int) bool {
				return parts[a].StartsAt.Before(parts[b].StartsAt)
			})
		}
	}
	return out
}

// proposePart converts one ExtractedPart into a ProposedPart, enriching flight
// parts via the resolver when possible. Returns the part and its 0..1
// confidence.
func proposePart(ctx context.Context, deps Deps, part ExtractedPart) (ProposedPart, float64) {
	conf := confidenceScore(part.Confidence)
	out := ProposedPart{
		Type:         part.Type,
		Status:       "planned",
		StartLabel:   part.StartLabel,
		EndLabel:     part.EndLabel,
		StartAddress: part.StartAddress,
		EndAddress:   part.EndAddress,
	}
	switch part.Type {
	case "flight":
		fd, originName, destName := enrichFlight(ctx, deps, part.Flight)
		out.Flight = fd
		out.StartsAt = fd.ScheduledOut
		end := fd.ScheduledIn
		out.EndsAt = &end
		if tz, ok := airports.LookupTZ(fd.OriginIATA); ok {
			out.StartTZ = tz
		}
		if tz, ok := airports.LookupTZ(fd.DestIATA); ok {
			out.EndTZ = tz
		}
		// A flight's place line IS its route, so default the label to the
		// friendly "Name (CODE)" (e.g. "Faro (FAO)") rather than the bare code.
		if out.StartLabel == "" {
			out.StartLabel = airports.Label(fd.OriginIATA, originName)
		}
		if out.EndLabel == "" {
			out.EndLabel = airports.Label(fd.DestIATA, destName)
		}
	case "hotel":
		out.StartsAt = combineLocal(part.StartDate, part.StartTime, 15)
		if part.EndDate != "" {
			e := combineLocal(part.EndDate, part.EndTime, 11)
			out.EndsAt = &e
		}
		out.Hotel = &store.HotelDetail{
			PropertyName: part.HotelName,
			Address:      part.Address,
			Phone:        part.Phone,
			RoomType:     part.RoomType,
		}
		if out.StartLabel == "" {
			out.StartLabel = part.HotelName
		}
	case "train":
		out.StartsAt = combineLocal(part.StartDate, part.StartTime, 9)
		if part.EndDate != "" || part.EndTime != "" {
			d := part.EndDate
			if d == "" {
				d = part.StartDate
			}
			e := combineLocal(d, part.EndTime, 9)
			out.EndsAt = &e
		}
		out.Train = &store.TrainDetail{
			Operator:  part.Operator,
			ServiceNo: part.ServiceNo,
			Class:     part.Class,
		}
	case "ground":
		out.StartsAt = combineLocal(part.StartDate, part.StartTime, 9)
		out.startTimeDefaulted = part.StartTime == "" && part.StartDate != ""
		out.Ground = &store.GroundDetail{Provider: part.Provider, Vehicle: part.Vehicle}
	case "dining":
		out.StartsAt = combineLocal(part.StartDate, part.StartTime, 19)
		out.Dining = &store.DiningDetail{ReservationName: part.ReservationName}
	case "excursion":
		out.StartsAt = combineLocal(part.StartDate, part.StartTime, 9)
		out.Excursion = &store.ExcursionDetail{}
		if out.StartLabel == "" {
			out.StartLabel = part.ExcursionTitle
		}
	}
	return out, conf
}

// enrichFlight builds a FlightDetail for a flight part: it asks the resolver to
// fill in the schedule, falling back to the email's own schedule details (or
// bare scheduled-out=now placeholders) when the resolver has no record. The
// resolver gap is not fatal here — Propose surfaces what it has for the user to
// confirm/correct. It also returns the provider's origin/dest airport names (for
// building friendly "Name (CODE)" labels off-table) — empty on the fallback
// path. The returned FlightDetail's Resolved flag records which path was taken.
func enrichFlight(ctx context.Context, deps Deps, leg FlightFields) (fd *store.FlightDetail, originName, destName string) {
	storedIdent := strings.ToUpper(strings.Join(strings.Fields(leg.Ident), ""))
	if deps.Resolver != nil {
		if d, err := time.Parse("2006-01-02", leg.Date); err == nil {
			if rf, rerr := deps.Resolver.Resolve(ctx, leg.Ident, d); rerr == nil {
				out := &store.FlightDetail{
					Ident:        storedIdent,
					ScheduledOut: rf.ScheduledOut,
					ScheduledIn:  rf.ScheduledIn,
					OriginIATA:   rf.OriginIATA,
					DestIATA:     rf.DestIATA,
					Resolved:     true,
				}
				if rf.ICAO24 != "" {
					icao := rf.ICAO24
					out.ICAO24 = &icao
				}
				return out, rf.OriginName, rf.DestName
			}
		}
	}
	// Fall back to the email's own schedule when we have it. Resolved stays
	// false: the route is the user's/email's, editable and not provider-tracked.
	return flightFromLeg(leg, storedIdent), "", ""
}

// flightFromLeg builds a best-effort FlightDetail purely from the extracted
// leg, parsing local times in each airport's tz when present.
func flightFromLeg(leg FlightFields, storedIdent string) *store.FlightDetail {
	fd := &store.FlightDetail{Ident: storedIdent, OriginIATA: leg.OriginIATA, DestIATA: leg.DestIATA}
	depDate := leg.Date
	if leg.DepartTimeLocal != "" {
		fd.ScheduledOut = parseLocalInTZ(depDate, leg.DepartTimeLocal, leg.OriginIATA)
	} else {
		fd.ScheduledOut = combineLocal(depDate, "", 0)
	}
	arrDate := leg.ArriveDate
	if arrDate == "" {
		arrDate = leg.Date
	}
	if leg.ArriveTimeLocal != "" {
		fd.ScheduledIn = parseLocalInTZ(arrDate, leg.ArriveTimeLocal, leg.DestIATA)
	} else {
		fd.ScheduledIn = fd.ScheduledOut
	}
	// Guard against an inverted/zero duration: a stated arrival that lands at or
	// before departure is almost always an overnight flight whose arrival date
	// was omitted (e.g. depart 23:10, arrive 06:30). Roll the arrival forward a
	// day at a time until it's after departure (capped), so downstream span /
	// long-haul / transfer-timing logic never sees ScheduledIn < ScheduledOut.
	if leg.ArriveTimeLocal != "" && leg.ArriveDate == "" {
		for i := 0; i < 2 && !fd.ScheduledIn.After(fd.ScheduledOut); i++ {
			fd.ScheduledIn = fd.ScheduledIn.Add(24 * time.Hour)
		}
	}
	// Last resort: never leave arrival strictly before departure.
	if fd.ScheduledIn.Before(fd.ScheduledOut) {
		fd.ScheduledIn = fd.ScheduledOut
	}
	return fd
}

// combineLocal builds a UTC instant from a YYYY-MM-DD date and an optional
// HH:MM time, defaulting to defaultHour when the time is absent. A blank date
// yields the zero time.
func combineLocal(date, hhmm string, defaultHour int) time.Time {
	if date == "" {
		return time.Time{}
	}
	if hhmm == "" {
		hhmm = fmt.Sprintf("%02d:00", defaultHour)
	}
	t, err := time.Parse("2006-01-02T15:04", date+"T"+hhmm)
	if err != nil {
		if d, derr := time.Parse("2006-01-02", date); derr == nil {
			return d
		}
		return time.Time{}
	}
	return t.UTC()
}

// parseLocalInTZ interprets date+time in the airport's tz, falling back to UTC.
func parseLocalInTZ(date, hhmm, iata string) time.Time {
	loc := time.UTC
	if tzName, ok := airports.LookupTZ(iata); ok {
		if l, err := time.LoadLocation(tzName); err == nil {
			loc = l
		}
	}
	t, err := time.ParseInLocation("2006-01-02T15:04", date+"T"+hhmm, loc)
	if err != nil {
		return combineLocal(date, hhmm, 0)
	}
	return t.UTC()
}
