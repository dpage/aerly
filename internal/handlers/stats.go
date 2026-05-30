package handlers

import (
	"net/http"

	"github.com/dpage/aerly/internal/api"
	"github.com/dpage/aerly/internal/auth"
)

// listMyFlights backs GET /api/me/flights: the viewer's flight bookings,
// projected into the FlightDTO shape the Statistics dialog consumes. The
// legacy flights table is gone — these are rebuilt from the plan model
// (flight-type plan_parts the viewer is a passenger on). Positions and
// share lists are irrelevant to the rollup, so they are left empty.
func (a *API) listMyFlights(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFrom(r.Context())
	rows, err := a.Store.MyFlights(r.Context(), u.ID)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	out := make([]api.FlightDTO, 0, len(rows))
	for i := range rows {
		f := rows[i].Flight
		out = append(out, api.ToFlightDTO(&f, rows[i].PassengerIDs, nil, nil, nil))
	}
	writeJSON(w, http.StatusOK, out)
}
