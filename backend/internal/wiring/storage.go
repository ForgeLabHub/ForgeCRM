package wiring

import (
	"context"
	"log/slog"
	"time"

	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/config"
	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/storage"
)

// BuildStorage constructs the attachment service.
//
// The result is always non-nil. A deployment that has not configured object
// storage gets a service that reports itself off, which lets the capabilities
// endpoint say "storage: off" and the document routes answer 503, rather than a
// nil dereference at the first upload. The bucket is created when it is missing so
// a deployment against a fresh MinIO works without a manual step.
func BuildStorage(cfg *config.Config, db *database.DB, auditLog *audit.Logger, log *slog.Logger) (*storage.Service, error) {
	svcCfg := storage.Config{
		Enabled:        cfg.Storage.Enabled,
		MaxObjectBytes: cfg.Storage.MaxObjectBytes,
		PresignTTL:     cfg.Storage.PresignTTL,
	}
	if !cfg.Storage.Enabled {
		return storage.New(db, nil, auditLog, log, svcCfg), nil
	}

	store, err := storage.NewS3(storage.S3Config{
		Endpoint:             cfg.Storage.Endpoint,
		Region:               cfg.Storage.Region,
		Bucket:               cfg.Storage.Bucket,
		AccessKeyID:          cfg.Storage.AccessKeyID,
		SecretAccessKey:      cfg.Storage.SecretAccessKey,
		UsePathStyle:         cfg.Storage.UsePathStyle,
		AllowPrivateEndpoint: cfg.Storage.AllowPrivateEndpoint,
		Timeout:              cfg.Storage.Timeout,
	})
	if err != nil {
		return nil, err
	}

	// Create the bucket at startup when it is missing. A failure here is a
	// configuration problem the operator should see immediately, not on the first
	// upload: an endpoint that is reachable but has no bucket is a deployment that
	// cannot store anything.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := store.EnsureBucket(ctx); err != nil {
		return nil, err
	}
	return storage.New(db, store, auditLog, log, svcCfg), nil
}
