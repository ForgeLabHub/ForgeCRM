package objectstore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/geff1778/forgecrm/internal/netguard"
)

// S3Config configures an S3-compatible store.
type S3Config struct {
	Endpoint        string
	Region          string
	Bucket          string
	AccessKeyID     string
	SecretAccessKey string
	UsePathStyle    bool
	// AllowPrivateEndpoint permits an endpoint on a private or loopback address.
	// Off by default: the endpoint is operator-supplied and therefore an SSRF target,
	// so it is checked with the same rules as an integration destination unless the
	// deployment explicitly says it runs MinIO on its own network.
	AllowPrivateEndpoint bool
	Timeout              time.Duration
}

// S3 is an ObjectStore backed by any S3-compatible server.
type S3 struct {
	client  *minio.Client
	bucket  string
	timeout time.Duration
}

// S3 must satisfy the provider contract. The assertion fails at compile time if a
// method signature drifts, rather than at the first upload in production.
var _ ObjectStore = (*S3)(nil)

// NewS3 builds an S3-compatible store.
//
// The endpoint is validated before a client is built. The guard runs even when the
// endpoint is only reached at request time, because a misconfigured endpoint
// should fail at startup with a clear message rather than on the first upload.
func NewS3(cfg S3Config) (*S3, error) {
	if strings.TrimSpace(cfg.Endpoint) == "" {
		return nil, errors.New("storage: an S3 endpoint is required")
	}
	if strings.TrimSpace(cfg.Bucket) == "" {
		return nil, errors.New("storage: an S3 bucket is required")
	}

	endpoint, secure, err := normaliseEndpoint(cfg.Endpoint)
	if err != nil {
		return nil, err
	}
	if !cfg.AllowPrivateEndpoint {
		if err := guardEndpoint(endpoint); err != nil {
			return nil, err
		}
	}

	region := cfg.Region
	if region == "" {
		region = "us-east-1"
	}

	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		Secure: secure,
		Region: region,
	})
	if err != nil {
		return nil, fmt.Errorf("storage: build the S3 client: %w", err)
	}
	client.SetAppInfo("forgecrm", "1")

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &S3{client: client, bucket: cfg.Bucket, timeout: timeout}, nil
}

// Enabled reports whether the store is usable. A nil store is not.
func (s *S3) Enabled() bool { return s != nil && s.client != nil }

func (s *S3) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if s.timeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, s.timeout)
}

// Put stores an object.
//
// The size is passed through to the provider so a large file is uploaded in parts
// rather than buffered; the provider reads the stream and reports the true size in
// the returned info, which the caller checks against its own limit.
func (s *S3) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) (ObjectInfo, error) {
	if !s.Enabled() {
		return ObjectInfo{}, ErrNotConfigured
	}
	ctx, cancel := s.withTimeout(ctx)
	defer cancel()

	opts := minio.PutObjectOptions{ContentType: contentType}
	info, err := s.client.PutObject(ctx, s.bucket, key, r, size, opts)
	if err != nil {
		return ObjectInfo{}, fmt.Errorf("storage: put %s: %w", key, err)
	}
	return ObjectInfo{
		Key:         key,
		Size:        info.Size,
		ContentType: contentType,
		ETag:        info.ETag,
	}, nil
}

// Get opens an object for reading.
func (s *S3) Get(ctx context.Context, key string) (io.ReadCloser, ObjectInfo, error) {
	if !s.Enabled() {
		return nil, ObjectInfo{}, ErrNotConfigured
	}
	ctx, cancel := s.withTimeout(ctx)

	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		cancel()
		return nil, ObjectInfo{}, mapS3Error(err)
	}
	// GetObject is lazy: the request is only sent when the object is first read, so
	// Stat is called now to turn a missing object into an error at the call site
	// rather than halfway through a response body.
	info, err := obj.Stat()
	if err != nil {
		obj.Close()
		cancel()
		return nil, ObjectInfo{}, mapS3Error(err)
	}
	return &cancelReadCloser{ReadCloser: obj, cancel: cancel}, ObjectInfo{
		Key:         key,
		Size:        info.Size,
		ContentType: info.ContentType,
		ETag:        info.ETag,
	}, nil
}

// Stat returns an object's metadata.
func (s *S3) Stat(ctx context.Context, key string) (ObjectInfo, error) {
	if !s.Enabled() {
		return ObjectInfo{}, ErrNotConfigured
	}
	ctx, cancel := s.withTimeout(ctx)
	defer cancel()

	info, err := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		return ObjectInfo{}, mapS3Error(err)
	}
	return ObjectInfo{
		Key:         key,
		Size:        info.Size,
		ContentType: info.ContentType,
		ETag:        info.ETag,
	}, nil
}

// Delete removes an object. A missing object is not an error, so the method is
// idempotent and a retried archive does not fail on its second attempt.
func (s *S3) Delete(ctx context.Context, key string) error {
	if !s.Enabled() {
		return ErrNotConfigured
	}
	ctx, cancel := s.withTimeout(ctx)
	defer cancel()

	err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{})
	if err != nil && !isNotFound(err) {
		return fmt.Errorf("storage: delete %s: %w", key, err)
	}
	return nil
}

