package emailingest

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/dpage/aerly/internal/providers"
)

func TestFailureReason_UnscheduledFlight(t *testing.T) {
	// flightops wraps the resolver error; failureReason must unwrap it.
	wrapped := fmt.Errorf("resolve EZY2824 on 2027-01-25: %w", providers.ErrFlightUnscheduled)
	got := failureReason(wrapped)
	if !strings.Contains(got, "hasn't published a schedule") {
		t.Errorf("got %q, want a hint that the schedule isn't published", got)
	}
}

func TestFailureReason_NotFound(t *testing.T) {
	wrapped := fmt.Errorf("resolve XX9 on 2026-12-01: %w", providers.ErrFlightNotFound)
	got := failureReason(wrapped)
	if !strings.Contains(got, "no matching flight") {
		t.Errorf("got %q, want a no-matching-flight hint", got)
	}
}

func TestFailureReason_OtherErrors(t *testing.T) {
	got := failureReason(errors.New("boom"))
	if got != "boom" {
		t.Errorf("got %q, want pass-through", got)
	}
}

func TestFailureReason_LongErrorTruncated(t *testing.T) {
	long := strings.Repeat("x", 500)
	got := failureReason(errors.New(long))
	// 200 ASCII chars + a 3-byte ellipsis rune = 203 bytes.
	if len(got) != 203 {
		t.Errorf("expected len 203 (200 + …), got %d", len(got))
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("expected ellipsis suffix, got %q", got[len(got)-5:])
	}
}
