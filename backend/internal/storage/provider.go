package storage

import (
	"github.com/geff1778/forgecrm/internal/objectstore"
)

// The provider contract and its implementations (Memory, S3) live in
// internal/objectstore. They are re-exported here so the public API of this
// package is unchanged: every existing caller keeps using storage.Memory,
// storage.S3, storage.NewS3, storage.NewMemory and the storage.S3Config, and the
// application layer keeps its authorization, metadata and reconciliation rules.

// Memory is an in-process ObjectStore.
type Memory = objectstore.Memory

// S3 is an ObjectStore backed by any S3-compatible server.
type S3 = objectstore.S3

// S3Config configures an S3-compatible store.
type S3Config = objectstore.S3Config

// NewMemory builds an empty in-memory store.
func NewMemory() *Memory { return objectstore.NewMemory() }

// NewS3 builds an S3-compatible store.
func NewS3(cfg S3Config) (*S3, error) { return objectstore.NewS3(cfg) }
