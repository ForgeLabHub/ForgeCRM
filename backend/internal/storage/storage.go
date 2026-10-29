// Package storage implements CRM file attachments on an S3-compatible object
// store.
//
// The package has two layers. ObjectStore is the provider contract: Put, Get,
// Stat, Delete and PresignGet. The S3 implementation lives in s3.go and speaks
// the S3 API, so it works against AWS S3, MinIO, Ceph RGW and any other
// S3-compatible server without a code change — only the endpoint differs.
//
// Service is the application layer. It owns the authorization, the metadata rows
// in the documents table, the size and type limits and the reconciliation between
// the object and the row. A caller never touches ObjectStore directly, because the
// rules that keep one organization's files out of another's reach live here.
package storage

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/httpx"
	"github.com/geff1778/forgecrm/internal/objectstore"
	"github.com/geff1778/forgecrm/internal/rbac"
)

// The provider contract and its implementations (Memory, S3) live in
// internal/objectstore, which has no database, audit or RBAC dependency and can be
// extracted on its own. They are re-exported here so every existing caller keeps
// using storage.ObjectStore, storage.Memory, storage.S3 and so on, and the public
// API of this package is unchanged.
var (
	// ErrNotConfigured is returned when object storage is disabled.
	ErrNotConfigured = objectstore.ErrNotConfigured
	// ErrObjectMissing is returned when a row exists but the object does not.
	ErrObjectMissing = objectstore.ErrObjectMissing
)

// Entity types a document may be attached to. A document with no entity is a
// free-standing attachment owned by the organization.
const (
	EntityAccount  = "account"
	EntityContact  = "contact"
	EntityDeal     = "deal"
	EntityTicket   = "ticket"
	EntityLead     = "lead"
	EntityTask     = "task"
	EntityDocument = "document"
)

// entityPermissions maps an attachable entity to the permission required to read
// the parent record. A document attached to an entity is only readable by a
// principal who could read that entity, so the entity's own permission is checked
// in addition to documents.read.
var entityPermissions = map[string]rbac.Permission{
	EntityAccount: rbac.PermAccountsRead,
	EntityContact: rbac.PermContactsRead,
	EntityDeal:    rbac.PermDealsRead,
	EntityTicket:  rbac.PermTicketsRead,
	EntityLead:    rbac.PermLeadsRead,
	EntityTask:    rbac.PermTasksRead,
}

// ObjectMeta, ObjectInfo and ObjectStore are the provider contract, defined in
// internal/objectstore and aliased here for the existing callers.
type ObjectMeta = objectstore.ObjectMeta
type ObjectInfo = objectstore.ObjectInfo
type ObjectStore = objectstore.ObjectStore

// Config configures the service.
type Config struct {
	Enabled        bool
	MaxObjectBytes int64
	PresignTTL     time.Duration
	// AllowedContentTypes, when non-empty, is the allow-list. A type not in the
	// list is refused with a validation error naming the type.
	AllowedContentTypes []string
}

// Service is the application layer over an ObjectStore.
type Service struct {
	db     *database.DB
	store  ObjectStore
	audit  *audit.Logger
	log    *slog.Logger
	cfg    Config
	now    func() time.Time
	random io.Reader
}

// New builds the service. A nil store or a disabled configuration yields a service
// that reports itself off rather than one that panics, so the diagnostics endpoint
// and the document routes can both ask whether storage is available.
func New(db *database.DB, store ObjectStore, auditLog *audit.Logger, log *slog.Logger, cfg Config) *Service {
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Service{
		db:     db,
		store:  store,
		audit:  auditLog,
		log:    log,
		cfg:    cfg,
		now:    time.Now,
		random: rand.Reader,
	}
}

// Enabled reports whether the service can store objects. It tolerates a nil
// receiver so an unconstructed subsystem reports "off".
func (s *Service) Enabled() bool {
	return s != nil && s.cfg.Enabled && s.store != nil && s.store.Enabled()
}

// MaxObjectBytes is the configured single-object limit.
func (s *Service) MaxObjectBytes() int64 {
	if s == nil {
		return 0
	}
	return s.cfg.MaxObjectBytes
}

