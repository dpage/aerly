package handlers

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/attachments"
	"github.com/dpage/aerly/internal/auth"
	"github.com/dpage/aerly/internal/config"
)

// withAttachments wires a real filesystem-backed store rooted at a temp dir and
// sets the upload cap, returning the env for chaining.
func withAttachments(t *testing.T, e *testEnv, maxBytes int64) *testEnv {
	t.Helper()
	st, err := attachments.NewFSStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFSStore: %v", err)
	}
	e.api.Attachments = st
	e.cfg.AttachmentsStore = t.TempDir()
	e.cfg.AttachmentsMaxBytes = maxBytes
	return e
}

// uploadFile posts a multipart "file" part to path as user uid.
func (e *testEnv) uploadFile(t *testing.T, path, filename, ctype string, data []byte, uid int64) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	hdr := make(map[string][]string)
	hdr["Content-Disposition"] = []string{fmt.Sprintf(`form-data; name="file"; filename=%q`, filename)}
	if ctype != "" {
		hdr["Content-Type"] = []string{ctype}
	}
	part, err := mw.CreatePart(hdr)
	if err != nil {
		t.Fatalf("CreatePart: %v", err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatalf("write part: %v", err)
	}
	mw.Close()

	r := httptest.NewRequest("POST", path, &buf)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	if uid != 0 {
		r.AddCookie(&http.Cookie{
			Name:  auth.SessionCookie,
			Value: auth.SignSession(sessKey, uid, 0, time.Now().Add(time.Hour)),
		})
	}
	w := httptest.NewRecorder()
	e.mux.ServeHTTP(w, r)
	return w
}

// seedHotelPlan creates a hotel plan in tripID and returns its id.
func seedHotelPlan(t *testing.T, e *testEnv, tripID, uid int64) int64 {
	t.Helper()
	body := map[string]any{
		"type": "hotel", "title": "Hotel",
		"parts": []map[string]any{{"type": "hotel", "starts_at": "2026-06-01T15:00:00Z"}},
	}
	w := e.req(t, "POST", fmt.Sprintf("/api/trips/%d/plans", tripID), body, uid)
	if w.Code != http.StatusCreated {
		t.Fatalf("seed plan: %d %s", w.Code, w.Body.String())
	}
	return int64(decodeBody[map[string]any](t, w)["id"].(float64))
}

func TestAttachmentUploadDownloadDelete(t *testing.T) {
	e := withAttachments(t, setup(t, nil, &config.Config{}), 1<<20)
	owner := e.user(t, "owner", false)
	stranger := e.user(t, "stranger", false)
	tid := newTrip(t, e, owner, "Trip")
	pid := seedHotelPlan(t, e, tid, owner)

	payload := []byte("%PDF-1.4 fake ticket bytes")

	// Stranger can't upload.
	if w := e.uploadFile(t, fmt.Sprintf("/api/plans/%d/attachments", pid), "t.pdf", "application/pdf", payload, stranger); w.Code != http.StatusForbidden {
		t.Errorf("stranger upload = %d, want 403", w.Code)
	}

	// Owner uploads.
	w := e.uploadFile(t, fmt.Sprintf("/api/plans/%d/attachments", pid), "ticket.pdf", "application/pdf", payload, owner)
	if w.Code != http.StatusCreated {
		t.Fatalf("upload = %d %s", w.Code, w.Body.String())
	}
	att := decodeBody[map[string]any](t, w)
	aid := int64(att["id"].(float64))
	if att["filename"] != "ticket.pdf" || att["content_type"] != "application/pdf" {
		t.Errorf("attachment metadata wrong: %v", att)
	}
	if int64(att["size_bytes"].(float64)) != int64(len(payload)) {
		t.Errorf("size_bytes = %v, want %d", att["size_bytes"], len(payload))
	}

	// The plan DTO now carries the attachment.
	pw := e.req(t, "GET", fmt.Sprintf("/api/trips/%d", tid), nil, owner)
	if pw.Code != http.StatusOK {
		t.Fatalf("get trip = %d %s", pw.Code, pw.Body.String())
	}

	// Download returns the exact bytes with a sensible disposition.
	dw := e.req(t, "GET", fmt.Sprintf("/api/attachments/%d", aid), nil, owner)
	if dw.Code != http.StatusOK {
		t.Fatalf("download = %d %s", dw.Code, dw.Body.String())
	}
	if !bytes.Equal(dw.Body.Bytes(), payload) {
		t.Errorf("download bytes mismatch")
	}
	if cd := dw.Header().Get("Content-Disposition"); cd == "" {
		t.Error("missing Content-Disposition on download")
	}
	if dw.Header().Get("Content-Type") != "application/pdf" {
		t.Errorf("download content-type = %q", dw.Header().Get("Content-Type"))
	}

	// Stranger can't download (not a viewer of the plan).
	if w := e.req(t, "GET", fmt.Sprintf("/api/attachments/%d", aid), nil, stranger); w.Code != http.StatusForbidden {
		t.Errorf("stranger download = %d, want 403", w.Code)
	}

	// Stranger can't delete; owner can.
	if w := e.req(t, "DELETE", fmt.Sprintf("/api/attachments/%d", aid), nil, stranger); w.Code != http.StatusForbidden {
		t.Errorf("stranger delete = %d, want 403", w.Code)
	}
	if w := e.req(t, "DELETE", fmt.Sprintf("/api/attachments/%d", aid), nil, owner); w.Code != http.StatusNoContent {
		t.Fatalf("delete = %d %s", w.Code, w.Body.String())
	}
	// Gone now.
	if w := e.req(t, "GET", fmt.Sprintf("/api/attachments/%d", aid), nil, owner); w.Code != http.StatusNotFound {
		t.Errorf("download after delete = %d, want 404", w.Code)
	}
}

