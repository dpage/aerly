package handlers

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/dpage/aerly/internal/airports"
	"github.com/dpage/aerly/internal/api"
	"github.com/dpage/aerly/internal/auth"
	"github.com/dpage/aerly/internal/flightcoord"
	"github.com/dpage/aerly/internal/geocode"
	"github.com/dpage/aerly/internal/planops"
	"github.com/dpage/aerly/internal/store"
)

// Wave 1B: plans, parts, passengers, visibility, and move.

type planPartReq struct {
	Type         string     `json:"type"`
	Seq          int        `json:"seq"`
	StartsAt     time.Time  `json:"starts_at"`
	EndsAt       *time.Time `json:"ends_at"`
	StartTZ      string     `json:"start_tz"`
	EndTZ        string     `json:"end_tz"`
	StartLabel   string     `json:"start_label"`
	StartLat     *float64   `json:"start_lat"`
	StartLon     *float64   `json:"start_lon"`
	StartAddress string     `json:"start_address"`
	EndLabel     string     `json:"end_label"`
	EndLat       *float64   `json:"end_lat"`
	EndLon       *float64   `json:"end_lon"`
	EndAddress   string     `json:"end_address"`
	Status       string     `json:"status"`

	Flight    *flightDetailReq    `json:"flight"`
	Hotel     *hotelDetailReq     `json:"hotel"`
	Train     *trainDetailReq     `json:"train"`
	Ground    *groundDetailReq    `json:"ground"`
	Dining    *diningDetailReq    `json:"dining"`
	Excursion *excursionDetailReq `json:"excursion"`
}

type flightDetailReq struct {
	Ident        string     `json:"ident"`
	ICAO24       *string    `json:"icao24"`
	Callsign     string     `json:"callsign"`
	ScheduledOut time.Time  `json:"scheduled_out"`
	ScheduledIn  time.Time  `json:"scheduled_in"`
	EstimatedOut *time.Time `json:"estimated_out"`
	EstimatedIn  *time.Time `json:"estimated_in"`
	ActualOut    *time.Time `json:"actual_out"`
	ActualIn     *time.Time `json:"actual_in"`
	OriginIATA   string     `json:"origin_iata"`
	DestIATA     string     `json:"dest_iata"`
	FlightStatus string     `json:"flight_status"`
}

type hotelDetailReq struct {
	PropertyName     string  `json:"property_name"`
	Address          string  `json:"address"`
	Phone            string  `json:"phone"`
	RoomType         string  `json:"room_type"`
	Guests           *int    `json:"guests"`
	StandardCheckin  *string `json:"standard_checkin"`
	StandardCheckout *string `json:"standard_checkout"`
}

type trainDetailReq struct {
	Operator  string `json:"operator"`
	ServiceNo string `json:"service_no"`
	Coach     string `json:"coach"`
	Seat      string `json:"seat"`
	Class     string `json:"class"`
	Platform  string `json:"platform"`
}

type groundDetailReq struct {
	Provider string `json:"provider"`
	Phone    string `json:"phone"`
	Vehicle  string `json:"vehicle"`
	Driver   string `json:"driver"`
	Pax      *int   `json:"pax"`
}

type diningDetailReq struct {
	PartySize       *int   `json:"party_size"`
	ReservationName string `json:"reservation_name"`
	Phone           string `json:"phone"`
}

type excursionDetailReq struct {
	Provider    string `json:"provider"`
	TicketCount *int   `json:"ticket_count"`
}

type createPlanReq struct {
	Type            string             `json:"type"`
	Title           string             `json:"title"`
	ConfirmationRef string             `json:"confirmation_ref"`
	TicketNumber    string             `json:"ticket_number"`
	Notes           string             `json:"notes"`
	Source          string             `json:"source"`
	CostAmount      *float64           `json:"cost_amount,omitempty"`
	CostCurrency    string             `json:"cost_currency"`
	SupplierName    string             `json:"supplier_name"`
	ContactEmail    string             `json:"contact_email"`
	ContactPhone    string             `json:"contact_phone"`
	Website         string             `json:"website"`
	PassengerIDs    []int64            `json:"passenger_ids"`
	Visibility      *planVisibilityReq `json:"visibility"`
	Parts           []planPartReq      `json:"parts"`
}

type updatePlanReq struct {
	Title           *string  `json:"title,omitempty"`
	ConfirmationRef *string  `json:"confirmation_ref,omitempty"`
	TicketNumber    *string  `json:"ticket_number,omitempty"`
	Notes           *string  `json:"notes,omitempty"`
	CostAmount      *float64 `json:"cost_amount,omitempty"`
	CostCurrency    *string  `json:"cost_currency,omitempty"`
	SupplierName    *string  `json:"supplier_name,omitempty"`
	ContactEmail    *string  `json:"contact_email,omitempty"`
	ContactPhone    *string  `json:"contact_phone,omitempty"`
	Website         *string  `json:"website,omitempty"`
}

type planVisibilityReq struct {
	Mode    string  `json:"mode"`
	UserIDs []int64 `json:"user_ids"`
}

