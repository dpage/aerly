package emailingest

import (
	"testing"
	"time"

	"github.com/dpage/aerly/internal/planops"
	"github.com/dpage/aerly/internal/store"
)

// flightProposal builds a single-leg flight proposal departing at day-offset
// days from a fixed base, flying origin→dest. Used to assemble batches for the
// looksLikeSeparateTrips connectivity check.
func flightProposal(dayOffset int, origin, dest string) planops.ProposedPlan {
	base := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	start := base.AddDate(0, 0, dayOffset)
	return planops.ProposedPlan{
		Type: "flight",
		Parts: []planops.ProposedPart{{
			Type:     "flight",
			StartsAt: start,
			Flight:   &store.FlightDetail{OriginIATA: origin, DestIATA: dest},
		}},
	}
}

func TestLooksLikeSeparateTrips(t *testing.T) {
	cases := []struct {
		name      string
		proposals []planops.ProposedPlan
		want      bool
	}{
		{
			name: "round trip stays out and back",
			proposals: []planops.ProposedPlan{
				flightProposal(0, "VIE", "SOF"),
				flightProposal(2, "SOF", "VIE"),
			},
			want: false,
		},
		{
			name: "connected multi-city open jaw",
			proposals: []planops.ProposedPlan{
				flightProposal(0, "LHR", "CDG"),
				flightProposal(4, "CDG", "FCO"),
				flightProposal(9, "FCO", "LHR"),
			},
			want: false,
		},
		{
			name: "single leg is never two trips",
			proposals: []planops.ProposedPlan{
				flightProposal(0, "LHR", "JFK"),
			},
			want: false,
		},
		{
			name: "two round trips returning home between them",
			proposals: []planops.ProposedPlan{
				flightProposal(0, "LHR", "CDG"),
				flightProposal(2, "CDG", "LHR"),
				flightProposal(20, "LHR", "JFK"),
				flightProposal(24, "JFK", "LHR"),
			},
			want: true,
		},
		{
			name: "disconnected one-way hops",
			proposals: []planops.ProposedPlan{
				flightProposal(0, "LHR", "JFK"),
				flightProposal(8, "CDG", "FCO"),
			},
			want: true,
		},
		{
			name: "order-independent: return leg listed first",
			proposals: []planops.ProposedPlan{
				flightProposal(2, "SOF", "VIE"),
				flightProposal(0, "VIE", "SOF"),
			},
			want: false,
		},
		{
			name: "non-flight bookings are ignored",
			proposals: []planops.ProposedPlan{
				{Type: "hotel", Parts: []planops.ProposedPart{{Type: "hotel", StartsAt: time.Date(2026, 10, 12, 15, 0, 0, 0, time.UTC)}}},
				{Type: "hotel", Parts: []planops.ProposedPart{{Type: "hotel", StartsAt: time.Date(2026, 10, 20, 15, 0, 0, 0, time.UTC)}}},
			},
			want: false,
		},
		{
			name: "flight legs missing IATA are skipped",
			proposals: []planops.ProposedPlan{
				flightProposal(0, "LHR", ""),
				flightProposal(8, "", "FCO"),
			},
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := looksLikeSeparateTrips(tc.proposals); got != tc.want {
				t.Errorf("looksLikeSeparateTrips = %v, want %v", got, tc.want)
			}
		})
	}
}
