package handlers

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/dpage/aerly/internal/api"
	"github.com/dpage/aerly/internal/auth"
	"github.com/dpage/aerly/internal/store"
)

// Wave 1A: trips, members, and tags.

type createTripReq struct {
	Name        string  `json:"name"`
	Destination string  `json:"destination"`
	StartsOn    *string `json:"starts_on"`
	EndsOn      *string `json:"ends_on"`
}

type updateTripReq struct {
	Name        *string `json:"name,omitempty"`
	Destination *string `json:"destination,omitempty"`
	StartsOn    *string `json:"starts_on,omitempty"`
	EndsOn      *string `json:"ends_on,omitempty"`
}

type addTripMemberReq struct {
	UserID int64  `json:"user_id"`
	Role   string `json:"role"`
}

type setTagsReq struct {
	Labels []string `json:"labels"`
}

// parseDate parses a "YYYY-MM-DD" string into a *time.Time, or nil for an empty
// string. A malformed value is reported as an error.
func parseDate(s *string) (*time.Time, error) {
	if s == nil || *s == "" {
		return nil, nil
	}
	t, err := time.Parse("2006-01-02", *s)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (a *API) listTrips(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	// Superuser-only diagnostic scopes (?include=friends|all): show all of the
	// viewer's friends' trips (even unshared), or every trip in the system. Any
	// include value from a non-superuser is ignored (normal owner+member list).
	var (
		trips []*store.Trip
		err   error
	)
	switch include := r.URL.Query().Get("include"); {
	case include == "friends" && me.IsSuperuser:
		trips, err = a.Store.ListFriendsTrips(r.Context(), me.ID)
	case include == "all" && me.IsSuperuser:
		trips, err = a.Store.ListAllTrips(r.Context())
	default:
		trips, err = a.Store.ListTrips(r.Context(), me.ID)
	}
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	out := make([]api.TripDTO, 0, len(trips))
	for _, t := range trips {
		dto, err := a.tripDTO(r, t, me.ID)
		if err != nil {
			handleStoreErr(w, err)
			return
		}
		// Lazily derive the flag country for any trip that hasn't got one yet
		// (covers both UI- and email-created trips, since both surface here).
		// Fire-and-forget: it republishes the trip when done so the flag appears.
		if t.CountryCode == "" {
			a.deriveTripCountryAsync(t.ID)
		}
		out = append(out, dto)
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) createTrip(w http.ResponseWriter, r *http.Request) {
	var in createTripReq
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if in.Name == "" {
		writeError(w, http.StatusBadRequest, "name required")
		return
	}
	starts, err := parseDate(in.StartsOn)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad starts_on")
		return
	}
	ends, err := parseDate(in.EndsOn)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad ends_on")
		return
	}
	me := auth.UserFrom(r.Context())
	t, err := a.Store.CreateTrip(r.Context(), store.CreateTripPayload{
		Name:        in.Name,
		Destination: in.Destination,
		StartsOn:    starts,
		EndsOn:      ends,
	}, me.ID)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	dto, err := a.tripDTO(r, t, me.ID)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	a.publishTripUpdated(r.Context(), t.ID)
	writeJSON(w, http.StatusCreated, dto)
}

// getTrip returns the trip plus its visible plans + parts (the timeline
// payload). The frontend contract is Trip & { plans: Plan[] }.
func (a *API) getTrip(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	me := auth.UserFrom(r.Context())
	ok, err := a.canViewTrip(r.Context(), id, me)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	t, err := a.Store.TripByID(r.Context(), id)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	tripDTO, err := a.tripDTO(r, t, me.ID)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	plans, err := a.visiblePlanDTOs(r, id, me)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	// Embed plans alongside the trip fields per the FE contract.
	writeJSON(w, http.StatusOK, struct {
		api.TripDTO
		Plans []api.PlanDTO `json:"plans"`
	}{TripDTO: tripDTO, Plans: plans})
}