func TestAttachmentDisabledReturns503(t *testing.T) {
	e := setup(t, nil, &config.Config{}) // no Attachments store wired
	owner := e.user(t, "owner", false)
	tid := newTrip(t, e, owner, "Trip")
	pid := seedHotelPlan(t, e, tid, owner)

	if w := e.uploadFile(t, fmt.Sprintf("/api/plans/%d/attachments", pid), "t.pdf", "application/pdf", []byte("x"), owner); w.Code != http.StatusServiceUnavailable {
		t.Errorf("upload with no store = %d, want 503", w.Code)
	}
	if w := e.req(t, "GET", "/api/attachments/1", nil, owner); w.Code != http.StatusServiceUnavailable {
		t.Errorf("download with no store = %d, want 503", w.Code)
	}
	if w := e.req(t, "DELETE", "/api/attachments/1", nil, owner); w.Code != http.StatusServiceUnavailable {
		t.Errorf("delete with no store = %d, want 503", w.Code)
	}
}

func TestAttachmentUploadTooLarge(t *testing.T) {
	e := withAttachments(t, setup(t, nil, &config.Config{}), 8)
	owner := e.user(t, "owner", false)
	tid := newTrip(t, e, owner, "Trip")
	pid := seedHotelPlan(t, e, tid, owner)

	// 9 bytes against an 8-byte cap.
	if w := e.uploadFile(t, fmt.Sprintf("/api/plans/%d/attachments", pid), "big.bin", "application/octet-stream", []byte("123456789"), owner); w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("oversize upload = %d, want 413", w.Code)
	}
}

func TestAttachmentConfigCapability(t *testing.T) {
	e := withAttachments(t, setup(t, nil, &config.Config{}), 5<<20)
	uid := e.user(t, "u", false)
	caps := decodeBody[map[string]any](t, e.req(t, "GET", "/api/config", nil, uid))
	if caps["attachments_enabled"] != true {
		t.Errorf("attachments_enabled = %v, want true", caps["attachments_enabled"])
	}
	if int64(caps["attachments_max_bytes"].(float64)) != 5<<20 {
		t.Errorf("attachments_max_bytes = %v", caps["attachments_max_bytes"])
	}

	// Storeless config omits the size and reports disabled.
	e2 := setup(t, nil, &config.Config{})
	caps2 := decodeBody[map[string]any](t, e2.req(t, "GET", "/api/config", nil, e2.user(t, "u2", false)))
	if caps2["attachments_enabled"] != false {
		t.Errorf("attachments_enabled = %v, want false", caps2["attachments_enabled"])
	}
	if _, ok := caps2["attachments_max_bytes"]; ok {
		t.Error("attachments_max_bytes should be omitted when disabled")
	}
}

func TestSanitizeFilename(t *testing.T) {
	cases := map[string]string{
		"../../etc/passwd":    "passwd",
		`c:\windows\evil.exe`: "evil.exe",
		"":                    "attachment",
		"..":                  "attachment",
		"normal name.pdf":     "normal name.pdf",
		"with\x00null.txt":    "withnull.txt",
		"/leading/slash.png":  "slash.png",
	}
	for in, want := range cases {
		if got := sanitizeFilename(in); got != want {
			t.Errorf("sanitizeFilename(%q) = %q, want %q", in, got, want)
		}
	}
}
