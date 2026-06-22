package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dpage/aerly/internal/attachments"
	"github.com/dpage/aerly/internal/config"
)

func coverSrvIsFS(t *testing.T, s attachments.Storage) {
	t.Helper()
	if _, ok := s.(*attachments.FSStore); !ok {
		t.Fatalf("expected *attachments.FSStore, got %T", s)
	}
}

func coverSrvIsS3(t *testing.T, s attachments.Storage) {
	t.Helper()
	if _, ok := s.(*attachments.S3Store); !ok {
		t.Fatalf("expected *attachments.S3Store, got %T", s)
	}
}

// TestBuildAttachmentStoreFS covers the filesystem branch of
// buildAttachmentStore, including a successful build into a temp dir.
func TestBuildAttachmentStoreFS(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{AttachmentsStore: filepath.Join(dir, "blobs")}
	store, err := buildAttachmentStore(cfg)
	if err != nil {
		t.Fatalf("buildAttachmentStore (fs): %v", err)
	}
	coverSrvIsFS(t, store)
}

// TestBuildAttachmentStoreFSError covers the filesystem error path: a root that
// cannot be created because a parent path component is a regular file.
func TestBuildAttachmentStoreFSError(t *testing.T) {
	dir := t.TempDir()
	notDir := filepath.Join(dir, "file")
	if err := os.WriteFile(notDir, []byte("x"), 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	cfg := &config.Config{AttachmentsStore: filepath.Join(notDir, "child")}
	if _, err := buildAttachmentStore(cfg); err == nil {
		t.Fatal("expected buildAttachmentStore to fail when the parent is a file")
	}
}

// TestBuildAttachmentStoreS3 covers the S3 branch. minio.New does not connect,
// so a synthetic bucket/credentials build a client without network access.
func TestBuildAttachmentStoreS3(t *testing.T) {
	cfg := &config.Config{
		AttachmentsStore:       "s3://test-bucket/prefix",
		AttachmentsS3Endpoint:  "127.0.0.1:9000",
		AttachmentsS3Region:    "us-east-1",
		AttachmentsS3AccessKey: "AKIATESTONLY000000",
		AttachmentsS3SecretKey: "test-secret-key-not-real",
		AttachmentsS3UseSSL:    false,
	}
	store, err := buildAttachmentStore(cfg)
	if err != nil {
		t.Fatalf("buildAttachmentStore (s3): %v", err)
	}
	coverSrvIsS3(t, store)
}