type updatePlanPartReq struct {
	StartsAt     *time.Time `json:"starts_at,omitempty"`
	EndsAt       *time.Time `json:"ends_at,omitempty"`
	StartTZ      *string    `json:"start_tz,omitempty"`
	EndTZ        *string    `json:"end_tz,omitempty"`
	StartLabel   *string    `json:"start_label,omitempty"`
	StartLat     *float64   `json:"start_lat,omitempty"`
	StartLon     *float64   `json:"start_lon,omitempty"`
	StartAddress *string    `json:"start_address,omitempty"`
	EndLabel     *string    `json:"end_label,omitempty"`
	EndLat       *float64   `json:"end_lat,omitempty"`
	EndLon       *float64   `json:"end_lon,omitempty"`
	EndAddress   *string    `json:"end_address,omitempty"`
	Status       *string    `json:"status,omitempty"`

	// Coords pin flags: a manual lat/lon override the geocoder must not touch.
	// Setting one true (with explicit coords) opts that endpoint out of
	// geocoding; setting it false reverts to address-derived coordinates.
	StartCoordsPinned *bool `json:"start_coords_pinned,omitempty"`
	EndCoordsPinned   *bool `json:"end_coords_pinned,omitempty"`

	// Flight carries a flight part's route/identity edit (PRD: flight route
	// labels & editing). Changing the ident re-resolves the flight; the
	// origin/dest IATA are only honoured for an unresolved flight.
	Flight *flightEditReq `json:"flight,omitempty"`
}

// flightEditReq is the editable subset of a flight's route. A nil field leaves
// it unchanged. The ident is the flight's identity (changing it re-resolves);
// the IATAs are the manual route, applied only when the flight is unresolved.
type flightEditReq struct {
	Ident      *string `json:"ident,omitempty"`
	OriginIATA *string `json:"origin_iata,omitempty"`
	DestIATA   *string `json:"dest_iata,omitempty"`
}

type moveReq struct {
	TripID int64 `json:"trip_id"`
}

type linkReq struct {
	PlanIDs []int64 `json:"plan_ids"`
}

type planUserIDReq struct {
	UserID int64 `json:"user_id"`
}

var validPlanTypes = map[string]bool{
	"flight": true, "train": true, "hotel": true,
	"ground": true, "dining": true, "excursion": true,
}

func (a *API) createPlan(w http.ResponseWriter, r *http.Request) {
	tripID, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid ID.")
		return
	}
	me := auth.UserFrom(r.Context())
	if err := a.requireTripEdit(r.Context(), tripID, me, w); err != nil {
		return
	}
	var in createPlanReq
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !validPlanTypes[in.Type] {
		writeError(w, http.StatusBadRequest, "Invalid plan type.")
		return
	}
	parts := make([]store.CreatePlanPartPayload, 0, len(in.Parts))
	for _, p := range in.Parts {
		parts = append(parts, toCreatePartPayload(in.Type, p))
	}
	plan, err := a.Store.CreatePlan(r.Context(), store.CreatePlanPayload{
		TripID:          tripID,
		Type:            in.Type,
		Title:           in.Title,
		ConfirmationRef: in.ConfirmationRef,
		TicketNumber:    in.TicketNumber,
		Notes:           in.Notes,
		Source:          in.Source,
		CostAmount:      in.CostAmount,
		CostCurrency:    in.CostCurrency,
		SupplierName:    in.SupplierName,
		ContactEmail:    in.ContactEmail,
		ContactPhone:    in.ContactPhone,
		Website:         in.Website,
		Parts:           parts,
	}, me.ID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	for _, uid := range in.PassengerIDs {
		// A passenger becomes a trip viewer (via the read-time friend gate), so
		// each must be an accepted/invited friend of the actor — the same check
		// the dedicated addPlanPassenger endpoint enforces. Without this, an
		// editor could expose the trip to an arbitrary user id at create time.
		if err := a.requireFriendTarget(r.Context(), me, uid, w); err != nil {
			return
		}
		if err := a.Store.AddPlanPassenger(r.Context(), plan.ID, uid); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if in.Visibility != nil {
		mode := in.Visibility.Mode
		if mode == "everyone" {
			mode = ""
		}
		if err := a.Store.SetPlanVisibility(r.Context(), plan.ID, mode, in.Visibility.UserIDs); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	dto, err := a.planDTO(r.Context(), plan.ID, me.ID)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	a.publishPlanUpdated(r.Context(), tripID, plan.ID)
	a.geocodePlanAsync(tripID, plan.ID)
	a.resolveFlightCoordsAsync(tripID, plan.ID)
	writeJSON(w, http.StatusCreated, dto)
}

func (a *API) updatePlan(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid ID.")
		return
	}
	me := auth.UserFrom(r.Context())
	if err := a.requirePlanEdit(r.Context(), id, me, w); err != nil {
		return
	}
	var in updatePlanReq
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := a.Store.UpdatePlan(r.Context(), id, store.UpdatePlanPayload{
		Title:           in.Title,
		ConfirmationRef: in.ConfirmationRef,
		TicketNumber:    in.TicketNumber,
		Notes:           in.Notes,
		CostAmount:      in.CostAmount,
		CostCurrency:    in.CostCurrency,
		SupplierName:    in.SupplierName,
		ContactEmail:    in.ContactEmail,
		ContactPhone:    in.ContactPhone,
		Website:         in.Website,
	}); err != nil {
		handleStoreErr(w, err)
		return
	}
	dto, err := a.planDTO(r.Context(), id, me.ID)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	a.publishPlanUpdated(r.Context(), dto.TripID, id)
	writeJSON(w, http.StatusOK, dto)
}

func (a *API) deletePlan(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid ID.")
		return
	}
	me := auth.UserFrom(r.Context())
	if err := a.requirePlanEdit(r.Context(), id, me, w); err != nil {
		return
	}
	// Resolve the trip id + visibility set before the delete; both are gone
	// once the plan row (and its plan_visibility rows) are removed.
	plan, err := a.Store.PlanByID(r.Context(), id)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	// Sweep the attachment blobs before the rows cascade away with the plan —
	// the DB can't reach the object store. Best-effort: a left-over blob is a
	// harmless orphan, so a sweep error doesn't block the delete.
	var blobKeys []string
	if a.Attachments != nil {
		if keys, kerr := a.Store.StorageKeysByPlan(r.Context(), id); kerr == nil {
			blobKeys = keys
		}
	}
	a.publishPlanDeleted(r.Context(), plan.TripID, id)
	if err := a.Store.DeletePlan(r.Context(), id); err != nil {
		handleStoreErr(w, err)
		return
	}
	for _, key := range blobKeys {
		if derr := a.Attachments.Delete(r.Context(), key); derr != nil {
			slog.Error("attachment blob cleanup failed on plan delete", "err", derr, "key", key)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) addPlanPassenger(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid ID.")
		return
	}
	me := auth.UserFrom(r.Context())
	if err := a.requirePlanEdit(r.Context(), id, me, w); err != nil {
		return
	}
	var in planUserIDReq
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if in.UserID == 0 {
		writeError(w, http.StatusBadRequest, "Missing user_id.")
		return
	}
	// A passenger becomes a trip viewer (via DB trigger), so they must be an
	// accepted friend of the actor — matching the FE picker (spec §6.4) and
	// preventing an editor from exposing the trip to an arbitrary user id.
	if err := a.requireFriendTarget(r.Context(), me, in.UserID, w); err != nil {
		return
	}
	// The DB trigger ensures the passenger becomes a trip viewer.
	if err := a.Store.AddPlanPassenger(r.Context(), id, in.UserID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	dto, err := a.planDTO(r.Context(), id, me.ID)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	a.publishPlanUpdated(r.Context(), dto.TripID, id)
	writeJSON(w, http.StatusOK, dto)
}

func (a *API) removePlanPassenger(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid ID.")
		return
	}
	uid, err := pathID(r, "userId")
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid user ID.")
		return
	}
	me := auth.UserFrom(r.Context())
	if err := a.requirePlanEdit(r.Context(), id, me, w); err != nil {
		return
	}
	if err := a.Store.RemovePlanPassenger(r.Context(), id, uid); err != nil {
		handleStoreErr(w, err)
		return
	}
	if plan, err := a.Store.PlanByID(r.Context(), id); err == nil {
		a.publishPlanUpdated(r.Context(), plan.TripID, id)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) setPlanVisibility(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid ID.")
		return
	}
	me := auth.UserFrom(r.Context())
	if err := a.requirePlanEdit(r.Context(), id, me, w); err != nil {
		return
	}
	var in planVisibilityReq
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	mode := in.Mode
	switch mode {
	case "everyone", "":
		mode = ""
	case "hidden_from", "only_visible_to":
		// ok
	default:
		writeError(w, http.StatusBadRequest, "Invalid visibility mode.")
		return
	}
	if err := a.Store.SetPlanVisibility(r.Context(), id, mode, in.UserIDs); err != nil {
		handleStoreErr(w, err)
		return
	}
	dto, err := a.planDTO(r.Context(), id, me.ID)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	a.publishPlanUpdated(r.Context(), dto.TripID, id)
	writeJSON(w, http.StatusOK, dto)
}

