// Command gen-airports writes internal/airports/table_gen.go, the table of
// every airport with an IATA code: its coordinates, name, IANA zone and the
// city it serves.
//
// Two sources are read from local files, so that neither the build nor the
// tests touch the network:
//
//   - OurAirports' airports.csv (public domain) is the authority for which
//     airport a code names, and for its name and coordinates. It has no zone
//     column, so each zone is looked up from the coordinates with the same
//     boundary data internal/geotz uses at run time.
//   - mwgg/Airports' airports.json (MIT licence) supplies the city an airport
//     serves ("Milan" for MXP, where OurAirports' municipality is "Ferno"). It
//     is used only when it describes the same airport, within cityMatchKm,
//     because some of its codes name a different airport; otherwise the
//     municipality is kept.
//
// To regenerate:
//
//	curl -s -o /tmp/airports.csv https://davidmegginson.github.io/ourairports-data/airports.csv
//	curl -s -o /tmp/airports.json https://raw.githubusercontent.com/mwgg/Airports/master/airports.json
//	go run ./cmd/gen-airports -in /tmp/airports.csv -cities /tmp/airports.json -out internal/airports/table_gen.go
//
// An airport whose coordinates give no valid zone is left out and reported on
// stderr, since a table entry without a zone would hide the coordinate
// fallback in timing.AirportZone. Hand corrections live in zoneFixes,
// cityFixes and extra below.
package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/format"
	"io"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dpage/aerly/internal/geotz"
)

// cityMatchKm is how close an mwgg airport must be to the OurAirports one
// with the same code for its city to be used.
const cityMatchKm = 10

// zoneFixes corrects zones the boundary data gets wrong. The data merges
// zones that agree on today's offset, so Hanoi comes back as Asia/Bangkok.
var zoneFixes = map[string]string{
	"HAN": "Asia/Ho_Chi_Minh",
}

// cityFixes names the city an airport serves where neither source gives the
// name a traveller would use: the suburb the runway sits in ("Arnavutkoy" for
// IST), a local spelling ("Venezia") or a name stripped of its accents.
var cityFixes = map[string]string{
	"AGP": "Málaga",
	"BEG": "Belgrade",
	"BOG": "Bogotá",
	"BSL": "Basel",
	"CEB": "Cebu",
	"CFU": "Corfu",
	"CGH": "São Paulo",
	"CIA": "Rome",
	"CTS": "Sapporo",
	"CUN": "Cancún",
	"CVG": "Cincinnati",
	"DEL": "Delhi",
	"DFW": "Dallas",
	"DPS": "Bali",
	"DUS": "Düsseldorf",
	"DWC": "Dubai",
	"EZE": "Buenos Aires",
	"FRA": "Frankfurt",
	"GDN": "Gdańsk",
	"GIG": "Rio de Janeiro",
	"GRU": "São Paulo",
	"IAD": "Washington",
	"IEV": "Kyiv",
	"IST": "Istanbul",
	"KBP": "Kyiv",
	"KEF": "Reykjavík",
	"KRK": "Kraków",
	"LED": "St Petersburg",
	"MFM": "Macau",
	"MLA": "Malta",
	"MMX": "Malmö",
	"NAP": "Naples",
	"NGO": "Nagoya",
	"PMI": "Palma",
	"PTY": "Panama City",
	"PWM": "Portland (ME)",
	"RDU": "Raleigh",
	"SID": "Sal",
	"SJO": "San José",
	"SNA": "Orange County",
	"TLS": "Toulouse",
	"VCE": "Venice",
	"YQB": "Quebec City",
}

// extra holds airports OurAirports no longer lists under an IATA code, which
// old trips still name.
var extra = map[string]entry{
	"TXL": {52.5597, 13.2877, "Berlin Tegel", "Europe/Berlin", "Berlin"},
}

func main() {
	if err := run(os.Args[1:], os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "gen-airports:", err)
		os.Exit(1)
	}
}

// run parses the flags, generates the table from -in and -cities and writes
// it to -out.
func run(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("gen-airports", flag.ContinueOnError)
	fs.SetOutput(stderr)
	in := fs.String("in", "", "path to OurAirports airports.csv")
	citiesPath := fs.String("cities", "", "path to mwgg airports.json")
	out := fs.String("out", "", "path of the Go file to write")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *in == "" || *citiesPath == "" || *out == "" {
		return errors.New("-in, -cities and -out are all required")
	}
	cf, err := os.Open(*citiesPath)
	if err != nil {
		return err
	}
	defer cf.Close()
	cities, err := readCities(cf)
	if err != nil {
		return fmt.Errorf("%s: %w", *citiesPath, err)
	}
	f, err := os.Open(*in)
	if err != nil {
		return err
	}
	defer f.Close()
	src, err := generate(f, cities, geotz.Lookup, stderr)
	if err != nil {
		return fmt.Errorf("%s: %w", *in, err)
	}
	return os.WriteFile(*out, src, 0o644)
}

