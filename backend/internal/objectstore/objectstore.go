// Package objectstore is the object-storage layer of ForgeCRM, on its own.
//
// It owns the provider contract (ObjectStore: Put, Get, Stat, Delete, PresignGet,
// List, Enabled), the two implementations that satisfy it — an in-process Memory
// and an S3-compatible S3 — and the pure helpers that decide where an object
// lives and which content types are accepted.
//
// It is deliberately free of the application layer. It imports no database, no
// audit logger, no RBAC principal and no HTTP error type: the only in-module
// dependency is internal/netguard, itself a standard-library-only leaf, used to
// refuse an S3 endpoint that resolves to a private address. The authorization,
// the metadata rows and the reconciliation between an object and its row stay in
// internal/storage, which depends on this package rather than containing it. That
// split is what makes this package extractable: a new project can take an object
// store without taking the CRM.
//
// The provider is never reached directly by a request handler. A handler goes
// through storage.Service, which enforces the tenant and permission rules. This
// package assumes a caller that already decided the request is allowed.
package objectstore

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/url"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ErrNotConfigured is returned when object storage is disabled. It is a distinct
// error so a caller can tell "off" from "broken".
var ErrNotConfigured = errors.New("object storage is not configured")

// ErrObjectMissing is returned when an object the caller expected is not in the
// store. It is the state a failed delete or a manual bucket edit leaves behind,
// and naming it lets the caller distinguish it from a store that is unreachable.
var ErrObjectMissing = errors.New("the stored object is missing")

// ObjectMeta is a listed object: enough to decide whether it is referenced and
// whether it is old enough to be considered an orphan.
type ObjectMeta struct {
	Key          string
	Size         int64
	LastModified time.Time
}

// ObjectInfo describes a stored object.
type ObjectInfo struct {
	Key         string
	Size        int64
	ContentType string
	ETag        string
}

// ObjectStore is the provider contract. Every method takes the object key, never
// a filesystem path: the key namespace belongs to this package, so a provider
// implementation cannot be handed a traversal.
type ObjectStore interface {
	// Put stores an object and returns its metadata. size may be -1 when unknown.
	Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) (ObjectInfo, error)
	// Get opens an object for reading.
	Get(ctx context.Context, key string) (io.ReadCloser, ObjectInfo, error)
	// Stat returns an object's metadata without reading it.
	Stat(ctx context.Context, key string) (ObjectInfo, error)
	// Delete removes an object. Deleting a missing object is not an error.
	Delete(ctx context.Context, key string) error
	// PresignGet returns a time-limited download URL.
	PresignGet(ctx context.Context, key string, ttl time.Duration) (*url.URL, error)
	// List returns the objects under a key prefix. It is used by reconciliation,
	// which is the only caller that walks the store rather than addressing a known key.
	List(ctx context.Context, prefix string) ([]ObjectMeta, error)
	// Enabled reports whether the store is usable.
	Enabled() bool
}

// DefaultDeniedTypes are refused when no explicit allow-list is configured. They
// are executables and active content that a CRM has no reason to serve back.
var DefaultDeniedTypes = []string{
	"application/x-msdownload",
	"application/x-executable",
	"application/x-sharedlib",
	"application/x-dosexec",
	"application/x-msdos-program",
}

// IsAllowedContentType reports whether a content type may be stored.
//
// With an allow-list, only the listed types are accepted. With no allow-list, the
// default deny-list is applied: the dangerous types are refused and everything
// else is allowed, so a deployment that has not configured a list is still not
// serving executables.
func IsAllowedContentType(contentType string, allowed []string) bool {
	if len(allowed) == 0 {
		for _, denied := range DefaultDeniedTypes {
			if strings.EqualFold(contentType, denied) {
				return false
			}
		}
		return true
	}
	for _, a := range allowed {
		if strings.EqualFold(contentType, a) {
			return true
		}
	}
	return false
}

// ObjectKey builds the storage key. The organization and document ids form the
// prefix so a listing is naturally partitioned by tenant, and the leaf is a
// sanitised extension only: the client filename never reaches the key, so a name
// containing ".." or a separator cannot escape the prefix.
func ObjectKey(orgID, docID uuid.UUID, filename string) string {
	ext := strings.ToLower(filepath.Ext(filepath.Base(filename)))
	if !IsSafeExtension(ext) {
		ext = ""
	}
	return path.Join("org", orgID.String(), "documents", docID.String()+ext)
}

// IsSafeExtension reports whether a file extension may appear in a key.
func IsSafeExtension(ext string) bool {
	if len(ext) < 2 || len(ext) > 12 {
		return false
	}
	if ext[0] != '.' {
		return false
	}
	for _, r := range ext[1:] {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

// NormaliseContentType picks a content type, preferring the declared one and
// falling back to the extension, then to a generic binary type.
func NormaliseContentType(declared, filename string) string {
	if ct := strings.TrimSpace(declared); ct != "" {
		if mt, _, err := mime.ParseMediaType(ct); err == nil {
			return mt
		}
	}
	if mt := mime.TypeByExtension(strings.ToLower(filepath.Ext(filename))); mt != "" {
		if parsed, _, err := mime.ParseMediaType(mt); err == nil {
			return parsed
		}
	}
	return "application/octet-stream"
}
