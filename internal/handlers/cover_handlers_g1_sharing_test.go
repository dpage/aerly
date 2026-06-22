package handlers

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/config"
)

// g1MailCfg returns a config whose mail pipeline is wired to a no-op sendmail
// (/bin/true accepts on stdin and exits 0), so the share/notify email branches
// run end-to-end without touching a real MTA.
func g1MailCfg() *config.Config {
	return &config.Config{
		MailFromAddress: "noreply@aerly.example",
		SendmailPath:    "/bin/true",
		PublicURL:       "https://aerly.example",
	}
}

// TestNotifySharesEmailBranchesG1 drives notifyTripShares with both an in-app
// user recipient (covering emailUser) and a pre-shared email recipient
// (covering sendShareEmailTo via the pending-invite allowlist).
func TestNotifySharesEmailBranchesG1(t *testing.T) {
	e := setup(t, nil, g1MailCfg())
	owner := e.user(t, "g1nsowner", false)
	bob := e.user(t, "g1nsbob", false)
	seedVerifiedEmail(t, e, bob, "bob@example.com")
	e.befriend(t, owner, bob)
	tripID := newTrip(t, e, owner, "Notify trip")

	// bob becomes a member so canSeeShared passes for the in-app branch.
	if w := e.req(t, "POST", "/api/trips/"+itoa(tripID)+"/members",
		map[string]any{"user_id": bob, "role": "viewer"}, owner); w.Code != http.StatusOK && w.Code != http.StatusCreated {
		t.Fatalf("add member: %d %s", w.Code, w.Body.String())
	}

	// Share with a brand-new address: this registers a pending friend invite,
	// which is what makes the address an allowed notify recipient.
	const newAddr = "newcomer@example.com"
	if w := e.req(t, "POST", "/api/trips/"+itoa(tripID)+"/share-by-email",
		map[string]any{"email": newAddr, "role": "viewer"}, owner); w.Code != http.StatusAccepted {
		t.Fatalf("share-by-email: %d %s", w.Code, w.Body.String())
	}

	w := e.req(t, "POST", "/api/trips/"+itoa(tripID)+"/notify-shares",
		map[string]any{"user_ids": []int64{bob}, "emails": []string{newAddr}}, owner)
	if w.Code != http.StatusNoContent {
		t.Fatalf("notify-shares: %d %s", w.Code, w.Body.String())
	}
	// bob got an in-app notification.
	if n, err := e.store.CountUnreadNotifications(context.Background(), bob); err != nil || n != 1 {
		t.Errorf("bob unread = %d (err=%v), want 1", n, err)
	}
}

// TestNotifySharesUnsolicitedRecipientsG1 covers the rejection branches: a
// user_id who can't see the resource, and an email with no pending invite.
func TestNotifySharesUnsolicitedRecipientsG1(t *testing.T) {
	e := setup(t, nil, g1MailCfg())
	owner := e.user(t, "g1unsowner", false)
	stranger := e.user(t, "g1unsstranger", false)
	tripID := newTrip(t, e, owner, "Unsolicited trip")

	// stranger is not a member -> canSeeShared false; the unsolicited email has
	// no matching pending invite. Both are silently skipped, still 204.
	w := e.req(t, "POST", "/api/trips/"+itoa(tripID)+"/notify-shares",
		map[string]any{"user_ids": []int64{stranger}, "emails": []string{"spam@example.com"}}, owner)
	if w.Code != http.StatusNoContent {
		t.Fatalf("notify-shares: %d %s", w.Code, w.Body.String())
	}
	if n, err := e.store.CountUnreadNotifications(context.Background(), stranger); err != nil || n != 0 {
		t.Errorf("stranger unread = %d (err=%v), want 0", n, err)
	}
}

// TestNotifyPlanSharesEmailG1 mirrors the trip path for a plan, covering the
// plan-titled email branch (name falls back to type when title is empty handled
// elsewhere; here the plan has a title).
func TestNotifyPlanSharesEmailG1(t *testing.T) {
	e := setup(t, nil, g1MailCfg())
	owner := e.user(t, "g1npsowner", false)
	bob := e.user(t, "g1npsbob", false)
	seedVerifiedEmail(t, e, bob, "planbob@example.com")
	e.befriend(t, owner, bob)
	tripID := newTrip(t, e, owner, "Plan notify trip")
	planID := newFlightPlan(t, e, tripID, owner, "BA286", time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC))
	if w := e.req(t, "POST", "/api/trips/"+itoa(tripID)+"/members",
		map[string]any{"user_id": bob, "role": "viewer"}, owner); w.Code != http.StatusOK && w.Code != http.StatusCreated {
		t.Fatalf("add member: %d %s", w.Code, w.Body.String())
	}

	w := e.req(t, "POST", "/api/plans/"+itoa(planID)+"/notify-shares",
		map[string]any{"user_ids": []int64{bob}, "emails": []string{}}, owner)
	if w.Code != http.StatusNoContent {
		t.Fatalf("plan notify-shares: %d %s", w.Code, w.Body.String())
	}
	if n, err := e.store.CountUnreadNotifications(context.Background(), bob); err != nil || n != 1 {
		t.Errorf("bob unread = %d (err=%v), want 1", n, err)
	}
}

