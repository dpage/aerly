package handlers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/mail"
	"strings"

	"github.com/dpage/aerly/internal/auth"
	"github.com/dpage/aerly/internal/mailer"
	"github.com/dpage/aerly/internal/store"
)

type shareAllFriendsTripReq struct {
	Role string `json:"role"` // "viewer"|"editor"|"" (clear)
}

// setTripShareAllFriends toggles the persistent trip-level "share with all
// friends" grant: every accepted friend gets the chosen role until it's cleared
// with an empty role. Owner-only, mirroring member management.
func (a *API) setTripShareAllFriends(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	me := auth.UserFrom(r.Context())
	if err := a.requireTripOwner(r.Context(), id, me, w); err != nil {
		return
	}
	var in shareAllFriendsTripReq
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if in.Role != "" && in.Role != "viewer" && in.Role != "editor" {
		writeError(w, http.StatusBadRequest, "role must be viewer, editor, or empty")
		return
	}
	if err := a.Store.SetTripShareAllFriends(r.Context(), id, in.Role); err != nil {
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

type shareAllFriendsPlanReq struct {
	Enabled bool `json:"enabled"`
}

// setPlanShareAllFriends toggles the per-plan "share with all friends" grant.
// Requires editor rights on the plan's trip.
func (a *API) setPlanShareAllFriends(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	me := auth.UserFrom(r.Context())
	if err := a.requirePlanEdit(r.Context(), id, me, w); err != nil {
		return
	}
	var in shareAllFriendsPlanReq
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := a.Store.SetPlanShareAllFriends(r.Context(), id, in.Enabled); err != nil {
		handleStoreErr(w, err)
		return
	}
	dto, err := a.planDTO(r.Context(), id, me.ID)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	a.publishPlanUpdated(r.Context(), dto.TripID, id)
	writeJSON(w, http.StatusOK, dto)
}

// parseShareEmail trims, validates and lowercases a share-by-email address.
// On a parse failure it writes a 400 and returns ok=false.
func parseShareEmail(w http.ResponseWriter, raw string) (string, bool) {
	addr := strings.TrimSpace(raw)
	if addr == "" {
		writeError(w, http.StatusBadRequest, "email required")
		return "", false
	}
	parsed, err := mail.ParseAddress(addr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid email address")
		return "", false
	}
	return strings.ToLower(parsed.Address), true
}

// writeShareAccepted mirrors inviteFriend's enumeration-safe 202 response so
// the share-by-email endpoints reveal nothing about whether the address
// belongs to an existing account.
func writeShareAccepted(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte(inviteFriendAcceptedBody))
}

type shareByEmailTripReq struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

// shareTripByEmail shares a trip with an email address — including one that
// has no account yet — by sending a friend request/invite and recording the
// grant (or pre-share) so it activates on acceptance. Owner-only.
func (a *API) shareTripByEmail(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	me := auth.UserFrom(r.Context())
	if err := a.requireTripOwner(r.Context(), id, me, w); err != nil {
		return
	}
	var in shareByEmailTripReq
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if in.Role != "viewer" && in.Role != "editor" {
		writeError(w, http.StatusBadRequest, "role must be viewer or editor")
		return
	}
	addr, ok := parseShareEmail(w, in.Email)
	if !ok {
		return
	}
	a.shareByEmail(r.Context(), me, addr, "trip", id, in.Role)
	writeShareAccepted(w)
}

type shareByEmailPlanReq struct {
	Email string `json:"email"`
}

// sharePlanByEmail shares a plan with an email address, mirroring
// shareTripByEmail. Requires editor rights on the plan's trip.
func (a *API) sharePlanByEmail(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	me := auth.UserFrom(r.Context())
	if err := a.requirePlanEdit(r.Context(), id, me, w); err != nil {
		return
	}
	var in shareByEmailPlanReq
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	addr, ok := parseShareEmail(w, in.Email)
	if !ok {
		return
	}
	a.shareByEmail(r.Context(), me, addr, "plan", id, "")
	writeShareAccepted(w)
}

// shareByEmail invites the address as a friend and records the share so it
// activates on acceptance. Best-effort and enumeration-safe (no signal about
// whether the address belongs to an existing user). kind "trip" uses role.
func (a *API) shareByEmail(ctx context.Context, me *store.User, addr, kind string, targetID int64, role string) {
	target, err := a.Store.UserByVerifiedEmail(ctx, addr)
	switch {
	case errors.Is(err, store.ErrNotFound):
		a.inviteFriendByEmail(ctx, me, addr, "")
		if err := a.Store.InsertPendingShare(ctx, store.PendingShare{
			EmailLower: addr, Kind: kind, TargetID: targetID, Role: role, InviterID: me.ID,
		}); err != nil {
			slog.Error("shareByEmail: insert pending share", "err", err)
		}
	case err != nil:
		slog.Error("shareByEmail: lookup", "err", err)
	default:
		if target.ID == me.ID {
			return // sharing with self: no-op
		}
		a.inviteFriendByUserID(ctx, me, target, addr, "")
		switch kind {
		case "trip":
			if err := a.Store.AddTripMember(ctx, targetID, target.ID, role); err != nil {
				slog.Error("shareByEmail: add trip member", "err", err)
			}
		case "plan":
			if err := a.Store.AddPlanPassenger(ctx, targetID, target.ID); err != nil {
				slog.Error("shareByEmail: add plan passenger", "err", err)
			}
		}
	}
}

// actorLabel renders the human-facing name for a sharing actor, falling back
// to the username when the display name is empty.
func actorLabel(u *store.User) string {
	if u == nil {
		return "Someone"
	}
	if name := strings.TrimSpace(u.Name); name != "" {
		return name
	}
	return u.Username
}

// emailUser sends the share notification email to an existing user, addressed
// to their first verified email. Best-effort: missing config, no verified
// email, or a send failure are logged and skipped — never fatal.
func (a *API) emailUser(ctx context.Context, userID int64, actorName, itemName, path string) {
	if a.Config == nil || a.Config.MailFromAddress == "" {
		return
	}
	addrs, err := a.Store.EmailsByUser(ctx, userID)
	if err != nil {
		slog.Error("notifyShares: load recipient emails", "err", err, "to", userID)
		return
	}
	to := ""
	for _, e := range addrs {
		if e.Verified {
			to = e.Address
			break
		}
	}
	if to == "" {
		return
	}
	a.sendShareEmailTo(ctx, to, actorName, itemName, path)
}

// sendShareEmailTo builds and sends a share notification email to a literal
// address. Best-effort: missing config or a send failure are logged, not fatal.
func (a *API) sendShareEmailTo(ctx context.Context, addr, actorName, itemName, path string) {
	if a.Config == nil || a.Config.MailFromAddress == "" {
		return
	}
	msg := buildShareEmail(shareEmailInput{
		FromAddr:  a.Config.MailFromAddress,
		ToAddr:    addr,
		PublicURL: a.Config.PublicURL,
		ActorName: actorName,
		ItemName:  itemName,
		Path:      path,
	})
	sendCtx, cancel := context.WithTimeout(ctx, friendEmailSendTimeout)
	defer cancel()
	if err := mailer.Send(sendCtx, a.Config.SendmailPath, a.Config.MailFromAddress, msg); err != nil {
		slog.Error("notifyShares: send share email failed", "err", err, "to", addr)
	}
}

type notifySharesReq struct {
	UserIDs []int64  `json:"user_ids"`
	Emails  []string `json:"emails"`
}

// notifyShares fans out share notifications to the newly-added sharees: each
// user_id gets an in-app notification (plus SSE badge) and an email; each bare
// email (a pre-shared address) gets an email. Failures are per-recipient and
// best-effort so a single bad recipient never blocks the rest.
//
// Recipients are validated before notifying: a user_id must actually be able to
// see the shared resource, and an email must be one the actor has a pending
// friend invite to. Without this, a trip/plan editor could use the endpoint to
// spam in-app notifications to arbitrary user ids and "X shared … with you"
// emails (attributed to themselves) to arbitrary addresses.
func (a *API) notifyShares(ctx context.Context, actor *store.User, in notifySharesReq, tripID, planID int64, itemName, path string) {
	var tp, pp *int64
	if tripID != 0 {
		tp = &tripID
	}
	if planID != 0 {
		pp = &planID
	}
	aid := actor.ID
	label := actorLabel(actor)
	for _, uid := range in.UserIDs {
		ok, err := a.canSeeShared(ctx, uid, tripID, planID)
		if err != nil {
			slog.Error("notifyShares: visibility check", "err", err, "to", uid)
			continue
		}
		if !ok {
			// Not actually a sharee of this resource — refuse to notify so the
			// endpoint can't be used to push notifications to arbitrary users.
			slog.Warn("notifyShares: skipping non-sharee user", "actor", aid, "to", uid)
			continue
		}
		msg := fmt.Sprintf("%s shared %q with you", label, itemName)
		if _, err := a.Store.InsertNotification(ctx, store.Notification{
			UserID: uid, Kind: "share", ActorID: &aid, TripID: tp, PlanID: pp, Message: msg,
		}); err != nil {
			slog.Error("notifyShares: insert notification", "err", err, "to", uid)
			continue
		}
		a.publishNotifications(ctx, uid)
		a.emailUser(ctx, uid, label, itemName, path)
	}
	if len(in.Emails) == 0 {
		return
	}
	invited, err := a.Store.ListOutgoingPendingInvites(ctx, actor.ID)
	if err != nil {
		slog.Error("notifyShares: list pending invites", "err", err, "actor", aid)
		return
	}
	allowed := make(map[string]bool, len(invited))
	for _, p := range invited {
		allowed[p.EmailLower] = true
	}
	for _, addr := range in.Emails {
		if !allowed[strings.ToLower(strings.TrimSpace(addr))] {
			// Only addresses the actor has a pending invite to are legitimate
			// pre-shared recipients; anything else is an arbitrary address.
			slog.Warn("notifyShares: skipping unsolicited email recipient", "actor", aid)
			continue
		}
		a.sendShareEmailTo(ctx, addr, label, itemName, path)
	}
}

// canSeeShared reports whether uid is a genuine sharee of the resource being
// announced: visible on the plan for a plan share, or on the trip otherwise.
func (a *API) canSeeShared(ctx context.Context, uid, tripID, planID int64) (bool, error) {
	if planID != 0 {
		return a.Store.CanViewPlan(ctx, planID, uid, false)
	}
	return a.Store.CanViewTrip(ctx, tripID, uid)
}

func (a *API) notifyTripShares(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	me := auth.UserFrom(r.Context())
	if err := a.requireTripEdit(r.Context(), id, me, w); err != nil {
		return
	}
	var in notifySharesReq
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	t, err := a.Store.TripByID(r.Context(), id)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	a.notifyShares(r.Context(), me, in, id, 0, t.Name, fmt.Sprintf("/trips/%d", id))
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) notifyPlanShares(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	me := auth.UserFrom(r.Context())
	if err := a.requirePlanEdit(r.Context(), id, me, w); err != nil {
		return
	}
	var in notifySharesReq
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	pl, err := a.Store.PlanByID(r.Context(), id)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	name := pl.Title
	if name == "" {
		name = pl.Type
	}
	a.notifyShares(r.Context(), me, in, pl.TripID, id, name, fmt.Sprintf("/trips/%d", pl.TripID))
	w.WriteHeader(http.StatusNoContent)
}