// PresignGet returns a time-limited download URL.
func (s *S3) PresignGet(ctx context.Context, key string, ttl time.Duration) (*url.URL, error) {
	if !s.Enabled() {
		return nil, ErrNotConfigured
	}
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	ctx, cancel := s.withTimeout(ctx)
	defer cancel()

	u, err := s.client.PresignedGetObject(ctx, s.bucket, key, ttl, nil)
	if err != nil {
		return nil, fmt.Errorf("storage: presign %s: %w", key, err)
	}
	return u, nil
}

// List returns the objects under a prefix.
//
// The S3 list API is paginated; the loop follows the continuation token so a
// bucket with more objects than one page reports does not silently return a
// partial listing, which reconciliation would then treat as a complete set.
func (s *S3) List(ctx context.Context, prefix string) ([]ObjectMeta, error) {
	if !s.Enabled() {
		return nil, ErrNotConfigured
	}
	ctx, cancel := s.withTimeout(ctx)
	defer cancel()

	var out []ObjectMeta
	for obj := range s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	}) {
		if obj.Err != nil {
			return nil, fmt.Errorf("storage: list %s: %w", prefix, obj.Err)
		}
		out = append(out, ObjectMeta{
			Key:          obj.Key,
			Size:         obj.Size,
			LastModified: obj.LastModified,
		})
	}
	return out, nil
}

// EnsureBucket creates the bucket when it is missing. It is called at startup so a
// deployment against a fresh MinIO works without a manual step; an existing bucket
// is left alone.
func (s *S3) EnsureBucket(ctx context.Context) error {
	if !s.Enabled() {
		return ErrNotConfigured
	}
	ctx, cancel := s.withTimeout(ctx)
	defer cancel()

	exists, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return fmt.Errorf("storage: check bucket %q: %w", s.bucket, err)
	}
	if exists {
		return nil
	}
	if err := s.client.MakeBucket(ctx, s.bucket, minio.MakeBucketOptions{}); err != nil {
		// A concurrent creator can win the race; the bucket existing is the goal, so
		// that is not a failure.
		if exists, checkErr := s.client.BucketExists(ctx, s.bucket); checkErr == nil && exists {
			return nil
		}
		return fmt.Errorf("storage: create bucket %q: %w", s.bucket, err)
	}
	return nil
}

// cancelReadCloser cancels the operation context when the body is closed, so the
// timeout timer does not outlive the read.
type cancelReadCloser struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c *cancelReadCloser) Close() error {
	err := c.ReadCloser.Close()
	c.cancel()
	return err
}

// normaliseEndpoint accepts a URL or a bare host:port and returns the host:port
// form minio expects plus whether TLS is used.
func normaliseEndpoint(raw string) (endpoint string, secure bool, err error) {
	raw = strings.TrimSpace(raw)
	if !strings.Contains(raw, "://") {
		// A bare host:port is assumed to be TLS, which is the safe assumption for a
		// public endpoint. A local test server passes an explicit http:// URL.
		return raw, true, nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", false, fmt.Errorf("storage: parse the endpoint: %w", err)
	}
	if u.Host == "" {
		return "", false, errors.New("storage: the endpoint has no host")
	}
	switch u.Scheme {
	case "https":
		return u.Host, true, nil
	case "http":
		return u.Host, false, nil
	default:
		return "", false, fmt.Errorf("storage: unsupported endpoint scheme %q", u.Scheme)
	}
}

// guardEndpoint refuses an endpoint that resolves to a private or loopback
// address. It uses the shared outbound guard so there is one definition of
// "internal" in the codebase.
func guardEndpoint(endpoint string) error {
	host, _, err := net.SplitHostPort(endpoint)
	if err != nil {
		host = endpoint
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("storage: resolve the endpoint %q: %w", host, err)
	}
	for _, ip := range ips {
		if netguard.IsBlockedIP(ip) {
			return fmt.Errorf(
				"storage: the endpoint %q resolves to %s, which is a private or loopback address; "+
					"set FORGECRM_STORAGE_ALLOW_PRIVATE_ENDPOINT=true to permit a self-hosted store",
				host, ip)
		}
	}
	return nil
}

// mapS3Error turns a provider error into a domain error. A missing object becomes
// ErrObjectMissing so a caller can distinguish "the object is gone" from "the
// store is unreachable", which are different failures with different remedies.
func mapS3Error(err error) error {
	if err == nil {
		return nil
	}
	if isNotFound(err) {
		return fmt.Errorf("%w: %v", ErrObjectMissing, err)
	}
	return err
}

func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	resp := minio.ToErrorResponse(err)
	switch resp.StatusCode {
	case 404:
		return true
	}
	switch resp.Code {
	case "NoSuchKey", "NoSuchBucket", "NotFound":
		return true
	}
	return false
}
