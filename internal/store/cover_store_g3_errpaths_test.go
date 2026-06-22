package store

import (
	"testing"
	"time"
)

// TestG3QueryErrorPaths drives the "query/exec failed" error-return branches of
// the round-3 store methods using an already-cancelled context. These branches
// are otherwise unreachable through the exported API against a healthy DB; the
// cancelled context makes pgx fail the operation so the guard is exercised. We
// only assert that an error comes back, not its exact value.
func TestG3QueryErrorPaths(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	cc := g3CancelledCtx(t)

	t.Run("notifications", func(t *testing.T) {
		if _, err := s.InsertNotification(cc, Notification{UserID: 1, Kind: "share", Message: "x"}); err == nil {
			t.Error("InsertNotification: want error")
		}
		if _, err := s.ListNotifications(cc, 1, 10); err == nil {
			t.Error("ListNotifications: want error")
		}
		if err := s.MarkNotificationsRead(cc, 1); err == nil {
			t.Error("MarkNotificationsRead: want error")
		}
		if _, err := s.CountUnreadNotifications(cc, 1); err == nil {
			t.Error("CountUnreadNotifications: want error")
		}
		if err := s.DeleteNotification(cc, 1, 1); err == nil {
			t.Error("DeleteNotification: want error")
		}
		if err := s.DeleteAllNotifications(cc, 1); err == nil {
			t.Error("DeleteAllNotifications: want error")
		}
	})

	t.Run("alerts", func(t *testing.T) {
		if _, err := s.AlertPrefsFor(cc, 1); err == nil {
			t.Error("AlertPrefsFor: want error")
		}
		if err := s.SetAlertPrefs(cc, AlertPrefs{UserID: 1}); err == nil {
			t.Error("SetAlertPrefs: want error")
		}
		if err := s.AddPlanAlertOptin(cc, 1, 1); err == nil {
			t.Error("AddPlanAlertOptin: want error")
		}
		if _, err := s.PlanAlertOptedIn(cc, 1, 1); err == nil {
			t.Error("PlanAlertOptedIn: want error")
		}
		if err := s.RemovePlanAlertOptin(cc, 1, 1); err == nil {
			t.Error("RemovePlanAlertOptin: want error")
		}
		if _, err := s.AlertRecipients(cc, 1); err == nil {
			t.Error("AlertRecipients: want error")
		}
		if _, err := s.AlertRecipientsWithPrefs(cc, 1); err == nil {
			t.Error("AlertRecipientsWithPrefs: want error")
		}
		if _, _, err := s.FlightPartAlertSig(cc, 1); err == nil {
			t.Error("FlightPartAlertSig: want error")
		}
		if err := s.SetFlightPartAlertSig(cc, 1, "x"); err == nil {
			t.Error("SetFlightPartAlertSig: want error")
		}
		if _, err := s.InsertFlightAlert(cc, FlightAlert{}); err == nil {
			t.Error("InsertFlightAlert: want error")
		}
		if _, err := s.ListFlightAlerts(cc, 1, 10); err == nil {
			t.Error("ListFlightAlerts: want error")
		}
		if err := s.MarkFlightAlertsRead(cc, 1); err == nil {
			t.Error("MarkFlightAlertsRead: want error")
		}
		if _, err := s.CountUnreadFlightAlerts(cc, 1); err == nil {
			t.Error("CountUnreadFlightAlerts: want error")
		}
		if err := s.DeleteFlightAlert(cc, 1, 1); err == nil {
			t.Error("DeleteFlightAlert: want error")
		}
		if err := s.DeleteAllFlightAlerts(cc, 1); err == nil {
			t.Error("DeleteAllFlightAlerts: want error")
		}
	})

	t.Run("stats", func(t *testing.T) {
		if _, err := s.MyFlights(cc, 1); err == nil {
			t.Error("MyFlights: want error")
		}
	})

	t.Run("attachments", func(t *testing.T) {
		if _, err := s.CreatePlanAttachment(cc, CreatePlanAttachmentPayload{}); err == nil {
			t.Error("CreatePlanAttachment: want error")
		}
		if _, err := s.PlanAttachmentByID(cc, 1); err == nil {
			t.Error("PlanAttachmentByID: want error")
		}
		if _, err := s.AttachmentsByPlan(cc, 1); err == nil {
			t.Error("AttachmentsByPlan: want error")
		}
		if err := s.DeletePlanAttachment(cc, 1); err == nil {
			t.Error("DeletePlanAttachment: want error")
		}
		if _, err := s.StorageKeysByPlan(cc, 1); err == nil {
			t.Error("StorageKeysByPlan: want error")
		}
	})

	t.Run("user_emails", func(t *testing.T) {
		if err := s.UpsertVerifiedEmail(cc, 1, "test@example.com"); err == nil {
			t.Error("UpsertVerifiedEmail: want error")
		}
		if _, err := s.SuperuserEmails(cc); err == nil {
			t.Error("SuperuserEmails: want error")
		}
		if _, err := s.EmailsByUser(cc, 1); err == nil {
			t.Error("EmailsByUser: want error")
		}
		if _, _, err := s.ResendVerification(cc, 1, 1); err == nil {
			t.Error("ResendVerification: want error")
		}
		if _, err := s.VerifyEmailByToken(cc, "some-token"); err == nil {
			t.Error("VerifyEmailByToken: want error")
		}
		if err := s.DeleteUserEmail(cc, 1, 1); err == nil {
			t.Error("DeleteUserEmail: want error")
		}
		if _, _, err := s.InsertUnverifiedEmail(cc, 1, "test@example.com"); err == nil {
			t.Error("InsertUnverifiedEmail: want error")
		}
	})

	t.Run("positions", func(t *testing.T) {
		if err := s.InsertPartPosition(cc, Position{FlightID: 1, Ts: time.Now()}); err == nil {
			t.Error("InsertPartPosition: want error")
		}
		if _, err := s.LatestRealPosition(cc, 1); err == nil {
			t.Error("LatestRealPosition: want error")
		}
		if _, err := s.LatestPosition(cc, 1); err == nil {
			t.Error("LatestPosition: want error")
		}
		if _, err := s.LatestPartPositions(cc, []int64{1}); err == nil {
			t.Error("LatestPartPositions: want error")
		}
		if _, err := s.PartTracks(cc, []int64{1}, 10); err == nil {
			t.Error("PartTracks: want error")
		}
		if _, err := s.PositionsForFlight(cc, 1, 10); err == nil {
			t.Error("PositionsForFlight: want error")
		}
		// SmoothEstimatedTrack: a non-estimated fix with an origin anchor reaches
		// the lastRealPositionBefore query, which fails on the cancelled context.
		f := &Flight{ID: 1, ScheduledOut: time.Now(), OriginLat: ptr(51.0), OriginLon: ptr(0.0)}
		if err := s.SmoothEstimatedTrack(cc, f, Position{FlightID: 1, Ts: time.Now()}); err == nil {
			t.Error("SmoothEstimatedTrack: want error")
		}
	})

	t.Run("webpush", func(t *testing.T) {
		if _, err := s.UpsertWebPushSubscription(cc, WebPushSubscription{}); err == nil {
			t.Error("UpsertWebPushSubscription: want error")
		}
		if _, err := s.WebPushSubscriptionsFor(cc, 1); err == nil {
			t.Error("WebPushSubscriptionsFor: want error")
		}
		if err := s.DeleteWebPushSubscriptionByEndpoint(cc, 1, "e"); err == nil {
			t.Error("DeleteWebPushSubscriptionByEndpoint: want error")
		}
		if err := s.DeleteWebPushSubscription(cc, 1); err == nil {
			t.Error("DeleteWebPushSubscription: want error")
		}
		if err := s.MarkWebPushSuccess(cc, 1); err == nil {
			t.Error("MarkWebPushSuccess: want error")
		}
		if _, err := s.BumpWebPushFailure(cc, 1); err == nil {
			t.Error("BumpWebPushFailure: want error")
		}
		if _, err := s.PushKindEnabled(cc, 1, "k"); err == nil {
			t.Error("PushKindEnabled: want error")
		}
		if _, err := s.PushKindPrefsFor(cc, 1); err == nil {
			t.Error("PushKindPrefsFor: want error")
		}
		if err := s.SetPushKindPref(cc, 1, "k", true); err == nil {
			t.Error("SetPushKindPref: want error")
		}
	})

	t.Run("users", func(t *testing.T) {
		if _, err := s.UsersByIDs(cc, []int64{1}); err == nil {
			t.Error("UsersByIDs: want error")
		}
		if _, err := s.ListUsers(cc); err == nil {
			t.Error("ListUsers: want error")
		}
		if _, err := s.IdentitiesByUser(cc, 1); err == nil {
			t.Error("IdentitiesByUser: want error")
		}
		if err := s.BumpSessionVersion(cc, 1); err == nil {
			t.Error("BumpSessionVersion: want error")
		}
		if _, _, err := s.LinkLogin(cc, OAuthProfile{Provider: "github", ProviderUserID: "1"}, false); err == nil {
			t.Error("LinkLogin: want error")
		}
	})
}
