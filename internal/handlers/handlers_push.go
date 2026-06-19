package handlers

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/dpage/aerly/internal/auth"
	"github.com/dpage/aerly/internal/push"
	"github.com/dpage/aerly/internal/store"
)

// Web Push (PWA push notifications). These endpoints let a browser subscribe a
// device for push and let the user toggle which notification kinds push. The
// whole surface is dormant unless VAPID keys are configured: vapid-key reports
// enabled=false and the mutating endpoints return 503, so the frontend can hide
// the feature cleanly.

// pushKinds enumerates the notification kinds a user can toggle for push. The
// absence of a stored pref means enabled, so GET fills these defaults and PATCH
// validates against this set.
var pushKinds = []string{"alert", "share"}

// pusher is the slice of *push.Sender the handlers need, as an interface so the
// share-notification tests can substitute a fake that records pushes.
type pusher interface {
	Enabled() bool
	Send(ctx context.Context, userIDs []int64, p push.Payload)
}

func isPushKind(kind string) bool {
	for _, k := range pushKinds {
		if k == kind {
			return true
		}
	}
	return false
}

// pushShare delivers a "X shared … with you" Web Push to a sharee's devices,
// gated on their 'share' push-kind pref (default on). Best-effort: a disabled
// Sender, push-off, or no-subscriptions recipient is a silent no-op, and the
// send never blocks the share flow. Mirrors the poller's pushAlert.
func (a *API) pushShare(ctx context.Context, userID int64, actorLabel, itemName, path string) {
	if a.Push == nil || !a.Push.Enabled() {
		return
	}
	on, err := a.Store.PushKindEnabled(ctx, userID, "share")
	if err != nil {
		slog.Error("notifyShares: push kind pref", "err", err, "to", userID)
		return
	}
	if !on {
		return
	}
	a.Push.Send(ctx, []int64{userID}, push.Payload{
		Title: "Shared with you on Aerly",
		Body:  fmt.Sprintf("%s shared %q with you", actorLabel, itemName),
		URL:   path,
		Tag:   "share",
		Kind:  "share",
	})
}

// vapidKeyDTO is the GET /api/push/vapid-key response. PublicKey is empty when
// push is disabled; the client checks Enabled before trying to subscribe.
type vapidKeyDTO struct {
	Enabled   bool   `json:"enabled"`
	PublicKey string `json:"public_key,omitempty"`
}

func (a *API) getPushVAPIDKey(w http.ResponseWriter, r *http.Request) {
	if !a.Config.WebPushEnabled() {
		writeJSON(w, http.StatusOK, vapidKeyDTO{Enabled: false})
		return
	}
	writeJSON(w, http.StatusOK, vapidKeyDTO{Enabled: true, PublicKey: a.Config.WebPushVAPIDPublic})
}

// pushSubscriptionInput mirrors the browser's PushSubscription.toJSON() shape so
// the frontend can post it through more or less verbatim.
type pushSubscriptionInput struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
}

func (a *API) subscribePush(w http.ResponseWriter, r *http.Request) {
	if !a.Config.WebPushEnabled() {
		writeError(w, http.StatusServiceUnavailable, "Push notifications are not enabled.")
		return
	}
	me := auth.UserFrom(r.Context())
	var in pushSubscriptionInput
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body.")
		return
	}
	if in.Endpoint == "" || in.Keys.P256dh == "" || in.Keys.Auth == "" {
		writeError(w, http.StatusBadRequest, "Subscription is missing endpoint or keys.")
		return
	}
	if _, err := a.Store.UpsertWebPushSubscription(r.Context(), store.WebPushSubscription{
		UserID:    me.ID,
		Endpoint:  in.Endpoint,
		P256dh:    in.Keys.P256dh,
		Auth:      in.Keys.Auth,
		UserAgent: r.UserAgent(),
	}); err != nil {
		handleStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// unsubscribePushInput carries the endpoint to remove. Sent as a body (rather
// than a path param) because a push endpoint is a long opaque URL.
type unsubscribePushInput struct {
	Endpoint string `json:"endpoint"`
}

func (a *API) unsubscribePush(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	var in unsubscribePushInput
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body.")
		return
	}
	if in.Endpoint == "" {
		writeError(w, http.StatusBadRequest, "Missing endpoint.")
		return
	}
	// Idempotent: deleting an already-absent subscription is success, so a
	// client that lost track of its registration can always converge to "off".
	if err := a.Store.DeleteWebPushSubscriptionByEndpoint(r.Context(), me.ID, in.Endpoint); err != nil {
		handleStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// pushPrefsDTO is the GET/PATCH response: each known kind mapped to whether push
// is on for it. Defaults to true for kinds the user hasn't explicitly toggled.
type pushPrefsDTO struct {
	Kinds map[string]bool `json:"kinds"`
}

func (a *API) getPushPrefs(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	stored, err := a.Store.PushKindPrefsFor(r.Context(), me.ID)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	kinds := make(map[string]bool, len(pushKinds))
	for _, k := range pushKinds {
		if v, ok := stored[k]; ok {
			kinds[k] = v
		} else {
			kinds[k] = true // default-enabled when no explicit row
		}
	}
	writeJSON(w, http.StatusOK, pushPrefsDTO{Kinds: kinds})
}

// setPushPrefInput is the PATCH body: flip one kind on or off.
type setPushPrefInput struct {
	Kind    string `json:"kind"`
	Enabled *bool  `json:"enabled"`
}

func (a *API) setPushPref(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	var in setPushPrefInput
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body.")
		return
	}
	if !isPushKind(in.Kind) || in.Enabled == nil {
		writeError(w, http.StatusBadRequest, "Unknown kind or missing enabled flag.")
		return
	}
	if err := a.Store.SetPushKindPref(r.Context(), me.ID, in.Kind, *in.Enabled); err != nil {
		handleStoreErr(w, err)
		return
	}
	a.getPushPrefs(w, r)
}
