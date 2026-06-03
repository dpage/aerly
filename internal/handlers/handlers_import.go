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
	"github.com/dpage/aerly/internal/tripitics"
)

// importTrip imports a whole .ics from a recognised source (currently TripIt)
// as its own trip. Unlike ingestTrip (which proposes plans into an already-open
// trip for review), this creates the trip from the calendar itself and
// auto-commits: the export is structured, so there's nothing to confirm, and no
// LLM is involved.
//
// It is idempotent. The trip is keyed on the source trip id, so re-importing
// the same .ics reuses the trip rather than duplicating it, and each plan is
// keyed on its source event UID so already-imported plans are skipped. This is
// the bulk-history path: upload an export and a fully-formed trip appears.
func (a *API) importTrip(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	text, docs, err := parseIngestBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	data, ok := icalUpload(text, docs)
	if !ok {
		writeError(w, http.StatusBadRequest, "no iCalendar (.ics) content found")
		return
	}
	cal, err := tripitics.Parse(bytes.NewReader(data))
	if err != nil {
		writeError(w, http.StatusBadRequest, "could not parse the .ics")
		return
	}
	mt, _, ok := tripitics.Map(cal)
	if !ok {
		writeError(w, http.StatusUnprocessableEntity,
			"couldn't recognise this calendar as a supported source (currently TripIt)")
		return
	}

	trip, err := a.findOrCreateImportTrip(r.Context(), me.ID, mt)
	if err != nil {
		handleStoreErr(w, err)
		return
	}

	deps := planops.Deps{Store: a.Store, Extractor: a.Extractor, Resolver: a.Resolver}
	added, skipped := 0, 0
	for _, plan := range mt.Plans {
		if plan.TripItUID != "" {
			exists, err := a.Store.PlanExistsByTripItUID(r.Context(), trip.ID, plan.TripItUID)
			if err != nil {
				handleStoreErr(w, err)
				return
			}
			if exists {
				skipped++
				continue
			}
		}
		// The importer is the sole passenger; they can re-share afterwards.
		plan.PassengerIDs = []int64{me.ID}
		created, err := planops.Commit(r.Context(), deps, trip.ID, me.ID, []planops.ConfirmPlanInput{plan})
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		added++
		for _, pl := range created {
			a.publishPlanUpdated(r.Context(), trip.ID, pl.ID)
		}
	}

	dto, err := a.tripDTO(r, trip, me.ID)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	a.publishTripUpdated(r.Context(), trip.ID)
	writeJSON(w, http.StatusOK, api.ImportResultDTO{Trip: dto, Added: added, Skipped: skipped})
}

// findOrCreateImportTrip reuses the caller's trip previously imported from the
// same source trip id (so re-import doesn't duplicate it), otherwise creates a
// new trip from the mapped name/dates.
func (a *API) findOrCreateImportTrip(ctx context.Context, userID int64, mt *tripitics.MappedTrip) (*store.Trip, error) {
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
