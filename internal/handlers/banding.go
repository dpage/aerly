package handlers

import (
	"strings"
	"time"
)

// Banding is the rule that turns one ranged booking into two itinerary entries:
// an entry on the day it opens and another on the day it closes, rather than a
// single banner covering every day in between. A three-night hotel therefore
// reads as a check-in on the Monday and a check-out on the Thursday, and a
// multi-day car hire as a pickup and a return, so the closing time gets a day,
// a place and a reminder of its own instead of hiding inside a span line on the
// opening day (issue #101).
//
// The rule is applied in three renderers that must agree: the subscribable iCal
// feed (ics.go), the printed itinerary (pdf.go), and the web timeline
// (web/src/lib/trip-format.ts). The two Go renderers share the definitions
// below so a new banded type only has to be added once.
//
// Banding is opt-in by plan type, and deliberately so. A type absent from
// bandedTypes never bands, however long it runs: generalising the rule to "any
// booking that ends on a later day" would split every red-eye flight and every
// overnight sleeper into a departure entry and an arrival entry, which is not
// how a journey should read; a journey is one continuous thing that happens to
// cross midnight, whilst a stay or a hire is a pair of appointments with a gap
// in between.

// bandEdgeLabels are the first/last entry labels for a banded type.
type bandEdgeLabels struct{ First, Last string }

// bandedTypes maps a plan type to its edge labels. A type absent from the map
// never bands.
var bandedTypes = map[string]bandEdgeLabels{
	"hotel":        {"Check-in", "Check-out"},
	"vehicle_hire": {"Pickup", "Return"},
}

// bandLabelsFor returns a plan type's edge labels, and whether the type bands
// at all.
func bandLabelsFor(planType string) (bandEdgeLabels, bool) {
	l, ok := bandedTypes[planType]
	return l, ok
}

// bandEdgeLabel returns the label for one edge of a banded type: the closing
// label when last is true, the opening one otherwise. An unbanded type has no
// edges and yields "".
func bandEdgeLabel(planType string, last bool) string {
	labels, ok := bandLabelsFor(planType)
	switch {
	case !ok:
		return ""
	case last:
		return labels.Last
	default:
		return labels.First
	}
}

// bandUIDSuffix returns the iCal UID suffix for one edge of a banded type, or
// "" for a type that does not band.
//
// Hotel keeps its historic "checkin"/"checkout" suffixes rather than a form
// derived from its labels, and that is not cosmetic: the feed is subscribable,
// so calendar clients in the wild already hold hotel events stored under
// plan-part-<id>-checkin@aerly and plan-part-<id>-checkout@aerly, and they
// match events on the UID. Emitting a different UID for the same booking would
// leave every subscriber with a duplicate event beside an orphaned original,
// and there is no way to withdraw that once it has synced. Types added since
// take a suffix derived from their own label, which is only safe because
// nothing has subscribed to them yet.
func bandUIDSuffix(planType string, last bool) string {
	label := bandEdgeLabel(planType, last)
	if label == "" {
		return ""
	}
	if planType == "hotel" {
		if last {
			return "checkout"
		}
		return "checkin"
	}
	return strings.ToLower(strings.ReplaceAll(label, " ", ""))
}

// bandsToLaterDay reports whether a booking closes on a later local calendar day
// than it opens, which is the condition (alongside a banding type) that makes it
// render as two entries. Each instant is resolved in its own zone, since a
// booking can close somewhere other than where it opened: a car collected in
// Geneva and dropped in Lyon is still one hire.
//
// This date-only comparison replaced the itinerary's older
// "end.After(start) && !sameDay(start, end)" guard when the two renderers were
// unified. The two predicates are not equivalent in general: they are
// identical whenever the effective end zone equals the start zone, which
// covers every hotel (hence the golden tests staying byte-for-byte), and they
// can differ only when the two ends sit in different zones whose offset gap
// exceeds the booking's elapsed time, reversing the ordering of their local
// dates.
func bandsToLaterDay(startsAt time.Time, startTZ string, endsAt time.Time, endTZ string) bool {
	return localDate(endsAt, endTZ).After(localDate(startsAt, startTZ))
}
