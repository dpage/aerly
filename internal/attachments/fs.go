package attachments

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// FSStore stores blobs as files under a root directory, sharded by the key's
// two leading nibble-directories so no single directory grows without bound.
type FSStore struct {
	root string
}

// NewFSStore prepares (creating if needed) the root directory and returns a
// filesystem-backed Storage. The root must be an absolute path; the caller
// (config) validates that.
func NewFSStore(root string) (*FSStore, error) {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("attachments: create root %q: %w", root, err)
	}
	return &FSStore{root: root}, nil
}

func (s *FSStore) path(key string) (string, error) {
	if !validKey(key) {
		return "", ErrInvalidKey
	}
	return filepath.Join(s.root, filepath.FromSlash(key)), nil
}

func (s *FSStore) Put(ctx context.Context, r io.Reader, size int64, contentType string) (string, error) {
	_ = ctx
	_ = contentType
	key := newKey()
	full, err := s.path(key)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		return "", fmt.Errorf("attachments: mkdir: %w", err)
	}
	// O_EXCL so a (vanishingly unlikely) key collision fails loudly rather than
	// silently overwriting another plan's attachment.
	f, err := os.OpenFile(full, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", fmt.Errorf("attachments: create blob: %w", err)
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		os.Remove(full)
		return "", fmt.Errorf("attachments: write blob: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(full)
		return "", fmt.Errorf("attachments: close blob: %w", err)
	}
	return key, nil
}

func (s *FSStore) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	_ = ctx
	full, err := s.path(key)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(full)
	if err != nil {
		return nil, fmt.Errorf("attachments: open blob: %w", err)
	}
	return f, nil
}

func (s *FSStore) Delete(ctx context.Context, key string) error {
	_ = ctx
	full, err := s.path(key)
	if err != nil {
		return err
	}
	if err := os.Remove(full); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("attachments: delete blob: %w", err)
	}
	return nil
}
