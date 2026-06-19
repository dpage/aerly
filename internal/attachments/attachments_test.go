package attachments

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewKeyShape(t *testing.T) {
	for i := 0; i < 100; i++ {
		k := newKey()
		if !validKey(k) {
			t.Fatalf("generated key %q does not match the expected shape", k)
		}
		// "ab/cd/<32 hex>" → two shard dirs plus the full name.
		if parts := strings.Split(k, "/"); len(parts) != 3 ||
			len(parts[0]) != 2 || len(parts[1]) != 2 || len(parts[2]) != 32 {
			t.Fatalf("key %q is not sharded as expected", k)
		}
	}
}

func TestValidKeyRejectsTraversal(t *testing.T) {
	bad := []string{
		"", "..", "../etc/passwd", "ab/cd/" + strings.Repeat("g", 32),
		"AB/CD/" + strings.Repeat("a", 32), "ab/cd/short", "ab/cd/../../x",
	}
	for _, k := range bad {
		if validKey(k) {
			t.Errorf("validKey(%q) = true, want false", k)
		}
	}
}

func TestFSStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := NewFSStore(dir)
	if err != nil {
		t.Fatalf("NewFSStore: %v", err)
	}
	ctx := context.Background()
	payload := []byte("hello, attachment world")
	key, err := s.Put(ctx, bytes.NewReader(payload), int64(len(payload)), "text/plain")
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if !validKey(key) {
		t.Fatalf("Put returned malformed key %q", key)
	}
	// The blob lands at root/<sharded key>.
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(key))); err != nil {
		t.Fatalf("blob not on disk: %v", err)
	}

	rc, err := s.Open(ctx, key)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if !bytes.Equal(got, payload) {
		t.Fatalf("read back %q, want %q", got, payload)
	}

	if err := s.Delete(ctx, key); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	// Delete is idempotent — a second delete of a gone blob is not an error.
	if err := s.Delete(ctx, key); err != nil {
		t.Fatalf("second Delete: %v", err)
	}
	if _, err := s.Open(ctx, key); err == nil {
		t.Fatal("Open after Delete should fail")
	}
}

func TestFSStoreRejectsInvalidKey(t *testing.T) {
	s, err := NewFSStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFSStore: %v", err)
	}
	ctx := context.Background()
	if _, err := s.Open(ctx, "../../etc/passwd"); !errors.Is(err, ErrInvalidKey) {
		t.Errorf("Open bad key err = %v, want ErrInvalidKey", err)
	}
	if err := s.Delete(ctx, "../../etc/passwd"); !errors.Is(err, ErrInvalidKey) {
		t.Errorf("Delete bad key err = %v, want ErrInvalidKey", err)
	}
}

func TestParseS3URL(t *testing.T) {
	cases := []struct {
		raw, bucket, prefix string
	}{
		{"s3://my-bucket", "my-bucket", ""},
		{"s3://my-bucket/", "my-bucket", ""},
		{"s3://my-bucket/some/prefix", "my-bucket", "some/prefix"},
		{"s3://my-bucket/some/prefix/", "my-bucket", "some/prefix"},
		{"s3:///just-prefix", "just-prefix", ""},
	}
	for _, c := range cases {
		b, p := ParseS3URL(c.raw)
		if b != c.bucket || p != c.prefix {
			t.Errorf("ParseS3URL(%q) = (%q,%q), want (%q,%q)", c.raw, b, p, c.bucket, c.prefix)
		}
	}
}

func TestNewS3StoreConfigures(t *testing.T) {
	// Constructing the client is offline (no network round-trip); just assert it
	// builds and the object key gets the configured prefix.
	s, err := NewS3Store(S3Config{
		Bucket: "b", Prefix: "p/q", Region: "us-east-1",
		AccessKey: "ak", SecretKey: "sk", UseSSL: true,
	})
	if err != nil {
		t.Fatalf("NewS3Store: %v", err)
	}
	obj, err := s.object("ab/cd/" + strings.Repeat("a", 32))
	if err != nil {
		t.Fatalf("object: %v", err)
	}
	if want := "p/q/ab/cd/" + strings.Repeat("a", 32); obj != want {
		t.Errorf("object = %q, want %q", obj, want)
	}
	if _, err := s.object("bad-key"); !errors.Is(err, ErrInvalidKey) {
		t.Errorf("object(bad) err = %v, want ErrInvalidKey", err)
	}
}
