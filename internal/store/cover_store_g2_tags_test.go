package store

import "testing"

func TestG2TagsSetAndList(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	trip := mkTrip(t, s, owner)

	// Blank labels and duplicate (by normalized key) collapse.
	if err := s.SetTripTags(ctx, trip, []string{"Beach", "  ", "beach", "City Break"}); err != nil {
		t.Fatalf("SetTripTags: %v", err)
	}
	tags, err := s.TagsByTrip(ctx, trip)
	if err != nil {
		t.Fatalf("TagsByTrip: %v", err)
	}
	// "Beach" and "City Break" survive; the blank and the duplicate "beach" drop.
	if len(tags) != 2 {
		t.Fatalf("tags = %v, want 2", tags)
	}
	// Ordered by normalized key: "beach" < "city break".
	if tags[0] != "Beach" || tags[1] != "City Break" {
		t.Errorf("tags order = %v, want [Beach City Break]", tags)
	}

	// Re-setting replaces the set wholesale.
	if err := s.SetTripTags(ctx, trip, []string{"Skiing"}); err != nil {
		t.Fatalf("SetTripTags replace: %v", err)
	}
	tags, _ = s.TagsByTrip(ctx, trip)
	if len(tags) != 1 || tags[0] != "Skiing" {
		t.Errorf("after replace tags = %v, want [Skiing]", tags)
	}

	// Empty set clears all.
	if err := s.SetTripTags(ctx, trip, nil); err != nil {
		t.Fatalf("SetTripTags clear: %v", err)
	}
	tags, _ = s.TagsByTrip(ctx, trip)
	if len(tags) != 0 {
		t.Errorf("after clear tags = %v, want empty", tags)
	}
}

func TestG2SuggestTags(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	stranger := mkUser(t, s)
	myTrip := mkTrip(t, s, owner)
	theirTrip := mkTrip(t, s, stranger)

	if err := s.SetTripTags(ctx, myTrip, []string{"Beach holiday", "Business"}); err != nil {
		t.Fatalf("SetTripTags mine: %v", err)
	}
	if err := s.SetTripTags(ctx, theirTrip, []string{"Beachfront"}); err != nil {
		t.Fatalf("SetTripTags theirs: %v", err)
	}

	// Empty query → no suggestions.
	if out, err := s.SuggestTags(ctx, owner, "   "); err != nil || out != nil {
		t.Errorf("SuggestTags empty = %v, %v; want nil", out, err)
	}

	// Prefix "bea" matches only the owner's own tag, not the stranger's trip.
	out, err := s.SuggestTags(ctx, owner, "Bea")
	if err != nil {
		t.Fatalf("SuggestTags: %v", err)
	}
	if len(out) != 1 || out[0] != "Beach holiday" {
		t.Errorf("SuggestTags(owner, Bea) = %v, want [Beach holiday]", out)
	}

	// A member of a trip can also see its tags.
	member := mkUser(t, s)
	addMember(t, s, theirTrip, member, "viewer")
	out, _ = s.SuggestTags(ctx, member, "bea")
	if len(out) != 1 || out[0] != "Beachfront" {
		t.Errorf("SuggestTags(member, bea) = %v, want [Beachfront]", out)
	}

	// LIKE metacharacters in the query are matched literally (no wildcard).
	if err := s.SetTripTags(ctx, myTrip, []string{"50%off", "Business"}); err != nil {
		t.Fatalf("SetTripTags literal: %v", err)
	}
	out, _ = s.SuggestTags(ctx, owner, "50%")
	if len(out) != 1 || out[0] != "50%off" {
		t.Errorf("SuggestTags(owner, 50%%) = %v, want [50%%off]", out)
	}
}

func TestG2TagsErrorPaths(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	cc := canceled()
	if _, err := s.TagsByTrip(cc, 1); err == nil {
		t.Error("TagsByTrip cancelled should error")
	}
	if err := s.SetTripTags(cc, 1, []string{"x"}); err == nil {
		t.Error("SetTripTags cancelled should error")
	}
	if _, err := s.SuggestTags(cc, 1, "x"); err == nil {
		t.Error("SuggestTags cancelled should error")
	}
}
