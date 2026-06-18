package handlers

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/dpage/aerly/internal/api"
	"github.com/dpage/aerly/internal/auth"
	"github.com/dpage/aerly/internal/geocode"
	"github.com/dpage/aerly/internal/importics"
	"github.com/dpage/aerly/internal/planops"
	"github.com/dpage/aerly/internal/store"
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
		added, skipped, planIDs, err := a.commitImportedPlans(r, deps, trip.ID, me.ID, mt.Plans)
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		totalAdded += added
		totalSkipped += skipped
		// Plot the imported plans, then auto-fill the trip's destination (and
		// flag) from where it spends the most time — calendars carry no
		// destination field of their own.
		a.geocodeAndDeriveImportedTripAsync(trip.ID, planIDs)

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
// counts plus the IDs of the newly-created plans. Flight-coordinate resolution
// is queued per plan; address geocoding and destination derivation are driven
// together at the trip level by the caller (see geocodeAndDeriveImportedTripAsync)
// so the destination is picked only once every plan is plotted.
func (a *API) commitImportedPlans(r *http.Request, deps planops.Deps, tripID, userID int64, plans []planops.ConfirmPlanInput) (added, skipped int, planIDs []int64, err error) {
	for _, plan := range plans {
		if plan.TripItUID != "" {
			exists, err := a.Store.PlanExistsByTripItUID(r.Context(), tripID, plan.TripItUID)
			if err != nil {
				return added, skipped, planIDs, err
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
			return added, skipped, planIDs, err
		}
		added++
		for _, pl := range created {
			a.publishPlanUpdated(r.Context(), tripID, pl.ID)
			// Resolve flight legs whose airports aren't in the embedded table
			// (e.g. NQY/FAO); async + best-effort. Address geocoding is handled
			// at the trip level afterwards.
			a.resolveFlightCoordsAsync(tripID, pl.ID)
			planIDs = append(planIDs, pl.ID)
		}
	}
	return added, skipped, planIDs, nil
}

// geocodeAndDeriveImportedTripAsync geocodes a freshly-imported trip's plan
// parts, then derives its destination (and flag) from where it spends the most
// time. The two run in one ordered background task — geocode first, derive after
// — so the destination is chosen from fully-plotted plans rather than racing the
// per-plan geocode (which lets the fastest endpoint, usually the origin, win).
// A no-op without a geocoder; the startup backfill is the safety net.
func (a *API) geocodeAndDeriveImportedTripAsync(tripID int64, planIDs []int64) {
	if a.Geocoder == nil {
		return
	}
	go func() {
		// Geocoding several plans at ~1 req/s plus the reverse lookups can take a
		// while; allow a few minutes.
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		for _, planID := range planIDs {
			if changed, err := geocode.PlanParts(ctx, a.Store, a.Geocoder, planID); err == nil && changed {
				a.publishPlanUpdated(ctx, tripID, planID)
			}
		}
		t, err := a.Store.TripByID(ctx, tripID)
		if err != nil {
			return
		}
		if a.deriveAndStoreTripPlace(ctx, t) {
			a.publishTripUpdated(ctx, tripID)
		}
	}()
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
