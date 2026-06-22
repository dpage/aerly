package api

import (
	"testing"
	"time"

	"github.com/dpage/aerly/internal/store"
)

func TestToDirectoryUserDTOOmitsAdminMetadata(t *testing.T) {
	last := time.Now()
	u := &store.User{
		ID: 9, Username: "octocat", Name: "Octo", AvatarURL: "a.png",
		IsSuperuser: true, IsActive: true, LastLoginAt: &last,
	}
	d := ToDirectoryUserDTO(u)
	if d.ID != 9 || d.Username != "octocat" || d.Name != "Octo" || d.AvatarURL != "a.png" {
		t.Errorf("identity fields wrong: %+v", d)
	}
	// The directory projection must not leak admin/activity metadata.
	if d.IsSuperuser || d.IsActive || d.HasLoggedIn || d.LastLoginAt != nil {
		t.Errorf("admin/activity metadata leaked: %+v", d)
	}
}

func TestToAutoShareDTOs(t *testing.T) {
	got := ToAutoShareDTOs([]store.AutoShare{
		{UserID: 1, ShareWithID: 2, Role: "viewer"},
		{UserID: 1, ShareWithID: 3, Role: "editor"},
	})
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].UserID != 2 || got[0].Role != "viewer" {
		t.Errorf("first entry = %+v", got[0])
	}
	if got[1].UserID != 3 || got[1].Role != "editor" {
		t.Errorf("second entry = %+v", got[1])
	}
	// Empty input yields a non-nil empty slice (serialises as [] not null).
	if empty := ToAutoShareDTOs(nil); empty == nil || len(empty) != 0 {
		t.Errorf("empty input = %v, want non-nil empty slice", empty)
	}
}

func TestToNotificationItemDTO(t *testing.T) {
	actor, trip, plan := int64(5), int64(6), int64(7)
	read := time.Unix(1700000500, 0)
	n := store.Notification{
		ID: 11, Kind: "trip_shared", ActorID: &actor, TripID: &trip, PlanID: &plan,
		Message: "shared a trip", CreatedAt: time.Unix(1700000000, 0), ReadAt: &read,
	}
	d := ToNotificationItemDTO(n)
	if d.ID != 11 || d.Source != NotificationSourceShare || d.Kind != "trip_shared" {
		t.Errorf("scalar fields wrong: %+v", d)
	}
	if d.ActorID == nil || *d.ActorID != 5 || d.TripID == nil || *d.TripID != 6 || d.PlanID == nil || *d.PlanID != 7 {
		t.Errorf("id pointers wrong: %+v", d)
	}
	// Share notifications never carry a plan-part deep link.
	if d.PlanPartID != nil {
		t.Errorf("PlanPartID = %v, want nil for share notifications", d.PlanPartID)
	}
	if d.Message != "shared a trip" || d.ReadAt == nil || !d.ReadAt.Equal(read) {
		t.Errorf("message/read wrong: %+v", d)
	}
}

func TestToAttachmentDTO(t *testing.T) {
	by := int64(3)
	a := &store.PlanAttachment{
		ID: 1, PlanID: 2, UploadedBy: &by, Filename: "boarding.pdf",
		ContentType: "application/pdf", SizeBytes: 4096,
		StorageKey: "s3/secret/handle", CreatedAt: time.Unix(1700000000, 0),
	}
	d := ToAttachmentDTO(a)
	if d.ID != 1 || d.PlanID != 2 || d.Filename != "boarding.pdf" {
		t.Errorf("scalar fields wrong: %+v", d)
	}
	if d.ContentType != "application/pdf" || d.SizeBytes != 4096 {
		t.Errorf("content/size wrong: %+v", d)
	}
	if d.UploadedBy == nil || *d.UploadedBy != 3 {
		t.Errorf("UploadedBy wrong: %+v", d)
	}
}

func TestToTripFeedDTO(t *testing.T) {
	fetched := time.Unix(1700000000, 0)
	f := &store.TripFeed{
		ID: 1, TripID: 2, URL: "https://example.com/cal.ics", Name: "Work",
		Timezone: "Europe/London", LastFetchedAt: &fetched, LastError: "boom",
	}
	d := ToTripFeedDTO(f)
	if d.ID != 1 || d.TripID != 2 || d.URL != "https://example.com/cal.ics" {
		t.Errorf("scalar fields wrong: %+v", d)
	}
	if d.Name != "Work" || d.Timezone != "Europe/London" || d.LastError != "boom" {
		t.Errorf("name/tz/error wrong: %+v", d)
	}
	if d.LastFetchedAt == nil || !d.LastFetchedAt.Equal(fetched) {
		t.Errorf("LastFetchedAt wrong: %+v", d)
	}
}