// TestShareByEmailSelfNoOpG1 covers the self-share no-op branch in shareByEmail:
// the owner shares with their own verified address, which must not create a
// trip_members row for themselves.
func TestShareByEmailSelfNoOpG1(t *testing.T) {
	e := setup(t, nil, g1MailCfg())
	owner := e.user(t, "g1selfowner", false)
	const selfAddr = "self@example.com"
	seedVerifiedEmail(t, e, owner, selfAddr)
	tripID := newTrip(t, e, owner, "Self share trip")
	ctx := context.Background()

	w := e.req(t, "POST", "/api/trips/"+itoa(tripID)+"/share-by-email",
		map[string]any{"email": selfAddr, "role": "viewer"}, owner)
	if w.Code != http.StatusAccepted {
		t.Fatalf("self share-by-email: %d %s", w.Code, w.Body.String())
	}
	// The self-share is a no-op: it creates no pending_shares row, and leaves
	// the owner's pre-existing membership untouched (exactly one owner row, not
	// a duplicate).
	var pending int
	if err := e.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM pending_shares WHERE email_lower=$1`, selfAddr).Scan(&pending); err != nil {
		t.Fatalf("count pending_shares: %v", err)
	}
	if pending != 0 {
		t.Errorf("self pending_shares rows = %d, want 0 (no-op)", pending)
	}
	var members int
	if err := e.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM trip_members WHERE trip_id=$1 AND user_id=$2`,
		tripID, owner).Scan(&members); err != nil {
		t.Fatalf("count trip_members: %v", err)
	}
	if members != 1 {
		t.Errorf("self trip_members rows = %d, want 1 (unchanged owner row)", members)
	}
}

// TestSharePlanByEmailExistingUserG1 covers sharePlanByEmail's existing-user
// default branch (AddPlanPassenger), complementing the new-address test.
func TestSharePlanByEmailExistingUserG1(t *testing.T) {
	e := setup(t, nil, g1MailCfg())
	owner := e.user(t, "g1spbeowner", false)
	mate := e.user(t, "g1spbemate", false)
	const mateAddr = "mate@example.com"
	seedVerifiedEmail(t, e, mate, mateAddr)
	tripID := newTrip(t, e, owner, "Plan share existing trip")
	planID := newFlightPlan(t, e, tripID, owner, "BA286", time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC))
	ctx := context.Background()

	w := e.req(t, "POST", "/api/plans/"+itoa(planID)+"/share-by-email",
		map[string]any{"email": mateAddr}, owner)
	if w.Code != http.StatusAccepted {
		t.Fatalf("plan share-by-email: %d %s", w.Code, w.Body.String())
	}
	var pax int
	if err := e.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM plan_passengers WHERE plan_id=$1 AND user_id=$2`,
		planID, mate).Scan(&pax); err != nil {
		t.Fatalf("count plan_passengers: %v", err)
	}
	if pax != 1 {
		t.Errorf("plan_passengers rows = %d, want 1", pax)
	}
}

// TestShareByEmailEmptyAddressG1 covers parseShareEmail's empty-address branch.
func TestShareByEmailEmptyAddressG1(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g1emptyowner", false)
	tripID := newTrip(t, e, owner, "Empty addr trip")

	w := e.req(t, "POST", "/api/trips/"+itoa(tripID)+"/share-by-email",
		map[string]any{"email": "   ", "role": "viewer"}, owner)
	if w.Code != http.StatusBadRequest {
		t.Errorf("empty email code = %d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// TestShareNotFoundIDsG1 covers the invalid-ID 400 branches across the sharing
// endpoints that parse a path ID.
func TestShareNotFoundIDsG1(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g1badidowner", false)

	cases := []struct {
		method, path string
		body         any
	}{
		{"PUT", "/api/trips/abc/share-all-friends", map[string]any{"role": "viewer"}},
		{"PUT", "/api/plans/abc/share-all-friends", map[string]any{"enabled": true}},
		{"POST", "/api/trips/abc/share-by-email", map[string]any{"email": "x@example.com", "role": "viewer"}},
		{"POST", "/api/plans/abc/share-by-email", map[string]any{"email": "x@example.com"}},
		{"POST", "/api/trips/abc/notify-shares", map[string]any{}},
		{"POST", "/api/plans/abc/notify-shares", map[string]any{}},
	}
	for _, c := range cases {
		if w := e.req(t, c.method, c.path, c.body, owner); w.Code != http.StatusBadRequest {
			t.Errorf("%s %s = %d, want 400", c.method, c.path, w.Code)
		}
	}
}

// TestShareBadBodyG1 covers the decode-failure 400 branches.
func TestShareBadBodyG1(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g1badbodyowner", false)
	tripID := newTrip(t, e, owner, "Bad body trip")
	planID := newFlightPlan(t, e, tripID, owner, "BA286", time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC))

	cases := []struct{ method, path string }{
		{"PUT", "/api/trips/" + itoa(tripID) + "/share-all-friends"},
		{"PUT", "/api/plans/" + itoa(planID) + "/share-all-friends"},
		{"POST", "/api/trips/" + itoa(tripID) + "/share-by-email"},
		{"POST", "/api/plans/" + itoa(planID) + "/share-by-email"},
		{"POST", "/api/trips/" + itoa(tripID) + "/notify-shares"},
		{"POST", "/api/plans/" + itoa(planID) + "/notify-shares"},
	}
	for _, c := range cases {
		if w := e.req(t, c.method, c.path, "??", owner); w.Code != http.StatusBadRequest {
			t.Errorf("%s %s bad body = %d, want 400", c.method, c.path, w.Code)
		}
	}
}
