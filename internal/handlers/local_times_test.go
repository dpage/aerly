package handlers

import (
	"errors"
	"testing"
	"time"
)

func TestLocalToInstant(t *testing.T) {
	at, err := localToInstant("2026-10-18T14:00", "Asia/Tashkent")
	if err != nil || !at.Equal(time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("Tashkent = %v, %v", at, err)
	}
	at, err = localToInstant("2026-10-18T14:00", "")
	if err != nil || !at.Equal(time.Date(2026, 10, 18, 14, 0, 0, 0, time.UTC)) {
		t.Fatalf("floating = %v, %v", at, err)
	}
	// 01:30 on 25 Oct 2026 happens twice in London; the earlier is BST.
	at, err = localToInstant("2026-10-25T01:30", "Europe/London")
	if err != nil || !at.Equal(time.Date(2026, 10, 25, 0, 30, 0, 0, time.UTC)) {
		t.Fatalf("overlap = %v, %v", at, err)
	}
}

func TestLocalToInstantErrors(t *testing.T) {
	cases := map[string]struct{ local, tz, want string }{
		"gap":     {"2026-03-29T01:30", "Europe/London", "That time doesn't exist in Europe/London (clocks go forward)."},
		"zone":    {"2026-10-18T14:00", "Mars/Olympus", "Invalid time zone."},
		"garbled": {"18/10/2026 14:00", "", "Times must look like 2026-10-18T14:00."},
	}
	for name, c := range cases {
		_, err := localToInstant(c.local, c.tz)
		var le localTimeError
		if !errors.As(err, &le) || le.Error() != c.want {
			t.Errorf("%s: err = %v, want %q", name, err, c.want)
		}
	}
}

func TestApplyLocalTimes(t *testing.T) {
	p := planPartReq{StartLocal: "2026-10-18T14:00", StartTZ: "Asia/Tashkent", EndLocal: "2026-10-18T18:00"}
	if err := applyLocalTimes(&p); err != nil {
		t.Fatal(err)
	}
	if !p.StartsAt.Equal(time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC)) || p.StartTZSource != "" {
		t.Errorf("start = %v %q", p.StartsAt, p.StartTZSource)
	}
	if p.EndsAt == nil || !p.EndsAt.Equal(time.Date(2026, 10, 18, 18, 0, 0, 0, time.UTC)) || p.EndTZSource != "floating" {
		t.Errorf("end = %v %q", p.EndsAt, p.EndTZSource)
	}

	floating := planPartReq{StartLocal: "2026-10-18T14:00", StartTZSource: "confirmed"}
	if err := applyLocalTimes(&floating); err != nil || floating.StartTZSource != "floating" {
		t.Errorf("floating start source = %q, %v", floating.StartTZSource, err)
	}

	both := planPartReq{StartLocal: "2026-10-18T14:00", StartsAt: time.Now()}
	if err := applyLocalTimes(&both); err == nil || err.Error() != "Send starts_at or start_local, not both." {
		t.Errorf("both starts err = %v", err)
	}
	end := time.Now()
	bothEnd := planPartReq{EndLocal: "2026-10-18T14:00", EndsAt: &end}
	if err := applyLocalTimes(&bothEnd); err == nil || err.Error() != "Send ends_at or end_local, not both." {
		t.Errorf("both ends err = %v", err)
	}
	badStart := planPartReq{StartLocal: "2026-03-29T01:30", StartTZ: "Europe/London"}
	if err := applyLocalTimes(&badStart); err == nil {
		t.Error("gap start accepted")
	}
	badEnd := planPartReq{EndLocal: "2026-03-29T01:30", EndTZ: "Europe/London"}
	if err := applyLocalTimes(&badEnd); err == nil {
		t.Error("gap end accepted")
	}
}

func TestResolveLocalEdit(t *testing.T) {
	sl := "2026-10-18T15:00"
	in := updatePlanPartReq{StartLocal: &sl}
	if err := resolveLocalEdit(&in, "Asia/Tashkent", ""); err != nil {
		t.Fatal(err)
	}
	if in.StartsAt == nil || !in.StartsAt.Equal(time.Date(2026, 10, 18, 10, 0, 0, 0, time.UTC)) || in.StartTZSource != nil {
		t.Errorf("current-zone start = %v %v", in.StartsAt, in.StartTZSource)
	}

	blank := ""
	el := "2026-10-18T19:00"
	in = updatePlanPartReq{EndLocal: &el, EndTZ: &blank}
	if err := resolveLocalEdit(&in, "Asia/Tashkent", "Asia/Tashkent"); err != nil {
		t.Fatal(err)
	}
	if in.EndsAt == nil || !in.EndsAt.Equal(time.Date(2026, 10, 18, 19, 0, 0, 0, time.UTC)) || in.EndTZSource == nil || *in.EndTZSource != "floating" {
		t.Errorf("blank-zone end = %v %v", in.EndsAt, in.EndTZSource)
	}

	london := "Europe/London"
	gap := "2026-03-29T01:30"
	in = updatePlanPartReq{StartLocal: &gap, StartTZ: &london}
	if err := resolveLocalEdit(&in, "", ""); err == nil {
		t.Error("gap start accepted")
	}
	in = updatePlanPartReq{EndLocal: &gap}
	if err := resolveLocalEdit(&in, "", london); err == nil {
		t.Error("gap end accepted")
	}
	now := time.Now()
	in = updatePlanPartReq{StartLocal: &sl, StartsAt: &now}
	if err := resolveLocalEdit(&in, "", ""); err == nil || err.Error() != "Send starts_at or start_local, not both." {
		t.Errorf("both starts err = %v", err)
	}
	in = updatePlanPartReq{EndLocal: &el, EndsAt: &now}
	if err := resolveLocalEdit(&in, "", ""); err == nil || err.Error() != "Send ends_at or end_local, not both." {
		t.Errorf("both ends err = %v", err)
	}
}
