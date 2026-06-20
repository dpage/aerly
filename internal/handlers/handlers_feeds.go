package handlers

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/dpage/aerly/internal/api"
	"github.com/dpage/aerly/internal/auth"
	"github.com/dpage/aerly/internal/feeds"
	"github.com/dpage/aerly/internal/store"
)

type feedReq struct {
	URL  string `json:"url"`
	Name string `json:"name"`
}

// listTripFeeds returns a trip's registered iCal feeds. Any trip viewer may
// read them (the Edit dialog is editor-only client-side, but the list itself
// leaks nothing a viewer can't already see).
func (a *API) listTripFeeds(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid ID.")
		return
	}
	me := auth.UserFrom(r.Context())
	if ok, err := a.canViewTrip(r.Context(), id, me); err != nil {
		handleStoreErr(w, err)
		return
	} else if !ok {
		writeError(w, http.StatusNotFound, "Not found.")
		return
	}
	feedsList, err := a.Store.ListTripFeeds(r.Context(), id)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	out := make([]api.TripFeedDTO, 0, len(feedsList))
	for _, f := range feedsList {
		out = append(out, api.ToTripFeedDTO(f))
	}
	writeJSON(w, http.StatusOK, out)
}

// addTripFeed registers a new feed URL on a trip (editor action), refreshes it
// once synchronously so its events are ready (and any "not an iCalendar feed"
// error surfaces) by the time the caller re-fetches, and returns the resulting
// feed row.
func (a *API) addTripFeed(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid ID.")
		return
	}
	me := auth.UserFrom(r.Context())
	if err := a.requireTripEdit(r.Context(), id, me, w); err != nil {
		return
	}
	var in feedReq
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	url, err := feeds.NormalizeURL(in.URL)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Enter a valid http(s) calendar feed URL.")
		return
	}
	f, err := a.Store.AddTripFeed(r.Context(), id, url, strings.TrimSpace(in.Name))
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if refreshed := a.refreshFeedNow(f.ID); refreshed != nil {
		f = refreshed
	}
	writeJSON(w, http.StatusCreated, api.ToTripFeedDTO(f))
}

// refreshFeedNow refreshes a feed once with a bounded, request-independent
// timeout and returns the reloaded row (so last_error / last_fetched_at reflect
// the attempt). Best-effort: any fetch/parse error is recorded on the row, not
// returned to the caller; a feed that can't be reloaded falls back to the
// original.
func (a *API) refreshFeedNow(feedID int64) *store.TripFeed {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	f, err := a.Store.TripFeedByID(ctx, feedID)
	if err != nil {
		return nil
	}
	_ = a.Feeds.RefreshFeed(ctx, f) // records last_error on the row
	if reloaded, err := a.Store.TripFeedByID(ctx, feedID); err == nil {
		return reloaded
	}
	return f
}

// updateTripFeed changes a feed's URL and/or name (editor action). A changed
// URL re-fetches from scratch; we always trigger a refresh afterwards.
func (a *API) updateTripFeed(w http.ResponseWriter, r *http.Request) {
	tripID, feedID, ok := a.resolveFeed(w, r)
	if !ok {
		return
	}
	me := auth.UserFrom(r.Context())
	if err := a.requireTripEdit(r.Context(), tripID, me, w); err != nil {
		return
	}
	var in feedReq
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	url, err := feeds.NormalizeURL(in.URL)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Enter a valid http(s) calendar feed URL.")
		return
	}
	f, err := a.Store.UpdateTripFeed(r.Context(), feedID, url, strings.TrimSpace(in.Name))
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if refreshed := a.refreshFeedNow(f.ID); refreshed != nil {
		f = refreshed
	}
	writeJSON(w, http.StatusOK, api.ToTripFeedDTO(f))
}

// deleteTripFeed removes a feed and its cached events (editor action).
func (a *API) deleteTripFeed(w http.ResponseWriter, r *http.Request) {
	tripID, feedID, ok := a.resolveFeed(w, r)
	if !ok {
		return
	}
	me := auth.UserFrom(r.Context())
	if err := a.requireTripEdit(r.Context(), tripID, me, w); err != nil {
		return
	}
	if err := a.Store.DeleteTripFeed(r.Context(), feedID); err != nil {
		handleStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// listTripExternalEvents returns the cached events across all of a trip's
// feeds. Read by any trip viewer; fetched lazily by the client only when the
// "Show external plans" toggle is on, so an unused feature costs nothing.
func (a *API) listTripExternalEvents(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid ID.")
		return
	}
	me := auth.UserFrom(r.Context())
	if ok, err := a.canViewTrip(r.Context(), id, me); err != nil {
		handleStoreErr(w, err)
		return
	} else if !ok {
		writeError(w, http.StatusNotFound, "Not found.")
		return
	}
	events, err := a.Store.TripFeedEventsForTrip(r.Context(), id)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	out := make([]api.ExternalEventDTO, 0, len(events))
	for _, e := range events {
		out = append(out, api.ToExternalEventDTO(e))
	}
	writeJSON(w, http.StatusOK, out)
}

// resolveFeed parses the {id}/{feedId} path pair and confirms the feed belongs
// to the trip, so a feed can't be addressed under an unrelated trip the caller
// happens to be able to edit. Writes the response and returns ok=false on any
// failure.
func (a *API) resolveFeed(w http.ResponseWriter, r *http.Request) (tripID, feedID int64, ok bool) {
	tripID, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid ID.")
		return 0, 0, false
	}
	feedID, err = pathID(r, "feedId")
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid feed ID.")
		return 0, 0, false
	}
	f, err := a.Store.TripFeedByID(r.Context(), feedID)
	if err != nil {
		handleStoreErr(w, err)
		return 0, 0, false
	}
	if f.TripID != tripID {
		writeError(w, http.StatusNotFound, "Not found.")
		return 0, 0, false
	}
	return tripID, feedID, true
}
