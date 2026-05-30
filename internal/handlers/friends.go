package handlers

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/dpage/aerly/internal/api"
	"github.com/dpage/aerly/internal/auth"
	"github.com/dpage/aerly/internal/mailer"
	"github.com/dpage/aerly/internal/store"
)

// friendEmailSendTimeout bounds each outbound sendmail invocation so a
// stalled MTA can't pin the request worker. The handler still waits for
// the send before responding (to keep delivery best-effort synchronous),
// but a hung send aborts at this deadline and the request returns.
const friendEmailSendTimeout = 5 * time.Second

// friendAcceptTokenTTL bounds how long the Accept button in the friend
// request email remains clickable. The underlying pending friendship
// row stays around longer — only the email link goes dead — so a
// recipient can still accept in-app.
const friendAcceptTokenTTL = 7 * 24 * time.Hour

// inviteFriendAcceptedBody is the response every successful POST to
// /api/friends/invite returns, regardless of whether the email matched a
// verified user, queued a pending sign-up invite, or self-matched. Keeping
// the body byte-identical across the three paths is the no-enumeration
// guarantee — see TestInviteFriendResponseIdenticalForKnownAndUnknown.
const inviteFriendAcceptedBody = `{"status":"accepted"}` + "\n"

func (a *API) listFriends(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	rows, err := a.Store.ListFriendships(r.Context(), me.ID)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	pending, err := a.Store.ListOutgoingPendingInvites(r.Context(), me.ID)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	out := make([]api.FriendshipDTO, 0, len(rows)+len(pending))
	// All pending friendships first (preserves internal order from SQL),
	// then pending_friend_invites (also outgoing pending), then accepted.
	// Without this split the email-only outgoing invites would land *after*
	// accepted friendships, breaking the pending-precedes-accepted grouping.
	for _, f := range rows {
		if f.Status == "pending" {
			out = append(out, api.ToFriendshipDTO(f, me.ID))
		}
	}
	for _, p := range pending {
		out = append(out, api.OutgoingInviteToFriendshipDTO(p))
	}
	for _, f := range rows {
		if f.Status == "accepted" {
			out = append(out, api.ToFriendshipDTO(f, me.ID))
		}
	}
	writeJSON(w, http.StatusOK, out)
}

type inviteFriendReq struct {
	Email   string `json:"email"`
	Message string `json:"message,omitempty"`
}

