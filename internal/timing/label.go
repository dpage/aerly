package timing

import (
	"fmt"
	"time"
)

// Block is one end of an item as a label needs it. The formatters below
// produce exactly what web/src/lib/time.ts does, so a server-written label
// (an email, a push notification) reads the same as the one on screen.
type Block struct {
	At     time.Time
	TZ     string // IANA; "" for floating or unknown
	Source Source
	Place  string // from PlaceName; may be ""
	Kind   Kind   // Derived adds the ≈ prefix
}

const (
	dateLayout  = "Mon 2 Jan"
	clockLayout = "15:04"
)

// loadZone returns the named zone, or UTC when it is blank or invalid, the
// same fallback as the web's safeZone.
func loadZone(tz string) *time.Location {
	if !ValidZone(tz) {
		return time.UTC
	}
	loc, _ := time.LoadLocation(tz)
	return loc
}

// OffsetLabel is the UTC offset of tz at an instant: "UTC" at zero, else
// "UTC+5", "UTC+5:30" or "UTC-3:30".
func OffsetLabel(at time.Time, tz string) string {
	return offsetIn(at, loadZone(tz))
}

func offsetIn(at time.Time, loc *time.Location) string {
	_, secs := at.In(loc).Zone()
	if secs == 0 {
		return "UTC"
	}
	sign := '+'
	if secs < 0 {
		sign, secs = '-', -secs
	}
	h, m := secs/3600, secs%3600/60
	if m == 0 {
		return fmt.Sprintf("UTC%c%d", sign, h)
	}
	return fmt.Sprintf("UTC%c%d:%02d", sign, h, m)
}

// view is how a block is shown: the zone its clock runs in, and whether an
// offset label goes with it. A floating block shows its UTC digits as
// wall-clock time with no label; an unknown one is a true instant, shown in
// the viewer's home zone (UTC without one) and always with an offset.
func (b Block) view(home string) (loc *time.Location, labelled bool, place string) {
	switch {
	case b.TZ != "":
		return loadZone(b.TZ), true, b.Place
	case b.Source == Unknown:
		return loadZone(home), true, ""
	}
	return time.UTC, false, ""
}

// clock is the time of day, with ≈ in front of a derived time.
func (b Block) clock(loc *time.Location) string {
	prefix := ""
	if b.Kind == Derived {
		prefix = "≈"
	}
	return prefix + b.At.In(loc).Format(clockLayout)
}

// Full is the form for detail views: "Sun 18 Oct, 14:00 Tashkent (UTC+5)",
// the offset alone when there is no place, and no label with no zone. home is
// used only for an unknown block.
func Full(b Block, home string) string {
	if b.At.IsZero() {
		return ""
	}
	loc, labelled, place := b.view(home)
	head := b.At.In(loc).Format(dateLayout) + ", " + b.clock(loc)
	if !labelled {
		return head
	}
	off := offsetIn(b.At, loc)
	if place != "" {
		return fmt.Sprintf("%s %s (%s)", head, place, off)
	}
	return head + " " + off
}

// Compact is the form for tiles: "14:00 UTC+5", or "14:00" with no zone.
func Compact(b Block, home string) string {
	if b.At.IsZero() {
		return ""
	}
	loc, labelled, _ := b.view(home)
	if !labelled {
		return b.clock(loc)
	}
	return b.clock(loc) + " " + offsetIn(b.At, loc)
}

// CompactPlace is Compact with the place named: "18:50 Istanbul (UTC+3)". With
// no place it equals Compact.
func CompactPlace(b Block, home string) string {
	if b.At.IsZero() {
		return ""
	}
	loc, labelled, place := b.view(home)
	if !labelled || place == "" {
		return Compact(b, home)
	}
	return fmt.Sprintf("%s %s (%s)", b.clock(loc), place, offsetIn(b.At, loc))
}

// YourTime is the block's moment in the viewer's home zone, "(15:50 your
// time)", with the date added when it differs from the block's own. It is
// empty when it would add nothing: no valid home zone, no zone on the block
// to compare against, or the same offset at that instant (offsets are
// compared, not names, like the web's fmtViewer).
func YourTime(b Block, home string) string {
	if b.At.IsZero() || b.TZ == "" || !ValidZone(home) {
		return ""
	}
	own, mine := loadZone(b.TZ), loadZone(home)
	if offsetIn(b.At, own) == offsetIn(b.At, mine) {
		return ""
	}
	t := b.At.In(mine)
	if t.Format(dateLayout) == b.At.In(own).Format(dateLayout) {
		return fmt.Sprintf("(%s your time)", t.Format(clockLayout))
	}
	return fmt.Sprintf("(%s, %s your time)", t.Format(dateLayout), t.Format(clockLayout))
}
