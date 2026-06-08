// Package notify publishes trip/plan SSE events scoped to who may see them. The
// logic lives here, in a neutral package, so both the HTTP handlers and the
// email-ingest service can emit the same events the front end already consumes
// (App.tsx onTrip/onPlan) — handlers imports emailingest, so emailingest can't
// import handlers.
package notify

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/dpage/aerly/internal/sse"
	"github.com/dpage/aerly/internal/store"
)

// tripEventPayload is the trip.updated body. Carries the trip id so the FE can
// refetch the right trip (and re-list trips).
type tripEventPayload struct {
	ID int64 `json:"id"`
}

// planEventPayload is the plan.updated / plan.deleted body. Carries the trip id
// (so the FE can refetch the owning trip + the tracker) and the plan id.
type planEventPayload struct {
	TripID int64 `json:"trip_id"`
	PlanID int64 `json:"plan_id"`
}

// TripUpdated broadcasts a trip.updated event scoped to the friend-gated tile
// audience: the owner plus every user who can currently see the trip under the
// CanViewTrip / ListTrips rule (explicit members, all-friends sharing, and
// plan-scoped grants alike). A nil hub/store is a no-op. All errors are logged,
// never returned — a dropped publish is covered by the SPA's refetch on focus /
// reconnect.
func TripUpdated(ctx context.Context, st *store.Store, hub *sse.Hub, tripID int64) {
	if st == nil || hub == nil {
		return
	}
	visible, err := st.VisibleTripUserIDs(ctx, tripID)
	if err != nil {
		slog.Error("notify.TripUpdated: visibility", "err", err, "trip", tripID)
		return
	}
	payload, err := json.Marshal(tripEventPayload{ID: tripID})
	if err != nil {
		slog.Error("notify.TripUpdated: marshal", "err", err, "trip", tripID)
		return
	}
	hub.Publish(sse.Event{Type: "trip.updated", Data: payload, VisibleTo: visible})
}

// PlanUpdated broadcasts a plan.updated event scoped to the per-plan visibility
// set (owner, passengers, and members who pass the §4 visibility rule).
func PlanUpdated(ctx context.Context, st *store.Store, hub *sse.Hub, tripID, planID int64) {
	planEvent(ctx, st, hub, "plan.updated", tripID, planID)
}

// PlanDeleted broadcasts a plan.deleted event. Must be called with the plan's
// pre-delete trip id + plan id and membership intact (the visibility set is gone
// after the delete).
func PlanDeleted(ctx context.Context, st *store.Store, hub *sse.Hub, tripID, planID int64) {
	planEvent(ctx, st, hub, "plan.deleted", tripID, planID)
}

func planEvent(ctx context.Context, st *store.Store, hub *sse.Hub, eventType string, tripID, planID int64) {
	if st == nil || hub == nil {
		return
	}
	visible, err := st.VisiblePlanUserIDs(ctx, planID)
	if err != nil {
		slog.Error("notify.planEvent: visibility", "err", err, "type", eventType, "plan", planID)
		return
	}
	payload, err := json.Marshal(planEventPayload{TripID: tripID, PlanID: planID})
	if err != nil {
		slog.Error("notify.planEvent: marshal", "err", err, "type", eventType, "plan", planID)
		return
	}
	hub.Publish(sse.Event{Type: eventType, Data: payload, VisibleTo: visible})
}
