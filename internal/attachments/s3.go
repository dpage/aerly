package attachments

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// S3Config configures the S3 (or S3-compatible) backend. Bucket/Prefix come from
// the ATTACHMENTS_STORE s3:// URL; the rest from the ATTACHMENTS_S3_* env vars.
type S3Config struct {
	Bucket    string
	Prefix    string // optional key prefix within the bucket, no leading/trailing slash
	Endpoint  string // host[:port], no scheme; empty → AWS default for Region
	Region    string
	AccessKey string
	SecretKey string
	UseSSL    bool
}

// S3Store stores blobs as objects in an S3 bucket via the minio client (which
// speaks plain S3, so it works against AWS and any S3-compatible endpoint).
type S3Store struct {
	client *minio.Client
	bucket string
	prefix string
}

// NewS3Store builds an S3-backed Storage. It does not verify connectivity
// eagerly (no bucket round-trip at startup); the first upload surfaces any
// credential or endpoint problem.
func NewS3Store(cfg S3Config) (*S3Store, error) {
	endpoint := cfg.Endpoint
	if endpoint == "" {
		// AWS regional endpoint; us-east-1 also answers on the global host.
		endpoint = "s3." + cfg.Region + ".amazonaws.com"
	}
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
		Region: cfg.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("attachments: s3 client: %w", err)
	}
	return &S3Store{client: client, bucket: cfg.Bucket, prefix: cfg.Prefix}, nil
}

func (s *S3Store) object(key string) (string, error) {
	if !validKey(key) {
		return "", ErrInvalidKey
	}
	if s.prefix == "" {
		return key, nil
	}
	return s.prefix + "/" + key, nil
}

func (s *S3Store) Put(ctx context.Context, r io.Reader, size int64, contentType string) (string, error) {
	key := newKey()
	obj, err := s.object(key)
	if err != nil {
		return "", err
	}
	if _, err := s.client.PutObject(ctx, s.bucket, obj, r, size, minio.PutObjectOptions{
		ContentType: contentType,
	}); err != nil {
		return "", fmt.Errorf("attachments: s3 put: %w", err)
	}
	return key, nil
}

func (s *S3Store) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	obj, err := s.object(key)
	if err != nil {
		return nil, err
	}
	// GetObject is lazy: it returns immediately and surfaces a missing-object or
	// auth error on the first Read. That's fine for a streamed download.
	o, err := s.client.GetObject(ctx, s.bucket, obj, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("attachments: s3 get: %w", err)
	}
	return o, nil
}

func (s *S3Store) Delete(ctx context.Context, key string) error {
	obj, err := s.object(key)
	if err != nil {
		return err
	}
	if err := s.client.RemoveObject(ctx, s.bucket, obj, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("attachments: s3 delete: %w", err)
	}
	return nil
}

// ParseS3URL splits an "s3://bucket[/prefix...]" URL into its bucket and
// (slash-trimmed) prefix. An empty bucket means the URL is malformed.
func ParseS3URL(raw string) (bucket, prefix string) {
	rest := strings.TrimPrefix(raw, "s3://")
	rest = strings.TrimPrefix(rest, "/")
	bucket, prefix, _ = strings.Cut(rest, "/")
	prefix = strings.Trim(prefix, "/")
	return bucket, prefix
}