// movePlan reassigns a plan to another trip. Requires editor rights on BOTH the
// source and destination trips (spec §4).
func (a *API) movePlan(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid ID.")
		return
	}
	var in moveReq
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if in.TripID == 0 {
		writeError(w, http.StatusBadRequest, "Missing trip_id.")
		return
	}
	me := auth.UserFrom(r.Context())
	// Editor on the source trip (the plan's current trip).
	if err := a.requirePlanEdit(r.Context(), id, me, w); err != nil {
		return
	}
	// Editor on the destination trip.
	if err := a.requireTripEdit(r.Context(), in.TripID, me, w); err != nil {
		return
	}
	// Capture the source trip before the move so its members can be told their
	// timeline lost a plan (trip.updated). nil is benign — we just skip it.
	var sourceTripID int64
	if src, err := a.Store.PlanByID(r.Context(), id); err == nil {
		sourceTripID = src.TripID
	}
	if err := a.Store.MovePlan(r.Context(), id, in.TripID); err != nil {
		handleStoreErr(w, err)
		return
	}
	dto, err := a.planDTO(r.Context(), id, me.ID)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	a.publishPlanUpdated(r.Context(), dto.TripID, id)
	if sourceTripID != 0 && sourceTripID != dto.TripID {
		a.publishTripUpdated(r.Context(), sourceTripID)
	}
	writeJSON(w, http.StatusOK, dto)
}

// linkPlans folds the plans named in the body into the path plan (the primary),
// making one multi-part booking (issue #12). The actor must be an editor of the
// primary's trip and of every absorbed plan; the store re-validates that all
// plans share the trip and a link-eligible type.
func (a *API) linkPlans(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid ID.")
		return
	}
	var in linkReq
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(in.PlanIDs) == 0 {
		writeError(w, http.StatusBadRequest, "Missing plan_ids.")
		return
	}
	me := auth.UserFrom(r.Context())
	if err := a.requirePlanEdit(r.Context(), id, me, w); err != nil {
		return
	}
	for _, pid := range in.PlanIDs {
		if err := a.requirePlanEdit(r.Context(), pid, me, w); err != nil {
			return
		}
	}
	// Capture the absorbed plans' trips before they vanish so their members can
	// be told the timeline lost a plan.
	absorbed := make(map[int64]int64, len(in.PlanIDs)) // planID -> tripID
	for _, pid := range in.PlanIDs {
		if p, err := a.Store.PlanByID(r.Context(), pid); err == nil {
			absorbed[pid] = p.TripID
		}
	}
	if err := a.Store.LinkPlans(r.Context(), id, in.PlanIDs); err != nil {
		// The plans were authorised above, so a not-found here means a bad id in
		// the body; everything else is a validation failure. Either way, don't
		// echo raw store/DB errors to the client.
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "Plan not found.")
			return
		}
		writeError(w, http.StatusBadRequest, "Cannot link the selected bookings.")
		return
	}
	dto, err := a.planDTO(r.Context(), id, me.ID)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	for pid, tripID := range absorbed {
		a.publishPlanDeleted(r.Context(), tripID, pid)
	}
	a.publishPlanUpdated(r.Context(), dto.TripID, id)
	writeJSON(w, http.StatusOK, dto)
}

