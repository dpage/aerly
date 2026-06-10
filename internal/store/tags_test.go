package store

import (
	"testing"
)

func TestSetAndGetTripTags(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	trip := mkTrip(t, s, owner)

	// Set with mixed case, whitespace, blanks, and a normalized duplicate.
	if err := s.SetTripTags(ctx, trip, []string{"Beach", " beach ", "  ", "Family"}); err != nil {
		t.Fatalf("SetTripTags: %v", err)
	}
	tags, err := s.TagsByTrip(ctx, trip)
	if err != nil {
		t.Fatalf("TagsByTrip: %v", err)
	}
	// "Beach" and " beach " collapse to one; "Family" survives; blank dropped.
	if len(tags) != 2 {
		t.Fatalf("tags = %v, want 2 distinct", tags)
	}

	// Replace set.
	if err := s.SetTripTags(ctx, trip, []string{"Work"}); err != nil {
		t.Fatalf("replace tags: %v", err)
	}
	tags, _ = s.TagsByTrip(ctx, trip)
	if len(tags) != 1 || tags[0] != "Work" {
		t.Errorf("after replace = %v, want [Work]", tags)
	}
}

func TestSuggestTagsVisibilityGated(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	member := mkUser(t, s)
	stranger := mkUser(t, s)

	mine := mkTrip(t, s, owner)
	addMember(t, s, mine, member, "viewer")
	if err := s.SetTripTags(ctx, mine, []string{"Beachfront", "Business"}); err != nil {
		t.Fatalf("set tags: %v", err)
	}

	// A trip the stranger owns with a matching tag, to prove the suggestion is
	// scoped to the *viewer's* trips, not all trips.
	other := mkTrip(t, s, stranger)
	if err := s.SetTripTags(ctx, other, []string{"Beachcomber"}); err != nil {
		t.Fatalf("set other tags: %v", err)
	}

	// Owner: prefix "bea" matches Beachfront only (Business doesn't).
	sug, err := s.SuggestTags(ctx, owner, "bea")
	if err != nil {
		t.Fatalf("SuggestTags owner: %v", err)
	}
	if len(sug) != 1 || sug[0] != "Beachfront" {
		t.Errorf("owner suggest = %v, want [Beachfront]", sug)
	}

	// Member of the trip also sees its tags.
	sug, _ = s.SuggestTags(ctx, member, "bus")
	if len(sug) != 1 || sug[0] != "Business" {
		t.Errorf("member suggest = %v, want [Business]", sug)
	}

	// Stranger sees only their own trip's tag, not the owner's Beachfront.
	sug, _ = s.SuggestTags(ctx, stranger, "bea")
	if len(sug) != 1 || sug[0] != "Beachcomber" {
		t.Errorf("stranger suggest = %v, want [Beachcomber] (own trip only)", sug)
	}

	// Empty query → no suggestions.
	if sug, _ := s.SuggestTags(ctx, owner, "  "); len(sug) != 0 {
		t.Errorf("empty query suggest = %v, want none", sug)
	}

	// A LIKE wildcard in the query is matched literally, not as a wildcard:
	// "%" must not match every tag.
	if sug, _ := s.SuggestTags(ctx, owner, "%"); len(sug) != 0 {
		t.Errorf("wildcard query suggest = %v, want none (literal match)", sug)
	}
}
