package mailer

import (
	"strings"
	"testing"
	"time"
)

func TestQuotaAlertSubject(t *testing.T) {
	if got, want := QuotaAlertSubject("OpenSky"), "Aerly hit a rate limit on OpenSky"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBuildQuotaAlertEmail_Structure(t *testing.T) {
	msg := BuildQuotaAlertEmail(QuotaAlertInput{
		FromAddr:   "alerts@aerly.test",
		ToAddr:     "admin@aerly.test",
		PublicURL:  "http://localhost:8080",
		Provider:   "AeroDataBox",
		Detail:     "throttled by RapidAPI; consider a higher AeroDataBox plan tier",
		OccurredAt: time.Date(2026, 6, 16, 8, 30, 0, 0, time.UTC),
	})
	for _, want := range []string{
		"From: alerts@aerly.test",
		"To: admin@aerly.test",
		"MIME-Version: 1.0",
		"multipart/alternative",
		"Subject: Aerly hit a rate limit on AeroDataBox",
		"AeroDataBox",
		"throttled by RapidAPI",
		"2026-06-16 08:30 UTC",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("message missing %q\n---\n%s", want, msg)
		}
	}
}

// A zero OccurredAt must not render a stray "at " / timestamp clause.
func TestBuildQuotaAlertEmail_NoTimestamp(t *testing.T) {
	msg := BuildQuotaAlertEmail(QuotaAlertInput{
		FromAddr:  "alerts@aerly.test",
		ToAddr:    "admin@aerly.test",
		PublicURL: "http://localhost:8080",
		Provider:  "OpenSky",
	})
	if strings.Contains(msg, "response at .") || strings.Contains(msg, "response at <") {
		t.Errorf("zero OccurredAt should omit the timestamp clause\n---\n%s", msg)
	}
}

// An empty Detail must not render a "Detail:" line.
func TestBuildQuotaAlertEmail_NoDetail(t *testing.T) {
	msg := BuildQuotaAlertEmail(QuotaAlertInput{
		FromAddr:  "alerts@aerly.test",
		ToAddr:    "admin@aerly.test",
		PublicURL: "http://localhost:8080",
		Provider:  "OpenSky",
	})
	if strings.Contains(msg, "Detail:") {
		t.Errorf("empty Detail should omit the Detail line\n---\n%s", msg)
	}
}