// zoneFinder gives the IANA zone at a coordinate; geotz.Lookup in production.
type zoneFinder func(lat, lon float64) (string, bool)

type entry struct {
	lat, lon       float64
	name, tz, city string
}

// city is an mwgg airport's location and the city it serves.
type city struct {
	Lat, Lon float64
	City     string
}

// readCities reads mwgg's airports.json into a map from IATA code to city.
func readCities(r io.Reader) (map[string]city, error) {
	var raw map[string]struct {
		IATA string  `json:"iata"`
		City string  `json:"city"`
		Lat  float64 `json:"lat"`
		Lon  float64 `json:"lon"`
	}
	if err := json.NewDecoder(r).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode airports.json: %w", err)
	}
	out := map[string]city{}
	for _, a := range raw {
		if code := strings.ToUpper(strings.TrimSpace(a.IATA)); len(code) == 3 && strings.TrimSpace(a.City) != "" {
			out[code] = city{a.Lat, a.Lon, strings.TrimSpace(a.City)}
		}
	}
	return out, nil
}

// distanceKm is the great-circle distance between two points.
func distanceKm(lat1, lon1, lat2, lon2 float64) float64 {
	const r = 6371.0
	rad := math.Pi / 180
	dLat, dLon := (lat2-lat1)*rad, (lon2-lon1)*rad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * r * math.Asin(math.Sqrt(a))
}

// shortName drops a trailing " Airport", in any case,, so labels read "London Heathrow
// (LHR)" rather than "London Heathrow Airport (LHR)".
func shortName(name string) string {
	name = strings.TrimSpace(name)
	const suffix = " airport"
	if len(name) > len(suffix) && strings.EqualFold(name[len(name)-len(suffix):], suffix) {
		return name[:len(name)-len(suffix)]
	}
	return name
}

// generate reads airports.csv and returns the formatted Go source of the
// table, sorted by code so a regeneration gives a stable diff. Rows without
// an IATA code, closed airports and rows whose coordinates give no valid zone
// are skipped; the last are listed on skipped.
func generate(r io.Reader, cities map[string]city, zoneAt zoneFinder, skipped io.Writer) ([]byte, error) {
	cr := csv.NewReader(r)
	header, err := cr.Read()
	if err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}
	col := map[string]int{}
	for i, h := range header {
		col[h] = i
	}
	for _, need := range []string{"type", "name", "latitude_deg", "longitude_deg", "municipality", "iata_code"} {
		if _, ok := col[need]; !ok {
			return nil, fmt.Errorf("no %q column", need)
		}
	}
	table := map[string]entry{}
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		code := strings.ToUpper(strings.TrimSpace(rec[col["iata_code"]]))
		if len(code) != 3 || rec[col["type"]] == "closed" {
			continue
		}
		lat, err1 := strconv.ParseFloat(rec[col["latitude_deg"]], 64)
		lon, err2 := strconv.ParseFloat(rec[col["longitude_deg"]], 64)
		if err1 != nil || err2 != nil {
			fmt.Fprintf(skipped, "skipped %s: bad coordinates\n", code)
			continue
		}
		tz, ok := zoneFixes[code]
		if !ok {
			tz, ok = zoneAt(lat, lon)
		}
		if ok {
			_, err := time.LoadLocation(tz)
			ok = err == nil
		}
		if !ok {
			fmt.Fprintf(skipped, "skipped %s: no zone at %v,%v\n", code, lat, lon)
			continue
		}
		if _, dup := table[code]; dup {
			fmt.Fprintf(skipped, "skipped %s: duplicate code\n", code)
			continue
		}
		served := strings.TrimSpace(rec[col["municipality"]])
		if c, ok := cities[code]; ok && distanceKm(lat, lon, c.Lat, c.Lon) <= cityMatchKm {
			served = c.City
		}
		if fix, ok := cityFixes[code]; ok {
			served = fix
		}
		table[code] = entry{lat, lon, shortName(rec[col["name"]]), tz, served}
	}
	if len(table) == 0 {
		return nil, errors.New("no airports with an IATA code found")
	}
	for code, e := range extra {
		if _, ok := table[code]; !ok {
			table[code] = e
		}
	}
	codes := make([]string, 0, len(table))
	for c := range table {
		codes = append(codes, c)
	}
	sort.Strings(codes)

	var b bytes.Buffer
	b.WriteString("// Code generated by cmd/gen-airports from OurAirports airports.csv and mwgg airports.json; DO NOT EDIT.\n\n")
	b.WriteString("package airports\n\n")
	b.WriteString("// table holds every airport with an IATA code.\n")
	b.WriteString("var table = map[string]Entry{\n")
	for _, c := range codes {
		e := table[c]
		fmt.Fprintf(&b, "\t%q: {%s, %s, %q, %q, %q},\n", c,
			strconv.FormatFloat(e.lat, 'f', -1, 64), strconv.FormatFloat(e.lon, 'f', -1, 64), e.name, e.tz, e.city)
	}
	b.WriteString("}\n")
	return format.Source(b.Bytes())
}
