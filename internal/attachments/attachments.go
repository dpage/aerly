// Package attachments provides the out-of-band blob store backing per-plan file
// attachments (issue #91). The database holds only metadata and the opaque
// storage key this package returns; the bytes live under a configured root —
// either a local filesystem directory or an S3 (or S3-compatible) bucket.
//
// The feature is off unless a store is configured: NewStorage returns (nil, nil)
// for an empty location, and callers treat a nil Storage as "disabled".
package attachments

import (
	"context"
	"errors"
	"io"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

// Storage persists attachment blobs keyed by an opaque, server-generated key.
// Implementations are safe for concurrent use.
type Storage interface {
	// Put streams up to size bytes from r into a freshly-generated key and
	// returns it. contentType is a hint stores may record (S3 does); it is never
	// trusted for access decisions.
	Put(ctx context.Context, r io.Reader, size int64, contentType string) (key string, err error)
	// Open returns a reader for the blob at key. The caller closes it.
	Open(ctx context.Context, key string) (io.ReadCloser, error)
	// Delete removes the blob at key. A missing blob is not an error, so delete
	// is idempotent and a half-rolled-back upload can always be cleaned up.
	Delete(ctx context.Context, key string) error
}

// ErrInvalidKey is returned when a key doesn't match the generated shape. Keys
// are server-generated, so this only fires on corruption or tampering, but it
// keeps a crafted "../" key from ever reaching the filesystem join.
var ErrInvalidKey = errors.New("attachments: invalid storage key")

// keyPattern is the exact shape newKey emits: "ab/cd/<32 hex>". Validating
// against it before any path join is the defence against traversal even though
// keys never come from the client.
var keyPattern = regexp.MustCompile(`^[0-9a-f]{2}/[0-9a-f]{2}/[0-9a-f]{32}$`)

// newKey generates a sharded storage key from a random UUID: the first two
// 2-char nibbles become directory levels so a backend never has to hold
// millions of entries in one directory (or one S3 listing prefix), with the
// full 32-hex name underneath. Collisions are astronomically unlikely.
func newKey() string {
	h := strings.ReplaceAll(uuid.NewString(), "-", "")
	return h[0:2] + "/" + h[2:4] + "/" + h
}

// validKey reports whether key matches the generated shape.
func validKey(key string) bool { return keyPattern.MatchString(key) }