// splitPlanPart separates one leg of a multi-part booking into its own plan
// (issue #12), returning the new plan. The new plan copies the parent's audience
// (passengers + visibility) so privacy is never widened.
func (a *API) splitPlanPart(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid ID.")
		return
	}
	me := auth.UserFrom(r.Context())
	if err := a.requirePartEdit(r.Context(), id, me, w); err != nil {
		return
	}
	newID, parentID, err := a.Store.SplitPlanPart(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotSplittable) {
			writeError(w, http.StatusBadRequest, "Plan has only one part to split.")
			return
		}
		handleStoreErr(w, err)
		return
	}
	dto, err := a.planDTO(r.Context(), newID, me.ID)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	a.publishPlanUpdated(r.Context(), dto.TripID, parentID)
	a.publishPlanUpdated(r.Context(), dto.TripID, newID)
	writeJSON(w, http.StatusCreated, dto)
}

func (a *API) updatePlanPart(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid ID.")
		return
	}
	me := auth.UserFrom(r.Context())
	if err := a.requirePartEdit(r.Context(), id, me, w); err != nil {
		return
	}
	var in updatePlanPartReq
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// A changed address is re-located via the shared geocode fallback chain
	// (address → name+country → tail backoff → airport label), the same path the
	// backfill uses. Explicit coordinates in the request, if any, still win.
	if a.Geocoder != nil {
		if cur, cerr := a.Store.PlanPartByID(r.Context(), id); cerr == nil {
			startLabel := cur.StartLabel
			if in.StartLabel != nil {
				startLabel = *in.StartLabel
			}
			endLabel := cur.EndLabel
			if in.EndLabel != nil {
				endLabel = *in.EndLabel
			}
			// A pinned endpoint keeps its manual coordinates untouched; only an
			// unpinned one is (re)geocoded — when its address changed, or when the
			// user just unpinned it (reverting a manual override to address-derived
			// coordinates). Explicit coordinates in the request always win.
			startPinned := cur.StartCoordsPinned
			if in.StartCoordsPinned != nil {
				startPinned = *in.StartCoordsPinned
			}
			endPinned := cur.EndCoordsPinned
			if in.EndCoordsPinned != nil {
				endPinned = *in.EndCoordsPinned
			}
			startUnpinned := in.StartCoordsPinned != nil && !*in.StartCoordsPinned && cur.StartCoordsPinned
			endUnpinned := in.EndCoordsPinned != nil && !*in.EndCoordsPinned && cur.EndCoordsPinned
			startAddr := cur.StartAddress
			if in.StartAddress != nil {
				startAddr = *in.StartAddress
			}
			endAddr := cur.EndAddress
			if in.EndAddress != nil {
				endAddr = *in.EndAddress
			}
			startAddrChanged := in.StartAddress != nil && *in.StartAddress != cur.StartAddress
			endAddrChanged := in.EndAddress != nil && *in.EndAddress != cur.EndAddress
			if !startPinned && in.StartLat == nil && startAddr != "" && (startAddrChanged || startUnpinned) {
				if lat, lon, ok := geocode.Endpoint(r.Context(), a.Geocoder, cur.Type, startAddr, startLabel); ok {
					in.StartLat, in.StartLon = &lat, &lon
				}
			}
			if !endPinned && in.EndLat == nil && endAddr != "" && (endAddrChanged || endUnpinned) {
				if lat, lon, ok := geocode.Endpoint(r.Context(), a.Geocoder, cur.Type, endAddr, endLabel); ok {
					in.EndLat, in.EndLon = &lat, &lon
				}
			}
		}
	}
	part, err := a.Store.UpdatePlanPart(r.Context(), id, store.UpdatePlanPartPayload{
		StartsAt:          in.StartsAt,
		EndsAt:            in.EndsAt,
		StartTZ:           in.StartTZ,
		EndTZ:             in.EndTZ,
		StartLabel:        in.StartLabel,
		StartLat:          in.StartLat,
		StartLon:          in.StartLon,
		StartAddress:      in.StartAddress,
		EndLabel:          in.EndLabel,
		EndLat:            in.EndLat,
		EndLon:            in.EndLon,
		EndAddress:        in.EndAddress,
		Status:            in.Status,
		StartCoordsPinned: in.StartCoordsPinned,
		EndCoordsPinned:   in.EndCoordsPinned,
	})
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	// A flight route/identity edit is applied after the generic part update so
	// a re-resolve's regenerated labels win over any label sent in the same
	// request. Reload the part afterwards so the DTO reflects the new route.
	if in.Flight != nil {
		if err := a.applyFlightEdit(r.Context(), id, *in.Flight); err != nil {
			handleStoreErr(w, err)
			return
		}
		if part, err = a.Store.PlanPartByID(r.Context(), id); err != nil {
			handleStoreErr(w, err)
			return
		}
	}
	dto, err := a.partDTO(r.Context(), part)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if planID, tripID, err := a.Store.PlanIDForPart(r.Context(), id); err == nil {
		a.publishPlanUpdated(r.Context(), tripID, planID)
	}
	writeJSON(w, http.StatusOK, dto)
}

