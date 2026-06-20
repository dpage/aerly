package migrations

import (
	"fmt"
	"io/fs"
	"strings"
	"testing"
)

func TestEmbeddedMigrations(t *testing.T) {
	entries, err := fs.ReadDir(FS, ".")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	var ups, downs int
	seenVersion := map[int]string{}
	for _, e := range entries {
		n := e.Name()
		if !strings.HasSuffix(n, ".sql") {
			continue
		}
		b, err := fs.ReadFile(FS, n)
		if err != nil {
			t.Fatalf("ReadFile %s: %v", n, err)
		}
		if len(b) == 0 {
			t.Errorf("%s is empty", n)
		}
		switch {
		case strings.HasSuffix(n, ".up.sql"):
			ups++
			// Each up migration must carry a unique NNNN version: the migrator
			// keys schema_migrations by that number, so a duplicate silently
			// skips one migration once the other's version is recorded.
			var v int
			if _, err := fmt.Sscanf(n, "%04d_", &v); err != nil {
				t.Errorf("%s: cannot parse version: %v", n, err)
				continue
			}
			if prev, dup := seenVersion[v]; dup {
				t.Errorf("duplicate migration version %04d: %s and %s", v, prev, n)
			}
			seenVersion[v] = n
		case strings.HasSuffix(n, ".down.sql"):
			downs++
		}
	}
	if ups == 0 {
		t.Error("expected at least one .up.sql migration")
	}
	if ups != downs {
		t.Errorf("mismatched migrations: %d up vs %d down", ups, downs)
	}
}
