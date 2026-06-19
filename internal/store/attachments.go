package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// Per-plan file attachments (issue #91). The bytes live in a configured object
// store (filesystem or S3); this table holds the metadata plus the opaque
// storage_key. Visibility/edit authorization is the plan's own (handled in the
// handler layer via the existing CanViewPlan / CanEditTrip gates) — there is
// nothing attachment-specific to authorize.

// PlanAttachment is one uploaded file on a plan.
type PlanAttachment struct {
	ID          int64
	PlanID      int64
	UploadedBy  *int64
	Filename    string
	ContentType string
	SizeBytes   int64
	StorageKey  string
	CreatedAt   time.Time
}

// CreatePlanAttachmentPayload is the metadata recorded after the blob is stored.
type CreatePlanAttachmentPayload struct {
	PlanID      int64
	UploadedBy  *int64
	Filename    string
	ContentType string
	SizeBytes   int64
	StorageKey  string
}

// CreatePlanAttachment inserts the metadata row for an already-stored blob and
// returns the persisted attachment.
func (s *Store) CreatePlanAttachment(ctx context.Context, p CreatePlanAttachmentPayload) (*PlanAttachment, error) {
	a := &PlanAttachment{
		PlanID:      p.PlanID,
		UploadedBy:  p.UploadedBy,
		Filename:    p.Filename,
		ContentType: p.ContentType,
		SizeBytes:   p.SizeBytes,
		StorageKey:  p.StorageKey,
	}
	err := s.pool.QueryRow(ctx, `
		INSERT INTO plan_attachments (plan_id, uploaded_by, filename, content_type, size_bytes, storage_key)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at`,
		p.PlanID, p.UploadedBy, p.Filename, p.ContentType, p.SizeBytes, p.StorageKey).
		Scan(&a.ID, &a.CreatedAt)
	if err != nil {
		return nil, err
	}
	return a, nil
}

// PlanAttachmentByID returns one attachment, or ErrNotFound.
func (s *Store) PlanAttachmentByID(ctx context.Context, id int64) (*PlanAttachment, error) {
	a := &PlanAttachment{}
	err := s.pool.QueryRow(ctx, `
		SELECT id, plan_id, uploaded_by, filename, content_type, size_bytes, storage_key, created_at
		FROM plan_attachments WHERE id = $1`, id).
		Scan(&a.ID, &a.PlanID, &a.UploadedBy, &a.Filename, &a.ContentType, &a.SizeBytes, &a.StorageKey, &a.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return a, nil
}

// AttachmentsByPlan returns a plan's attachments, newest first.
func (s *Store) AttachmentsByPlan(ctx context.Context, planID int64) ([]*PlanAttachment, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, plan_id, uploaded_by, filename, content_type, size_bytes, storage_key, created_at
		FROM plan_attachments WHERE plan_id = $1
		ORDER BY created_at DESC, id DESC`, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*PlanAttachment
	for rows.Next() {
		a := &PlanAttachment{}
		if err := rows.Scan(&a.ID, &a.PlanID, &a.UploadedBy, &a.Filename, &a.ContentType,
			&a.SizeBytes, &a.StorageKey, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// DeletePlanAttachment removes one attachment's metadata row, returning
// ErrNotFound when it doesn't exist. The blob is swept by the caller.
func (s *Store) DeletePlanAttachment(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM plan_attachments WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// StorageKeysByPlan returns the storage keys of a plan's attachments. Used to
// sweep the blobs before a plan (and its cascading attachment rows) is deleted.
func (s *Store) StorageKeysByPlan(ctx context.Context, planID int64) ([]string, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT storage_key FROM plan_attachments WHERE plan_id = $1`, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}
