package handlers

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/api"
)

// TestBookingDetailsHiddenFromShares covers issue #163: a plan's confirmation
// reference, ticket number and cost reach the trip owner, editors, passengers
// and superusers, but not a viewer member or a friend the plan is only shared
// with. Both the trip read and the calendar feeds are checked.
func TestBookingDetailsHiddenFromShares(t *testing.T) {
	e := setup(t, nil, nil)
	ctx := context.Background()
	owner := e.user(t, "bkowner", false)
	viewer := e.user(t, "bkviewer", false)
	editor := e.user(t, "bkeditor", false)
	pax := e.user(t, "bkpax", false)
	friend := e.user(t, "bkfriend", false)
	admin := e.user(t, "bkadmin", true)
	for _, u := range []int64{viewer, editor, pax, friend, admin} {
		e.befriend(t, owner, u)
	}
	tid := newTrip(t, e, owner, "Trip")
	addMember(t, e, tid, viewer, "viewer")
	addMember(t, e, tid, editor, "editor")
	addMember(t, e, tid, pax, "viewer")
	addMember(t, e, tid, admin, "viewer")
	pid := newFlightPlan(t, e, tid, owner, "BA117", time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC))
	if w := e.req(t, "PATCH", "/api/plans/"+itoa(pid), map[string]any{
		"ticket_number": "1250000000000", "cost_amount": 123.45, "cost_currency": "GBP",
	}, owner); w.Code != http.StatusOK {
		t.Fatalf("patch plan: %d %s", w.Code, w.Body.String())
	}
	if w := e.req(t, "POST", "/api/plans/"+itoa(pid)+"/passengers",
		map[string]any{"user_id": pax}, owner); w.Code >= 300 {
		t.Fatalf("add passenger: %d %s", w.Code, w.Body.String())
	}
	if w := e.req(t, "PUT", "/api/plans/"+itoa(pid)+"/share-all-friends",
		map[string]any{"enabled": true}, owner); w.Code >= 300 {
		t.Fatalf("share with all friends: %d %s", w.Code, w.Body.String())
	}

	planFor := func(uid int64) api.PlanDTO {
		t.Helper()
		w := e.req(t, "GET", "/api/trips/"+itoa(tid), nil, uid)
		if w.Code != http.StatusOK {
			t.Fatalf("get trip as %d: %d %s", uid, w.Code, w.Body.String())
		}
		body := decodeBody[struct {
			Plans []api.PlanDTO `json:"plans"`
		}](t, w)
		for _, p := range body.Plans {
			if p.ID == pid {
				return p
			}
		}
		t.Fatalf("plan %d not visible to %d", pid, uid)
		return api.PlanDTO{}
	}

	for name, uid := range map[string]int64{"owner": owner, "editor": editor, "passenger": pax, "superuser": admin} {
		p := planFor(uid)
		if p.ConfirmationRef != "PNR" || p.TicketNumber != "1250000000000" ||
			p.CostAmount == nil || *p.CostAmount != 123.45 || p.CostCurrency != "GBP" {
			t.Errorf("%s: booking details = %q %q %v %q, want them shown",
				name, p.ConfirmationRef, p.TicketNumber, p.CostAmount, p.CostCurrency)
		}
	}
	for name, uid := range map[string]int64{"viewer": viewer, "friend": friend} {
		p := planFor(uid)
		if p.ConfirmationRef != "" || p.TicketNumber != "" || p.CostAmount != nil || p.CostCurrency != "" {
			t.Errorf("%s: booking details = %q %q %v %q, want them hidden",
				name, p.ConfirmationRef, p.TicketNumber, p.CostAmount, p.CostCurrency)
		}
		if p.Title != "BA117" {
			t.Errorf("%s: title = %q, want the plan itself still shown", name, p.Title)
		}
	}

	refIn := func(uid int64, friends bool) string {
		t.Helper()
		var err error
		var refs []string
		if friends {
			evs, ferr := e.store.CalendarEventsForFriends(ctx, uid)
			err = ferr
			for _, ev := range evs {
				if ev.PlanID == pid {
					refs = append(refs, ev.ConfirmationRef)
				}
			}
		} else {
			evs, terr := e.store.CalendarEventsForTrip(ctx, uid, tid)
			err = terr
			for _, ev := range evs {
				if ev.PlanID == pid {
					refs = append(refs, ev.ConfirmationRef)
				}
			}
		}
		if err != nil {
			t.Fatalf("calendar events for %d: %v", uid, err)
		}
		if len(refs) == 0 {
			t.Fatalf("plan %d missing from %d's feed", pid, uid)
		}
		return refs[0]
	}
	if got := refIn(pax, false); got != "PNR" {
		t.Errorf("passenger's trip feed ref = %q, want PNR", got)
	}
	if got := refIn(viewer, false); got != "" {
		t.Errorf("viewer's trip feed ref = %q, want it hidden", got)
	}
	if got := refIn(admin, false); got != "PNR" {
		t.Errorf("superuser's trip feed ref = %q, want PNR", got)
	}
	if got := refIn(friend, true); got != "" {
		t.Errorf("friend's friends feed ref = %q, want it hidden", got)
	}
}

// TestPlanDTONoViewerHidesBookingDetails covers server-side assembly with no
// viewer, which leaves the booking details out.
func TestPlanDTONoViewerHidesBookingDetails(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "bknoviewer", false)
	tid := newTrip(t, e, owner, "Trip")
	pid := newFlightPlan(t, e, tid, owner, "BA117", time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC))
	dto, err := e.api.planDTO(context.Background(), pid, 0)
	if err != nil {
		t.Fatalf("planDTO: %v", err)
	}
	if dto.ConfirmationRef != "" {
		t.Errorf("confirmation ref = %q, want it hidden with no viewer", dto.ConfirmationRef)
	}
}

// TestPlanDTOBookingDetailsStoreErr covers planDTO's CanSeeBookingDetails
// error branch.
func TestPlanDTOBookingDetailsStoreErr(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "bkstoreerr", false)
	tid := newTrip(t, e, owner, "Trip")
	pid := newFlightPlan(t, e, tid, owner, "BA117", time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC))
	g1dropColumn(t, e, "users", "is_superuser")
	if _, err := e.api.planDTO(context.Background(), pid, owner); err == nil {
		t.Error("planDTO with a broken users table = nil error, want one")
	}
}
