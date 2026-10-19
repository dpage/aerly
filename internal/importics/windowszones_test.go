package importics

import (
	"bufio"
	"os"
	"strings"
	"testing"
)

// TestWindowsZonesAreCanonical: no Windows mapping targets an IANA backward
// link (tzdata's "L" lines), so a regeneration that brings a new one fails
// here until cmd/gen-windows-zones' legacyIDs is extended.
func TestWindowsZonesAreCanonical(t *testing.T) {
	f, err := os.Open("/usr/share/zoneinfo/tzdata.zi")
	if err != nil {
		t.Skipf("no tzdata.zi on this host: %v", err)
	}
	defer f.Close()
	links := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if fields := strings.Fields(sc.Text()); len(fields) == 3 && fields[0] == "L" {
			links[fields[2]] = fields[1]
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	for win, tz := range windowsZones {
		if to, ok := links[tz]; ok {
			t.Errorf("%q maps to %s, a link to %s", win, tz, to)
		}
	}
}