// applyFlightEdit applies a flight part's route/identity edit. Changing the
// ident re-resolves the flight (resolve-or-fallback, never reject): on success
// the provider's route/schedule is adopted and the flight is marked resolved; on
// failure the last-known route is kept and the flight is marked unresolved so the
// poller retries and the user can correct the IATAs by hand. An IATA edit is
// honoured only for an unresolved flight (otherwise a re-resolve would clobber
// it), where it rewrites the route, regenerates the label, and recomputes (or
// clears) the airport's coordinates/timezone.
func (a *API) applyFlightEdit(ctx context.Context, partID int64, in flightEditReq) error {
	fd, err := a.Store.FlightDetailFor(ctx, partID)
	if err != nil {
		return err
	}
	if fd == nil {
		return nil // not a flight part — nothing to do
	}

	if in.Ident != nil {
		newIdent := normalizeIdent(*in.Ident)
		if newIdent != "" && newIdent != fd.Ident {
			return a.reresolveFlightPart(ctx, partID, newIdent, fd.ScheduledOut)
		}
	}

	// IATA edits are only meaningful on an unresolved flight; for a tracked
	// flight the route is provider-owned and editing it would be clobbered.
	if !fd.Resolved {
		if up, changed := manualRouteUpdate(fd, in); changed {
			return a.Store.UpdateFlightPartRoute(ctx, partID, up)
		}
	}
	return nil
}

// reresolveFlightPart re-resolves a flight against a new ident and writes the
// result. Success adopts the provider's route; failure (or no resolver) keeps
// the last-known route and only records the new ident + unresolved state.
func (a *API) reresolveFlightPart(ctx context.Context, partID int64, ident string, date time.Time) error {
	if a.Resolver != nil {
		if rf, rerr := a.Resolver.Resolve(ctx, ident, date); rerr == nil {
			up := flightcoord.RouteUpdateFromResolved(rf)
			up.Ident = &ident
			return a.Store.UpdateFlightPartRoute(ctx, partID, up)
		}
	}
	notResolved := false
	return a.Store.UpdateFlightPartRoute(ctx, partID, store.FlightRouteUpdate{Ident: &ident, Resolved: &notResolved})
}

// normalizeIdent upper-cases and strips whitespace from a flight number, so
// "ba 286" and "BA286" canonicalise alike.
func normalizeIdent(s string) string {
	return strings.ToUpper(strings.Join(strings.Fields(s), ""))
}

// manualRouteUpdate builds the FlightRouteUpdate for a hand-typed IATA edit on
// an unresolved flight: it rewrites the changed leg's code, friendly label and
// timezone, and recomputes coordinates from the embedded table (clearing them
// when the new airport is off-table, so a stale pin doesn't linger). The
// resolved flag is left untouched (stays false). Returns whether anything
// changed.
func manualRouteUpdate(fd *store.FlightDetail, in flightEditReq) (store.FlightRouteUpdate, bool) {
	var up store.FlightRouteUpdate
	changed := false
	if in.OriginIATA != nil {
		if code := strings.ToUpper(strings.TrimSpace(*in.OriginIATA)); code != fd.OriginIATA {
			label := airports.Label(code, "")
			up.OriginIATA, up.StartLabel = &code, &label
			if tz, ok := airports.LookupTZ(code); ok {
				up.StartTZ = &tz
			}
			up.StartLat, up.StartLon, up.ClearStartCoords = flightcoord.AirportCoords(code, 0, 0)
			changed = true
		}
	}
	if in.DestIATA != nil {
		if code := strings.ToUpper(strings.TrimSpace(*in.DestIATA)); code != fd.DestIATA {
			label := airports.Label(code, "")
			up.DestIATA, up.EndLabel = &code, &label
			if tz, ok := airports.LookupTZ(code); ok {
				up.EndTZ = &tz
			}
			up.EndLat, up.EndLon, up.ClearEndCoords = flightcoord.AirportCoords(code, 0, 0)
			changed = true
		}
	}
	return up, changed
}

func (a *API) dismissPlanPart(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid ID.")
		return
	}
	me := auth.UserFrom(r.Context())
	if err := a.requirePartEdit(r.Context(), id, me, w); err != nil {
		return
	}
	if err := a.Store.DismissPlanPart(r.Context(), id); err != nil {
		handleStoreErr(w, err)
		return
	}
	if planID, tripID, err := a.Store.PlanIDForPart(r.Context(), id); err == nil {
		a.publishPlanUpdated(r.Context(), tripID, planID)
	}
	w.WriteHeader(http.StatusNoContent)
}

// ----- assembly helpers -----

