package geotz

import (
	"errors"
	"testing"

	"github.com/ringsaturn/tzf"
)

// stubFinder is a tzFinder that returns a fixed zone name.
type stubFinder struct{ name string }

func (s stubFinder) GetTimezoneName(lng, lat float64) string { return s.name }

func TestResolve(t *testing.T) {
	// Init failure: no finder, returns not-found regardless of coords.
	if name, ok := resolve(nil, errors.New("boom"), 1, 2); ok || name != "" {
		t.Errorf("init error: got (%q,%v), want (\"\",false)", name, ok)
	}
	// Nil finder with no error (defensive): also not-found.
	if name, ok := resolve(nil, nil, 1, 2); ok || name != "" {
		t.Errorf("nil finder: got (%q,%v), want (\"\",false)", name, ok)
	}
	// Finder returns an empty name (no zone at the point): not-found.
	if name, ok := resolve(stubFinder{name: ""}, nil, 1, 2); ok || name != "" {
		t.Errorf("empty name: got (%q,%v), want (\"\",false)", name, ok)
	}
	// Finder returns a zone: found.
	if name, ok := resolve(stubFinder{name: "Europe/London"}, nil, 51, 0); !ok || name != "Europe/London" {
		t.Errorf("happy path: got (%q,%v), want (\"Europe/London\",true)", name, ok)
	}
}

func TestInitFinderLogsAndRecordsError(t *testing.T) {
	// Swap in a failing constructor and call initFinder directly (bypassing the
	// sync.Once), saving and restoring the shared state so other tests are
	// unaffected.
	origNew, origFinder, origErr := newFinder, finder, initEr
	t.Cleanup(func() { newFinder, finder, initEr = origNew, origFinder, origErr })

	wantErr := errors.New("init failed")
	newFinder = func() (tzf.F, error) { return nil, wantErr }
	initFinder()

	if !errors.Is(initEr, wantErr) {
		t.Errorf("initEr = %v, want %v", initEr, wantErr)
	}
	if finder != nil {
		t.Errorf("finder = %v, want nil after a failed init", finder)
	}
}
