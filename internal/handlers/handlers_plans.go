package handlers

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/dpage/aerly/internal/api"
	"github.com/dpage/aerly/internal/auth"
	"github.com/dpage/aerly/internal/planops"
	"github.com/dpage/aerly/internal/store"
)

// Wave 1B: plans, parts, passengers, visibility, and move.

type planPartReq struct {
	Type       string     `json:"type"`
	Seq        int        `json:"seq"`
	StartsAt   time.Time  `json:"starts_at"`
	EndsAt     *time.Time `json:"ends_at"`
	StartTZ    string     `json:"start_tz"`
	EndTZ      string     `json:"end_tz"`
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
	Notes           string             `json:"notes"`
	Source          string             `json:"source"`
	PassengerIDs    []int64            `json:"passenger_ids"`
	Visibility      *planVisibilityReq `json:"visibility"`
	Parts           []planPartReq      `json:"parts"`
}

type updatePlanReq struct {
	Title           *string `json:"title,omitempty"`
	ConfirmationRef *string `json:"confirmation_ref,omitempty"`
	Notes           *string `json:"notes,omitempty"`
}

type planVisibilityReq struct {
	Mode    string  `json:"mode"`
	UserIDs []int64 `json:"user_ids"`
}

type updatePlanPartReq struct {
	StartsAt   *time.Time `json:"starts_at,omitempty"`
	EndsAt     *time.Time `json:"ends_at,omitempty"`
	StartTZ    *string    `json:"start_tz,omitempty"`
	EndTZ      *string    `json:"end_tz,omitempty"`
	StartLabel   *string    `json:"start_label,omitempty"`
	StartLat     *float64   `json:"start_lat,omitempty"`
	StartLon     *float64   `json:"start_lon,omitempty"`
	StartAddress *string    `json:"start_address,omitempty"`
	EndLabel     *string    `json:"end_label,omitempty"`
	EndLat       *float64   `json:"end_lat,omitempty"`
	EndLon       *float64   `json:"end_lon,omitempty"`
	EndAddress   *string    `json:"end_address,omitempty"`
	Status       *string    `json:"status,omitempty"`
}

type moveReq struct {
	TripID int64 `json:"trip_id"`
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
		writeError(w, http.StatusBadRequest, "bad id")
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
		writeError(w, http.StatusBadRequest, "invalid plan type")
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
		Notes:           in.Notes,
		Source:          in.Source,
		Parts:           parts,
	}, me.ID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	for _, uid := range in.PassengerIDs {
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
	writeJSON(w, http.StatusCreated, dto)
}

