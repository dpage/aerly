package handlers

import "testing"

// TestBandLabelsFor covers the opt-in nature of banding: a type bands only when
// it is present in the map, and it carries its own pair of edge labels.
func TestBandLabelsFor(t *testing.T) {
	if l, ok := bandLabelsFor("hotel"); !ok || l.First != "Check-in" || l.Last != "Check-out" {
		t.Fatalf("hotel labels = %+v ok=%v", l, ok)
	}
	if l, ok := bandLabelsFor("vehicle_hire"); !ok || l.First != "Pickup" || l.Last != "Return" {
		t.Fatalf("hire labels = %+v ok=%v", l, ok)
	}
	if _, ok := bandLabelsFor("ground"); ok {
		t.Fatal("ground must never band: a transfer is a point-to-point journey")
	}
	if _, ok := bandLabelsFor("flight"); ok {
		t.Fatal("flight must never band, or every red-eye splits into two tiles")
	}
	if _, ok := bandLabelsFor(""); ok {
		t.Fatal("an untyped part must never band")
	}
}

// TestBandUIDSuffixPreservesHotelHistory guards the iCal UID suffixes. The feed
// is subscribable, so calendar clients already hold hotel events keyed on the
// historic checkin/checkout suffixes; deriving those from the label instead
// would silently rename them and duplicate every subscriber's event.
func TestBandUIDSuffixPreservesHotelHistory(t *testing.T) {
	if got := bandUIDSuffix("hotel", false); got != "checkin" {
		t.Fatalf("hotel first-edge UID suffix = %q, want checkin; changing it orphans existing calendar subscriptions", got)
	}
	if got := bandUIDSuffix("hotel", true); got != "checkout" {
		t.Fatalf("hotel last-edge UID suffix = %q, want checkout", got)
	}
	if got := bandUIDSuffix("vehicle_hire", false); got != "pickup" {
		t.Fatalf("hire first-edge UID suffix = %q, want pickup", got)
	}
	if got := bandUIDSuffix("vehicle_hire", true); got != "return" {
		t.Fatalf("hire last-edge UID suffix = %q, want return", got)
	}
}

// TestBandUIDSuffixUnbandedType documents the defensive fallback: a type that
// does not band has no edges, so it has no suffix either.
func TestBandUIDSuffixUnbandedType(t *testing.T) {
	if got := bandUIDSuffix("flight", false); got != "" {
		t.Errorf("flight first-edge UID suffix = %q, want empty", got)
	}
	if got := bandUIDSuffix("flight", true); got != "" {
		t.Errorf("flight last-edge UID suffix = %q, want empty", got)
	}
}