func toCreatePartPayload(planType string, p planPartReq) store.CreatePlanPartPayload {
	out := store.CreatePlanPartPayload{
		Seq:          p.Seq,
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
	}
	switch planType {
	case "flight":
		if p.Flight != nil {
			out.Flight = &store.FlightDetail{
				Ident:        p.Flight.Ident,
				ICAO24:       p.Flight.ICAO24,
				Callsign:     strPtrIfSet(p.Flight.Callsign),
				ScheduledOut: defaultTime(p.Flight.ScheduledOut, p.StartsAt),
				ScheduledIn:  defaultTime(p.Flight.ScheduledIn, endOr(p.EndsAt, p.StartsAt)),
				EstimatedOut: p.Flight.EstimatedOut,
				EstimatedIn:  p.Flight.EstimatedIn,
				ActualOut:    p.Flight.ActualOut,
				ActualIn:     p.Flight.ActualIn,
				OriginIATA:   p.Flight.OriginIATA,
				DestIATA:     p.Flight.DestIATA,
				FlightStatus: p.Flight.FlightStatus,
			}
		} else {
			out.Flight = &store.FlightDetail{
				ScheduledOut: p.StartsAt,
				ScheduledIn:  endOr(p.EndsAt, p.StartsAt),
			}
		}
	case "hotel":
		if p.Hotel != nil {
			out.Hotel = &store.HotelDetail{
				PropertyName:     p.Hotel.PropertyName,
				Address:          p.Hotel.Address,
				Phone:            p.Hotel.Phone,
				RoomType:         p.Hotel.RoomType,
				Guests:           p.Hotel.Guests,
				StandardCheckin:  p.Hotel.StandardCheckin,
				StandardCheckout: p.Hotel.StandardCheckout,
			}
		}
	case "train":
		if p.Train != nil {
			out.Train = &store.TrainDetail{
				Operator:  p.Train.Operator,
				ServiceNo: p.Train.ServiceNo,
				Coach:     p.Train.Coach,
				Seat:      p.Train.Seat,
				Class:     p.Train.Class,
				Platform:  p.Train.Platform,
			}
		}
	case "ground":
		if p.Ground != nil {
			out.Ground = &store.GroundDetail{
				Provider: p.Ground.Provider,
				Phone:    p.Ground.Phone,
				Vehicle:  p.Ground.Vehicle,
				Driver:   p.Ground.Driver,
				Pax:      p.Ground.Pax,
			}
		}
	case "dining":
		if p.Dining != nil {
			out.Dining = &store.DiningDetail{
				PartySize:       p.Dining.PartySize,
				ReservationName: p.Dining.ReservationName,
				Phone:           p.Dining.Phone,
			}
		}
	case "excursion":
		if p.Excursion != nil {
			out.Excursion = &store.ExcursionDetail{
				Provider:    p.Excursion.Provider,
				TicketCount: p.Excursion.TicketCount,
			}
		}
	}
	return out
}