func (a *API) inviteFriend(w http.ResponseWriter, r *http.Request) {
	var in inviteFriendReq
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	addr := strings.TrimSpace(in.Email)
	if addr == "" {
		writeError(w, http.StatusBadRequest, "email required")
		return
	}
	parsed, err := mail.ParseAddress(addr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid email address")
		return
	}
	addr = parsed.Address

	me := auth.UserFrom(r.Context())

	// Hide whether the target email exists or not — both branches return
	// the same response. We still do the work synchronously so the email
	// hits Postfix's queue before the response (callers that retry on
	// failure get a useful answer).
	target, err := a.Store.UserByVerifiedEmail(r.Context(), addr)
	switch {
	case errors.Is(err, store.ErrNotFound):
		a.inviteFriendByEmail(r.Context(), me, addr, in.Message)
	case err != nil:
		handleStoreErr(w, err)
		return
	default:
		// If they're trying to friend their own verified address, treat as
		// success — no edge to create, but we don't expose the self-match.
		if target.ID != me.ID {
			a.inviteFriendByUserID(r.Context(), me, target, addr, in.Message)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte(inviteFriendAcceptedBody))
}

// inviteFriendByUserID issues (or no-ops on) a pending friendship and
// emails the recipient. Errors are logged but never surfaced to the caller
// — see inviteFriend's enumeration-leak comment.
func (a *API) inviteFriendByUserID(ctx context.Context, me, target *store.User, invitedEmail, message string) {
	friendship, err := a.Store.RequestFriendship(ctx, me.ID, target.ID, invitedEmail)
	if err != nil {
		slog.Error("friend invite: request failed", "err", err, "from", me.ID, "to", target.ID)
		return
	}
	// Only notify on a brand-new pending request; duplicate calls or
	// already-accepted friendships stay silent.
	if friendship.Status != "pending" || friendship.RequestedBy != me.ID {
		return
	}
	a.sendFriendRequestNotification(ctx, me, target, message)
	a.publishNotifications(ctx, target.ID)
}

// inviteFriendByEmail queues a pending invite for an address that doesn't
// have an account yet and emails the recipient. Suppresses the email when
// the same inviter has already queued the same address (duplicate clicks).
func (a *API) inviteFriendByEmail(ctx context.Context, me *store.User, addr, message string) {
	created, err := a.Store.UpsertPendingFriendInvite(ctx, me.ID, addr, message)
	if err != nil {
		slog.Error("friend invite: upsert pending failed", "err", err, "from", me.ID)
		return
	}
	if !created {
		return
	}
	a.sendFriendInviteEmail(ctx, me, addr, message)
}

func (a *API) sendFriendRequestNotification(ctx context.Context, inviter, recipient *store.User, message string) {
	if a.Config == nil || a.Config.MailFromAddress == "" {
		slog.Warn("friend invite: MAIL_FROM_ADDRESS unset, skipping notification email",
			"from", inviter.ID, "to", recipient.ID)
		return
	}
	addrs, err := a.Store.EmailsByUser(ctx, recipient.ID)
	if err != nil {
		slog.Error("friend invite: load recipient emails failed", "err", err, "to", recipient.ID)
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
		// Edge case: matched user has no verified email row anymore.
		return
	}
	token := auth.MintFriendAcceptToken(
		a.Config.SessionKey,
		recipient.ID, inviter.ID,
		time.Now().Add(friendAcceptTokenTTL),
	)
	msg := buildFriendRequestEmail(friendRequestInput{
		FromAddr:     a.Config.MailFromAddress,
		ToAddr:       to,
		PublicURL:    a.Config.PublicURL,
		InviterName:  inviter.Name,
		InviterLogin: inviter.Username,
		Message:      message,
		Token:        token,
	})
	sendCtx, cancel := context.WithTimeout(ctx, friendEmailSendTimeout)
	defer cancel()
	if err := mailer.Send(sendCtx, a.Config.SendmailPath, a.Config.MailFromAddress, msg); err != nil {
		slog.Error("friend invite: send notification failed", "err", err)
	}
}

func (a *API) sendFriendInviteEmail(ctx context.Context, inviter *store.User, to, message string) {
	if a.Config == nil || a.Config.MailFromAddress == "" {
		slog.Warn("friend invite: MAIL_FROM_ADDRESS unset, skipping invite email",
			"from", inviter.ID, "to", to)
		return
	}
	msg := buildFriendInviteEmail(friendInviteInput{
		FromAddr:     a.Config.MailFromAddress,
		ToAddr:       to,
		PublicURL:    a.Config.PublicURL,
		InviterName:  inviter.Name,
		InviterLogin: inviter.Username,
		Message:      message,
	})
	sendCtx, cancel := context.WithTimeout(ctx, friendEmailSendTimeout)
	defer cancel()
	if err := mailer.Send(sendCtx, a.Config.SendmailPath, a.Config.MailFromAddress, msg); err != nil {
		slog.Error("friend invite: send invite failed", "err", err)
	}
}

func (a *API) acceptFriend(w http.ResponseWriter, r *http.Request) {
	otherID, err := pathID(r, "userId")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad user id")
		return
	}
	me := auth.UserFrom(r.Context())
	f, err := a.Store.AcceptFriendship(r.Context(), me.ID, otherID)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	// Notify both ends: the accepter's pending count drops, and the
	// inviter's friend list now has a new accepted edge so their open
	// FriendsDialog (and the share/passenger pickers) refresh live.
	a.publishNotifications(r.Context(), me.ID)
	a.publishNotifications(r.Context(), otherID)
	writeJSON(w, http.StatusOK, api.ToFriendshipDTO(f, me.ID))
}

