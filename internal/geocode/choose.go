package geocode

import "slices"

// Outcome is what Choose concluded about a candidate list.
type Outcome int

const (
	// OutcomeNone means nothing matched. The caller must not plot a pin.
	OutcomeNone Outcome = iota
	// OutcomeAccept means one candidate is clearly right.
	OutcomeAccept
	// OutcomeAmbiguous means there are candidates but none is clearly right.
	// The caller may re-rank; if it cannot, it must not plot a pin.
	OutcomeAmbiguous
)

func (o Outcome) String() string {
	switch o {
	case OutcomeAccept:
		return "accept"
	case OutcomeAmbiguous:
		return "ambiguous"
	default:
		return "none"
	}
}

// Decision is Choose's verdict. Best is meaningful only for OutcomeAccept;
// Ranked is the confidence-sorted list, used by the re-ranker and by the
// confirmation UI.
type Decision struct {
	Outcome Outcome
	Best    Candidate
	Ranked  []Candidate
}

// Choose applies the confidence policy to a candidate list. A candidate is
// accepted only when it clears minConf AND leads the runner-up by at least
// margin: a strong-looking result with an equally strong rival is exactly the
// case where a blind first-hit plots the wrong branch of a chain hotel.
//
// Anything else is ambiguous, and ambiguity resolves to no pin unless a
// re-ranker settles it. A missing pin is acceptable; a wrong one is not.
func Choose(cands []Candidate, minConf, margin float64) Decision {
	if len(cands) == 0 {
		return Decision{Outcome: OutcomeNone}
	}
	ranked := slices.Clone(cands)
	slices.SortStableFunc(ranked, func(a, b Candidate) int {
		switch {
		case a.Confidence > b.Confidence:
			return -1
		case a.Confidence < b.Confidence:
			return 1
		default:
			return 0
		}
	})
	d := Decision{Outcome: OutcomeAmbiguous, Ranked: ranked}
	top := ranked[0]
	if top.Confidence < minConf {
		return d
	}
	if len(ranked) > 1 && top.Confidence-ranked[1].Confidence < margin {
		return d
	}
	d.Outcome, d.Best = OutcomeAccept, top
	return d
}
