package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/dpage/flight-tracker/internal/api"
	"github.com/dpage/flight-tracker/internal/auth"
	"github.com/dpage/flight-tracker/internal/sse"
	"github.com/dpage/flight-tracker/internal/store"
)

type createFlightReq struct {
	Ident        string    `json:"ident"`
	ScheduledOut time.Time `json:"scheduled_out"`
	ScheduledIn  time.Time `json:"scheduled_in"`
	OriginIATA   string    `json:"origin_iata"`
	DestIATA     string    `json:"dest_iata"`
	ICAO24       string    `json:"icao24"`
	Notes        string    `json:"notes"`
	PassengerIDs []int64   `json:"passenger_ids"`
}

type updateFlightReq struct {
	ScheduledOut *time.Time `json:"scheduled_out,omitempty"`
	ScheduledIn  *time.Time `json:"scheduled_in,omitempty"`
	OriginIATA   *string    `json:"origin_iata,omitempty"`
	DestIATA     *string    `json:"dest_iata,omitempty"`
	ICAO24       *string    `json:"icao24,omitempty"`
	Notes        *string    `json:"notes,omitempty"`
	Status       *string    `json:"status,omitempty"`
}

type addPassengerReq struct {
	UserID int64 `json:"user_id"`
}

func (a *API) listFlights(w http.ResponseWriter, r *http.Request) {
	flights, err := a.Store.ListFlights(r.Context())
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	ids := make([]int64, 0, len(flights))
	for _, f := range flights {
		ids = append(ids, f.ID)
	}
	passengers, err := a.Store.PassengersByFlight(r.Context(), ids)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	latest, err := a.Store.LatestPositions(r.Context(), ids)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	tracks, err := a.Store.RecentTracks(r.Context(), ids, 200)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	out := make([]api.FlightDTO, 0, len(flights))
	for _, f := range flights {
		out = append(out, api.ToFlightDTO(f, passengers[f.ID], latest[f.ID], tracks[f.ID]))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) getFlight(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	f, err := a.Store.FlightByID(r.Context(), id)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	passengers, _ := a.Store.PassengersByFlight(r.Context(), []int64{id})
	latest, _ := a.Store.LatestPositions(r.Context(), []int64{id})
	tracks, _ := a.Store.RecentTracks(r.Context(), []int64{id}, 200)
	writeJSON(w, http.StatusOK, api.ToFlightDTO(f, passengers[id], latest[id], tracks[id]))
}

func (a *API) createFlight(w http.ResponseWriter, r *http.Request) {
	var in createFlightReq
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	me := auth.UserFrom(r.Context())
	f, err := a.Store.CreateFlight(r.Context(), store.CreateFlightPayload{
		Ident:        in.Ident,
		ScheduledOut: in.ScheduledOut,
		ScheduledIn:  in.ScheduledIn,
		OriginIATA:   in.OriginIATA,
		DestIATA:     in.DestIATA,
		ICAO24:       in.ICAO24,
		Notes:        in.Notes,
	}, me.ID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	for _, uid := range in.PassengerIDs {
		if err := a.Store.AddPassenger(r.Context(), f.ID, uid); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	passengers, _ := a.Store.PassengersByFlight(r.Context(), []int64{f.ID})
	dto := api.ToFlightDTO(f, passengers[f.ID], nil, nil)
	a.publishFlightUpdate(dto)
	writeJSON(w, http.StatusCreated, dto)
}

func (a *API) updateFlight(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	var in updateFlightReq
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	f, err := a.Store.UpdateFlight(r.Context(), id, store.UpdateFlightPayload{
		ScheduledOut: in.ScheduledOut,
		ScheduledIn:  in.ScheduledIn,
		OriginIATA:   in.OriginIATA,
		DestIATA:     in.DestIATA,
		ICAO24:       in.ICAO24,
		Notes:        in.Notes,
		Status:       in.Status,
	})
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	passengers, _ := a.Store.PassengersByFlight(r.Context(), []int64{id})
	latest, _ := a.Store.LatestPositions(r.Context(), []int64{id})
	tracks, _ := a.Store.RecentTracks(r.Context(), []int64{id}, 200)
	dto := api.ToFlightDTO(f, passengers[id], latest[id], tracks[id])
	a.publishFlightUpdate(dto)
	writeJSON(w, http.StatusOK, dto)
}

func (a *API) deleteFlight(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := a.Store.DeleteFlight(r.Context(), id); err != nil {
		handleStoreErr(w, err)
		return
	}
	a.publishFlightDelete(id)
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) addPassenger(w http.ResponseWriter, r *http.Request) {
	fid, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	var in addPassengerReq
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if in.UserID == 0 {
		writeError(w, http.StatusBadRequest, "user_id required")
		return
	}
	if err := a.Store.AddPassenger(r.Context(), fid, in.UserID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	a.publishFlightByID(r.Context(), fid)
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) removePassenger(w http.ResponseWriter, r *http.Request) {
	fid, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	uid, err := pathID(r, "userId")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad userId")
		return
	}
	if err := a.Store.RemovePassenger(r.Context(), fid, uid); err != nil {
		handleStoreErr(w, err)
		return
	}
	a.publishFlightByID(r.Context(), fid)
	w.WriteHeader(http.StatusNoContent)
}

// publishFlightUpdate fans a flight.updated SSE event to every connected
// client with the given DTO. Mirrors the poller's broadcast format, so the
// frontend can use a single applyFlightUpdate handler for both poll-driven
// refreshes and user-driven write events. Best-effort: marshal errors are
// logged and swallowed (the HTTP response to the caller has already succeeded
// by the time we get here).
func (a *API) publishFlightUpdate(dto api.FlightDTO) {
	if a.Hub == nil {
		return
	}
	payload, err := json.Marshal(dto)
	if err != nil {
		slog.Error("publishFlightUpdate: marshal", "err", err, "id", dto.ID)
		return
	}
	a.Hub.Publish(sse.Event{Type: "flight.updated", Data: payload})
}

// publishFlightByID refetches the flight + its associated passengers,
// positions, and track, then broadcasts the DTO. Used by endpoints that
// mutate a flight indirectly (passenger add/remove) and so don't already
// have a complete DTO in hand.
func (a *API) publishFlightByID(ctx context.Context, id int64) {
	if a.Hub == nil {
		return
	}
	f, err := a.Store.FlightByID(ctx, id)
	if err != nil {
		slog.Warn("publishFlightByID: refetch", "err", err, "id", id)
		return
	}
	passengers, _ := a.Store.PassengersByFlight(ctx, []int64{id})
	latest, _ := a.Store.LatestPositions(ctx, []int64{id})
	tracks, _ := a.Store.RecentTracks(ctx, []int64{id}, 200)
	a.publishFlightUpdate(api.ToFlightDTO(f, passengers[id], latest[id], tracks[id]))
}

// publishFlightDelete fans a flight.deleted SSE event so connected clients
// can drop the flight from their local state. Payload is a minimal {"id":N}
// envelope since the row is gone — no DTO to send.
func (a *API) publishFlightDelete(id int64) {
	if a.Hub == nil {
		return
	}
	payload, err := json.Marshal(struct {
		ID int64 `json:"id"`
	}{ID: id})
	if err != nil {
		slog.Error("publishFlightDelete: marshal", "err", err, "id", id)
		return
	}
	a.Hub.Publish(sse.Event{Type: "flight.deleted", Data: payload})
}
