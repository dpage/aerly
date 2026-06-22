package attachments

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewFSStoreMkdirFails(t *testing.T) {
	// Point the root at a path whose parent is a regular file, so MkdirAll can't
	// create it and the constructor surfaces the wrapped error.
	f := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
		t.Fatalf("write fixture file: %v", err)
	}
	if _, err := NewFSStore(filepath.Join(f, "child")); err == nil {
		t.Fatal("NewFSStore under a file should error")
	} else if !strings.Contains(err.Error(), "create root") {
		t.Errorf("error %v should mention create root", err)
	}
}

func TestFSStorePutMkdirFails(t *testing.T) {
	// Make the store root unwritable so Put's per-key MkdirAll of the shard dirs
	// fails. Skipped when running as root, which ignores the permission bits.
	if os.Geteuid() == 0 {
		t.Skip("running as root: directory permissions are not enforced")
	}
	dir := t.TempDir()
	s, err := NewFSStore(dir)
	if err != nil {
		t.Fatalf("NewFSStore: %v", err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod root: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	_, err = s.Put(context.Background(), strings.NewReader("data"), 4, "text/plain")
	if err == nil {
		t.Fatal("Put into a read-only root should error")
	}
	if !strings.Contains(err.Error(), "mkdir") {
		t.Errorf("error %v should mention mkdir", err)
	}
}

func TestFSStorePutCopyFailsThenCleanup(t *testing.T) {
	// A reader that errors mid-stream makes io.Copy fail; Put must clean up the
	// half-written blob and surface a wrapped "write blob" error.
	dir := t.TempDir()
	s, err := NewFSStore(dir)
	if err != nil {
		t.Fatalf("NewFSStore: %v", err)
	}
	_, err = s.Put(context.Background(), errReader{}, 10, "text/plain")
	if err == nil {
		t.Fatal("Put with a failing reader should error")
	}
	if !strings.Contains(err.Error(), "write blob") {
		t.Errorf("error %v should mention write blob", err)
	}
	// No stray blob files left behind: the only entries are the shard dirs, which
	// should hold nothing (the leaf file was removed on the copy failure).
	var files int
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, _ error) error {
		if info != nil && !info.IsDir() {
			files++
		}
		return nil
	})
	if files != 0 {
		t.Errorf("found %d leftover blob files, want 0", files)
	}
}

func TestFSStoreDeleteRealError(t *testing.T) {
	// Removing a key whose parent directory is not writable yields a non-IsNotExist
	// error, exercising Delete's error-wrapping branch.
	if os.Geteuid() == 0 {
		t.Skip("running as root: directory permissions are not enforced")
	}
	dir := t.TempDir()
	s, err := NewFSStore(dir)
	if err != nil {
		t.Fatalf("NewFSStore: %v", err)
	}
	ctx := context.Background()
	payload := []byte("to be deleted")
	key, err := s.Put(ctx, bytes.NewReader(payload), int64(len(payload)), "text/plain")
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	// Lock the leaf shard dir so os.Remove of the blob fails with EACCES.
	leaf := filepath.Dir(filepath.Join(dir, filepath.FromSlash(key)))
	if err := os.Chmod(leaf, 0o500); err != nil {
		t.Fatalf("chmod leaf: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(leaf, 0o700) })

	if err := s.Delete(ctx, key); err == nil {
		t.Fatal("Delete of a blob in a read-only dir should error")
	} else if !strings.Contains(err.Error(), "delete blob") {
		t.Errorf("error %v should mention delete blob", err)
	}
}

// errReader fails on first Read, simulating a truncated/broken upload stream.
type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errTestRead }

var errTestRead = &readError{}

type readError struct{}

func (*readError) Error() string { return "simulated read failure" }
