package handlers

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
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
	for _, addr := range in.Emails {
		a.sendShareEmailTo(ctx, addr, label, itemName, path)
	}
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
