// Package flightident canonicalises and splits flight numbers (issue #118).
//
// A flight number is written a dozen ways in the wild: "BA286", "BA 286",
// "ba286", and on a boarding pass often with an operational suffix, "BA286A".
// Everything Aerly stores and looks up wants one form, so every path that takes
// a flight number from a person, an email, or a calendar feed passes it through
// Normalise first.
//
// Splitting one back into its airline designator and number is the fiddly part,
// because an IATA designator may itself contain a digit: easyJet is "U2",
// Jet Airways "9W", Germanwings "4U". A letters-only prefix rule reads "U21234"
// as flight 21234 on airline "U", which is nobody's flight. Split therefore
// prefers the two-character designator IATA actually assigns, and only falls
// back to a three-character ICAO one (BAW, DLH) when two characters leave a
// remainder that isn't a flight number.
package flightident

import "strings"

// maxNumberDigits bounds the numeric part. IATA flight numbers run to four
// digits; anything longer is not a flight number we should be splitting.
const maxNumberDigits = 4

// Normalise canonicalises a hand-written flight number: upper-cased with all
// whitespace removed, so "ba 286", " BA286 " and "BA  286" all become "BA286".
// It does not validate — an unparseable string comes back tidied, not rejected.
func Normalise(s string) string {
	return strings.ToUpper(strings.Join(strings.Fields(s), ""))
}

// Split separates a flight number into its airline designator and the numeric
// part, dropping any leading zeros from the number and returning the trailing
// operational suffix letter separately ("BA286A" → "BA", "286", "A"). The input
// is normalised first, so a spaced-out "U2 1234" splits as readily as "U21234".
//
// ok is false when the string isn't an airline designator followed by a flight
// number: a bare number, an all-digit prefix, a number longer than four digits,
// or an all-zero number.
func Split(ident string) (airline, number, suffix string, ok bool) {
	s := Normalise(ident)
	// Two characters first (the IATA designator), then three (ICAO). Trying
	// two first is what keeps "U21234" from being read as airline "U", and the
	// three-character pass insists on letters because ICAO designators have no
	// digits: without that, "AA12345" would split as airline "AA1".
	for _, c := range []struct {
		n     int
		valid func(string) bool
	}{{2, isIATADesignator}, {3, isICAODesignator}} {
		if len(s) <= c.n {
			continue
		}
		prefix, rest := s[:c.n], s[c.n:]
		if !c.valid(prefix) {
			continue
		}
		num, suf, numOK := splitNumber(rest)
		if !numOK {
			continue
		}
		return prefix, num, suf, true
	}
	return "", "", "", false
}

// Valid reports whether s is a plausible flight number: an airline designator
// followed by a one-to-four-digit number and an optional suffix letter.
func Valid(s string) bool {
	_, _, _, ok := Split(s)
	return ok
}

// Canonical returns the ident in the form the flight-data providers store,
// which is the designator followed by a zero-padded four-digit number
// ("BA87" → "BA0087"). ok is false when the ident won't split, in which case
// the caller should stay with what it was given.
func Canonical(ident string) (string, bool) {
	airline, number, suffix, ok := Split(ident)
	if !ok {
		return "", false
	}
	return airline + strings.Repeat("0", maxNumberDigits-len(number)) + number + suffix, true
}

// isIATADesignator reports whether s is a plausible two-character IATA
// designator: alphanumeric with at least one letter. An all-digit prefix is
// rejected, since no designator is purely numeric and accepting one would let a
// bare flight number split.
func isIATADesignator(s string) bool {
	letters := 0
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z':
			letters++
		case r >= '0' && r <= '9':
		default:
			return false
		}
	}
	return letters > 0
}

// isICAODesignator reports whether s is a plausible three-letter ICAO
// designator (BAW, DLH). Letters only.
func isICAODesignator(s string) bool {
	for _, r := range s {
		if r < 'A' || r > 'Z' {
			return false
		}
	}
	return s != ""
}

// splitNumber parses the part after the designator: digits with an optional
// single trailing letter. Leading zeros are stripped before the width is
// checked, so an over-padded "BA00087" still reads as 87 whilst a genuinely
// five-digit "AC12345" is rejected as not a flight number; an all-zero number
// is rejected as meaningless.
func splitNumber(s string) (number, suffix string, ok bool) {
	if s == "" {
		return "", "", false
	}
	if last := s[len(s)-1]; last >= 'A' && last <= 'Z' {
		suffix, s = string(last), s[:len(s)-1]
	}
	if s == "" {
		return "", "", false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return "", "", false
		}
	}
	trimmed := strings.TrimLeft(s, "0")
	if trimmed == "" || len(trimmed) > maxNumberDigits {
		return "", "", false
	}
	return trimmed, suffix, true
}