// Document is a stored attachment.
type Document struct {
	ID             uuid.UUID `json:"id"`
	OrganizationID uuid.UUID `json:"organization_id"`
	Filename       string    `json:"filename"`
	ContentType    string    `json:"content_type"`
	SizeBytes      int64     `json:"size_bytes"`
	ChecksumSHA256 string    `json:"checksum_sha256"`
	// StoragePath is the object key. It is internal: it is never serialised to a
	// client, because it names a bucket layout the caller has no business knowing.
	StoragePath string     `json:"-"`
	Title       *string    `json:"title,omitempty"`
	AccountID   *uuid.UUID `json:"account_id,omitempty"`
	DealID      *uuid.UUID `json:"deal_id,omitempty"`
	ContactID   *uuid.UUID `json:"contact_id,omitempty"`
	TicketID    *uuid.UUID `json:"ticket_id,omitempty"`
	LeadID      *uuid.UUID `json:"lead_id,omitempty"`
	TaskID      *uuid.UUID `json:"task_id,omitempty"`
	UploadedBy  *uuid.UUID `json:"uploaded_by,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	ArchivedAt  *time.Time `json:"archived_at,omitempty"`
}

const documentColumns = `id, organization_id, filename, content_type, size_bytes,
	checksum_sha256, title, account_id, deal_id, contact_id, ticket_id, lead_id, task_id,
	uploaded_by, created_at, archived_at`

func scanDocument(row pgx.Row) (*Document, error) {
	var d Document
	err := row.Scan(&d.ID, &d.OrganizationID, &d.Filename, &d.ContentType, &d.SizeBytes,
		&d.ChecksumSHA256, &d.Title, &d.AccountID, &d.DealID, &d.ContactID, &d.TicketID,
		&d.LeadID, &d.TaskID, &d.UploadedBy, &d.CreatedAt, &d.ArchivedAt)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// UploadInput describes an upload. At most one entity id may be set; none means a
// free-standing organization document.
type UploadInput struct {
	Filename    string
	ContentType string
	Title       string
	Size        int64
	AccountID   *uuid.UUID
	ContactID   *uuid.UUID
	DealID      *uuid.UUID
	TicketID    *uuid.UUID
	LeadID      *uuid.UUID
	TaskID      *uuid.UUID
}

// entityRef returns the entity type and id the document is attached to.
func (in UploadInput) entityRef() (string, *uuid.UUID, error) {
	set := 0
	var entityType string
	var entityID *uuid.UUID
	for _, c := range []struct {
		name string
		id   *uuid.UUID
	}{
		{EntityAccount, in.AccountID},
		{EntityContact, in.ContactID},
		{EntityDeal, in.DealID},
		{EntityTicket, in.TicketID},
		{EntityLead, in.LeadID},
		{EntityTask, in.TaskID},
	} {
		if c.id != nil {
			set++
			entityType = c.name
			entityID = c.id
		}
	}
	if set > 1 {
		return "", nil, httpx.ValidationError(map[string]string{
			"entity": "a document may be attached to only one entity"})
	}
	return entityType, entityID, nil
}

// Upload stores a file and records its metadata.
//
// Ordering matters and is chosen deliberately: the object is written first, then
// the row. If the row insert fails the object is deleted, so a failure leaves no
// orphan object. The reverse order would leave a row pointing at an object that
// was never written, which every download would then fail on. If the compensating
// delete itself fails, the object is left orphaned and logged; Reconcile finds it
// later. An orphan object costs storage but is invisible to users, whereas an
// orphan row is a broken download.
func (s *Service) Upload(ctx context.Context, p *rbac.Principal, in UploadInput, content io.Reader) (*Document, error) {
	if !s.Enabled() {
		return nil, ErrNotConfigured
	}
	if err := requirePerm(p, rbac.PermDocumentsCreate); err != nil {
		return nil, err
	}
	if in.Size > s.cfg.MaxObjectBytes {
		return nil, httpx.NewError(413, httpx.CodePayloadTooLarge, fmt.Sprintf(
			"the file exceeds the maximum of %d bytes", s.cfg.MaxObjectBytes))
	}

	entityType, entityID, err := in.entityRef()
	if err != nil {
		return nil, err
	}
	// An attachment to an entity must name an entity the caller can already read, so
	// a user cannot use an upload to write into a record they cannot see.
	if entityID != nil {
		if err := s.assertEntityReadable(ctx, p, entityType, *entityID); err != nil {
			return nil, err
		}
	}

	contentType := normaliseContentType(in.ContentType, in.Filename)
	if err := s.checkContentType(contentType); err != nil {
		return nil, err
	}

	id := uuid.New()
	key := objectKey(p.OrganizationID, id, in.Filename)

	// The size is enforced again while streaming: a Content-Length can lie, and the
	// limit must hold for the bytes actually read.
	limited := io.LimitReader(content, s.cfg.MaxObjectBytes+1)
	hasher := sha256.New()
	tee := io.TeeReader(limited, hasher)

	info, err := s.store.Put(ctx, key, tee, in.Size, contentType)
	if err != nil {
		return nil, fmt.Errorf("store object: %w", err)
	}
	if info.Size > s.cfg.MaxObjectBytes {
		// The provider reports the true size. If it exceeds the limit the object is
		// already stored, so it must be removed before returning.
		s.removeOrphan(ctx, key)
		return nil, httpx.NewError(413, httpx.CodePayloadTooLarge, fmt.Sprintf(
			"the file exceeds the maximum of %d bytes", s.cfg.MaxObjectBytes))
	}

	checksum := hex.EncodeToString(hasher.Sum(nil))

	doc, err := s.insertDocument(ctx, p, id, in, entityType, entityID, key, contentType, info.Size, checksum)
	if err != nil {
		// Compensate: the object has no row, so it must not remain.
		s.removeOrphan(ctx, key)
		return nil, err
	}

	s.record(ctx, p, audit.ActionDocumentUploaded, doc.ID, map[string]any{
		"filename":     doc.Filename,
		"size_bytes":   doc.SizeBytes,
		"content_type": doc.ContentType,
	})
	return doc, nil
}

func (s *Service) insertDocument(ctx context.Context, p *rbac.Principal, id uuid.UUID,
	in UploadInput, entityType string, entityID *uuid.UUID, key, contentType string,
	size int64, checksum string) (*Document, error) {

	var doc *Document
	err := s.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		// The entity id column is chosen from a fixed set, never from user text, so
		// the column name cannot be injected.
		var accountID, contactID, dealID, ticketID, leadID, taskID *uuid.UUID
		switch entityType {
		case EntityAccount:
			accountID = entityID
		case EntityContact:
			contactID = entityID
		case EntityDeal:
			dealID = entityID
		case EntityTicket:
			ticketID = entityID
		case EntityLead:
			leadID = entityID
		case EntityTask:
			taskID = entityID
		}

		const q = `
			INSERT INTO documents (id, organization_id, filename, content_type, size_bytes,
				checksum_sha256, storage_path, title, account_id, contact_id, deal_id,
				ticket_id, lead_id, task_id, uploaded_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
			RETURNING ` + documentColumns
		d, err := scanDocument(s.db.Q(ctx).QueryRow(ctx, q, id, p.OrganizationID, in.Filename,
			contentType, size, checksum, key, nullableString(in.Title), accountID, contactID,
			dealID, ticketID, leadID, taskID, p.UserID))
		if err != nil {
			return fmt.Errorf("record document: %w", err)
		}
		doc = d
		return nil
	})
	if err != nil {
		return nil, err
	}
	return doc, nil
}

// Get loads a document's metadata, scoped to the caller's organization and to the
// permissions on the entity it is attached to.
func (s *Service) Get(ctx context.Context, p *rbac.Principal, id uuid.UUID) (*Document, error) {
	if err := requirePerm(p, rbac.PermDocumentsRead); err != nil {
		return nil, err
	}
	doc, err := s.load(ctx, p.OrganizationID, id)
	if err != nil {
		return nil, err
	}
	if err := s.assertDocumentEntityReadable(ctx, p, doc); err != nil {
		return nil, err
	}
	return doc, nil
}

// load reads a row by id within an organization. A row from another organization
// is reported as not found, never as forbidden, so the endpoint does not confirm
// that an id exists elsewhere.
func (s *Service) load(ctx context.Context, orgID, id uuid.UUID) (*Document, error) {
	const q = `SELECT ` + documentColumns + ` FROM documents WHERE id = $1 AND organization_id = $2`
	doc, err := scanDocument(s.db.Q(ctx).QueryRow(ctx, q, id, orgID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load document: %w", err)
	}
	return doc, nil
}

// Open returns a document's metadata and a reader over its bytes. It verifies the
// object actually exists, so a row whose object was removed reports
// ErrObjectMissing instead of handing back an empty stream.
func (s *Service) Open(ctx context.Context, p *rbac.Principal, id uuid.UUID) (*Document, io.ReadCloser, error) {
	if !s.Enabled() {
		return nil, nil, ErrNotConfigured
	}
	doc, err := s.Get(ctx, p, id)
	if err != nil {
		return nil, nil, err
	}
	if doc.ArchivedAt != nil {
		return nil, nil, httpx.ErrNotFound
	}

	key, err := s.storagePath(ctx, p.OrganizationID, id)
	if err != nil {
		return nil, nil, err
	}
	rc, _, err := s.store.Get(ctx, key)
	if err != nil {
		if errors.Is(err, ErrObjectMissing) {
			return nil, nil, ErrObjectMissing
		}
		return nil, nil, fmt.Errorf("open object: %w", err)
	}
	return doc, rc, nil
}

// PresignDownload returns a time-limited URL for a document.
func (s *Service) PresignDownload(ctx context.Context, p *rbac.Principal, id uuid.UUID) (*url.URL, time.Time, error) {
	if !s.Enabled() {
		return nil, time.Time{}, ErrNotConfigured
	}
	doc, err := s.Get(ctx, p, id)
	if err != nil {
		return nil, time.Time{}, err
	}
	if doc.ArchivedAt != nil {
		return nil, time.Time{}, httpx.ErrNotFound
	}
	key, err := s.storagePath(ctx, p.OrganizationID, id)
	if err != nil {
		return nil, time.Time{}, err
	}
	// A presigned URL hands out access without a session, so the object is confirmed
	// to exist first: signing a URL for a missing object would look like success.
	if _, err := s.store.Stat(ctx, key); err != nil {
		if errors.Is(err, ErrObjectMissing) {
			return nil, time.Time{}, ErrObjectMissing
		}
		return nil, time.Time{}, fmt.Errorf("stat object: %w", err)
	}
	u, err := s.store.PresignGet(ctx, key, s.cfg.PresignTTL)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("presign: %w", err)
	}
	return u, s.now().Add(s.cfg.PresignTTL), nil
}

// Archive marks a document archived and removes the object.
//
// The row is marked first and the object deleted second. If the delete fails the
// row is already archived, so the object is unreachable through the API and only
// consumes storage; Reconcile removes it. The reverse order would leave a live row
// whose object is gone, which is a broken download.
func (s *Service) Archive(ctx context.Context, p *rbac.Principal, id uuid.UUID) error {
	if !s.Enabled() {
		return ErrNotConfigured
	}
	if err := requirePerm(p, rbac.PermDocumentsArchive); err != nil {
		return err
	}
	doc, err := s.load(ctx, p.OrganizationID, id)
	if err != nil {
		return err
	}
	if err := s.assertDocumentEntityReadable(ctx, p, doc); err != nil {
		return err
	}

	key, err := s.storagePath(ctx, p.OrganizationID, id)
	if err != nil {
		return err
	}

	err = s.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		const q = `UPDATE documents SET archived_at = now() WHERE id = $1 AND organization_id = $2 AND archived_at IS NULL`
		tag, err := s.db.Q(ctx).Exec(ctx, q, id, p.OrganizationID)
		if err != nil {
			return fmt.Errorf("archive document: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return httpx.ErrNotFound
		}
		return nil
	})
	if err != nil {
		return err
	}

	if err := s.store.Delete(ctx, key); err != nil {
		// The row is archived, so the file is already unreachable. Report the failure
		// but do not pretend the archive failed.
		s.log.Warn("archived a document but could not remove its object",
			"document_id", id, "error", err)
	}

	s.record(ctx, p, audit.ActionDocumentArchived, id, map[string]any{"filename": doc.Filename})
	return nil
}

// List returns an organization's documents, newest first.
func (s *Service) List(ctx context.Context, p *rbac.Principal, includeArchived bool) ([]Document, error) {
	if !s.Enabled() {
		return nil, ErrNotConfigured
	}
	if err := requirePerm(p, rbac.PermDocumentsRead); err != nil {
		return nil, err
	}
	q := `SELECT ` + documentColumns + ` FROM documents WHERE organization_id = $1`
	if !includeArchived {
		q += ` AND archived_at IS NULL`
	}
	q += ` ORDER BY created_at DESC LIMIT 500`

	rows, err := s.db.Q(ctx).Query(ctx, q, p.OrganizationID)
	if err != nil {
		return nil, fmt.Errorf("list documents: %w", err)
	}
	defer rows.Close()

	var out []Document
	for rows.Next() {
		d, err := scanDocument(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

// storagePath reads the object key from the row. The key is stored rather than
// derived, so an object written under an older naming scheme stays reachable.
func (s *Service) storagePath(ctx context.Context, orgID, id uuid.UUID) (string, error) {
	var key string
	err := s.db.Q(ctx).QueryRow(ctx,
		`SELECT storage_path FROM documents WHERE id = $1 AND organization_id = $2`,
		id, orgID).Scan(&key)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", httpx.ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("read storage path: %w", err)
	}
	return key, nil
}

// assertEntityReadable checks that the caller may read the entity a document is
// attached to. A document inherits its parent's visibility, so a user who cannot
// see a deal cannot read its attachment even holding documents.read.
func (s *Service) assertEntityReadable(ctx context.Context, p *rbac.Principal, entityType string, entityID uuid.UUID) error {
	perm, ok := entityPermissions[entityType]
	if !ok {
		return httpx.ValidationError(map[string]string{"entity": "unsupported entity type"})
	}
	if err := requirePerm(p, perm); err != nil {
		return err
	}
	return s.entityExists(ctx, p.OrganizationID, entityType, entityID)
}

// assertDocumentEntityReadable applies the same rule to a loaded document.
func (s *Service) assertDocumentEntityReadable(ctx context.Context, p *rbac.Principal, doc *Document) error {
	for _, c := range []struct {
		entityType string
		id         *uuid.UUID
	}{
		{EntityAccount, doc.AccountID},
		{EntityContact, doc.ContactID},
		{EntityDeal, doc.DealID},
		{EntityTicket, doc.TicketID},
		{EntityLead, doc.LeadID},
		{EntityTask, doc.TaskID},
	} {
		if c.id == nil {
			continue
		}
		if err := s.assertEntityReadable(ctx, p, c.entityType, *c.id); err != nil {
			return err
		}
	}
	return nil
}

// entityTables maps an entity type to its table. The table name comes from this
// fixed map, never from the request, so it cannot be injected.
var entityTables = map[string]string{
	EntityAccount: "accounts",
	EntityContact: "contacts",
	EntityDeal:    "deals",
	EntityLead:    "leads",
	EntityTask:    "tasks",
	EntityTicket:  "tickets",
}

func (s *Service) entityExists(ctx context.Context, orgID uuid.UUID, entityType string, entityID uuid.UUID) error {
	table, ok := entityTables[entityType]
	if !ok {
		return httpx.ValidationError(map[string]string{"entity": "unsupported entity type"})
	}
	var exists bool
	err := s.db.Q(ctx).QueryRow(ctx,
		`SELECT true FROM `+table+` WHERE id = $1 AND organization_id = $2`,
		entityID, orgID).Scan(&exists)
	if errors.Is(err, pgx.ErrNoRows) {
		// The entity is not in the caller's organization. Reported as not found so the
		// response does not confirm that an id exists elsewhere.
		return httpx.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("check entity: %w", err)
	}
	return nil
}

// checkContentType enforces the allow-list when one is configured.
func (s *Service) checkContentType(contentType string) error {
	if objectstore.IsAllowedContentType(contentType, s.cfg.AllowedContentTypes) {
		return nil
	}
	if len(s.cfg.AllowedContentTypes) == 0 {
		return httpx.ValidationError(map[string]string{
			"content_type": "this file type is not accepted"})
	}
	return httpx.ValidationError(map[string]string{
		"content_type": fmt.Sprintf("content type %q is not accepted", contentType)})
}

// objectKey and normaliseContentType are the pure key/type helpers, defined in
// internal/objectstore. They are kept here as thin wrappers so the package's
// existing unexported call sites and tests are unchanged.
func objectKey(orgID, docID uuid.UUID, filename string) string {
	return objectstore.ObjectKey(orgID, docID, filename)
}

func isSafeExtension(ext string) bool { return objectstore.IsSafeExtension(ext) }

func normaliseContentType(declared, filename string) string {
	return objectstore.NormaliseContentType(declared, filename)
}

func requirePerm(p *rbac.Principal, perm rbac.Permission) error {
	if p == nil {
		return httpx.NewError(401, httpx.CodeUnauthorized, "authentication required")
	}
	if !p.Has(perm) {
		return httpx.ErrForbidden
	}
	return nil
}

func nullableString(s string) *string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return &s
}

func (s *Service) removeOrphan(ctx context.Context, key string) {
	if err := s.store.Delete(ctx, key); err != nil {
		// Storage costs money but a user never sees it; Reconcile removes it later.
		s.log.Error("could not remove an orphan object", "key", key, "error", err)
	}
}

func (s *Service) record(ctx context.Context, p *rbac.Principal, action audit.Action, id uuid.UUID, meta map[string]any) {
	if s.audit == nil || p == nil {
		return
	}
	meta["document_id"] = id.String()
	_ = s.audit.Record(ctx, audit.Entry{
		OrganizationID: &p.OrganizationID,
		ActorUserID:    &p.UserID,
		Action:         string(action),
		EntityType:     EntityDocument,
		EntityID:       id.String(),
		Metadata:       meta,
	})
}
