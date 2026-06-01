// Command tripit-ics is a POC inspector for TripIt .ics exports.
//
// TripIt's public API is closed to new integrations, so the import path is the
// per-trip "Export trip to calendar" .ics download. This tool parses such a
// file and prints what it found, so we can see exactly how TripIt encodes a
// flight/hotel/etc. inside the iCalendar events before writing the mapper that
// turns them into Aerly plans.
//
// Usage:
//
//	go run ./cmd/tripit-ics path/to/trip.ics
//	go run ./cmd/tripit-ics -raw path/to/trip.ics   # dump every raw property
//	go run ./cmd/tripit-ics -map path/to/trip.ics   # dry-run the mapped trip/plans
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/dpage/aerly/internal/tripitics"
)

func main() {
	raw := flag.Bool("raw", false, "print every raw property of each event")
	mapMode := flag.Bool("map", false, "show the Aerly trip/plans the .ics would import to (dry run)")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: tripit-ics [-raw] [-map] path/to/trip.ics")
		os.Exit(2)
	}

	f, err := os.Open(flag.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, "open:", err)
		os.Exit(1)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			fmt.Fprintln(os.Stderr, "close:", cerr)
		}
	}()

	cal, err := tripitics.Parse(f)
	if err != nil {
		fmt.Fprintln(os.Stderr, "parse:", err)
		os.Exit(1)
	}

	if *mapMode {
		printMapped(tripitics.MapCalendar(cal))
		return
	}

	if cal.ProdID != "" {
		fmt.Printf("PRODID: %s\n", cal.ProdID)
	}
	fmt.Printf("%d event(s)\n\n", len(cal.Events))
	for i, ev := range cal.Events {
		fmt.Printf("── event %d ──\n", i+1)
		fmt.Printf("  SUMMARY:  %s\n", ev.Summary)
		fmt.Printf("  START:    %s\n", fmtDateTime(ev.Start))
		fmt.Printf("  END:      %s\n", fmtDateTime(ev.End))
		if ev.Location != "" {
			fmt.Printf("  LOCATION: %s\n", ev.Location)
		}
		if ev.Description != "" {
			fmt.Printf("  DESCRIPTION:\n%s\n", indent(ev.Description, "    "))
		}
		if *raw {
			fmt.Printf("  RAW:\n")
			for _, p := range ev.Props {
				fmt.Printf("    %s%s = %q\n", p.Name, fmtParams(p.Params), p.Value)
			}
		}
		fmt.Println()
	}
}

// printMapped renders the dry-run import: the trip and the plans (with their
// single part's key fields) that MapCalendar produced.
func printMapped(mt *tripitics.MappedTrip) {
	span := "(no dates)"
	if mt.StartsOn != nil && mt.EndsOn != nil {
		span = fmt.Sprintf("%s → %s", mt.StartsOn.Format("2006-01-02"), mt.EndsOn.Format("2006-01-02"))
	}
	fmt.Printf("TRIP: %s   %s\n", mt.Name, span)
	fmt.Printf("%d plan(s):\n\n", len(mt.Plans))
	for _, p := range mt.Plans {
		fmt.Printf("  [%s] %s\n", p.Type, p.Title)
		for _, part := range p.Parts {
			when := part.StartsAt.Format("2006-01-02 15:04 MST")
			if part.StartTZ != "" {
				when += " (" + part.StartTZ + ")"
			}
			fmt.Printf("      start: %s\n", when)
			if part.EndsAt != nil {
				end := part.EndsAt.Format("2006-01-02 15:04 MST")
				if part.EndTZ != "" {
					end += " (" + part.EndTZ + ")"
				}
				fmt.Printf("      end:   %s\n", end)
			}
			switch {
			case part.Flight != nil:
				fmt.Printf("      flight %s %s→%s, status=%s\n",
					part.Flight.Ident, part.Flight.OriginIATA, part.Flight.DestIATA, part.Flight.FlightStatus)
			case part.Hotel != nil:
				fmt.Printf("      hotel %q  %s\n", part.Hotel.PropertyName, part.Hotel.Phone)
			case part.Train != nil:
				fmt.Printf("      train %q  %s→%s\n", part.Train.Operator, part.StartLabel, part.EndLabel)
			case part.Ground != nil:
				fmt.Printf("      ground %q  %s→%s\n", part.Ground.Provider, part.StartLabel, part.EndLabel)
			}
		}
		fmt.Println()
	}
}

func fmtDateTime(dt tripitics.DateTime) string {
	if dt.Raw == "" {
		return "(none)"
	}
	switch {
	case !dt.HasTime:
		return fmt.Sprintf("%s (date only)", dt.Raw)
	case dt.IsUTC:
		return fmt.Sprintf("%s (UTC)", dt.Raw)
	case dt.TZID != "":
		return fmt.Sprintf("%s [%s]", dt.Raw, dt.TZID)
	case dt.Floating:
		return fmt.Sprintf("%s (floating)", dt.Raw)
	default:
		return dt.Raw
	}
}

func fmtParams(params map[string]string) string {
	if len(params) == 0 {
		return ""
	}
	out := ""
	for k, v := range params {
		out += fmt.Sprintf(";%s=%s", k, v)
	}
	return out
}

func indent(s, pad string) string {
	out := ""
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == '\n' {
			out += pad + s[start:i] + "\n"
			start = i + 1
		}
	}
	return out
}