func (a *API) updatePlan(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
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
		Notes:           in.Notes,
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
		writeError(w, http.StatusBadRequest, "bad id")
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
	a.publishPlanDeleted(r.Context(), plan.TripID, id)
	if err := a.Store.DeletePlan(r.Context(), id); err != nil {
		handleStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) addPlanPassenger(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
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
		writeError(w, http.StatusBadRequest, "user_id required")
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
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	uid, err := pathID(r, "userId")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad userId")
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
		writeError(w, http.StatusBadRequest, "bad id")
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
		writeError(w, http.StatusBadRequest, "invalid visibility mode")
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
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	var in moveReq
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if in.TripID == 0 {
		writeError(w, http.StatusBadRequest, "trip_id required")
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

func (a *API) updatePlanPart(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
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
	// A changed address is re-geocoded synchronously so the map pin moves with
	// the edit (the caller owns time/tz, so we only refresh coordinates).
	// Explicit coordinates in the request, if any, still win.
	if a.Geocoder != nil {
		if cur, cerr := a.Store.PlanPartByID(r.Context(), id); cerr == nil {
			if in.StartAddress != nil && *in.StartAddress != "" && *in.StartAddress != cur.StartAddress && in.StartLat == nil {
				if lat, lon, ok, gerr := a.Geocoder.Geocode(r.Context(), *in.StartAddress); gerr == nil && ok {
					in.StartLat, in.StartLon = &lat, &lon
				}
			}
			if in.EndAddress != nil && *in.EndAddress != "" && *in.EndAddress != cur.EndAddress && in.EndLat == nil {
				if lat, lon, ok, gerr := a.Geocoder.Geocode(r.Context(), *in.EndAddress); gerr == nil && ok {
					in.EndLat, in.EndLon = &lat, &lon
				}
			}
		}
	}
	part, err := a.Store.UpdatePlanPart(r.Context(), id, store.UpdatePlanPartPayload{
		StartsAt:   in.StartsAt,
		EndsAt:     in.EndsAt,
		StartTZ:      in.StartTZ,
		EndTZ:        in.EndTZ,
		StartLabel:   in.StartLabel,
		StartLat:     in.StartLat,
		StartLon:     in.StartLon,
		StartAddress: in.StartAddress,
		EndLabel:     in.EndLabel,
		EndLat:       in.EndLat,
		EndLon:       in.EndLon,
		EndAddress:   in.EndAddress,
		Status:       in.Status,
	})
	if err != nil {
		handleStoreErr(w, err)
		return
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

func (a *API) dismissPlanPart(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
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
	partDTOs := make([]api.PlanPartDTO, 0, len(parts))
	for _, p := range parts {
		dto, err := a.partDTOWithFlights(ctx, p, flights)
		if err != nil {
			return api.PlanDTO{}, err
		}
		partDTOs = append(partDTOs, dto)
	}
	pids := pax[planID]
	if pids == nil {
		pids = []int64{}
	}
	var optedIn bool
	if viewerID != 0 {
		optedIn, err = a.Store.PlanAlertOptedIn(ctx, planID, viewerID)
		if err != nil {
			return api.PlanDTO{}, err
		}
	}
	return api.PlanDTO{
		ID:              plan.ID,
		TripID:          plan.TripID,
		Type:            plan.Type,
		Title:           plan.Title,
		ConfirmationRef: plan.ConfirmationRef,
		Notes:           plan.Notes,
		Source:          plan.Source,
		CreatedBy:       plan.CreatedBy,
		PassengerIDs:    pids,
		Visibility:      visDTO,
		AlertOptedIn:    optedIn,
		Parts:           partDTOs,
		CreatedAt:       plan.CreatedAt,
		UpdatedAt:       plan.UpdatedAt,
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
// trip flight parts.
func (a *API) partDTOWithFlights(ctx context.Context, p *store.PlanPart, tripFlights []*store.PlanPart) (api.PlanPartDTO, error) {
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
	dto := api.ToPlanPartDTO(p, flight, hotel, train, ground, dining, excursion, nil, nil)
	if p.Type == "hotel" && dto.Hotel != nil {
		applyHotelSmartTimes(p, hotel, tripFlights, dto.Hotel)
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

// flankingFlights finds the inbound (latest arriving before the stay begins)
// and outbound (earliest departing after the stay begins) flight parts.
func flankingFlights(stay *store.PlanPart, flights []*store.PlanPart) planops.HotelTimeFlights {
	var f planops.HotelTimeFlights
	stayStart := stay.StartsAt
	for _, fl := range flights {
		arrival := fl.StartsAt
		if fl.EndsAt != nil {
			arrival = *fl.EndsAt
		}
		if !arrival.After(stayStart) {
			if f.Inbound == nil || arrival.After(inboundArrival(f.Inbound)) {
				f.Inbound = fl
			}
		}
		if fl.StartsAt.After(stayStart) {
			if f.Outbound == nil || fl.StartsAt.Before(f.Outbound.StartsAt) {
				f.Outbound = fl
			}
		}
	}
	return f
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
		writeError(w, http.StatusUnauthorized, "unauthorized")
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
		writeError(w, http.StatusForbidden, "forbidden")
		return errors.New("forbidden")
	}
	return nil
}

// requirePartEdit gates a part mutation on editor rights over the owning plan's
// trip.
func (a *API) requirePartEdit(ctx context.Context, partID int64, u *store.User, w http.ResponseWriter) error {
	if u == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
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
		writeError(w, http.StatusForbidden, "forbidden")
		return errors.New("forbidden")
	}
	return nil
}
