package store

import (
	"errors"
	"testing"
	"time"
)

func containsID(ids []int64, want int64) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

// mkTrip inserts a trip owned by ownerID (with the owner trip_members row) and
// returns its id. The plan/trip CRUD is stubbed in Wave 0a, so the visibility
// tests build their fixtures with direct SQL.
func mkTrip(t *testing.T, s *Store, ownerID int64) int64 {
	t.Helper()
	var id int64
	if err := s.pool.QueryRow(ctx,
		`INSERT INTO trips (name, created_by) VALUES ('Trip', $1) RETURNING id`, ownerID,
	).Scan(&id); err != nil {
		t.Fatalf("insert trip: %v", err)
	}
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO trip_members (trip_id, user_id, role) VALUES ($1, $2, 'owner')`, id, ownerID,
	); err != nil {
		t.Fatalf("insert owner member: %v", err)
	}
	return id
}

func addMember(t *testing.T, s *Store, tripID, userID int64, role string) {
	t.Helper()
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO trip_members (trip_id, user_id, role) VALUES ($1, $2, $3)
		 ON CONFLICT (trip_id, user_id) DO UPDATE SET role = EXCLUDED.role`,
		tripID, userID, role); err != nil {
		t.Fatalf("add member: %v", err)
	}
}

func mkPlan(t *testing.T, s *Store, tripID int64, createdBy int64) int64 {
	t.Helper()
	var id int64
	if err := s.pool.QueryRow(ctx,
		`INSERT INTO plans (trip_id, type, created_by) VALUES ($1, 'flight', $2) RETURNING id`,
		tripID, createdBy,
	).Scan(&id); err != nil {
		t.Fatalf("insert plan: %v", err)
	}
	return id
}

func addPlanPart(t *testing.T, s *Store, planID int64, startsAt time.Time) int64 {
	t.Helper()
	var id int64
	if err := s.pool.QueryRow(ctx,
		`INSERT INTO plan_parts (plan_id, starts_at) VALUES ($1, $2) RETURNING id`,
		planID, startsAt,
	).Scan(&id); err != nil {
		t.Fatalf("insert plan_part: %v", err)
	}
	return id
}

func addPlanPassenger(t *testing.T, s *Store, planID, userID int64) {
	t.Helper()
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO plan_passengers (plan_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		planID, userID); err != nil {
		t.Fatalf("add plan passenger: %v", err)
	}
}

