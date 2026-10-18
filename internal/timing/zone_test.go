package timing

import (
	"errors"
	"testing"
	"time"
)

func fp(f float64) *float64 { return &f }

func TestValidZone(t *testing.T) {
	for tz, want := range map[string]bool{
		"Europe/London": true, "Asia/Kolkata": true, "UTC": true,
		"": false, "Local": false, "Mars/Olympus": false,
	} {
		if got := ValidZone(tz); got != want {
			t.Errorf("ValidZone(%q) = %v, want %v", tz, got, want)
		}
	}
}

func TestSourceValid(t *testing.T) {
	for _, s := range []Source{Confirmed, Inferred, Unknown, Floating} {
		if !s.Valid() {
			t.Errorf("%q should be valid", s)
		}
	}
	if Source("guessed").Valid() || Source("").Valid() {
		t.Error("unexpected valid source")
	}
}

func TestAnchor(t *testing.T) {
	floating := time.Date(2026, 7, 1, 15, 0, 0, 0, time.UTC) // "15:00 local"
	got, ok := Anchor(floating, "Europe/Paris")
	if !ok || !got.Equal(time.Date(2026, 7, 1, 13, 0, 0, 0, time.UTC)) {
		t.Fatalf("Paris summer: got %v %v", got, ok)
	}
	got, ok = Anchor(floating, "Asia/Kolkata")
	if !ok || !got.Equal(time.Date(2026, 7, 1, 9, 30, 0, 0, time.UTC)) {
		t.Fatalf("Kolkata: got %v", got)
	}
	// Fall-back overlap: 01:30 on 25-10-2026 happens twice in London; earlier is BST (00:30Z).
	got, _ = Anchor(time.Date(2026, 10, 25, 1, 30, 0, 0, time.UTC), "Europe/London")
	if !got.Equal(time.Date(2026, 10, 25, 0, 30, 0, 0, time.UTC)) {
		t.Fatalf("overlap: got %v, want 00:30Z", got)
	}
	// Spring-forward gap: 01:30 on 29-03-2026 doesn't exist in London; must not fail.
	if _, ok := Anchor(time.Date(2026, 3, 29, 1, 30, 0, 0, time.UTC), "Europe/London"); !ok {
		t.Fatal("gap should still anchor")
	}
	if _, ok := Anchor(floating, "Nope/Nope"); ok {
		t.Fatal("bad zone should fail")
	}
}

func TestLocalInstant(t *testing.T) {
	if _, err := LocalInstant(2026, 3, 29, 1, 30, "Europe/London"); !errors.Is(err, ErrNonexistentLocal) {
		t.Fatalf("gap: err = %v", err)
	}
	got, err := LocalInstant(2026, 10, 25, 1, 30, "Europe/London")
	if err != nil || !got.Equal(time.Date(2026, 10, 25, 0, 30, 0, 0, time.UTC)) {
		t.Fatalf("overlap: %v %v", got, err)
	}
	if _, err := LocalInstant(2026, 1, 1, 9, 0, ""); !errors.Is(err, ErrInvalidZone) {
		t.Fatalf("empty zone: %v", err)
	}
}

func TestInfer(t *testing.T) {
	cases := []struct {
		name string
		h    Hints
		tz   string
		src  Source
	}{
		{"coords", Hints{Lat: fp(48.8566), Lon: fp(2.3522)}, "Europe/Paris", Confirmed},
		{"airport", Hints{IATA: "jfk"}, "America/New_York", Confirmed},
		{"neighbour", Hints{NeighbourTZ: "Europe/London", TripTZ: "Asia/Tokyo"}, "Europe/London", Inferred},
		{"trip", Hints{TripTZ: "Asia/Tokyo"}, "Asia/Tokyo", Inferred},
		{"bad neighbour falls through", Hints{NeighbourTZ: "Nope/Nope", TripTZ: "Asia/Tokyo"}, "Asia/Tokyo", Inferred},
		{"nothing", Hints{}, "", Unknown},
	}
	for _, c := range cases {
		tz, src := Infer(c.h)
		if tz != c.tz || src != c.src {
			t.Errorf("%s: got (%q,%q), want (%q,%q)", c.name, tz, src, c.tz, c.src)
		}
	}
}
