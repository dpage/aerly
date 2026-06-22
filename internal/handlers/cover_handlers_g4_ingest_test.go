package handlers

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/auth"
	"github.com/dpage/aerly/internal/planops"
)

// g4errExtractor returns an error from ExtractPlans so planops.Propose fails,
// exercising the ingestTrip 502 branch.
type g4errExtractor struct{}

func (g4errExtractor) ExtractPlans(context.Context, string, []planops.Document) ([]planops.ExtractedPlan, error) {
	return nil, errors.New("llm exploded")
}

// g4multipart issues a multipart/form-data POST (file + optional text/source
// fields) as the given user, since the JSON req helper can't build multipart.
func g4multipart(t *testing.T, e *testEnv, path string, asUser int64, filename, ctype string, data []byte, fields map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		_ = mw.WriteField(k, v)
	}
	if filename != "" {
		hdr := make(map[string][]string)
		hdr["Content-Disposition"] = []string{fmt.Sprintf(`form-data; name="file"; filename="%s"`, filename)}
		if ctype != "" {
			hdr["Content-Type"] = []string{ctype}
		}
		pw, err := mw.CreatePart(hdr)
		if err != nil {
			t.Fatalf("create part: %v", err)
		}
		_, _ = pw.Write(data)
	}
	_ = mw.Close()

	r := httptest.NewRequest("POST", path, &buf)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	if asUser != 0 {
		r.AddCookie(&http.Cookie{
			Name:  auth.SessionCookie,
			Value: auth.SignSession(sessKey, asUser, 0, time.Now().Add(time.Hour)),
		})
	}
	w := httptest.NewRecorder()
	e.mux.ServeHTTP(w, r)
	return w
}

// TestG4DocumentMediaType covers documentMediaType's fallback ladder directly:
// a valid declared type wins; a .pdf filename overrides a blank/octet-stream
// declaration; an unknown non-empty declaration is returned verbatim; and an
// empty declaration with a non-pdf name falls back to octet-stream.
func TestG4DocumentMediaType(t *testing.T) {
	cases := []struct {
		declared, filename, want string
	}{
		{"application/pdf", "x.pdf", "application/pdf"},
		{"application/octet-stream", "ticket.PDF", "application/pdf"},
		{"", "ticket.pdf", "application/pdf"},
		{"text/calendar; charset=utf-8", "booking.ics", "text/calendar"},
		{"weird/thing", "note.txt", "weird/thing"},
		{"", "note.txt", "application/octet-stream"},
	}
	for _, c := range cases {
		if got := documentMediaType(c.declared, c.filename); got != c.want {
			t.Errorf("documentMediaType(%q,%q) = %q, want %q", c.declared, c.filename, got, c.want)
		}
	}
}

// TestG4ICalProposalsGarbage covers icalProposals' parse-error branch directly:
// bytes that don't parse as iCalendar yield recognised=false.
func TestG4ICalProposalsGarbage(t *testing.T) {
	if _, ok := icalProposals([]byte("not a calendar at all")); ok {
		t.Error("garbage should not be recognised")
	}
}

