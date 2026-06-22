package handlers

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/store"
)

// g1dropTable removes a table so a subsequent store query fails, letting the
// handler's handleStoreErr (500) branch run. Each test gets its own fresh,
// migrated database (testsupport.NewPool), so this is isolated and safe; auth
// still works because it reads other tables. Used to exercise the otherwise
// hard-to-reach store-error paths without a production seam.
func g1dropTable(t *testing.T, e *testEnv, table string) {
	t.Helper()
	if _, err := e.pool.Exec(context.Background(), "DROP TABLE "+table+" CASCADE"); err != nil {
		t.Fatalf("drop %s: %v", table, err)
	}
}

func g1dropColumn(t *testing.T, e *testEnv, table, col string) {
	t.Helper()
	if _, err := e.pool.Exec(context.Background(),
		"ALTER TABLE "+table+" DROP COLUMN "+col); err != nil {
		t.Fatalf("drop column %s.%s: %v", table, col, err)
	}
}

func TestListMyFlightsStoreErrG1(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g1mfstore", false)
	g1dropTable(t, e, "flight_details")
	if w := e.req(t, "GET", "/api/me/flights", nil, owner); w.Code != http.StatusInternalServerError {
		t.Errorf("code = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

func TestFeedsCanViewErrG1(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g1cvowner", false)
	viewer := e.user(t, "g1cvviewer", false)
	tripID := newTrip(t, e, owner, "CanView err trip")
	// plan_visibility participates in CanViewTrip but not in auth/owner checks,
	// so dropping it makes the viewer's canViewTrip error without breaking login.
	g1dropTable(t, e, "plan_visibility")

	if w := e.req(t, "GET", "/api/trips/"+itoa(tripID)+"/feeds", nil, viewer); w.Code != http.StatusInternalServerError {
		t.Errorf("list feeds canView err = %d, want 500", w.Code)
	}
	if w := e.req(t, "GET", "/api/trips/"+itoa(tripID)+"/external-events", nil, viewer); w.Code != http.StatusInternalServerError {
		t.Errorf("external-events canView err = %d, want 500", w.Code)
	}
}

func TestFeedsStoreErrG1(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g1feerr", false)
	tripID := newTrip(t, e, owner, "Feed store err trip")
	base := "/api/trips/" + itoa(tripID) + "/feeds"

	// Seed a feed so the update/delete/resolve paths have a row to address.
	g1FeedServer(t, e)
	w := e.req(t, "POST", base, map[string]any{"url": g1feedURL, "name": "S"}, owner)
	if w.Code != http.StatusCreated {
		t.Fatalf("seed feed = %d %s", w.Code, w.Body.String())
	}
	feedID := int64(decodeBody[map[string]any](t, w)["id"].(float64))
	feedPath := base + "/" + itoa(feedID)

	// Drop the events table: list-external-events store error (owner bypasses the
	// view check, so this isolates TripFeedEventsForTrip).
	g1dropTable(t, e, "trip_feed_events")
	if w := e.req(t, "GET", "/api/trips/"+itoa(tripID)+"/external-events", nil, owner); w.Code != http.StatusInternalServerError {
		t.Errorf("external-events store err = %d, want 500", w.Code)
	}

	// Drop the feeds table: list, add and resolveFeed (for update/delete) error.
	g1dropTable(t, e, "trip_feeds")
	if w := e.req(t, "GET", base, nil, owner); w.Code != http.StatusInternalServerError {
		t.Errorf("list feeds store err = %d, want 500", w.Code)
	}
	if w := e.req(t, "POST", base, map[string]any{"url": g1feedURL}, owner); w.Code != http.StatusInternalServerError {
		t.Errorf("add feed store err = %d, want 500", w.Code)
	}
	// resolveFeed's TripFeedByID now errors -> 500 for update/delete.
	if w := e.req(t, "PATCH", feedPath, map[string]any{"url": g1feedURL}, owner); w.Code != http.StatusInternalServerError {
		t.Errorf("update feed resolve err = %d, want 500", w.Code)
	}
	if w := e.req(t, "DELETE", feedPath, nil, owner); w.Code != http.StatusInternalServerError {
		t.Errorf("delete feed resolve err = %d, want 500", w.Code)
	}
}

func TestSetShareAllFriendsStoreErrG1(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g1safserr", false)
	tripID := newTrip(t, e, owner, "SAF err trip")
	planID := newFlightPlan(t, e, tripID, owner, "BA286", time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC))

	// Drop the share-all-friends column so the UPDATE errors while auth/owner
	// checks (which don't touch that column) keep working.
	g1dropColumn(t, e, "trips", "share_all_friends_role")
	if w := e.req(t, "PUT", "/api/trips/"+itoa(tripID)+"/share-all-friends",
		map[string]any{"role": "viewer"}, owner); w.Code != http.StatusInternalServerError {
		t.Errorf("set trip SAF store err = %d, want 500; body=%s", w.Code, w.Body.String())
	}

	g1dropColumn(t, e, "plans", "share_all_friends")
	if w := e.req(t, "PUT", "/api/plans/"+itoa(planID)+"/share-all-friends",
		map[string]any{"enabled": true}, owner); w.Code != http.StatusInternalServerError {
		t.Errorf("set plan SAF store err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestShareByEmailLookupErrG1 covers shareByEmail's UserByVerifiedEmail
// non-NotFound error branch: dropping user_emails makes the lookup error. The
// endpoint stays 202 (best-effort), but the error branch is exercised.
func TestShareByEmailLookupErrG1(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g1sbelookerr", false)
	tripID := newTrip(t, e, owner, "Lookup err trip")

	g1dropTable(t, e, "user_emails")
	if w := e.req(t, "POST", "/api/trips/"+itoa(tripID)+"/share-by-email",
		map[string]any{"email": "ghost@example.com", "role": "viewer"}, owner); w.Code != http.StatusAccepted {
		t.Errorf("share-by-email lookup err = %d, want 202 (best-effort); body=%s", w.Code, w.Body.String())
	}
}

func TestNotifyTripByIDErrG1(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g1ntbierr", false)
	tripID := newTrip(t, e, owner, "TripByID err trip")

	// notifyTripShares calls TripByID after requireTripEdit; dropping a column
	// TripByID selects (but the edit check does not) makes it error.
	g1dropColumn(t, e, "trips", "share_all_friends_role")
	if w := e.req(t, "POST", "/api/trips/"+itoa(tripID)+"/notify-shares",
		map[string]any{"user_ids": []int64{}, "emails": []string{}}, owner); w.Code != http.StatusInternalServerError {
		t.Errorf("notify trip TripByID err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestSetTripSAFDTOErrG1 covers setTripShareAllFriends' tripDTO error branch:
// the set + TripByID succeed, but building the DTO fails because trip_tags is
// gone (TagsByTrip errors).
func TestSetTripSAFDTOErrG1(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g1tdtoerr", false)
	tripID := newTrip(t, e, owner, "Trip DTO err")
	g1dropTable(t, e, "trip_tags")
	if w := e.req(t, "PUT", "/api/trips/"+itoa(tripID)+"/share-all-friends",
		map[string]any{"role": "viewer"}, owner); w.Code != http.StatusInternalServerError {
		t.Errorf("trip SAF DTO err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestSetPlanSAFDTOErrG1 covers setPlanShareAllFriends' planDTO error branch:
// the set succeeds but the DTO build fails because plan_passengers is gone.
func TestSetPlanSAFDTOErrG1(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g1pdtoerr", false)
	tripID := newTrip(t, e, owner, "Plan DTO err")
	planID := newFlightPlan(t, e, tripID, owner, "BA286", time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC))
	g1dropTable(t, e, "plan_passengers")
	if w := e.req(t, "PUT", "/api/plans/"+itoa(planID)+"/share-all-friends",
		map[string]any{"enabled": true}, owner); w.Code != http.StatusInternalServerError {
		t.Errorf("plan SAF DTO err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestShareByEmailInsertPendingErrG1 covers shareByEmail's InsertPendingShare
// error branch (new address): dropping pending_shares makes the insert fail.
// Best-effort, so still 202.
func TestShareByEmailInsertPendingErrG1(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g1insperr", false)
	tripID := newTrip(t, e, owner, "Insert pending err")
	g1dropTable(t, e, "pending_shares")
	if w := e.req(t, "POST", "/api/trips/"+itoa(tripID)+"/share-by-email",
		map[string]any{"email": "newbie@example.com", "role": "viewer"}, owner); w.Code != http.StatusAccepted {
		t.Errorf("insert pending err = %d, want 202; body=%s", w.Code, w.Body.String())
	}
}

// TestShareByEmailAddMemberErrG1 covers shareByEmail's AddTripMember error
// branch (existing user): dropping trip_members makes the add fail. Best-effort.
func TestShareByEmailAddMemberErrG1(t *testing.T) {
	e := setup(t, nil, nil)
	// A superuser owner bypasses requireTripOwner's trip_members read, so we can
	// drop trip_members to make AddTripMember (the share write) fail.
	owner := e.user(t, "g1addmemerr", true)
	bob := e.user(t, "g1addmembob", false)
	seedVerifiedEmail(t, e, bob, "addmembob@example.com")
	tripID := newTrip(t, e, owner, "Add member err")
	g1dropTable(t, e, "trip_members")
	if w := e.req(t, "POST", "/api/trips/"+itoa(tripID)+"/share-by-email",
		map[string]any{"email": "addmembob@example.com", "role": "viewer"}, owner); w.Code != http.StatusAccepted {
		t.Errorf("add member err = %d, want 202; body=%s", w.Code, w.Body.String())
	}
}

// TestSharePlanByEmailAddPaxErrG1 covers shareByEmail's AddPlanPassenger error
// branch (existing user, plan kind): dropping plan_passengers makes it fail.
func TestSharePlanByEmailAddPaxErrG1(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g1addpaxerr", false)
	mate := e.user(t, "g1addpaxmate", false)
	seedVerifiedEmail(t, e, mate, "addpaxmate@example.com")
	tripID := newTrip(t, e, owner, "Add pax err")
	planID := newFlightPlan(t, e, tripID, owner, "BA286", time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC))
	g1dropTable(t, e, "plan_passengers")
	if w := e.req(t, "POST", "/api/plans/"+itoa(planID)+"/share-by-email",
		map[string]any{"email": "addpaxmate@example.com"}, owner); w.Code != http.StatusAccepted {
		t.Errorf("add pax err = %d, want 202; body=%s", w.Code, w.Body.String())
	}
}

// TestNotifySharesEmailsErrG1 covers notifyShares' EmailsByUser error branch
// (in emailUser) and the ListOutgoingPendingInvites error branch: a recipient
// that can see the trip but whose email lookup fails, plus a pending-invites
// lookup that fails. Mail must be configured so emailUser runs past its guard.
func TestNotifySharesEmailErrG1(t *testing.T) {
	e := setup(t, nil, g1MailCfg())
	owner := e.user(t, "g1nseerr", false)
	bob := e.user(t, "g1nsebob", false)
	seedVerifiedEmail(t, e, bob, "nsebob@example.com")
	e.befriend(t, owner, bob)
	tripID := newTrip(t, e, owner, "Notify email err")
	if w := e.req(t, "POST", "/api/trips/"+itoa(tripID)+"/members",
		map[string]any{"user_id": bob, "role": "viewer"}, owner); w.Code != http.StatusOK && w.Code != http.StatusCreated {
		t.Fatalf("add member: %d %s", w.Code, w.Body.String())
	}
	// Drop user_emails AFTER membership is set: canSeeShared (trips/members) still
	// works, the in-app notification inserts, then emailUser's EmailsByUser errors
	// and ListOutgoingPendingInvites errors for the email recipient.
	g1dropTable(t, e, "user_emails")
	if w := e.req(t, "POST", "/api/trips/"+itoa(tripID)+"/notify-shares",
		map[string]any{"user_ids": []int64{bob}, "emails": []string{"x@example.com"}}, owner); w.Code != http.StatusNoContent {
		t.Errorf("notify email err = %d, want 204; body=%s", w.Code, w.Body.String())
	}
}

// TestNotifySharesVisibilityErrG1 covers notifyShares' canSeeShared error
// branch: dropping plan_visibility makes CanViewTrip error for the recipient.
func TestNotifySharesVisibilityErrG1(t *testing.T) {
	e := setup(t, nil, g1MailCfg())
	owner := e.user(t, "g1nsviserr", false)
	bob := e.user(t, "g1nsvisbob", false)
	tripID := newTrip(t, e, owner, "Notify vis err")
	g1dropTable(t, e, "plan_visibility")
	if w := e.req(t, "POST", "/api/trips/"+itoa(tripID)+"/notify-shares",
		map[string]any{"user_ids": []int64{bob}, "emails": []string{}}, owner); w.Code != http.StatusNoContent {
		t.Errorf("notify vis err = %d, want 204; body=%s", w.Code, w.Body.String())
	}
}

// TestNotifySharesInsertNotifErrG1 covers notifyShares' InsertNotification
// error branch: the recipient can see the trip, but the notifications table is
// gone so the insert fails.
func TestNotifySharesInsertNotifErrG1(t *testing.T) {
	e := setup(t, nil, g1MailCfg())
	owner := e.user(t, "g1nsinserr", false)
	bob := e.user(t, "g1nsinsbob", false)
	e.befriend(t, owner, bob)
	tripID := newTrip(t, e, owner, "Notify insert err")
	if w := e.req(t, "POST", "/api/trips/"+itoa(tripID)+"/members",
		map[string]any{"user_id": bob, "role": "viewer"}, owner); w.Code != http.StatusOK && w.Code != http.StatusCreated {
		t.Fatalf("add member: %d %s", w.Code, w.Body.String())
	}
	g1dropTable(t, e, "notifications")
	if w := e.req(t, "POST", "/api/trips/"+itoa(tripID)+"/notify-shares",
		map[string]any{"user_ids": []int64{bob}, "emails": []string{}}, owner); w.Code != http.StatusNoContent {
		t.Errorf("notify insert err = %d, want 204; body=%s", w.Code, w.Body.String())
	}
}

// TestSendShareEmailNoConfigG1 covers sendShareEmailTo / emailUser early returns
// when mail is not configured (Config has no MailFromAddress): notify-shares
// still succeeds but sends nothing.
func TestSendShareEmailNoConfigG1(t *testing.T) {
	e := setup(t, nil, nil) // no MailFromAddress
	owner := e.user(t, "g1nocfgowner", false)
	bob := e.user(t, "g1nocfgbob", false)
	seedVerifiedEmail(t, e, bob, "nocfgbob@example.com")
	e.befriend(t, owner, bob)
	tripID := newTrip(t, e, owner, "No mail cfg trip")
	if w := e.req(t, "POST", "/api/trips/"+itoa(tripID)+"/members",
		map[string]any{"user_id": bob, "role": "viewer"}, owner); w.Code != http.StatusOK && w.Code != http.StatusCreated {
		t.Fatalf("add member: %d %s", w.Code, w.Body.String())
	}
	if w := e.req(t, "POST", "/api/trips/"+itoa(tripID)+"/notify-shares",
		map[string]any{"user_ids": []int64{bob}, "emails": []string{}}, owner); w.Code != http.StatusNoContent {
		t.Errorf("notify no-cfg = %d, want 204; body=%s", w.Code, w.Body.String())
	}
}

// TestSendShareEmailSendFailsG1 covers sendShareEmailTo's mailer.Send error
// branch: a bogus sendmail path makes the send fail (logged, not fatal).
func TestSendShareEmailSendFailsG1(t *testing.T) {
	cfg := g1MailCfg()
	cfg.SendmailPath = "/nonexistent/sendmail-binary"
	e := setup(t, nil, cfg)
	owner := e.user(t, "g1sendfailowner", false)
	tripID := newTrip(t, e, owner, "Send fail trip")
	// Register a pending invite so the email recipient is allowed.
	const addr = "sendfail@example.com"
	if w := e.req(t, "POST", "/api/trips/"+itoa(tripID)+"/share-by-email",
		map[string]any{"email": addr, "role": "viewer"}, owner); w.Code != http.StatusAccepted {
		t.Fatalf("share-by-email: %d %s", w.Code, w.Body.String())
	}
	if w := e.req(t, "POST", "/api/trips/"+itoa(tripID)+"/notify-shares",
		map[string]any{"user_ids": []int64{}, "emails": []string{addr}}, owner); w.Code != http.StatusNoContent {
		t.Errorf("notify send-fail = %d, want 204; body=%s", w.Code, w.Body.String())
	}
}

// TestNotifyPlanSharesTitleFallbackG1 covers notifyPlanShares' name fallback to
// the plan type when the title is empty.
func TestNotifyPlanSharesTitleFallbackG1(t *testing.T) {
	e := setup(t, nil, g1MailCfg())
	owner := e.user(t, "g1titleowner", false)
	tripID := newTrip(t, e, owner, "Title fallback trip")
	// Create a plan with no title (an untitled hotel plan).
	out := time.Date(2026, 6, 1, 14, 0, 0, 0, time.UTC)
	w := e.req(t, "POST", "/api/trips/"+itoa(tripID)+"/plans", map[string]any{
		"type": "hotel",
		"parts": []map[string]any{{
			"type": "hotel", "starts_at": out, "ends_at": out.Add(24 * time.Hour),
			"start_label": "Hotel Example",
		}},
	}, owner)
	if w.Code != http.StatusCreated {
		t.Fatalf("create untitled plan: %d %s", w.Code, w.Body.String())
	}
	planID := int64(decodeBody[map[string]any](t, w)["id"].(float64))

	if w := e.req(t, "POST", "/api/plans/"+itoa(planID)+"/notify-shares",
		map[string]any{"user_ids": []int64{}, "emails": []string{}}, owner); w.Code != http.StatusNoContent {
		t.Errorf("notify untitled plan = %d, want 204; body=%s", w.Code, w.Body.String())
	}
}

func TestActorLabelG1(t *testing.T) {
	if got := actorLabel(nil); got != "Someone" {
		t.Errorf("actorLabel(nil) = %q, want Someone", got)
	}
	// Empty/whitespace name falls back to username.
	if got := actorLabel(&store.User{Name: "  ", Username: "loginname"}); got != "loginname" {
		t.Errorf("actorLabel blank name = %q, want loginname", got)
	}
	if got := actorLabel(&store.User{Name: "Test User", Username: "loginname"}); got != "Test User" {
		t.Errorf("actorLabel with name = %q, want Test User", got)
	}
}
