package handlers

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/dpage/aerly/internal/airports"
	"github.com/dpage/aerly/internal/providers"
)

type resolveReq struct {
	Ident string `json:"ident"`
	Date  string `json:"date"` // YYYY-MM-DD (UTC)
}

type resolvedFlightDTO struct {
	Ident        string    `json:"ident"`
	ScheduledOut time.Time `json:"scheduled_out"`
	ScheduledIn  time.Time `json:"scheduled_in"`
	OriginIATA   string    `json:"origin_iata"`
	OriginLat    float64   `json:"origin_lat"`
	OriginLon    float64   `json:"origin_lon"`
	OriginTZ     string    `json:"origin_tz,omitempty"`
	DestIATA     string    `json:"dest_iata"`
	DestLat      float64   `json:"dest_lat"`
	DestLon      float64   `json:"dest_lon"`
	DestTZ       string    `json:"dest_tz,omitempty"`
	ICAO24       string    `json:"icao24"`
	Notes        string    `json:"notes"`
}

func (a *API) resolveFlight(w http.ResponseWriter, r *http.Request) {
	if a.Resolver == nil {
		writeError(w, http.StatusNotImplemented,
			"No flight resolver is configured on this server.")
		return
	}
	var in resolveReq
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if in.Ident == "" || in.Date == "" {
		writeError(w, http.StatusBadRequest, "Both ident and date are required.")
		return
	}
	date, err := time.Parse("2006-01-02", in.Date)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Date must be in YYYY-MM-DD format.")
		return
	}
	rf, err := a.Resolver.Resolve(r.Context(), in.Ident, date)
	if err != nil {
		// Surface only the curated, user-facing sentinels; anything else may
		// carry provider URLs/quota detail, so log it and return a generic
		// message rather than echoing the raw error to the client.
		switch {
		case errors.Is(err, providers.ErrFlightNotFound):
			writeError(w, http.StatusUnprocessableEntity, "Flight not found for that date.")
		case errors.Is(err, providers.ErrFlightUnscheduled):
			writeError(w, http.StatusUnprocessableEntity, "No published schedule for that flight/date yet.")
		default:
			// Keep the 422 the client contract uses, but don't echo the raw
			// error — it can carry provider URLs/quota detail; log it instead.
			slog.Error("resolveFlight: resolver error", "err", err, "ident", in.Ident)
			writeError(w, http.StatusUnprocessableEntity, "Could not resolve that flight.")
		}
		return
	}
	originTZ, _ := airports.LookupTZ(rf.OriginIATA)
	destTZ, _ := airports.LookupTZ(rf.DestIATA)
	writeJSON(w, http.StatusOK, resolvedFlightDTO{
		Ident:        rf.Ident,
		ScheduledOut: rf.ScheduledOut,
		ScheduledIn:  rf.ScheduledIn,
		OriginIATA:   rf.OriginIATA,
		OriginLat:    rf.OriginLat,
		OriginLon:    rf.OriginLon,
		OriginTZ:     originTZ,
		DestIATA:     rf.DestIATA,
		DestLat:      rf.DestLat,
		DestLon:      rf.DestLon,
		DestTZ:       destTZ,
		ICAO24:       rf.ICAO24,
		Notes:        rf.Notes,
	})
}