// TestG4IngestBadID covers the invalid-trip-id 400 branch of ingestTrip.
func TestG4IngestBadID(t *testing.T) {
	e := setup(t, nil, nil)
	uid := e.user(t, "g4ingbad", false)
	if w := e.req(t, "POST", "/api/trips/abc/ingest", map[string]any{"text": "x"}, uid); w.Code != http.StatusBadRequest {
		t.Errorf("bad id = %d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// TestG4IngestBadBody covers the parseIngestBody decode-error 400 branch.
func TestG4IngestBadBody(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g4ingbody", false)
	tid := newTrip(t, e, owner, "Trip")
	if w := e.req(t, "POST", fmt.Sprintf("/api/trips/%d/ingest", tid), map[string]any{"bogus": 1}, owner); w.Code != http.StatusBadRequest {
		t.Errorf("bad body = %d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// TestG4IngestProposeError covers the planops.Propose-failed 502 branch.
func TestG4IngestProposeError(t *testing.T) {
	e := setup(t, nil, nil)
	e.api.Extractor = g4errExtractor{}
	owner := e.user(t, "g4ingerr", false)
	tid := newTrip(t, e, owner, "Trip")
	if w := e.req(t, "POST", fmt.Sprintf("/api/trips/%d/ingest", tid), map[string]any{"text": "stay"}, owner); w.Code != http.StatusBadGateway {
		t.Errorf("propose err = %d, want 502; body=%s", w.Code, w.Body.String())
	}
}

// TestG4IngestUnrecognisedICalFallsThrough covers ingestTrip's fall-through when
// the upload sniffs as iCal but no source-specific mapper recognises it: the
// content is handed to the LLM path, so with no extractor it 503s (line 84 +
// 87). It also covers icalProposals' map-not-recognised branch.
func TestG4IngestUnrecognisedICalFallsThrough(t *testing.T) {
	e := setup(t, nil, nil) // no extractor
	owner := e.user(t, "g4ingical", false)
	tid := newTrip(t, e, owner, "Trip")
	// A minimal but unrecognised calendar: parses, but no mapper claims it.
	ics := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//G4//Test//EN\r\n" +
		"BEGIN:VEVENT\r\nUID:g4-1\r\nDTSTART:20260601T090000Z\r\nDTEND:20260601T100000Z\r\n" +
		"SUMMARY:Mystery Event\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	w := e.req(t, "POST", fmt.Sprintf("/api/trips/%d/ingest", tid), map[string]any{"text": ics}, owner)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("unrecognised ical w/o extractor = %d, want 503; body=%s", w.Code, w.Body.String())
	}
}

// TestG4IngestICalGarbageFallsThrough covers icalProposals' parse-error branch:
// content that starts with the VCALENDAR marker but is otherwise unparseable
// still falls through to the LLM path (503 with no extractor).
func TestG4IngestICalGarbageFallsThrough(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g4ingical2", false)
	tid := newTrip(t, e, owner, "Trip")
	garbage := "BEGIN:VCALENDAR\nthis is not valid ical at all\x00\x01"
	w := e.req(t, "POST", fmt.Sprintf("/api/trips/%d/ingest", tid), map[string]any{"text": garbage}, owner)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("garbage ical w/o extractor = %d, want 503; body=%s", w.Code, w.Body.String())
	}
}

// TestG4IngestMultipartUpload covers parseIngestBody's multipart path
// (ParseMultipartForm, FormFile, documentMediaType) and icalUpload's document
// branch by uploading an unrecognised .ics file. With no extractor it 503s after
// the iCal fall-through, but parseIngestBody and icalUpload run end-to-end.
func TestG4IngestMultipartUpload(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g4ingmp", false)
	tid := newTrip(t, e, owner, "Trip")
	ics := []byte("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//G4//EN\r\nEND:VCALENDAR\r\n")
	w := g4multipart(t, e, fmt.Sprintf("/api/trips/%d/ingest", tid), owner,
		"booking.ics", "text/calendar", ics, map[string]string{"source": "upload"})
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("multipart ical upload = %d, want 503; body=%s", w.Code, w.Body.String())
	}
}

// TestG4IngestMultipartNoFile covers parseIngestBody's "multipart with no file"
// branch (ErrMissingFile → text-only). Plain text with no extractor → 503.
func TestG4IngestMultipartNoFile(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g4ingmpnf", false)
	tid := newTrip(t, e, owner, "Trip")
	w := g4multipart(t, e, fmt.Sprintf("/api/trips/%d/ingest", tid), owner,
		"", "", nil, map[string]string{"text": "some pasted booking text"})
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("multipart no-file = %d, want 503; body=%s", w.Code, w.Body.String())
	}
}

// TestG4IngestMultipartPDFGuessesMediaType covers documentMediaType's
// filename-extension fallback (a .pdf part declared octet-stream resolves to
// application/pdf) by uploading a tiny non-iCal "PDF" with an extractor wired so
// the propose path runs (the extractor returns no plans → empty result, 200).
func TestG4IngestMultipartPDFGuessesMediaType(t *testing.T) {
	e := setup(t, nil, nil)
	e.api.Extractor = &fakeIngestExtractor{} // returns no plans, no error
	owner := e.user(t, "g4ingpdf", false)
	tid := newTrip(t, e, owner, "Trip")
	w := g4multipart(t, e, fmt.Sprintf("/api/trips/%d/ingest", tid), owner,
		"ticket.pdf", "application/octet-stream", []byte("%PDF-1.4 not a real pdf"), nil)
	if w.Code != http.StatusOK {
		t.Errorf("multipart pdf = %d, want 200; body=%s", w.Code, w.Body.String())
	}
}

// TestG4ConfirmBadID covers the invalid-trip-id 400 branch of ingestTripConfirm.
func TestG4ConfirmBadID(t *testing.T) {
	e := setup(t, nil, nil)
	uid := e.user(t, "g4cfbad", false)
	if w := e.req(t, "POST", "/api/trips/abc/ingest/confirm", map[string]any{"plans": []any{}}, uid); w.Code != http.StatusBadRequest {
		t.Errorf("bad id = %d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// TestG4ConfirmForbidden covers ingestTripConfirm's requireTripEdit gate.
func TestG4ConfirmForbidden(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g4cfowner", false)
	stranger := e.user(t, "g4cfstranger", false)
	tid := newTrip(t, e, owner, "Trip")
	if w := e.req(t, "POST", fmt.Sprintf("/api/trips/%d/ingest/confirm", tid), map[string]any{"plans": []any{}}, stranger); w.Code != http.StatusForbidden {
		t.Errorf("stranger confirm = %d, want 403; body=%s", w.Code, w.Body.String())
	}
}

// TestG4ConfirmBadBody covers the decode-error 400 branch of ingestTripConfirm.
func TestG4ConfirmBadBody(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g4cfbody", false)
	tid := newTrip(t, e, owner, "Trip")
	if w := e.req(t, "POST", fmt.Sprintf("/api/trips/%d/ingest/confirm", tid), map[string]any{"bogus": 1}, owner); w.Code != http.StatusBadRequest {
		t.Errorf("bad body = %d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// TestG4ConfirmInvalidPlanType covers the invalid-plan-type 400 branch.
func TestG4ConfirmInvalidPlanType(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g4cftype", false)
	tid := newTrip(t, e, owner, "Trip")
	body := map[string]any{"plans": []map[string]any{{"type": "spaceship"}}}
	if w := e.req(t, "POST", fmt.Sprintf("/api/trips/%d/ingest/confirm", tid), body, owner); w.Code != http.StatusBadRequest {
		t.Errorf("invalid type = %d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// TestG4ConfirmCommitErrorWithVisibility covers two branches at once: the
// toConfirmPlanInput visibility mapping (a plan carrying a visibility block) and
// the planops.Commit-failed 400 branch (the plans table is dropped, so the
// commit can't write). A superuser clears requireTripEdit without a DB read.
func TestG4ConfirmCommitErrorWithVisibility(t *testing.T) {
	e := setup(t, nil, nil)
	super := e.user(t, "g4cfcommit", true)
	tid := newTrip(t, e, super, "Trip")
	g1dropTable(t, e, "plans")
	body := map[string]any{"plans": []map[string]any{{
		"type":       "hotel",
		"title":      "Hotel Plaza",
		"visibility": map[string]any{"mode": "only", "user_ids": []int64{super}},
		"parts": []map[string]any{{
			"type":        "hotel",
			"start_label": "Hotel Plaza",
			"hotel":       map[string]any{"property_name": "Hotel Plaza"},
		}},
	}}}
	if w := e.req(t, "POST", fmt.Sprintf("/api/trips/%d/ingest/confirm", tid), body, super); w.Code != http.StatusBadRequest {
		t.Errorf("commit err = %d, want 400; body=%s", w.Code, w.Body.String())
	}
}
