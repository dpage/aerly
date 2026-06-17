package handlers

import (
	"bytes"
	"context"
	"errors"
	"net/http"

	"github.com/dpage/aerly/internal/api"
	"github.com/dpage/aerly/internal/auth"
	"github.com/dpage/aerly/internal/planops"
	"github.com/dpage/aerly/internal/store"
	"github.com/dpage/aerly/internal/importics"
)

// importTrip imports a whole .ics from a recognised source (TripIt or Kayak)
// as its own trip(s). Unlike ingestTrip (which proposes plans into an
// already-open trip for review), this creates the trip from the calendar itself
// and auto-commits: the export is structured, so there's nothing to confirm, and
// no LLM is involved.
//
// A TripIt export holds one trip; a Kayak account feed holds many, so each
// mapped trip is created (or reused) and its plans committed independently. It
// is idempotent. Each trip is keyed on the source trip id, so re-importing the
// same .ics reuses the trip rather than duplicating it, and each plan is keyed
// on its source event UID so already-imported plans are skipped. This is the
// bulk-history path: upload an export and fully-formed trips appear.
func (a *API) importTrip(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	text, docs, err := parseIngestBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	data, ok := icalUpload(text, docs)
	if !ok {
		writeError(w, http.StatusBadRequest, "No iCalendar (.ics) content found.")
		return
	}
	cal, err := importics.Parse(bytes.NewReader(data))
	if err != nil {
		writeError(w, http.StatusBadRequest, "Could not parse the .ics.")
		return
	}
	trips, _, ok := importics.MapAll(cal)
	if !ok || len(trips) == 0 {
		writeError(w, http.StatusUnprocessableEntity,
			"Could not recognise this calendar as a supported source (currently TripIt and Kayak).")
		return
	}

	deps := planops.Deps{Store: a.Store, Extractor: a.Extractor, Resolver: a.Resolver}
	totalAdded, totalSkipped := 0, 0
	dtos := make([]api.TripDTO, 0, len(trips))
	for _, mt := range trips {
		trip, err := a.findOrCreateImportTrip(r.Context(), me.ID, mt)
		if err != nil {
			handleStoreErr(w, err)
			return
		}
		added, skipped, err := a.commitImportedPlans(r, deps, trip.ID, me.ID, mt.Plans)
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		totalAdded += added
		totalSkipped += skipped

		dto, err := a.tripDTO(r, trip, me.ID)
		if err != nil {
			handleStoreErr(w, err)
			return
		}
		a.publishTripUpdated(r.Context(), trip.ID)
		dtos = append(dtos, dto)
	}

	writeJSON(w, http.StatusOK, api.ImportResultDTO{
		Trip:    dtos[0],
		Trips:   dtos,
		Added:   totalAdded,
		Skipped: totalSkipped,
	})
}

// commitImportedPlans commits one trip's mapped plans, skipping any already
// imported (matched on their source event UID), and returns the added/skipped
// counts. Each committed plan is published and queued for async geocode /
// flight-coordinate resolution.
func (a *API) commitImportedPlans(r *http.Request, deps planops.Deps, tripID, userID int64, plans []planops.ConfirmPlanInput) (added, skipped int, err error) {
	for _, plan := range plans {
		if plan.TripItUID != "" {
			exists, err := a.Store.PlanExistsByTripItUID(r.Context(), tripID, plan.TripItUID)
			if err != nil {
				return added, skipped, err
			}
			if exists {
				skipped++
				continue
			}
		}
		// The importer is the sole passenger; they can re-share afterwards.
		plan.PassengerIDs = []int64{userID}
		created, err := planops.Commit(r.Context(), deps, tripID, userID, []planops.ConfirmPlanInput{plan})
		if err != nil {
			return added, skipped, err
		}
		added++
		for _, pl := range created {
			a.publishPlanUpdated(r.Context(), tripID, pl.ID)
			// Plot the newly-imported plan: geocode addressed parts (hotels,
			// transfers) and resolve flight legs whose airports aren't in the
			// embedded table (e.g. NQY/FAO). Both are async + best-effort, so a
			// missing geocoder/resolver simply leaves the parts for the next
			// startup backfill / sweep.
			a.geocodePlanAsync(tripID, pl.ID)
			a.resolveFlightCoordsAsync(tripID, pl.ID)
		}
	}
	return added, skipped, nil
}

// findOrCreateImportTrip reuses the caller's trip previously imported from the
// same source trip id (so re-import doesn't duplicate it), otherwise creates a
// new trip from the mapped name/dates.
func (a *API) findOrCreateImportTrip(ctx context.Context, userID int64, mt *importics.MappedTrip) (*store.Trip, error) {
	if mt.TripItID != "" {
		t, err := a.Store.TripByTripItID(ctx, userID, mt.TripItID)
		if err == nil {
			return t, nil
		}
		if !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
	}
	name := mt.Name
	if name == "" {
		name = "Imported trip"
	}
	return a.Store.CreateTrip(ctx, store.CreateTripPayload{
		Name:     name,
		StartsOn: mt.StartsOn,
		EndsOn:   mt.EndsOn,
		TripItID: mt.TripItID,
	}, userID)
}
