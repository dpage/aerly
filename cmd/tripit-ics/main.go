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
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/dpage/aerly/internal/tripitics"
)

func main() {
	raw := flag.Bool("raw", false, "print every raw property of each event")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: tripit-ics [-raw] path/to/trip.ics")
		os.Exit(2)
	}

	f, err := os.Open(flag.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, "open:", err)
		os.Exit(1)
	}
	defer f.Close()

	cal, err := tripitics.Parse(f)
	if err != nil {
		fmt.Fprintln(os.Stderr, "parse:", err)
		os.Exit(1)
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