func strPtrIfSet(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func defaultTime(t, fallback time.Time) time.Time {
	if t.IsZero() {
		return fallback
	}
	return t
}

func endOr(t *time.Time, fallback time.Time) time.Time {
	if t != nil {
		return *t
	}
	return fallback
}

// planDTO assembles a full PlanDTO: the plan row, its passengers, visibility,
// and every part with its type-specific detail (and smart hotel times).
// viewerID is the requesting user, used to compute alert_opted_in; pass 0 for
// server-side assembly with no viewer (the flag is then false).
func (a *API) planDTO(ctx context.Context, planID, viewerID int64) (api.PlanDTO, error) {
	plan, err := a.Store.PlanByID(ctx, planID)
	if err != nil {
		return api.PlanDTO{}, err
	}
	pax, err := a.Store.PassengersByPlan(ctx, []int64{planID})
	if err != nil {
		return api.PlanDTO{}, err
	}
	vis, err := a.Store.PlanVisibilityFor(ctx, planID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return api.PlanDTO{}, err
	}
	visDTO := api.PlanVisibilityDTO{Mode: "everyone", UserIDs: []int64{}}
	if vis != nil {
		visDTO.Mode = vis.Mode
		if vis.UserIDs != nil {
			visDTO.UserIDs = vis.UserIDs
		}
	}
	parts, err := a.Store.PartsByPlan(ctx, planID)
	if err != nil {
		return api.PlanDTO{}, err
	}
	// Gather flight parts in the trip for hotel smart-times.
	flights, err := a.tripFlightParts(ctx, plan.TripID)
	if err != nil {
		return api.PlanDTO{}, err
	}
	// Batch-load the live position + flown track for this plan's flight parts so
	// the map can draw them (parity with the tracker view).
	flightIDs := make([]int64, 0, len(parts))
	for _, p := range parts {
		if p.Type == "flight" {
			flightIDs = append(flightIDs, p.ID)
		}
	}
	latest, err := a.Store.LatestPartPositions(ctx, flightIDs)
	if err != nil {
		return api.PlanDTO{}, err
	}
	tracks, err := a.Store.PartTracks(ctx, flightIDs, 0)
	if err != nil {
		return api.PlanDTO{}, err
	}
	partDTOs := make([]api.PlanPartDTO, 0, len(parts))
	for _, p := range parts {
		dto, err := a.partDTOWithPositions(ctx, p, flights, latest[p.ID], tracks[p.ID])
		if err != nil {
			return api.PlanDTO{}, err
		}
		partDTOs = append(partDTOs, dto)
	}
	pids := pax[planID]
	if pids == nil {
		pids = []int64{}
	}
	// Surface who added the plan + who's on it on each part, so the map view
	// can show it (parity with the tracker).
	userIDs := append([]int64{}, pids...)
	if plan.CreatedBy != nil {
		userIDs = append(userIDs, *plan.CreatedBy)
	}
	people, err := a.Store.UsersByIDs(ctx, userIDs)
	if err != nil {
		return api.PlanDTO{}, err
	}
	var ownerDTO *api.UserDTO
	if plan.CreatedBy != nil {
		if u := people[*plan.CreatedBy]; u != nil {
			d := api.ToUserDTO(u)
			ownerDTO = &d
		}
	}
	paxDTOs := make([]api.UserDTO, 0, len(pids))
	for _, uid := range pids {
		if u := people[uid]; u != nil {
			paxDTOs = append(paxDTOs, api.ToUserDTO(u))
		}
	}
	tripOwners, err := a.Store.TripOwnersByPlan(ctx, []int64{planID})
	if err != nil {
		return api.PlanDTO{}, err
	}
	for i := range partDTOs {
		partDTOs[i].Owner = ownerDTO
		partDTOs[i].TripOwnerID = tripOwners[planID]
		partDTOs[i].SupplierName = plan.SupplierName
		if len(paxDTOs) > 0 {
			partDTOs[i].Passengers = paxDTOs
		}
	}
	attachments := make([]api.AttachmentDTO, 0)
	if a.Attachments != nil {
		atts, err := a.Store.AttachmentsByPlan(ctx, planID)
		if err != nil {
			return api.PlanDTO{}, err
		}
		for _, att := range atts {
			attachments = append(attachments, api.ToAttachmentDTO(att))
		}
	}
	var optedIn bool
	reminder := store.PlanReminder{Override: "inherit", LeadHours: 24}
	if viewerID != 0 {
		optedIn, err = a.Store.PlanAlertOptedIn(ctx, planID, viewerID)
		if err != nil {
			return api.PlanDTO{}, err
		}
		reminder, err = a.Store.PlanReminderFor(ctx, planID, viewerID)
		if err != nil {
			return api.PlanDTO{}, err
		}
	}
	return api.PlanDTO{
		ID:                plan.ID,
		TripID:            plan.TripID,
		Type:              plan.Type,
		Title:             plan.Title,
		ConfirmationRef:   plan.ConfirmationRef,
		TicketNumber:      plan.TicketNumber,
		Notes:             plan.Notes,
		Source:            plan.Source,
		CostAmount:        plan.CostAmount,
		CostCurrency:      plan.CostCurrency,
		SupplierName:      plan.SupplierName,
		ContactEmail:      plan.ContactEmail,
		ContactPhone:      plan.ContactPhone,
		Website:           plan.Website,
		CreatedBy:         plan.CreatedBy,
		PassengerIDs:      pids,
		Visibility:        visDTO,
		AlertOptedIn:      optedIn,
		ReminderOverride:  reminder.Override,
		ReminderLeadHours: reminder.LeadHours,
		ShareAllFriends:   plan.ShareAllFriends,
		Parts:             partDTOs,
		Attachments:       attachments,
		CreatedAt:         plan.CreatedAt,
		UpdatedAt:         plan.UpdatedAt,
	}, nil
}

// partDTO assembles a single part DTO, fetching the trip's flight parts for the
// hotel smart-times calc when needed.
func (a *API) partDTO(ctx context.Context, p *store.PlanPart) (api.PlanPartDTO, error) {
	plan, err := a.Store.PlanByID(ctx, p.PlanID)
	if err != nil {
		return api.PlanPartDTO{}, err
	}
	var flights []*store.PlanPart
	if p.Type == "hotel" {
		flights, err = a.tripFlightParts(ctx, plan.TripID)
		if err != nil {
			return api.PlanPartDTO{}, err
		}
	}
	return a.partDTOWithFlights(ctx, p, flights)
}

// partDTOWithFlights builds a part DTO with the correct satellite loaded and,
// for hotel parts, the §10 smart check-in/out times derived from the supplied
// trip flight parts. No live position/track is attached.
func (a *API) partDTOWithFlights(ctx context.Context, p *store.PlanPart, tripFlights []*store.PlanPart) (api.PlanPartDTO, error) {
	return a.partDTOWithPositions(ctx, p, tripFlights, nil, nil)
}

// partDTOWithPositions is partDTOWithFlights plus an optional latest position +
// flown track for a flight part — so the map can draw the live marker + flown
// polyline. latest/track are ignored for non-flight parts.
func (a *API) partDTOWithPositions(ctx context.Context, p *store.PlanPart, tripFlights []*store.PlanPart, latest *store.Position, track []*store.Position) (api.PlanPartDTO, error) {
	var (
		flight    *store.FlightDetail
		hotel     *store.HotelDetail
		train     *store.TrainDetail
		ground    *store.GroundDetail
		dining    *store.DiningDetail
		excursion *store.ExcursionDetail
		err       error
	)
	switch p.Type {
	case "flight":
		flight, err = a.Store.FlightDetailFor(ctx, p.ID)
	case "hotel":
		hotel, err = a.Store.HotelDetailFor(ctx, p.ID)
	case "train":
		train, err = a.Store.TrainDetailFor(ctx, p.ID)
	case "ground":
		ground, err = a.Store.GroundDetailFor(ctx, p.ID)
	case "dining":
		dining, err = a.Store.DiningDetailFor(ctx, p.ID)
	case "excursion":
		excursion, err = a.Store.ExcursionDetailFor(ctx, p.ID)
	}
	if err != nil {
		return api.PlanPartDTO{}, err
	}
	dto := api.ToPlanPartDTO(p, flight, hotel, train, ground, dining, excursion, latest, track)
	if p.Type == "hotel" && dto.Hotel != nil {
		applyHotelSmartTimes(p, hotel, tripFlights, dto.Hotel)
		// Order the timeline/map by the smart check-in (after the inbound
		// flight's arrival) rather than the raw default check-in, so a hotel
		// doesn't sort ahead of the flight that gets you there.
		if dto.Hotel.CheckinSuggested != nil {
			dto.EffectiveAt = *dto.Hotel.CheckinSuggested
		}
	}
	return dto, nil
}

// applyHotelSmartTimes computes the §10 suggestions and writes them onto the
// hotel detail DTO when a flanking flight exists; otherwise leaves them nil.
func applyHotelSmartTimes(stay *store.PlanPart, detail *store.HotelDetail, tripFlights []*store.PlanPart, out *api.HotelDetailDTO) {
	flanking := flankingFlights(stay, tripFlights)
	res := planops.SuggestHotelTimes(stay, detail, flanking)
	out.CheckinSuggested = res.Checkin
	out.CheckoutSuggested = res.Checkout
}

// flankingFlights finds the inbound (the flight that brought the traveller to
// the stay) and outbound (the one that takes them home) flight parts.
//
// Classification is by calendar day, NOT by the stored check-in/out instant:
// a hotel's stored check-in is often just the property's default (15:00), which
// can be *earlier* than the flight that actually arrives that afternoon. Keying
// off the instant then mistakes the arriving flight for an outbound one and the
// smart-times calc finds no inbound. So: a flight arriving on or before the
// check-in day is an inbound candidate (latest arrival wins); one departing on
// or after the check-out day is an outbound candidate (earliest departure wins).
func flankingFlights(stay *store.PlanPart, flights []*store.PlanPart) planops.HotelTimeFlights {
	var f planops.HotelTimeFlights
	ciDay := dayOf(stay.StartsAt)
	coDay := ciDay
	if stay.EndsAt != nil {
		coDay = dayOf(*stay.EndsAt)
	}
	for _, fl := range flights {
		arrival := inboundArrival(fl)
		if !dayOf(arrival).After(ciDay) {
			if f.Inbound == nil || arrival.After(inboundArrival(f.Inbound)) {
				f.Inbound = fl
			}
		}
		if !dayOf(fl.StartsAt).Before(coDay) {
			if f.Outbound == nil || fl.StartsAt.Before(f.Outbound.StartsAt) {
				f.Outbound = fl
			}
		}
	}
	return f
}

// dayOf truncates an instant to its UTC calendar day, for day-granular
// comparisons that don't depend on the (sometimes defaulted) time of day.
func dayOf(t time.Time) time.Time {
	return t.UTC().Truncate(24 * time.Hour)
}

func inboundArrival(p *store.PlanPart) time.Time {
	if p.EndsAt != nil {
		return *p.EndsAt
	}
	return p.StartsAt
}

// tripFlightParts returns the (non-dismissed) flight parts in a trip, used as
// candidates for the hotel smart-times calc.
func (a *API) tripFlightParts(ctx context.Context, tripID int64) ([]*store.PlanPart, error) {
	plans, err := a.Store.PlansByTrip(ctx, tripID)
	if err != nil {
		return nil, err
	}
	var out []*store.PlanPart
	for _, pl := range plans {
		if pl.Type != "flight" {
			continue
		}
		parts, err := a.Store.PartsByPlan(ctx, pl.ID)
		if err != nil {
			return nil, err
		}
		for _, part := range parts {
			if part.DismissedAt != nil {
				continue
			}
			out = append(out, part)
		}
	}
	return out, nil
}

// visiblePlanDTOs returns the plans in a trip the viewer is allowed to see,
// fully assembled. The §4 predicate is applied per plan.
func (a *API) visiblePlanDTOs(r *http.Request, tripID int64, u *store.User) ([]api.PlanDTO, error) {
	ctx := r.Context()
	plans, err := a.Store.PlansByTrip(ctx, tripID)
	if err != nil {
		return nil, err
	}
	out := make([]api.PlanDTO, 0, len(plans))
	for _, pl := range plans {
		ok, err := a.Store.CanViewPlan(ctx, pl.ID, u.ID, u.IsSuperuser)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		dto, err := a.planDTO(ctx, pl.ID, u.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, dto)
	}
	return out, nil
}

// ----- plan authorization -----

// requirePlanEdit gates a plan mutation on editor rights over the plan's trip.
func (a *API) requirePlanEdit(ctx context.Context, planID int64, u *store.User, w http.ResponseWriter) error {
	if u == nil {
		writeError(w, http.StatusUnauthorized, "Unauthorised.")
		return errors.New("unauthorized")
	}
	plan, err := a.Store.PlanByID(ctx, planID)
	if err != nil {
		handleStoreErr(w, err)
		return err
	}
	if u.IsSuperuser {
		return nil
	}
	ok, err := a.Store.CanEditTrip(ctx, plan.TripID, u.ID)
	if err != nil {
		handleStoreErr(w, err)
		return err
	}
	if !ok {
		writeError(w, http.StatusForbidden, "Forbidden.")
		return errors.New("forbidden")
	}
	return nil
}

// requirePartEdit gates a part mutation on editor rights over the owning plan's
// trip.
func (a *API) requirePartEdit(ctx context.Context, partID int64, u *store.User, w http.ResponseWriter) error {
	if u == nil {
		writeError(w, http.StatusUnauthorized, "Unauthorised.")
		return errors.New("unauthorized")
	}
	_, tripID, err := a.Store.PlanIDForPart(ctx, partID)
	if err != nil {
		handleStoreErr(w, err)
		return err
	}
	if u.IsSuperuser {
		return nil
	}
	ok, err := a.Store.CanEditTrip(ctx, tripID, u.ID)
	if err != nil {
		handleStoreErr(w, err)
		return err
	}
	if !ok {
		writeError(w, http.StatusForbidden, "Forbidden.")
		return errors.New("forbidden")
	}
	return nil
}
