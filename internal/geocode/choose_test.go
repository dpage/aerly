package geocode

import "testing"

func TestChoose(t *testing.T) {
	tests := []struct {
		name  string
		cands []Candidate
		want  Outcome
	}{
		{"no candidates is a miss", nil, OutcomeNone},
		{"lone confident candidate is accepted",
			[]Candidate{{Confidence: 0.9}}, OutcomeAccept},
		{"lone doubtful candidate is ambiguous",
			[]Candidate{{Confidence: 0.3}}, OutcomeAmbiguous},
		{"clear leader is accepted",
			[]Candidate{{Confidence: 0.9}, {Confidence: 0.4}}, OutcomeAccept},
		{"close pair is ambiguous even when both are confident",
			[]Candidate{{Confidence: 0.9}, {Confidence: 0.85}}, OutcomeAmbiguous},
		{"confident leader over a doubtful field is accepted",
			[]Candidate{{Confidence: 0.8}, {Confidence: 0.2}, {Confidence: 0.1}}, OutcomeAccept},
		{"all doubtful is ambiguous",
			[]Candidate{{Confidence: 0.2}, {Confidence: 0.1}}, OutcomeAmbiguous},
		// These two pin the >= boundary on both thresholds against a future
		// calibration pass silently flipping it to a strict >. Choose's doc
		// says a candidate must "clear" minConf and lead "by at least"
		// margin, so a candidate sitting exactly on either line must still
		// be accepted.
		{"confidence exactly at minConf is accepted",
			[]Candidate{{Confidence: 0.5}}, OutcomeAccept},
		{"gap exactly equal to margin is accepted",
			[]Candidate{{Confidence: 0.9}, {Confidence: 0.75}}, OutcomeAccept},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Choose(tt.cands, 0.5, 0.15)
			if got.Outcome != tt.want {
				t.Errorf("got %v, want %v", got.Outcome, tt.want)
			}
		})
	}
}

func TestChooseSortsByConfidence(t *testing.T) {
	d := Choose([]Candidate{{Confidence: 0.4, Formatted: "b"}, {Confidence: 0.9, Formatted: "a"}}, 0.5, 0.15)
	if d.Outcome != OutcomeAccept || d.Best.Formatted != "a" {
		t.Fatalf("want the strongest candidate regardless of input order, got %+v", d)
	}
}

func TestChooseDoesNotMutateInput(t *testing.T) {
	in := []Candidate{{Confidence: 0.4}, {Confidence: 0.9}}
	Choose(in, 0.5, 0.15)
	if in[0].Confidence != 0.4 {
		t.Fatal("Choose must not reorder its caller's slice")
	}
}
