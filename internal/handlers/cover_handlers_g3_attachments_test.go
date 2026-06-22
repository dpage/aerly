package handlers

import (
	"bytes"
	"context"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/auth"
	"github.com/dpage/aerly/internal/config"
	"github.com/dpage/aerly/internal/store"
)

// TestG3UploadBadPlanID drives the invalid plan-id 400.
func TestG3UploadBadPlanID(t *testing.T) {
	e := withAttachments(t, setup(t, nil, &config.Config{}), 1<<20)
	owner := e.user(t, "g3ubp", false)
	w := e.uploadFile(t, "/api/plans/notanumber/attachments", "t.pdf", "application/pdf", []byte("x"), owner)
	if w.Code != http.StatusBadRequest {
		t.Errorf("bad plan id = %d, want 400", w.Code)
	}
}

// TestG3UploadMissingFile drives the missing-"file"-part 400: a multipart body
// with a different field name.
func TestG3UploadMissingFile(t *testing.T) {
	e := withAttachments(t, setup(t, nil, &config.Config{}), 1<<20)
	owner := e.user(t, "g3umf", false)
	tid := newTrip(t, e, owner, "Trip")
	pid := seedHotelPlan(t, e, tid, owner)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormField("notfile")
	_, _ = fw.Write([]byte("hello"))
	mw.Close()
	r := httptest.NewRequest("POST", fmt.Sprintf("/api/plans/%d/attachments", pid), &buf)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	r.AddCookie(&http.Cookie{
		Name:  auth.SessionCookie,
		Value: auth.SignSession(sessKey, owner, 0, time.Now().Add(time.Hour)),
	})
	w := httptest.NewRecorder()
	e.mux.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("missing file = %d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// TestG3UploadEmptyFile drives the empty-file 400 (header.Size <= 0).
func TestG3UploadEmptyFile(t *testing.T) {
	e := withAttachments(t, setup(t, nil, &config.Config{}), 1<<20)
	owner := e.user(t, "g3uef", false)
	tid := newTrip(t, e, owner, "Trip")
	pid := seedHotelPlan(t, e, tid, owner)

	w := e.uploadFile(t, fmt.Sprintf("/api/plans/%d/attachments", pid), "empty.pdf", "application/pdf", []byte{}, owner)
	if w.Code != http.StatusBadRequest {
		t.Errorf("empty file = %d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// TestG3UploadCreateMetadataStoreErr drives the CreatePlanAttachment error
// branch (the blob is stored, then the metadata insert fails and the blob is
// cleaned up). Dropping plan_attachments after the plan-edit check passes makes
// the insert fail.
func TestG3UploadCreateMetadataStoreErr(t *testing.T) {
	e := withAttachments(t, setup(t, nil, &config.Config{}), 1<<20)
	owner := e.user(t, "g3ucm", false)
	tid := newTrip(t, e, owner, "Trip")
	pid := seedHotelPlan(t, e, tid, owner)
	g1dropTable(t, e, "plan_attachments")
	w := e.uploadFile(t, fmt.Sprintf("/api/plans/%d/attachments", pid), "t.pdf", "application/pdf", []byte("%PDF-1.4 bytes"), owner)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("create metadata store err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestG3DownloadBadID drives attachmentForView's invalid-id 400 via the download
// route.
func TestG3DownloadBadID(t *testing.T) {
	e := withAttachments(t, setup(t, nil, &config.Config{}), 1<<20)
	owner := e.user(t, "g3dlbad", false)
	if w := e.req(t, "GET", "/api/attachments/notanumber", nil, owner); w.Code != http.StatusBadRequest {
		t.Errorf("download bad id = %d, want 400", w.Code)
	}
}

// TestG3DownloadEmptyContentType drives the empty-content-type fallback to
// application/octet-stream on download.
func TestG3DownloadEmptyContentType(t *testing.T) {
	e := withAttachments(t, setup(t, nil, &config.Config{}), 1<<20)
	owner := e.user(t, "g3dlect", false)
	tid := newTrip(t, e, owner, "Trip")
	pid := seedHotelPlan(t, e, tid, owner)

	// Store an attachment row with an empty content type directly: the blob is a
	// real object in the FS store so Open succeeds.
	payload := []byte("raw bytes no type")
	key, err := e.api.Attachments.Put(context.Background(), bytes.NewReader(payload), int64(len(payload)), "")
	if err != nil {
		t.Fatalf("put blob: %v", err)
	}
	att, err := e.store.CreatePlanAttachment(context.Background(), store.CreatePlanAttachmentPayload{
		PlanID: pid, UploadedBy: &owner, Filename: "notype.bin",
		ContentType: "", SizeBytes: int64(len(payload)), StorageKey: key,
	})
	if err != nil {
		t.Fatalf("create attachment: %v", err)
	}
	w := e.req(t, "GET", fmt.Sprintf("/api/attachments/%d", att.ID), nil, owner)
	if w.Code != http.StatusOK {
		t.Fatalf("download = %d, body=%s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/octet-stream" {
		t.Errorf("content-type = %q, want application/octet-stream fallback", ct)
	}
}

// TestG3DownloadOpenErr drives the blob-Open error branch: the metadata row
// points at a storage key with no backing blob.
func TestG3DownloadOpenErr(t *testing.T) {
	e := withAttachments(t, setup(t, nil, &config.Config{}), 1<<20)
	owner := e.user(t, "g3dlopen", false)
	tid := newTrip(t, e, owner, "Trip")
	pid := seedHotelPlan(t, e, tid, owner)
	att, err := e.store.CreatePlanAttachment(context.Background(), store.CreatePlanAttachmentPayload{
		PlanID: pid, UploadedBy: &owner, Filename: "missing.pdf",
		ContentType: "application/pdf", SizeBytes: 10, StorageKey: "does/not/exist",
	})
	if err != nil {
		t.Fatalf("create attachment: %v", err)
	}
	if w := e.req(t, "GET", fmt.Sprintf("/api/attachments/%d", att.ID), nil, owner); w.Code != http.StatusInternalServerError {
		t.Errorf("open err = %d, want 500", w.Code)
	}
}

// TestG3DownloadCanViewStoreErr drives attachmentForView's CanViewPlan error
// 500.
func TestG3DownloadCanViewStoreErr(t *testing.T) {
	e := withAttachments(t, setup(t, nil, &config.Config{}), 1<<20)
	owner := e.user(t, "g3dlcv", false)
	tid := newTrip(t, e, owner, "Trip")
	pid := seedHotelPlan(t, e, tid, owner)
	att, err := e.store.CreatePlanAttachment(context.Background(), store.CreatePlanAttachmentPayload{
		PlanID: pid, UploadedBy: &owner, Filename: "x.pdf",
		ContentType: "application/pdf", SizeBytes: 3, StorageKey: "k",
	})
	if err != nil {
		t.Fatalf("create attachment: %v", err)
	}
	// plan_visibility participates in CanViewPlan; dropping it errors the check.
	g1dropTable(t, e, "plan_visibility")
	if w := e.req(t, "GET", fmt.Sprintf("/api/attachments/%d", att.ID), nil, owner); w.Code != http.StatusInternalServerError {
		t.Errorf("canview err = %d, want 500", w.Code)
	}
}

// TestG3DeleteBadID drives deleteAttachment's invalid-id 400.
func TestG3DeleteBadID(t *testing.T) {
	e := withAttachments(t, setup(t, nil, &config.Config{}), 1<<20)
	owner := e.user(t, "g3delbad", false)
	if w := e.req(t, "DELETE", "/api/attachments/notanumber", nil, owner); w.Code != http.StatusBadRequest {
		t.Errorf("delete bad id = %d, want 400", w.Code)
	}
}

// TestG3DeleteNotFound drives deleteAttachment's PlanAttachmentByID 404.
func TestG3DeleteNotFound(t *testing.T) {
	e := withAttachments(t, setup(t, nil, &config.Config{}), 1<<20)
	owner := e.user(t, "g3delnf", false)
	if w := e.req(t, "DELETE", "/api/attachments/999999", nil, owner); w.Code != http.StatusNotFound {
		t.Errorf("delete missing = %d, want 404", w.Code)
	}
}

// TestG3DeleteBlobCleanupFails drives the delete path where the metadata row is
// removed but the blob delete fails (a missing blob): the handler logs and still
// returns 204.
func TestG3DeleteBlobCleanupFails(t *testing.T) {
	e := withAttachments(t, setup(t, nil, &config.Config{}), 1<<20)
	owner := e.user(t, "g3delblob", false)
	tid := newTrip(t, e, owner, "Trip")
	pid := seedHotelPlan(t, e, tid, owner)
	att, err := e.store.CreatePlanAttachment(context.Background(), store.CreatePlanAttachmentPayload{
		PlanID: pid, UploadedBy: &owner, Filename: "ghost.pdf",
		ContentType: "application/pdf", SizeBytes: 5, StorageKey: "no/such/blob",
	})
	if err != nil {
		t.Fatalf("create attachment: %v", err)
	}
	if w := e.req(t, "DELETE", fmt.Sprintf("/api/attachments/%d", att.ID), nil, owner); w.Code != http.StatusNoContent {
		t.Errorf("delete with failing blob cleanup = %d, want 204; body=%s", w.Code, w.Body.String())
	}
}

// TestG3SanitizeFilenameLong covers the length-bound branch (>255 chars).
func TestG3SanitizeFilenameLong(t *testing.T) {
	long := strings.Repeat("a", 300) + ".pdf"
	got := sanitizeFilename(long)
	if len(got) != 255 {
		t.Errorf("sanitizeFilename length = %d, want 255 (truncated)", len(got))
	}
}

// TestG3ContentDisposition covers contentDisposition's ASCII fallback collapse,
// the all-non-ascii empty-fallback case, and the RFC 5987 encoder's attr-char
// passthrough vs percent-encoding.
func TestG3ContentDisposition(t *testing.T) {
	// Mixed ASCII + a non-ASCII rune and a quote: the quote/non-ASCII collapse to
	// '_' in the fallback, and the UTF-8 name is percent-encoded.
	cd := contentDisposition(`re"sumé.pdf`)
	if !strings.Contains(cd, `filename="re_sum_.pdf"`) {
		t.Errorf("ascii fallback wrong: %q", cd)
	}
	if !strings.Contains(cd, "filename*=UTF-8''") {
		t.Errorf("missing RFC 5987 filename*: %q", cd)
	}
	// An empty name yields the "attachment" fallback for the ASCII filename.
	cd2 := contentDisposition("")
	if !strings.Contains(cd2, `filename="attachment"`) {
		t.Errorf("empty-fallback wrong: %q", cd2)
	}

	// urlEncodeRFC5987: attr-chars pass through, others percent-encode.
	if got := urlEncodeRFC5987("a!b c"); got != "a!b%20c" {
		t.Errorf("urlEncodeRFC5987 = %q, want a!b%%20c", got)
	}
}
