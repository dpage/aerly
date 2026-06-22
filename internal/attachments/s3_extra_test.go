package attachments

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// newTestS3Store builds an S3Store whose minio client points at the given test
// server. The endpoint is the bare host[:port] (no scheme) and SSL is off, so
// requests go to the httptest server over plain HTTP. Everything here is
// synthetic: fake bucket, fake credentials, a loopback endpoint.
func newTestS3Store(t *testing.T, srv *httptest.Server, prefix string) *S3Store {
	t.Helper()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse test server URL: %v", err)
	}
	s, err := NewS3Store(S3Config{
		Bucket:    "test-bucket",
		Prefix:    prefix,
		Endpoint:  u.Host,
		Region:    "us-east-1",
		AccessKey: "AKIAEXAMPLE0000000000",
		SecretKey: "examplesecretkey00000000000000000000",
		UseSSL:    false,
	})
	if err != nil {
		t.Fatalf("NewS3Store: %v", err)
	}
	return s
}

const goodKey = "ab/cd/00000000000000000000000000000000"

func TestS3StorePutOpenDelete(t *testing.T) {
	const blob = "blob bytes"
	var (
		gotPut    bool
		gotDelete bool
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			gotPut = true
			// Drain the (chunk-signed) upload body so the client sees a clean write.
			_, _ = io.Copy(io.Discard, r.Body)
			// A bare-bones successful PutObject response: 200 with an ETag.
			w.Header().Set("ETag", `"d41d8cd98f00b204e9800998ecf8427e"`)
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			// GetObject is lazy; the bytes are read by the caller via the reader.
			w.Header().Set("Content-Type", "text/plain")
			w.Header().Set("Content-Length", strconv.Itoa(len(blob)))
			w.Header().Set("ETag", `"d41d8cd98f00b204e9800998ecf8427e"`)
			w.Header().Set("Last-Modified", "Mon, 02 Jan 2006 15:04:05 GMT")
			w.Header().Set("Accept-Ranges", "bytes")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(blob))
		case http.MethodDelete:
			gotDelete = true
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	s := newTestS3Store(t, srv, "")
	ctx := context.Background()

	payload := []byte("hello s3")
	key, err := s.Put(ctx, bytes.NewReader(payload), int64(len(payload)), "text/plain")
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if !validKey(key) {
		t.Fatalf("Put returned malformed key %q", key)
	}
	if !gotPut {
		t.Fatal("server never saw a PUT")
	}

	rc, err := s.Open(ctx, key)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if !bytes.Equal(got, []byte(blob)) {
		t.Errorf("Open read %q, want %q", got, blob)
	}

	if err := s.Delete(ctx, key); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if !gotDelete {
		t.Fatal("server never saw a DELETE")
	}
}

func TestS3StoreOpenReadMissing(t *testing.T) {
	// A 404 from the endpoint surfaces on the first Read of the lazy reader, not
	// from Open itself; assert the read fails so the missing-object path is real.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`<?xml version="1.0"?><Error><Code>NoSuchKey</Code></Error>`))
	}))
	defer srv.Close()

	s := newTestS3Store(t, srv, "")
	rc, err := s.Open(context.Background(), goodKey)
	if err != nil {
		t.Fatalf("Open (lazy) unexpected err: %v", err)
	}
	defer rc.Close()
	if _, err := io.ReadAll(rc); err == nil {
		t.Fatal("reading a missing object should fail")
	}
}

func TestS3StorePutError(t *testing.T) {
	// The endpoint rejects the upload; Put must wrap and surface the error.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`<?xml version="1.0"?><Error><Code>AccessDenied</Code></Error>`))
	}))
	defer srv.Close()

	s := newTestS3Store(t, srv, "")
	_, err := s.Put(context.Background(), strings.NewReader("x"), 1, "text/plain")
	if err == nil {
		t.Fatal("Put against a 403 endpoint should error")
	}
	if !strings.Contains(err.Error(), "s3 put") {
		t.Errorf("error %v should mention s3 put", err)
	}
}

func TestS3StoreDeleteError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`<?xml version="1.0"?><Error><Code>AccessDenied</Code></Error>`))
	}))
	defer srv.Close()

	s := newTestS3Store(t, srv, "")
	err := s.Delete(context.Background(), goodKey)
	if err == nil {
		t.Fatal("Delete against a 403 endpoint should error")
	}
	if !strings.Contains(err.Error(), "s3 delete") {
		t.Errorf("error %v should mention s3 delete", err)
	}
}

func TestS3StoreInvalidKeyPaths(t *testing.T) {
	// object()/Put-Open-Delete reject keys that fail validKey before any network.
	s := newTestS3Store(t, httptest.NewServer(http.NotFoundHandler()), "")
	ctx := context.Background()
	if _, err := s.Open(ctx, "../../etc/passwd"); !errors.Is(err, ErrInvalidKey) {
		t.Errorf("Open bad key err = %v, want ErrInvalidKey", err)
	}
	if err := s.Delete(ctx, "../../etc/passwd"); !errors.Is(err, ErrInvalidKey) {
		t.Errorf("Delete bad key err = %v, want ErrInvalidKey", err)
	}
}

func TestNewS3StoreDefaultEndpoint(t *testing.T) {
	// Empty Endpoint takes the AWS regional-host default branch; construction is
	// offline, so this just exercises the fallback without any network call.
	s, err := NewS3Store(S3Config{
		Bucket: "b", Region: "eu-west-2",
		AccessKey: "ak", SecretKey: "sk", UseSSL: true,
	})
	if err != nil {
		t.Fatalf("NewS3Store default endpoint: %v", err)
	}
	if s.bucket != "b" {
		t.Errorf("bucket = %q, want b", s.bucket)
	}
}

func TestNewS3StoreBadEndpoint(t *testing.T) {
	// A malformed endpoint (scheme included) makes minio.New fail, hitting the
	// constructor's error branch.
	_, err := NewS3Store(S3Config{
		Bucket: "b", Endpoint: "http://bad endpoint/", Region: "us-east-1",
		AccessKey: "ak", SecretKey: "sk",
	})
	if err == nil {
		t.Fatal("NewS3Store with a malformed endpoint should error")
	}
}
