package geocode

import "testing"

func TestCandidateBetterThan(t *testing.T) {
	a := Candidate{Confidence: 0.9}
	b := Candidate{Confidence: 0.4}
	if !a.BetterThan(b) {
		t.Fatal("0.9 should beat 0.4")
	}
	if b.BetterThan(a) {
		t.Fatal("0.4 must not beat 0.9")
	}
}