func setVisibility(t *testing.T, s *Store, planID int64, mode string, members ...int64) {
	t.Helper()
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO plan_visibility (plan_id, mode) VALUES ($1, $2)
		 ON CONFLICT (plan_id) DO UPDATE SET mode = EXCLUDED.mode`, planID, mode); err != nil {
		t.Fatalf("set visibility mode: %v", err)
	}
	for _, m := range members {
		if _, err := s.pool.Exec(ctx,
			`INSERT INTO plan_visibility_members (plan_id, user_id) VALUES ($1, $2)
			 ON CONFLICT DO NOTHING`, planID, m); err != nil {
			t.Fatalf("set visibility member: %v", err)
		}
	}
}

func mustCanView(t *testing.T, s *Store, planID, viewerID int64) bool {
	t.Helper()
	ok, err := s.CanViewPlan(ctx, planID, viewerID, false)
	if err != nil {
		t.Fatalf("CanViewPlan: %v", err)
	}
	return ok
}

// TestCanViewPlanDefault: a trip member sees a plan with no visibility row,
// while a non-member never does.
func TestCanViewPlanDefault(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	member := mkUser(t, s)
	stranger := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	addMember(t, s, trip, member, "viewer")
	befriendStore(t, s, owner, member) // friend gate: a member must be an accepted friend
	plan := mkPlan(t, s, trip, owner)

	if !mustCanView(t, s, plan, member) {
		t.Error("trip member should see a default-visibility plan")
	}
	if mustCanView(t, s, plan, stranger) {
		t.Error("non-member must not see the plan")
	}
}

// TestCanViewPlanOwnerAlwaysSees: the trip owner sees every plan even when a
// stray hidden_from row names them.
func TestCanViewPlanOwnerAlwaysSees(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	creator := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	addMember(t, s, trip, creator, "editor")
	plan := mkPlan(t, s, trip, creator)
	setVisibility(t, s, plan, "hidden_from", owner) // inert against the owner

	if !mustCanView(t, s, plan, owner) {
		t.Error("trip owner must always see the plan, even when named in hidden_from")
	}
}

// TestCanViewPlanPassengerAlwaysSees: a passenger sees a plan even under
// only_visible_to that doesn't name them.
func TestCanViewPlanPassengerAlwaysSees(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	pax := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	befriendStore(t, s, owner, pax) // friend gate: any grant requires accepted friendship
	plan := mkPlan(t, s, trip, owner)
	addPlanPassenger(t, s, plan, pax) // plan-scoped grant (the trigger is gone, no trip-member row)
	setVisibility(t, s, plan, "only_visible_to" /* nobody named */)

	if !mustCanView(t, s, plan, pax) {
		t.Error("passenger must always see their own plan")
	}
}

// TestCanViewPlanHiddenFrom: a member named in hidden_from cannot see it; an
// unnamed member still can.
func TestCanViewPlanHiddenFrom(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	hidden := mkUser(t, s)
	allowed := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	addMember(t, s, trip, hidden, "viewer")
	addMember(t, s, trip, allowed, "viewer")
	befriendStore(t, s, owner, hidden) // friend gate
	befriendStore(t, s, owner, allowed)
	plan := mkPlan(t, s, trip, owner)
	setVisibility(t, s, plan, "hidden_from", hidden)

	if mustCanView(t, s, plan, hidden) {
		t.Error("member named in hidden_from must not see the plan")
	}
	if !mustCanView(t, s, plan, allowed) {
		t.Error("member not named in hidden_from should see the plan")
	}
}

// TestCanViewPlanOnlyVisibleTo: only named members (plus the always-granted
// trio) see the plan.
func TestCanViewPlanOnlyVisibleTo(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	named := mkUser(t, s)
	other := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	addMember(t, s, trip, named, "viewer")
	addMember(t, s, trip, other, "viewer")
	befriendStore(t, s, owner, named) // friend gate
	befriendStore(t, s, owner, other)
	plan := mkPlan(t, s, trip, owner)
	setVisibility(t, s, plan, "only_visible_to", named)

	if !mustCanView(t, s, plan, named) {
		t.Error("named member must see an only_visible_to plan")
	}
	if mustCanView(t, s, plan, other) {
		t.Error("un-named member must not see an only_visible_to plan")
	}
}

// TestCanViewPlanSuperuserShowAll: the showAllForSuperuser bypass is a mere
// existence check — it sees a plan the viewer otherwise couldn't, but a missing
// plan still returns false.
func TestCanViewPlanSuperuserShowAll(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	super := mkUser(t, s) // not a member of the trip
	trip := mkTrip(t, s, owner)
	plan := mkPlan(t, s, trip, owner)
	setVisibility(t, s, plan, "only_visible_to") // names nobody

	// Without the bypass the non-member superuser can't see it.
	if mustCanView(t, s, plan, super) {
		t.Error("non-member should not see an only_visible_to plan without the bypass")
	}
	// With the bypass they can.
	ok, err := s.CanViewPlan(ctx, plan, super, true)
	if err != nil || !ok {
		t.Errorf("showAll bypass should see any existing plan: ok=%v err=%v", ok, err)
	}
	// But a missing plan is still false, even with the bypass.
	if ok, err := s.CanViewPlan(ctx, 9_999_999, super, true); err != nil || ok {
		t.Errorf("showAll bypass on a missing plan: ok=%v err=%v, want false/nil", ok, err)
	}
}

// TestCanViewPlanMissingPlan: the §4 predicate returns false (no error) for a
// plan that doesn't exist.
func TestCanViewPlanMissingPlan(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	viewer := mkUser(t, s)
	if mustCanView(t, s, 9_999_999, viewer) {
		t.Error("a non-existent plan must not be viewable")
	}
}

// TestTripOwnersByPlan maps each plan id to its containing trip's owner, so the
// map can colour parts by whose trip they belong to (issue #13).
func TestTripOwnersByPlan(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	alice := mkUser(t, s)
	bob := mkUser(t, s)
	tripA := mkTrip(t, s, alice)
	tripB := mkTrip(t, s, bob)
	planA1 := mkPlan(t, s, tripA, alice)
	planA2 := mkPlan(t, s, tripA, bob) // bob (an editor) added a plan to alice's trip
	planB := mkPlan(t, s, tripB, bob)

	got, err := s.TripOwnersByPlan(ctx, []int64{planA1, planA2, planB})
	if err != nil {
		t.Fatalf("TripOwnersByPlan: %v", err)
	}
	// Both of trip A's plans map to alice (the TRIP owner), regardless of who
	// created each plan; trip B's plan maps to bob.
	if got[planA1] != alice || got[planA2] != alice {
		t.Errorf("trip A plans owner = %d/%d, want alice %d", got[planA1], got[planA2], alice)
	}
	if got[planB] != bob {
		t.Errorf("trip B plan owner = %d, want bob %d", got[planB], bob)
	}
	// Empty input → empty map, no query.
	if m, err := s.TripOwnersByPlan(ctx, nil); err != nil || len(m) != 0 {
		t.Errorf("empty input: m=%v err=%v", m, err)
	}
}

// TestSuppliersByPlan maps each plan id to its booked supplier name, so the map
// row can show who a booking is with (the airline/operator). Plans with no
// supplier are omitted, not returned as empty strings.
func TestSuppliersByPlan(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	alice := mkUser(t, s)
	trip := mkTrip(t, s, alice)
	withSupplier := mkPlan(t, s, trip, alice)
	noSupplier := mkPlan(t, s, trip, alice)
	if _, err := s.pool.Exec(ctx,
		`UPDATE plans SET supplier_name = 'Ryanair' WHERE id = $1`, withSupplier); err != nil {
		t.Fatalf("set supplier: %v", err)
	}

	got, err := s.SuppliersByPlan(ctx, []int64{withSupplier, noSupplier})
	if err != nil {
		t.Fatalf("SuppliersByPlan: %v", err)
	}
	if got[withSupplier] != "Ryanair" {
		t.Errorf("supplier = %q, want Ryanair", got[withSupplier])
	}
	// An empty supplier is omitted entirely, not returned as "".
	if _, ok := got[noSupplier]; ok {
		t.Errorf("plan with no supplier should be absent, got %q", got[noSupplier])
	}
	// Empty input → empty map, no query.
	if m, err := s.SuppliersByPlan(ctx, nil); err != nil || len(m) != 0 {
		t.Errorf("empty input: m=%v err=%v", m, err)
	}
}

// TestIsTripPassenger reports whether the viewer travels on the trip (a
// passenger on some plan), distinct from merely being a shared member (#19).
func TestIsTripPassenger(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	pax := mkUser(t, s)
	sharedViewer := mkUser(t, s)
	stranger := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	plan := mkPlan(t, s, trip, owner)
	addPlanPassenger(t, s, plan, pax)             // pax travels on the trip
	addMember(t, s, trip, sharedViewer, "viewer") // shared, but not a passenger

	check := func(uid int64, want bool) {
		t.Helper()
		got, err := s.IsTripPassenger(ctx, trip, uid)
		if err != nil {
			t.Fatalf("IsTripPassenger(%d): %v", uid, err)
		}
		if got != want {
			t.Errorf("IsTripPassenger(%d) = %v, want %v", uid, got, want)
		}
	}
	check(pax, true)
	check(sharedViewer, false)
	check(owner, false) // owning a trip isn't being a passenger on it
	check(stranger, false)
}

// TestTripPassengerLifecycle covers adding a trip-level passenger (materialised
// onto existing plans + auto-membership), back-filling a newly created plan,
// and removing them (cleaning plan_passengers and the auto viewer row) — #20.
func TestTripPassengerLifecycle(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	partner := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	plan1 := mkPlan(t, s, trip, owner)

	if err := s.AddTripPassenger(ctx, trip, partner); err != nil {
		t.Fatalf("AddTripPassenger: %v", err)
	}
	// Materialised onto the existing plan.
	pax, err := s.PassengersByPlan(ctx, []int64{plan1})
	if err != nil {
		t.Fatalf("PassengersByPlan: %v", err)
	}
	if !containsID(pax[plan1], partner) {
		t.Error("trip passenger not materialised onto the existing plan")
	}
	// Became a trip viewer (the passenger⇒member trigger) and is listed.
	if role, err := s.TripRole(ctx, trip, partner); err != nil || role != "viewer" {
		t.Errorf("trip role = %q, err=%v, want viewer", role, err)
	}
	if tp, _ := s.TripPassengers(ctx, trip); !containsID(tp, partner) {
		t.Error("partner not listed by TripPassengers")
	}
	if ok, _ := s.IsTripPassenger(ctx, trip, partner); !ok {
		t.Error("IsTripPassenger should be true after add")
	}

	// A newly created plan inherits the trip passenger.
	plan2, err := s.CreatePlan(ctx, CreatePlanPayload{TripID: trip, Type: "flight", Title: "BA1"}, owner)
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	pax2, _ := s.PassengersByPlan(ctx, []int64{plan2.ID})
	if !containsID(pax2[plan2.ID], partner) {
		t.Error("new plan did not inherit the trip passenger")
	}

	// Remove: clears trip_passengers, plan_passengers across the trip, and the
	// auto viewer membership.
	if err := s.RemoveTripPassenger(ctx, trip, partner); err != nil {
		t.Fatalf("RemoveTripPassenger: %v", err)
	}
	if ok, _ := s.IsTripPassenger(ctx, trip, partner); ok {
		t.Error("IsTripPassenger should be false after remove")
	}
	if tp, _ := s.TripPassengers(ctx, trip); containsID(tp, partner) {
		t.Error("partner still listed after remove")
	}
	pax3, _ := s.PassengersByPlan(ctx, []int64{plan1, plan2.ID})
	if containsID(pax3[plan1], partner) || containsID(pax3[plan2.ID], partner) {
		t.Error("plan_passengers not cleaned up on remove")
	}
	if _, err := s.TripRole(ctx, trip, partner); !errors.Is(err, ErrNotFound) {
		t.Errorf("auto viewer membership not removed: err=%v", err)
	}
	// Removing again is ErrNotFound.
	if err := s.RemoveTripPassenger(ctx, trip, partner); !errors.Is(err, ErrNotFound) {
		t.Errorf("re-remove err = %v, want ErrNotFound", err)
	}
}

// TestTripPassengerRespectsHiddenPlans: a trip passenger is materialised only
// onto plans they may see; hidden plans stay hidden (visibility AND passenger
// list), and changing a plan's visibility reconciles their materialisation (#20).
func TestTripPassengerRespectsHiddenPlans(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	partner := mkUser(t, s)
	befriendStore(t, s, owner, partner) // friend gate: passenger grants require friendship
	trip := mkTrip(t, s, owner)
	visible := mkPlan(t, s, trip, owner)
	hidden := mkPlan(t, s, trip, owner)
	setVisibility(t, s, hidden, "hidden_from", partner) // hidden from the partner

	if err := s.AddTripPassenger(ctx, trip, partner); err != nil {
		t.Fatalf("AddTripPassenger: %v", err)
	}
	pax, _ := s.PassengersByPlan(ctx, []int64{visible, hidden})
	if !containsID(pax[visible], partner) {
		t.Error("passenger not materialised onto the visible plan")
	}
	if containsID(pax[hidden], partner) {
		t.Error("passenger materialised onto a plan hidden from them")
	}
	if !mustCanView(t, s, visible, partner) {
		t.Error("passenger should see the visible plan")
	}
	if mustCanView(t, s, hidden, partner) {
		t.Error("passenger must not see a plan hidden from them")
	}

	// Un-hiding the hidden plan reconciles: they're materialised and can see it.
	if err := s.SetPlanVisibility(ctx, hidden, "", nil); err != nil {
		t.Fatalf("SetPlanVisibility(clear): %v", err)
	}
	pax, _ = s.PassengersByPlan(ctx, []int64{hidden})
	if !containsID(pax[hidden], partner) || !mustCanView(t, s, hidden, partner) {
		t.Error("un-hiding a plan did not re-materialise / reveal it to the passenger")
	}

	// Hiding the previously-visible plan reconciles the other way: their via_trip
	// row is removed and they can no longer see it.
	if err := s.SetPlanVisibility(ctx, visible, "hidden_from", []int64{partner}); err != nil {
		t.Fatalf("SetPlanVisibility(hide): %v", err)
	}
	pax, _ = s.PassengersByPlan(ctx, []int64{visible})
	if containsID(pax[visible], partner) || mustCanView(t, s, visible, partner) {
		t.Error("hiding a plan did not de-materialise / hide it from the passenger")
	}
}

// TestTripPassengerEmptyTrip: adding a passenger to a trip with no plans still
// makes them a member and files it under their My trips (#20, fixes empty trip).
func TestTripPassengerEmptyTrip(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	partner := mkUser(t, s)
	trip := mkTrip(t, s, owner) // no plans

	if err := s.AddTripPassenger(ctx, trip, partner); err != nil {
		t.Fatalf("AddTripPassenger: %v", err)
	}
	if role, err := s.TripRole(ctx, trip, partner); err != nil || role != "viewer" {
		t.Errorf("empty-trip passenger role = %q err=%v, want viewer", role, err)
	}
	if ok, _ := s.IsTripPassenger(ctx, trip, partner); !ok {
		t.Error("empty-trip passenger should still count for My trips")
	}
}

// TestRemoveTripPassengerPreservesManual: removing a trip passenger keeps an
// explicit per-plan passenger assignment for the same user (#20, finding [3]).
func TestRemoveTripPassengerPreservesManual(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	partner := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	plan := mkPlan(t, s, trip, owner)

	// Partner is a manual passenger on the plan, AND a trip-level passenger.
	if err := s.AddPlanPassenger(ctx, plan, partner); err != nil {
		t.Fatalf("AddPlanPassenger: %v", err)
	}
	if err := s.AddTripPassenger(ctx, trip, partner); err != nil {
		t.Fatalf("AddTripPassenger: %v", err)
	}
	// Removing the trip passenger must leave the manual per-plan row + membership.
	if err := s.RemoveTripPassenger(ctx, trip, partner); err != nil {
		t.Fatalf("RemoveTripPassenger: %v", err)
	}
	pax, _ := s.PassengersByPlan(ctx, []int64{plan})
	if !containsID(pax[plan], partner) {
		t.Error("manual per-plan passenger was stripped by trip-passenger removal")
	}
	if _, err := s.TripRole(ctx, trip, partner); err != nil {
		t.Errorf("membership removed despite a remaining manual passenger row: %v", err)
	}
}

// TestRemovePlanPassengerKeepsTripPassenger: per-plan removal can't evict a
// trip-level passenger from a visible plan — the row is re-derived as
// trip-level rather than deleted (#20, CodeRabbit follow-up).
func TestRemovePlanPassengerKeepsTripPassenger(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	partner := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	plan := mkPlan(t, s, trip, owner)

	if err := s.AddTripPassenger(ctx, trip, partner); err != nil {
		t.Fatalf("AddTripPassenger: %v", err)
	}
	// Also add them manually (manual override on top of the trip-level one).
	if err := s.AddPlanPassenger(ctx, plan, partner); err != nil {
		t.Fatalf("AddPlanPassenger: %v", err)
	}
	// Per-plan removal drops the manual override but keeps them on (trip-level).
	if err := s.RemovePlanPassenger(ctx, plan, partner); err != nil {
		t.Fatalf("RemovePlanPassenger: %v", err)
	}
	if pax, _ := s.PassengersByPlan(ctx, []int64{plan}); !containsID(pax[plan], partner) {
		t.Error("trip passenger was evicted from a visible plan by a per-plan removal")
	}
	// A second removal still keeps them (idempotent — they're a trip passenger).
	if err := s.RemovePlanPassenger(ctx, plan, partner); err != nil {
		t.Fatalf("RemovePlanPassenger (2nd): %v", err)
	}
	if pax, _ := s.PassengersByPlan(ctx, []int64{plan}); !containsID(pax[plan], partner) {
		t.Error("trip passenger gone after a second per-plan removal")
	}
	// Once they're no longer a trip passenger, per-plan removal deletes the row.
	if err := s.RemoveTripPassenger(ctx, trip, partner); err != nil {
		t.Fatalf("RemoveTripPassenger: %v", err)
	}
	if pax, _ := s.PassengersByPlan(ctx, []int64{plan}); containsID(pax[plan], partner) {
		t.Error("passenger still on the plan after trip-passenger removal")
	}
}

// TestListVisiblePlanParts respects the same predicate as CanViewPlan.
func TestListVisiblePlanParts(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	member := mkUser(t, s)
	stranger := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	addMember(t, s, trip, member, "viewer")
	befriendStore(t, s, owner, member) // friend gate
	plan := mkPlan(t, s, trip, owner)
	part := addPlanPart(t, s, plan, time.Now().Add(48*time.Hour))

	parts, err := s.ListVisiblePlanParts(ctx, member, ListVisiblePlanPartsOpts{TripID: trip})
	if err != nil {
		t.Fatalf("ListVisiblePlanParts: %v", err)
	}
	if len(parts) != 1 || parts[0].ID != part {
		t.Fatalf("member should see exactly the one part, got %d", len(parts))
	}

	parts, err = s.ListVisiblePlanParts(ctx, stranger, ListVisiblePlanPartsOpts{TripID: trip})
	if err != nil {
		t.Fatalf("ListVisiblePlanParts stranger: %v", err)
	}
	if len(parts) != 0 {
		t.Errorf("stranger should see no parts, got %d", len(parts))
	}
}

// TestVisiblePlanUserIDs returns exactly the set that can see the plan.
func TestVisiblePlanUserIDs(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	named := mkUser(t, s)
	other := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	addMember(t, s, trip, named, "viewer")
	addMember(t, s, trip, other, "viewer")
	befriendStore(t, s, owner, named) // friend gate
	befriendStore(t, s, owner, other)
	plan := mkPlan(t, s, trip, owner)
	setVisibility(t, s, plan, "only_visible_to", named)

	ids, err := s.VisiblePlanUserIDs(ctx, plan)
	if err != nil {
		t.Fatalf("VisiblePlanUserIDs: %v", err)
	}
	got := map[int64]bool{}
	for _, id := range ids {
		got[id] = true
	}
	if !got[owner] || !got[named] {
		t.Errorf("expected owner(%d) and named(%d) in %v", owner, named, ids)
	}
	if got[other] {
		t.Errorf("un-named member %d must not be in visible set %v", other, ids)
	}
}
