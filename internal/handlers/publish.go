package handlers

import (
	"context"

	"github.com/dpage/aerly/internal/notify"
)

// SSE publishers for trip/plan edits. The poller already emits
// plan_part.updated (a TrackerPartDTO) when a tracked part refreshes; these
// cover the user-driven edits the FE consumes via onTrip/onPlan (sse.ts /
// App.tsx). The scoping/marshalling lives in internal/notify so the email-ingest
// service can emit the same events; these methods are thin delegators.

// publishTripUpdated broadcasts a trip.updated event scoped to the trip's
// members (owner + every membership row). Viewers see metadata edits live.
func (a *API) publishTripUpdated(ctx context.Context, tripID int64) {
	notify.TripUpdated(ctx, a.Store, a.Hub, tripID)
}

// publishPlanUpdated broadcasts a plan.updated event scoped to the per-plan
// visibility set (the §4 predicate — owner, passengers, and members who pass
// the visibility rule), matching the poller's plan_part.updated scoping.
func (a *API) publishPlanUpdated(ctx context.Context, tripID, planID int64) {
	notify.PlanUpdated(ctx, a.Store, a.Hub, tripID, planID)
}

// publishPlanDeleted broadcasts a plan.deleted event. Computed before the
// delete (the visibility set is gone afterwards), so callers must invoke this
// with the plan's pre-delete trip id + plan id and the membership intact.
func (a *API) publishPlanDeleted(ctx context.Context, tripID, planID int64) {
	notify.PlanDeleted(ctx, a.Store, a.Hub, tripID, planID)
}
