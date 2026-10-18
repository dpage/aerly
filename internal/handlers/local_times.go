package handlers

import (
	"errors"
	"fmt"
	"time"

	"github.com/dpage/aerly/internal/timing"
)

// localLayout is the wall-clock form the web sends for a typed time.
const localLayout = "2006-01-02T15:04"

// localTimeError is a conversion failure worded for the person who typed the
// time; handlers return its text as a 400.
type localTimeError struct{ msg string }

func (e localTimeError) Error() string { return e.msg }

// localToInstant converts wall-clock digits at tz to an instant. A blank zone
// keeps the digits as UTC, the floating convention. A time skipped by a
// spring-forward change is refused; a repeated one takes the earlier instance.
func localToInstant(local, tz string) (time.Time, error) {
	d, err := time.Parse(localLayout, local)
	if err != nil {
		return time.Time{}, localTimeError{"Times must look like 2026-10-18T14:00."}
	}
	if tz == "" {
		return d, nil
	}
	at, err := timing.LocalInstant(d.Year(), d.Month(), d.Day(), d.Hour(), d.Minute(), tz)
	switch {
	case errors.Is(err, timing.ErrNonexistentLocal):
		return time.Time{}, localTimeError{fmt.Sprintf("That time doesn't exist in %s (clocks go forward).", tz)}
	case err != nil:
		return time.Time{}, localTimeError{"Invalid time zone."}
	}
	return at, nil
}

// applyLocalTimes turns a create part's start_local/end_local into
// StartsAt/EndsAt. A time with no zone is stored floating; a time with a zone
// keeps the source the client sent, or falls to ResolveSource's confirmed.
func applyLocalTimes(p *planPartReq) error {
	if p.StartLocal != "" {
		if !p.StartsAt.IsZero() {
			return localTimeError{"Send starts_at or start_local, not both."}
		}
		at, err := localToInstant(p.StartLocal, p.StartTZ)
		if err != nil {
			return err
		}
		p.StartsAt = at
		if p.StartTZ == "" {
			p.StartTZSource = string(timing.Floating)
		}
	}
	if p.EndLocal != "" {
		if p.EndsAt != nil {
			return localTimeError{"Send ends_at or end_local, not both."}
		}
		at, err := localToInstant(p.EndLocal, p.EndTZ)
		if err != nil {
			return err
		}
		p.EndsAt = &at
		if p.EndTZ == "" {
			p.EndTZSource = string(timing.Floating)
		}
	}
	return nil
}

// resolveLocalEdit does the same for a PATCH. The zone is the one sent with
// the time, else the part's current zone; a blank zone sets the source to
// floating explicitly, since the stored one may name a zone.
func resolveLocalEdit(in *updatePlanPartReq, curStartTZ, curEndTZ string) error {
	floating := string(timing.Floating)
	if in.StartLocal != nil {
		if in.StartsAt != nil {
			return localTimeError{"Send starts_at or start_local, not both."}
		}
		tz := curStartTZ
		if in.StartTZ != nil {
			tz = *in.StartTZ
		}
		at, err := localToInstant(*in.StartLocal, tz)
		if err != nil {
			return err
		}
		in.StartsAt = &at
		if tz == "" {
			in.StartTZSource = &floating
		}
	}
	if in.EndLocal != nil {
		if in.EndsAt != nil {
			return localTimeError{"Send ends_at or end_local, not both."}
		}
		tz := curEndTZ
		if in.EndTZ != nil {
			tz = *in.EndTZ
		}
		at, err := localToInstant(*in.EndLocal, tz)
		if err != nil {
			return err
		}
		in.EndsAt = &at
		if tz == "" {
			in.EndTZSource = &floating
		}
	}
	return nil
}
