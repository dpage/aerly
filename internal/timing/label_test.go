package timing

import (
	"testing"
	"time"
)

func at(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestOffsetLabel(t *testing.T) {
	cases := []struct {
		name, at, tz, want string
	}{
		{"utc", "2026-10-18T09:00:00Z", "UTC", "UTC"},
		{"london winter", "2026-01-18T09:00:00Z", "Europe/London", "UTC"},
		{"london dst before", "2026-03-29T00:30:00Z", "Europe/London", "UTC"},
		{"london dst after", "2026-03-29T01:30:00Z", "Europe/London", "UTC+1"},
		{"tashkent", "2026-10-18T09:00:00Z", "Asia/Tashkent", "UTC+5"},
		{"kolkata", "2026-10-18T09:00:00Z", "Asia/Kolkata", "UTC+5:30"},
		{"st johns summer", "2026-07-18T09:00:00Z", "America/St_Johns", "UTC-2:30"},
		{"st johns winter", "2026-01-18T09:00:00Z", "America/St_Johns", "UTC-3:30"},
		{"honolulu", "2026-10-18T09:00:00Z", "Pacific/Honolulu", "UTC-10"},
		{"invalid", "2026-10-18T09:00:00Z", "Mars/Olympus", "UTC"},
		{"blank", "2026-10-18T09:00:00Z", "", "UTC"},
		{"local refused", "2026-10-18T09:00:00Z", "Local", "UTC"},
	}
	for _, c := range cases {
		if got := OffsetLabel(at(c.at), c.tz); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

func TestLabels(t *testing.T) {
	tash := Block{At: at("2026-10-18T09:00:00Z"), TZ: "Asia/Tashkent", Source: Confirmed, Place: "Tashkent", Kind: Scheduled}
	noPlace := Block{At: at("2026-10-18T09:00:00Z"), TZ: "Asia/Kolkata", Source: Inferred}
	floating := Block{At: at("2026-10-18T14:00:00Z"), Source: Floating}
	unknown := Block{At: at("2026-10-18T09:00:00Z"), Source: Unknown, Place: "Ignored"}
	derived := tash
	derived.Kind = Derived
	bad := Block{At: at("2026-10-18T09:00:00Z"), TZ: "Mars/Olympus", Source: Confirmed}
	ist := Block{At: at("2026-10-18T15:50:00Z"), TZ: "Europe/Istanbul", Source: Confirmed, Place: "Istanbul"}

	cases := []struct {
		name string
		fn   func(Block, string) string
		b    Block
		home string
		want string
	}{
		{"full place", Full, tash, "", "Sun 18 Oct, 14:00 Tashkent (UTC+5)"},
		{"full no place", Full, noPlace, "", "Sun 18 Oct, 14:30 UTC+5:30"},
		{"full floating", Full, floating, "Europe/London", "Sun 18 Oct, 14:00"},
		{"full derived", Full, derived, "", "Sun 18 Oct, ≈14:00 Tashkent (UTC+5) (based on departure delay)"},
		{"full derived floating", Full, Block{At: at("2026-10-18T14:00:00Z"), Source: Floating, Kind: Derived}, "", "Sun 18 Oct, ≈14:00 (based on departure delay)"},
		{"full derived unknown", Full, Block{At: at("2026-10-18T09:00:00Z"), Source: Unknown, Kind: Derived}, "", "Sun 18 Oct, ≈09:00 UTC (based on departure delay)"},
		{"full unknown home", Full, unknown, "Europe/London", "Sun 18 Oct, 10:00 UTC+1"},
		{"full unknown no home", Full, unknown, "", "Sun 18 Oct, 09:00 UTC"},
		{"full unknown bad home", Full, unknown, "Mars/Olympus", "Sun 18 Oct, 09:00 UTC"},
		{"full blank source", Full, Block{At: at("2026-10-18T09:00:00Z")}, "Europe/London", "Sun 18 Oct, 10:00 UTC+1"},
		{"full blank confirmed", Full, Block{At: at("2026-10-18T09:00:00Z"), Source: Confirmed, Place: "X"}, "", "Sun 18 Oct, 09:00 UTC"},
		{"compact blank inferred", Compact, Block{At: at("2026-10-18T09:00:00Z"), Source: Inferred}, "Europe/London", "10:00 UTC+1"},
		{"compact place blank source", CompactPlace, Block{At: at("2026-10-18T09:00:00Z"), Place: "X"}, "Europe/London", "10:00 UTC+1"},
		{"your time blank source", YourTime, Block{At: at("2026-10-18T09:00:00Z")}, "Europe/London", ""},
		{"full invalid tz", Full, bad, "", "Sun 18 Oct, 09:00 UTC"},
		{"full zero time", Full, Block{}, "", ""},
		{"compact", Compact, tash, "", "14:00 UTC+5"},
		{"compact floating", Compact, floating, "", "14:00"},
		{"compact derived", Compact, derived, "", "≈14:00 UTC+5"},
		{"compact place derived", CompactPlace, derived, "", "≈14:00 Tashkent (UTC+5)"},
		{"compact unknown", Compact, unknown, "Europe/London", "10:00 UTC+1"},
		{"compact zero time", Compact, Block{}, "", ""},
		{"compact place", CompactPlace, ist, "", "18:50 Istanbul (UTC+3)"},
		{"compact place none", CompactPlace, noPlace, "", "14:30 UTC+5:30"},
		{"compact place floating", CompactPlace, floating, "", "14:00"},
		{"compact place zero", CompactPlace, Block{}, "", ""},
		{"your time same day", YourTime, ist, "Europe/London", "(16:50 your time)"},
		{"your time other day", YourTime, Block{At: at("2026-10-18T22:50:00Z"), TZ: "Asia/Tashkent", Source: Confirmed}, "America/New_York", "(Sun 18 Oct, 18:50 your time)"},
		{"your time earlier day", YourTime, Block{At: at("2026-10-18T01:00:00Z"), TZ: "Asia/Tashkent", Source: Confirmed}, "America/New_York", "(Sat 17 Oct, 21:00 your time)"},
		{"your time equal offset", YourTime, Block{At: at("2026-10-18T09:00:00Z"), TZ: "Europe/Paris", Source: Confirmed}, "Europe/Berlin", ""},
		{"your time no home", YourTime, tash, "", ""},
		{"your time bad home", YourTime, tash, "Mars/Olympus", ""},
		{"your time floating", YourTime, floating, "Europe/London", ""},
		{"your time unknown", YourTime, unknown, "Europe/London", ""},
		{"your time zero", YourTime, Block{}, "Europe/London", ""},
		{"your time derived no approx", YourTime, derived, "Europe/London", "(10:00 your time)"},
	}
	for _, c := range cases {
		if got := c.fn(c.b, c.home); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

func TestDay(t *testing.T) {
	// 22:30 UTC on 18 Oct is already the 19th in Tokyo, and still the 18th in
	// London (BST until 25 Oct) and in UTC.
	late := time.Date(2026, 10, 18, 22, 30, 0, 0, time.UTC)
	// 00:30 UTC on 19 Oct is the 19th in London (01:30 BST) and in Tokyo.
	early := time.Date(2026, 10, 19, 0, 30, 0, 0, time.UTC)
	day := func(d int) time.Time { return time.Date(2026, 10, d, 0, 0, 0, 0, time.UTC) }
	cases := []struct {
		name string
		b    Block
		home string
		want time.Time
	}{
		{"zone Tokyo", Block{At: late, TZ: "Asia/Tokyo", Source: Confirmed}, "", day(19)},
		{"zone London", Block{At: late, TZ: "Europe/London", Source: Confirmed}, "", day(18)},
		{"zone London past local midnight", Block{At: time.Date(2026, 10, 18, 23, 30, 0, 0, time.UTC), TZ: "Europe/London", Source: Confirmed}, "", day(19)},
		{"zone London after midnight", Block{At: early, TZ: "Europe/London", Source: Confirmed}, "", day(19)},
		{"zone London summer boundary", Block{At: time.Date(2026, 7, 1, 23, 30, 0, 0, time.UTC), TZ: "Europe/London", Source: Confirmed}, "", time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)},
		{"zone ignores home", Block{At: late, TZ: "Europe/London", Source: Confirmed}, "Asia/Tokyo", day(18)},
		{"floating uses UTC digits", Block{At: late, Source: Floating}, "Asia/Tokyo", day(18)},
		{"unknown uses home", Block{At: late, Source: Unknown}, "Asia/Tokyo", day(19)},
		{"unknown blank home", Block{At: late, Source: Unknown}, "", day(18)},
		{"unknown invalid home", Block{At: late, Source: Unknown}, "Mars/Olympus", day(18)},
		{"invalid zone reads as UTC", Block{At: late, TZ: "Mars/Olympus", Source: Confirmed}, "", day(18)},
	}
	for _, c := range cases {
		if got := Day(c.b, c.home); !got.Equal(c.want) || got.Location() != time.UTC {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}
