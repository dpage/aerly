package handlers

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/dpage/aerly/internal/sse"
)

// SSE publishers for trip/plan edits. The poller already emits
// plan_part.updated (a TrackerPartDTO) when a tracked part refreshes; these
// cover the user-driven edits the FE consumes via onTrip/onPlan (sse.ts /
// App.tsx). All errors are logged and never surfaced to the HTTP caller — the
// SPA refetch on focus / reconnect is the safety net for any dropped publish.

// tripEventPayload is the trip.updated body. Carries the trip id so the FE can
// refetch the right trip (App.tsx onTrip).
type tripEventPayload struct {
	ID int64 `json:"id"`
}

// planEventPayload is the plan.updated / plan.deleted body. Carries the trip id
// (so the FE can refetch the owning trip + the tracker) and the plan id.
type planEventPayload struct {
	TripID int64 `json:"trip_id"`
	PlanID int64 `json:"plan_id"`
}

// publishTripUpdated broadcasts a trip.updated event scoped to the trip's
// members (owner + every membership row). Viewers see metadata edits live.
func (a *API) publishTripUpdated(ctx context.Context, tripID int64) {
	members, err := a.Store.TripMembers(ctx, tripID)
	if err != nil {
		slog.Error("publishTripUpdated: members", "err", err, "trip", tripID)
		return
	}
	visible := make([]int64, 0, len(members))
	for _, m := range members {
		visible = append(visible, m.UserID)
	}
	payload, err := json.Marshal(tripEventPayload{ID: tripID})
	if err != nil {
		slog.Error("publishTripUpdated: marshal", "err", err, "trip", tripID)
		return
	}
	a.Hub.Publish(sse.Event{Type: "trip.updated", Data: payload, VisibleTo: visible})
}

// publishPlanUpdated broadcasts a plan.updated event scoped to the per-plan
// visibility set (the §4 predicate — owner, passengers, and members who pass
// the visibility rule), matching the poller's plan_part.updated scoping.
func (a *API) publishPlanUpdated(ctx context.Context, tripID, planID int64) {
	a.publishPlanEvent(ctx, "plan.updated", tripID, planID)
}

// publishPlanDeleted broadcasts a plan.deleted event. Computed before the
// delete (the visibility set is gone afterwards), so callers must invoke this
// with the plan's pre-delete trip id + plan id and the membership intact.
func (a *API) publishPlanDeleted(ctx context.Context, tripID, planID int64) {
	a.publishPlanEvent(ctx, "plan.deleted", tripID, planID)
}

func (a *API) publishPlanEvent(ctx context.Context, eventType string, tripID, planID int64) {
	visible, err := a.Store.VisiblePlanUserIDs(ctx, planID)
	if err != nil {
		slog.Error("publishPlanEvent: visibility", "err", err, "type", eventType, "plan", planID)
		return
	}
	payload, err := json.Marshal(planEventPayload{TripID: tripID, PlanID: planID})
	if err != nil {
		slog.Error("publishPlanEvent: marshal", "err", err, "type", eventType, "plan", planID)
		return
	}
	a.Hub.Publish(sse.Event{Type: eventType, Data: payload, VisibleTo: visible})
}
