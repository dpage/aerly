package flightident

import "testing"

func TestNormalise(t *testing.T) {
	cases := map[string]string{
		"BA286":    "BA286",
		"ba 286":   "BA286",
		" BA 286 ": "BA286",
		"BA  286":  "BA286",
		"lh\t441":  "LH441",
		"":         "",
	}
	for in, want := range cases {
		if got := Normalise(in); got != want {
			t.Errorf("Normalise(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSplit(t *testing.T) {
	cases := []struct {
		in                      string
		airline, number, suffix string
		ok                      bool
	}{
		// The ordinary two-letter designator, spaced or not.
		{"BA286", "BA", "286", "", true},
		{"BA 286", "BA", "286", "", true},
		{"ba286", "BA", "286", "", true},

		// Designators carrying a digit: the whole point of issue #118. A
		// letters-first rule would read these as airline "U" flight 21234.
		{"U21234", "U2", "1234", "", true},
		{"U2 1234", "U2", "1234", "", true},
		{"U287", "U2", "87", "", true},
		{"9W420", "9W", "420", "", true},
		{"4U2678", "4U", "2678", "", true},

		// Leading zeros are cosmetic; the operational suffix is not.
		{"BA0087", "BA", "87", "", true},
		{"BA00087", "BA", "87", "", true},
		{"BA286A", "BA", "286", "A", true},

		// Three-letter ICAO designators, only when two characters won't do.
		{"BAW123", "BAW", "123", "", true},
		{"DLH1", "DLH", "1", "", true},

		// Not flight numbers.
		{"", "", "", "", false},
		{"BA", "", "", "", false},
		{"12345", "", "", "", false},
		{"1234", "", "", "", false},
		{"BA0000", "", "", "", false},
		{"AC12345", "", "", "", false},
		{"GIBBERISH", "", "", "", false},
		{"BA-286", "", "", "", false},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			airline, number, suffix, ok := Split(c.in)
			if ok != c.ok || airline != c.airline || number != c.number || suffix != c.suffix {
				t.Errorf("Split(%q) = (%q, %q, %q, %v), want (%q, %q, %q, %v)",
					c.in, airline, number, suffix, ok, c.airline, c.number, c.suffix, c.ok)
			}
		})
	}
}

func TestValid(t *testing.T) {
	if !Valid("lh 441") {
		t.Error("a spaced, lower-case flight number should be valid")
	}
	if Valid("nonsense") {
		t.Error("a word is not a flight number")
	}
}

func TestCanonical(t *testing.T) {
	cases := []struct {
		in, want string
		ok       bool
	}{
		{"BA87", "BA0087", true},
		{"BA 87", "BA0087", true},
		{"BA0087", "BA0087", true},
		{"U287", "U20087", true}, // not "U0287": the designator owns that digit
		{"9W420", "9W0420", true},
		{"BA286A", "BA0286A", true},
		{"GIBBERISH", "", false},
	}
	for _, c := range cases {
		got, ok := Canonical(c.in)
		if ok != c.ok || got != c.want {
			t.Errorf("Canonical(%q) = (%q, %v), want (%q, %v)", c.in, got, ok, c.want, c.ok)
		}
	}
}
