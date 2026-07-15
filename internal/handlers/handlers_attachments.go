package handlers

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/dpage/aerly/internal/api"
	"github.com/dpage/aerly/internal/auth"
	"github.com/dpage/aerly/internal/store"
)

// Per-plan file attachments (issue #91). Uploads stream into the configured
// object store (filesystem or S3); the DB holds only metadata. Every endpoint is
// gated by the plan's own authorization: edit rights to add/remove, view rights
// to download. All return 503 when no store is configured.

// uploadAttachment accepts a multipart "file" part and stores it against a plan.
func (a *API) uploadAttachment(w http.ResponseWriter, r *http.Request) {
	if a.Attachments == nil {
		writeError(w, http.StatusServiceUnavailable, "Attachments are not enabled.")
		return
	}
	planID, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid ID.")
		return
	}
	me := auth.UserFrom(r.Context())
	if err := a.requirePlanEdit(r.Context(), planID, me, w); err != nil {
		return
	}

	// A 25 MiB upload over a slow link can outlast the short global ReadTimeout,
	// so extend this request's read deadline before streaming the body.
	extendUploadDeadline(w)
	maxBytes := a.Config.AttachmentsMaxBytes
	// Cap the whole request body (file + multipart overhead) so a client can't
	// stream gigabytes into ParseMultipartForm before we reject it.
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes+(1<<20))
	if err := r.ParseMultipartForm(maxBytes + 1); err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "Upload too large.")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "Missing file.")
		return
	}
	defer file.Close()
	if header.Size <= 0 {
		writeError(w, http.StatusBadRequest, "Empty file.")
		return
	}
	if header.Size > maxBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "Upload too large.")
		return
	}

	filename := sanitizeFilename(header.Filename)
	contentType := documentMediaType(header.Header.Get("Content-Type"), filename)

	key, err := a.Attachments.Put(r.Context(), io.LimitReader(file, header.Size), header.Size, contentType)
	if err != nil {
		serverError(w, err)
		return
	}
	att, err := a.Store.CreatePlanAttachment(r.Context(), store.CreatePlanAttachmentPayload{
		PlanID:      planID,
		UploadedBy:  &me.ID,
		Filename:    filename,
		ContentType: contentType,
		SizeBytes:   header.Size,
		StorageKey:  key,
	})
	if err != nil {
		// Don't orphan the blob if the metadata insert fails.
		_ = a.Attachments.Delete(r.Context(), key)
		handleStoreErr(w, err)
		return
	}
	if plan, perr := a.Store.PlanByID(r.Context(), planID); perr == nil {
		a.publishPlanUpdated(r.Context(), plan.TripID, planID)
	}
	writeJSON(w, http.StatusCreated, api.ToAttachmentDTO(att))
}

// downloadAttachment streams an attachment's bytes to a viewer of its plan.
func (a *API) downloadAttachment(w http.ResponseWriter, r *http.Request) {
	if a.Attachments == nil {
		writeError(w, http.StatusServiceUnavailable, "Attachments are not enabled.")
		return
	}
	att, ok := a.attachmentForView(w, r)
	if !ok {
		return
	}
	blob, err := a.Attachments.Open(r.Context(), att.StorageKey)
	if err != nil {
		serverError(w, err)
		return
	}
	defer blob.Close()

	ct := att.ContentType
	if ct == "" {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Length", strconv.FormatInt(att.SizeBytes, 10))
	// X-Content-Type-Options stops a browser sniffing a user-supplied blob into
	// something executable; the inline-safe disposition is "attachment".
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", contentDisposition(att.Filename))
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, blob)
}

// deleteAttachment removes an attachment (blob + metadata). The blob goes first;
// a failed row delete leaves no dangling bytes, and the (idempotent) blob delete
// means a retry is always safe.
func (a *API) deleteAttachment(w http.ResponseWriter, r *http.Request) {
	if a.Attachments == nil {
		writeError(w, http.StatusServiceUnavailable, "Attachments are not enabled.")
		return
	}
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid ID.")
		return
	}
	att, err := a.Store.PlanAttachmentByID(r.Context(), id)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	me := auth.UserFrom(r.Context())
	if err := a.requirePlanEdit(r.Context(), att.PlanID, me, w); err != nil {
		return
	}
	if err := a.Store.DeletePlanAttachment(r.Context(), id); err != nil {
		handleStoreErr(w, err)
		return
	}
	if derr := a.Attachments.Delete(r.Context(), att.StorageKey); derr != nil {
		// The metadata is gone; a lingering blob is a harmless orphan, not a
		// client-facing failure. Log and report success.
		slog.Error("attachment blob cleanup failed", "err", derr, "key", att.StorageKey)
	}
	if plan, perr := a.Store.PlanByID(r.Context(), att.PlanID); perr == nil {
		a.publishPlanUpdated(r.Context(), plan.TripID, att.PlanID)
	}
	w.WriteHeader(http.StatusNoContent)
}

// attachmentForView loads the path attachment and authorizes the caller as a
// viewer of its plan, writing the appropriate error and returning ok=false on
// any failure.
func (a *API) attachmentForView(w http.ResponseWriter, r *http.Request) (*store.PlanAttachment, bool) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid ID.")
		return nil, false
	}
	att, err := a.Store.PlanAttachmentByID(r.Context(), id)
	if err != nil {
		handleStoreErr(w, err)
		return nil, false
	}
	me := auth.UserFrom(r.Context())
	if me == nil {
		writeError(w, http.StatusUnauthorized, "Unauthorised.")
		return nil, false
	}
	ok, err := a.Store.CanViewPlan(r.Context(), att.PlanID, me.ID, me.IsSuperuser)
	if err != nil {
		handleStoreErr(w, err)
		return nil, false
	}
	if !ok {
		writeError(w, http.StatusForbidden, "Forbidden.")
		return nil, false
	}
	return att, true
}

// sanitizeFilename reduces an uploaded filename to a safe base name: directory
// components are stripped (defeating "../"), control characters dropped, and the
// length bounded. An empty result falls back to "attachment".
func sanitizeFilename(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, `\`, "/"))
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return "attachment"
	}
	if len(name) > 255 {
		name = name[:255]
	}
	return name
}

// contentDisposition builds an attachment Content-Disposition with both a
// quoted ASCII fallback and an RFC 5987 UTF-8 filename* so non-ASCII names
// survive. The ASCII fallback collapses anything non-printable-ASCII to '_'.
func contentDisposition(filename string) string {
	var ascii strings.Builder
	for _, r := range filename {
		if r >= 0x20 && r < 0x7f && r != '"' && r != '\\' {
			ascii.WriteRune(r)
		} else {
			ascii.WriteByte('_')
		}
	}
	fallback := ascii.String()
	if fallback == "" {
		fallback = "attachment"
	}
	return fmt.Sprintf("attachment; filename=%q; filename*=UTF-8''%s",
		fallback, urlEncodeRFC5987(filename))
}

// urlEncodeRFC5987 percent-encodes a filename for the filename* parameter,
// leaving the RFC 5987 attr-char set untouched.
func urlEncodeRFC5987(s string) string {
	const upperhex = "0123456789ABCDEF"
	var b strings.Builder
	for _, c := range []byte(s) {
		// attr-char: ALPHA / DIGIT / "!#$&+-.^_`|~"
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') ||
			strings.IndexByte("!#$&+-.^_`|~", c) >= 0 {
			b.WriteByte(c)
		} else {
			b.WriteByte('%')
			b.WriteByte(upperhex[c>>4])
			b.WriteByte(upperhex[c&0x0f])
		}
	}
	return b.String()
}
