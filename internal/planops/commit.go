package planops

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/dpage/aerly/internal/store"
)

// ConfirmPartInput is one part of a confirmed/edited proposal sent back to
// commit. It mirrors the FE PlanPartInput shape.
type ConfirmPartInput struct {
	Type         string
	Seq          int
	StartsAt     time.Time
	EndsAt       *time.Time
	StartTZ      string
	EndTZ        string
	StartLabel   string
	StartLat     *float64
	StartLon     *float64
	StartAddress string
	EndLabel     string
	EndLat       *float64
	EndLon       *float64
	EndAddress   string
	Status       string

	Flight    *store.FlightDetail
	Hotel     *store.HotelDetail
	Train     *store.TrainDetail
	Ground    *store.GroundDetail
	Dining    *store.DiningDetail
	Excursion *store.ExcursionDetail
	Meeting   *store.MeetingDetail
	Event     *store.EventDetail
}

// ConfirmPlanInput is one confirmed/edited proposal. It mirrors the FE
// ConfirmPlanInput contract: a plan with its parts, passengers, visibility, and
// an optional rebooking supersession target.
type ConfirmPlanInput struct {
	Type            string
	Title           string
	ConfirmationRef string
	TicketNumber    string
	Notes           string
	Source          string
	CostAmount      *float64
	CostCurrency    string
	SupplierName    string
	ContactEmail    string
	ContactPhone    string
	Website         string
	PassengerIDs    []int64
	Visibility      *ConfirmVisibility

	// TripItUID is the source TripIt event UID for .ics imports; persisted on
	// the plan for re-import dedupe. Empty for the LLM/manual paths.
	TripItUID string

	Parts []ConfirmPartInput

	// SupersedesPartID, when set, is the existing part this plan's (single,
	// flight) part replaces. On commit the new part links to it via
	// supersedes_id and the old part is stamped status='cancelled'.
	SupersedesPartID *int64
}

// ConfirmVisibility carries a per-plan privacy override on confirm.
type ConfirmVisibility struct {
	Mode    string // ""|everyone → default; hidden_from|only_visible_to
	UserIDs []int64
}