func (a *API) updateTrip(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	me := auth.UserFrom(r.Context())
	if err := a.requireTripEdit(r.Context(), id, me, w); err != nil {
		return
	}
	var in updateTripReq
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	starts, err := parseDate(in.StartsOn)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad starts_on")
		return
	}
	ends, err := parseDate(in.EndsOn)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad ends_on")
		return
	}
	t, err := a.Store.UpdateTrip(r.Context(), id, store.UpdateTripPayload{
		Name:        in.Name,
		Destination: in.Destination,
		StartsOn:    starts,
		EndsOn:      ends,
	})
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	dto, err := a.tripDTO(r, t, me.ID)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	// The destination may have changed, so re-derive the flag country: clear it
	// and kick off a fresh derivation (which republishes when done).
	if err := a.Store.SetTripCountry(r.Context(), t.ID, ""); err == nil {
		a.deriveTripCountryAsync(t.ID)
	}
	a.publishTripUpdated(r.Context(), t.ID)
	writeJSON(w, http.StatusOK, dto)
}

func (a *API) deleteTrip(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	me := auth.UserFrom(r.Context())
	// Only the owner may delete a trip.
	if err := a.requireTripOwner(r.Context(), id, me, w); err != nil {
		return
	}
	// Resolve the member set before the delete: the membership rows (and thus
	// the trip.updated VisibleTo) are gone once the trip row is removed.
	a.publishTripUpdated(r.Context(), id)
	if err := a.Store.DeleteTrip(r.Context(), id); err != nil {
		handleStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) addTripMember(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	me := auth.UserFrom(r.Context())
	// Managing members is an owner action.
	if err := a.requireTripOwner(r.Context(), id, me, w); err != nil {
		return
	}
	var in addTripMemberReq
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if in.UserID == 0 {
		writeError(w, http.StatusBadRequest, "user_id required")
		return
	}
	if in.Role != "editor" && in.Role != "viewer" && in.Role != "owner" {
		writeError(w, http.StatusBadRequest, "role must be owner, editor, or viewer")
		return
	}
	if err := a.requireFriendTarget(r.Context(), me, in.UserID, w); err != nil {
		return
	}
	if err := a.Store.AddTripMember(r.Context(), id, in.UserID, in.Role); err != nil {
		handleStoreErr(w, err)
		return
	}
	t, err := a.Store.TripByID(r.Context(), id)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	dto, err := a.tripDTO(r, t, me.ID)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	a.publishTripUpdated(r.Context(), id)
	writeJSON(w, http.StatusOK, dto)
}

func (a *API) removeTripMember(w http.ResponseWriter, r *http.Request) {
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
	if err := a.requireTripOwner(r.Context(), id, me, w); err != nil {
		return
	}
	if err := a.Store.RemoveTripMember(r.Context(), id, uid); err != nil {
		handleStoreErr(w, err)
		return
	}
	a.publishTripUpdated(r.Context(), id)
	w.WriteHeader(http.StatusNoContent)
}

type tripPassengerReq struct {
	UserID int64 `json:"user_id"`
}

// addTripPassenger adds a trip-level passenger: a traveller on the whole trip,
// a passenger on every plan in it (#20). Any trip member may add one of their
// accepted friends (so e.g. a passenger can bring their partner), matching the
// per-plan passenger gate.
func (a *API) addTripPassenger(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	me := auth.UserFrom(r.Context())
	// The actor must be on the trip (a member) — but need not be the owner, so a
	// passenger can add their partner.
	if ok, err := a.canViewTrip(r.Context(), id, me); err != nil {
		handleStoreErr(w, err)
		return
	} else if !ok {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	var in tripPassengerReq
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if in.UserID == 0 {
		writeError(w, http.StatusBadRequest, "user_id required")
		return
	}
	if err := a.requireFriendTarget(r.Context(), me, in.UserID, w); err != nil {
		return
	}
	if err := a.Store.AddTripPassenger(r.Context(), id, in.UserID); err != nil {
		handleStoreErr(w, err)
		return
	}
	t, err := a.Store.TripByID(r.Context(), id)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	dto, err := a.tripDTO(r, t, me.ID)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	a.publishTripUpdated(r.Context(), id)
	writeJSON(w, http.StatusOK, dto)
}

// removeTripPassenger removes a trip-level passenger. Trip editors/owners may
// remove anyone; a user may always remove themselves.
func (a *API) removeTripPassenger(w http.ResponseWriter, r *http.Request) {
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
	if me == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if me.ID != uid {
		if err := a.requireTripEdit(r.Context(), id, me, w); err != nil {
			return
		}
	}
	if err := a.Store.RemoveTripPassenger(r.Context(), id, uid); err != nil {
		handleStoreErr(w, err)
		return
	}
	a.publishTripUpdated(r.Context(), id)
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) setTripTags(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	me := auth.UserFrom(r.Context())
	if err := a.requireTripEdit(r.Context(), id, me, w); err != nil {
		return
	}
	var in setTagsReq
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := a.Store.SetTripTags(r.Context(), id, in.Labels); err != nil {
		handleStoreErr(w, err)
		return
	}
	t, err := a.Store.TripByID(r.Context(), id)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	dto, err := a.tripDTO(r, t, me.ID)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	a.publishTripUpdated(r.Context(), id)
	writeJSON(w, http.StatusOK, dto)
}

func (a *API) suggestTags(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	q := r.URL.Query().Get("q")
	labels, err := a.Store.SuggestTags(r.Context(), me.ID, q)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	out := make([]api.TagSuggestionDTO, 0, len(labels))
	for _, l := range labels {
		out = append(out, api.TagSuggestionDTO{Label: l})
	}
	writeJSON(w, http.StatusOK, out)
}

// ----- shared helpers -----

// tripDTO gathers the viewer's role, members, and tags for a trip and projects
// it. Superusers who aren't members report role "viewer".
func (a *API) tripDTO(r *http.Request, t *store.Trip, viewerID int64) (api.TripDTO, error) {
	role, err := a.Store.TripRole(r.Context(), t.ID, viewerID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			role = "viewer" // superuser / owner-by-created_by but no membership row
		} else {
			return api.TripDTO{}, err
		}
	}
	members, err := a.Store.TripMembers(r.Context(), t.ID)
	if err != nil {
		return api.TripDTO{}, err
	}
	memberDTOs := make([]api.TripMemberDTO, 0, len(members))
	for _, m := range members {
		memberDTOs = append(memberDTOs, api.ToTripMemberDTO(m))
	}
	tags, err := a.Store.TagsByTrip(r.Context(), t.ID)
	if err != nil {
		return api.TripDTO{}, err
	}
	isPassenger, err := a.Store.IsTripPassenger(r.Context(), t.ID, viewerID)
	if err != nil {
		return api.TripDTO{}, err
	}
	passengers, err := a.Store.TripPassengers(r.Context(), t.ID)
	if err != nil {
		return api.TripDTO{}, err
	}
	dto := api.ToTripDTO(t, role, memberDTOs, tags)
	dto.ViewerIsPassenger = isPassenger
	dto.PassengerIDs = passengers
	return dto, nil
}

// canViewTrip reports whether u may see the trip: trip membership/ownership, or
// superuser.
func (a *API) canViewTrip(ctx context.Context, tripID int64, u *store.User) (bool, error) {
	if u == nil {
		return false, nil
	}
	if u.IsSuperuser {
		return true, nil
	}
	return a.Store.CanViewTrip(ctx, tripID, u.ID)
}

func (a *API) requireTripEdit(ctx context.Context, tripID int64, u *store.User, w http.ResponseWriter) error {
	if u == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return errors.New("unauthorized")
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

func (a *API) requireTripOwner(ctx context.Context, tripID int64, u *store.User, w http.ResponseWriter) error {
	if u == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return errors.New("unauthorized")
	}
	if u.IsSuperuser {
		return nil
	}
	role, err := a.Store.TripRole(ctx, tripID, u.ID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusForbidden, "forbidden")
			return err
		}
		handleStoreErr(w, err)
		return err
	}
	if role != "owner" {
		writeError(w, http.StatusForbidden, "forbidden")
		return errors.New("forbidden")
	}
	return nil
}

// requireFriendTarget ensures `target` may be added to a shared resource by
// `actor`: the target must be the actor themselves or an accepted friend. This
// mirrors the front end, which only offers accepted friends as passengers/trip
// members (spec §6.4); enforcing it server-side stops an editor adding (and
// thereby granting trip visibility to) an arbitrary user id. Superusers bypass.
// Writes the response and returns a non-nil error when the check fails.
func (a *API) requireFriendTarget(ctx context.Context, actor *store.User, target int64, w http.ResponseWriter) error {
	if actor == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return errors.New("unauthorized")
	}
	if actor.IsSuperuser || actor.ID == target {
		return nil
	}
	ok, err := a.Store.AreAcceptedFriends(ctx, actor.ID, target)
	if err != nil {
		serverError(w, err)
		return err
	}
	if !ok {
		writeError(w, http.StatusForbidden, "user must be an accepted friend")
		return errors.New("not an accepted friend")
	}
	return nil
}