func (a *API) removeFriend(w http.ResponseWriter, r *http.Request) {
	otherID, err := pathID(r, "userId")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad user id")
		return
	}
	me := auth.UserFrom(r.Context())

	if err := a.Store.RemoveFriendship(r.Context(), me.ID, otherID); err != nil {
		handleStoreErr(w, err)
		return
	}

	// Both sides get a notifications.updated so the friend list and user
	// cache refresh. The legacy per-flight visibility fan-out that used to
	// live here was retired with the flights table in Wave 3 — plan-level
	// visibility is trip-membership based and unaffected by unfriending.
	a.publishNotifications(r.Context(), me.ID)
	a.publishNotifications(r.Context(), otherID)
	w.WriteHeader(http.StatusNoContent)
}

type cancelOutgoingReq struct {
	Email string `json:"email"`
}

func (a *API) cancelOutgoingInvite(w http.ResponseWriter, r *http.Request) {
	var in cancelOutgoingReq
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	addr := strings.TrimSpace(in.Email)
	if addr == "" {
		writeError(w, http.StatusBadRequest, "email required")
		return
	}
	parsed, err := mail.ParseAddress(addr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid email address")
		return
	}
	me := auth.UserFrom(r.Context())
	if err := a.Store.CancelOutgoingInvite(r.Context(), me.ID, parsed.Address); err != nil {
		// Don't surface store errors that would leak which path matched —
		// log internally, return 204 either way.
		slog.Error("cancel outgoing invite failed", "err", err, "by", me.ID)
	}
	w.WriteHeader(http.StatusNoContent)
}

type acceptFriendTokenReq struct {
	Token string `json:"token"`
}

type acceptFriendTokenResp struct {
	Friendship *api.FriendshipDTO `json:"friendship,omitempty"`
	Already    bool               `json:"already,omitempty"`
}

func (a *API) acceptFriendToken(w http.ResponseWriter, r *http.Request) {
	var in acceptFriendTokenReq
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "bad request body")
		return
	}
	if strings.TrimSpace(in.Token) == "" {
		writeError(w, http.StatusBadRequest, "token required")
		return
	}
	recipientID, inviterID, err := auth.VerifyFriendAcceptToken(a.Config.SessionKey, in.Token)
	switch {
	case errors.Is(err, auth.ErrExpiredAcceptToken):
		writeError(w, http.StatusGone, "invitation link expired — ask the sender to resend")
		return
	case err != nil:
		writeError(w, http.StatusBadRequest, "invalid invitation link")
		return
	}
	me := auth.UserFrom(r.Context())
	if me.ID != recipientID {
		writeError(w, http.StatusForbidden, "this invitation isn't for your account")
		return
	}

	f, err := a.Store.AcceptFriendship(r.Context(), me.ID, inviterID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		// No pending row to accept. The recipient may already be friends
		// with the inviter, or the inviter cancelled the request. Surface
		// as a quiet "already" so the SPA shows an informational toast.
		writeJSON(w, http.StatusOK, acceptFriendTokenResp{Already: true})
		return
	case err != nil:
		handleStoreErr(w, err)
		return
	}
	// Notify both ends — same reason as acceptFriend: the inviter's
	// FriendsDialog and share/passenger pickers refresh live off this
	// event when they accept by email-link too.
	a.publishNotifications(r.Context(), me.ID)
	a.publishNotifications(r.Context(), inviterID)
	dto := api.ToFriendshipDTO(f, me.ID)
	writeJSON(w, http.StatusOK, acceptFriendTokenResp{Friendship: &dto})
}