// Commit writes the confirmed plans, their parts, and per-type satellites via
// the store, then applies any rebooking supersessions: the new part's
// supersedes_id points at the matched part and the OLD part is stamped
// status='cancelled' (the signal the front end greys on — spec §6.1). Returns
// the created plans.
func Commit(ctx context.Context, deps Deps, tripID, createdBy int64, plans []ConfirmPlanInput) ([]*store.Plan, error) {
	if deps.Store == nil {
		return nil, errors.New("planops.Commit: nil Store")
	}
	out := make([]*store.Plan, 0, len(plans))
	for _, in := range plans {
		// Validate any rebooking supersession before writing anything. The
		// superseded part MUST belong to this trip — otherwise an editor of
		// trip A could cancel an arbitrary part in another user's trip B
		// simply by passing its id (the confirm body is client-controlled).
		// Supersession also only applies to a plan's single (flight) part, per
		// the contract; reject multi-part plans carrying a supersession rather
		// than silently cancelling without linking.
		if in.SupersedesPartID != nil {
			if len(in.Parts) != 1 {
				return nil, fmt.Errorf("supersedes is only valid for a single-part plan, got %d parts", len(in.Parts))
			}
			_, superTripID, err := deps.Store.PlanIDForPart(ctx, *in.SupersedesPartID)
			if err != nil {
				return nil, fmt.Errorf("resolve superseded part %d: %w", *in.SupersedesPartID, err)
			}
			if superTripID != tripID {
				return nil, fmt.Errorf("superseded part %d does not belong to trip %d", *in.SupersedesPartID, tripID)
			}
		}
		// Friend-gate every passenger before writing anything. A passenger
		// becomes a trip viewer (via the read-time friend gate), so an editor
		// must not be able to expose the trip to an arbitrary user id by naming
		// them here — the PassengerIDs are client/LLM-controlled (the HTTP
		// confirm body and the email pipeline both flow through Commit). Mirrors
		// the dedicated addPlanPassenger endpoint's requireFriendTarget check.
		for _, uid := range in.PassengerIDs {
			if uid == createdBy {
				continue
			}
			ok, err := deps.Store.AnyFriendshipEdge(ctx, createdBy, uid)
			if err != nil {
				return nil, fmt.Errorf("check passenger friendship: %w", err)
			}
			if !ok {
				return nil, fmt.Errorf("passenger %d must be a friend (or invited) of the trip editor", uid)
			}
		}
		source := in.Source
		if source == "" {
			source = "paste"
		}
		parts := make([]store.CreatePlanPartPayload, 0, len(in.Parts))
		for i, p := range in.Parts {
			seq := p.Seq
			if seq == 0 {
				seq = i
			}
			cp := store.CreatePlanPartPayload{
				Seq:          seq,
				StartsAt:     p.StartsAt,
				EndsAt:       p.EndsAt,
				StartTZ:      p.StartTZ,
				EndTZ:        p.EndTZ,
				StartLabel:   p.StartLabel,
				StartLat:     p.StartLat,
				StartLon:     p.StartLon,
				StartAddress: p.StartAddress,
				EndLabel:     p.EndLabel,
				EndLat:       p.EndLat,
				EndLon:       p.EndLon,
				EndAddress:   p.EndAddress,
				Status:       p.Status,
				Flight:       p.Flight,
				Hotel:        p.Hotel,
				Train:        p.Train,
				Ground:       p.Ground,
				Dining:       p.Dining,
				Excursion:    p.Excursion,
				Meeting:      p.Meeting,
				Event:        p.Event,
			}
			// Link the new part to the part it supersedes (rebooking). The
			// supersession is a plan-level field in the contract; it applies to
			// the plan's single flight part.
			if in.SupersedesPartID != nil && len(in.Parts) == 1 {
				cp.SupersedesID = in.SupersedesPartID
			}
			parts = append(parts, cp)
		}
		plan, err := deps.Store.CreatePlan(ctx, store.CreatePlanPayload{
			TripID:          tripID,
			Type:            in.Type,
			Title:           in.Title,
			ConfirmationRef: in.ConfirmationRef,
			TicketNumber:    in.TicketNumber,
			Notes:           in.Notes,
			Source:          source,
			CostAmount:      in.CostAmount,
			CostCurrency:    in.CostCurrency,
			SupplierName:    in.SupplierName,
			ContactEmail:    in.ContactEmail,
			ContactPhone:    in.ContactPhone,
			Website:         in.Website,
			TripItUID:       in.TripItUID,
			Parts:           parts,
		}, createdBy)
		if err != nil {
			return nil, fmt.Errorf("create plan %q: %w", in.Title, err)
		}
		// CreatePlan is atomic, but the passenger/visibility/supersede writes
		// below are separate transactions. If any fails the plan is already
		// persisted; since the email pipeline treats a nil error as "processed"
		// and never retries, a half-configured plan would be a silent orphan.
		// Compensate by deleting the just-created plan (cascades to its parts /
		// passengers / visibility) so each plan commits all-or-nothing. The
		// supersession cancel is applied last, so a failure here never leaves the
		// old part cancelled without its replacement.
		if err := commitPlanExtras(ctx, deps, plan.ID, in); err != nil {
			if delErr := deps.Store.DeletePlan(ctx, plan.ID); delErr != nil {
				return nil, fmt.Errorf("%w (and rollback of plan %d failed: %v)", err, plan.ID, delErr)
			}
			return nil, err
		}
		out = append(out, plan)
	}
	return out, nil
}

// commitPlanExtras applies the per-plan writes that follow CreatePlan:
// passengers, the optional visibility override, and any rebooking supersession
// (applied last so it is never left half-done). The caller compensates by
// deleting the plan if this returns an error, giving each plan all-or-nothing
// commit semantics across these separate store transactions.
func commitPlanExtras(ctx context.Context, deps Deps, planID int64, in ConfirmPlanInput) error {
	for _, uid := range in.PassengerIDs {
		if err := deps.Store.AddPlanPassenger(ctx, planID, uid); err != nil {
			return fmt.Errorf("add passenger: %w", err)
		}
	}
	if in.Visibility != nil {
		mode := in.Visibility.Mode
		if mode == "everyone" {
			mode = ""
		}
		if err := deps.Store.SetPlanVisibility(ctx, planID, mode, in.Visibility.UserIDs); err != nil {
			return fmt.Errorf("set visibility: %w", err)
		}
	}
	// Apply the supersession: cancel the old part so the FE greys it.
	if in.SupersedesPartID != nil {
		if err := cancelSuperseded(ctx, deps, *in.SupersedesPartID); err != nil {
			return fmt.Errorf("cancel superseded part %d: %w", *in.SupersedesPartID, err)
		}
	}
	return nil
}

// cancelSuperseded stamps the old part status='cancelled'. It stays on the
// timeline (greyed) until the user tidies it away via the dismiss endpoint.
func cancelSuperseded(ctx context.Context, deps Deps, partID int64) error {
	cancelled := "cancelled"
	_, err := deps.Store.UpdatePlanPart(ctx, partID, store.UpdatePlanPartPayload{Status: &cancelled})
	return err
}