func TestToExternalEventDTOMapsSummaryToTitle(t *testing.T) {
	ends := time.Unix(1700003600, 0)
	e := &store.TripFeedEvent{
		ID: 1, FeedID: 2, FeedName: "Work", Summary: "Standup", Location: "Room 1",
		Description: "daily", StartsAt: time.Unix(1700000000, 0), EndsAt: &ends,
		StartTZ: "Europe/London", AllDay: true,
	}
	d := ToExternalEventDTO(e)
	// Summary becomes Title on the wire.
	if d.Title != "Standup" {
		t.Errorf("Title = %q, want Standup (from Summary)", d.Title)
	}
	if d.ID != 1 || d.FeedID != 2 || d.FeedName != "Work" {
		t.Errorf("ids/name wrong: %+v", d)
	}
	if d.Location != "Room 1" || d.Description != "daily" || d.StartTZ != "Europe/London" || !d.AllDay {
		t.Errorf("detail fields wrong: %+v", d)
	}
	if d.EndsAt == nil || !d.EndsAt.Equal(ends) {
		t.Errorf("EndsAt wrong: %+v", d)
	}
}

func TestToTripDTOWithExplicitDates(t *testing.T) {
	starts := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	ends := time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC)
	by := int64(3)
	tr := &store.Trip{
		ID: 1, Name: "Summer", Destination: "Nice", StartsOn: &starts, EndsOn: &ends,
		CreatedBy: &by, CountryCode: "fr", ShareAllFriendsRole: "viewer",
		CreatedAt: time.Unix(1700000000, 0), UpdatedAt: time.Unix(1700000100, 0),
	}
	members := []TripMemberDTO{{UserID: 3, Role: "owner"}}
	d := ToTripDTO(tr, "owner", members, []string{"beach"})
	if d.ID != 1 || d.Name != "Summer" || d.Destination != "Nice" || d.MyRole != "owner" {
		t.Errorf("scalar fields wrong: %+v", d)
	}
	if d.StartsOn == nil || *d.StartsOn != "2026-07-01" || d.EndsOn == nil || *d.EndsOn != "2026-07-08" {
		t.Errorf("explicit dates wrong: starts=%v ends=%v", d.StartsOn, d.EndsOn)
	}
	if d.CountryCode != "fr" || d.ShareAllFriendsRole != "viewer" {
		t.Errorf("country/share wrong: %+v", d)
	}
	if len(d.Members) != 1 || len(d.Tags) != 1 || d.Tags[0] != "beach" {
		t.Errorf("members/tags wrong: %+v", d)
	}
}

func TestToTripDTOWithEffectiveDatesAndNilSlices(t *testing.T) {
	es := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	ee := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	tr := &store.Trip{
		ID: 2, Name: "Inferred", EffectiveStart: &es, EffectiveEnd: &ee,
	}
	// nil members and tags must become non-nil empty slices for the wire.
	d := ToTripDTO(tr, "viewer", nil, nil)
	if d.StartsOn != nil || d.EndsOn != nil {
		t.Errorf("explicit dates should be nil: starts=%v ends=%v", d.StartsOn, d.EndsOn)
	}
	if d.EffectiveStart == nil || *d.EffectiveStart != "2026-08-02" {
		t.Errorf("EffectiveStart = %v, want 2026-08-02", d.EffectiveStart)
	}
	if d.EffectiveEnd == nil || *d.EffectiveEnd != "2026-08-09" {
		t.Errorf("EffectiveEnd = %v, want 2026-08-09", d.EffectiveEnd)
	}
	if d.Members == nil || len(d.Members) != 0 || d.Tags == nil || len(d.Tags) != 0 {
		t.Errorf("nil slices should become empty: members=%v tags=%v", d.Members, d.Tags)
	}
}

func TestToMeetingDetailDTO(t *testing.T) {
	d := ToMeetingDetailDTO(&store.MeetingDetail{
		Location: "HQ", Organiser: "Alex", Platform: "Zoom",
	})
	if d.Location != "HQ" || d.Organiser != "Alex" || d.Platform != "Zoom" {
		t.Errorf("unexpected dto: %+v", d)
	}
}

func TestToEventDetailDTO(t *testing.T) {
	d := ToEventDetailDTO(&store.EventDetail{
		Performer: "The Band", Category: "Concert", VenueArea: "Floor", URL: "https://tix.example.com",
	})
	if d.Performer != "The Band" || d.Category != "Concert" || d.VenueArea != "Floor" {
		t.Errorf("unexpected dto: %+v", d)
	}
	if d.URL != "https://tix.example.com" {
		t.Errorf("URL = %q", d.URL)
	}
}
