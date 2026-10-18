package timing

import (
	"testing"

	"github.com/dpage/aerly/internal/airports"
)

func TestPlaceName(t *testing.T) {
	tests := []struct {
		name      string
		tz        string
		iata      string
		wantCheck func(got string) bool
		wantStr   string
	}{
		{
			name:    "Asia/Tashkent",
			tz:      "Asia/Tashkent",
			iata:    "",
			wantStr: "Tashkent",
		},
		{
			name:    "America/New_York",
			tz:      "America/New_York",
			iata:    "",
			wantStr: "New York",
		},
		{
			name:    "America/Argentina/Buenos_Aires",
			tz:      "America/Argentina/Buenos_Aires",
			iata:    "",
			wantStr: "Buenos Aires",
		},
		{
			name:    "Etc/UTC",
			tz:      "Etc/UTC",
			iata:    "",
			wantStr: "UTC",
		},
		{
			name:    "empty zone and iata",
			tz:      "",
			iata:    "",
			wantStr: "",
		},
		{
			name:   "Europe/London with LHR",
			tz:     "Europe/London",
			iata:   "LHR",
			wantStr: "", // Will check using airports.LookupCity
			wantCheck: func(got string) bool {
				city, ok := airports.LookupCity("LHR")
				return ok && city != "" && got == city
			},
		},
		{
			name:    "Europe/Paris with unknown airport ZZZ",
			tz:      "Europe/Paris",
			iata:    "ZZZ",
			wantStr: "Paris",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PlaceName(tt.tz, tt.iata)

			if tt.wantCheck != nil {
				if !tt.wantCheck(got) {
					t.Errorf("PlaceName(%q, %q) = %q, want city from airports.LookupCity", tt.tz, tt.iata, got)
				}
			} else if got != tt.wantStr {
				t.Errorf("PlaceName(%q, %q) = %q, want %q", tt.tz, tt.iata, got, tt.wantStr)
			}
		})
	}
}
