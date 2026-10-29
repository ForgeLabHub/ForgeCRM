package api

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/httpx"
	"github.com/geff1778/forgecrm/internal/rbac"
	"github.com/geff1778/forgecrm/internal/storage"
)

// mountDocuments registers the document attachment routes.
//
// A document is a file bound to an organization and optionally to one CRM
// record. Every route checks a permission on the server: hiding an upload button
// is not access control, and a document inherits the visibility of the record it
// is attached to.
func (s *Server) mountDocuments(r chi.Router) {
	r.Route("/documents", func(r chi.Router) {
		r.With(s.perm(rbac.PermDocumentsRead)).Get("/", s.handleListDocuments)
		r.With(s.perm(rbac.PermDocumentsCreate)).Post("/", s.handleUploadDocument)
		r.Route("/{id}", func(r chi.Router) {
			r.With(s.perm(rbac.PermDocumentsRead)).Get("/", s.handleGetDocument)
			r.With(s.perm(rbac.PermDocumentsRead)).Get("/download", s.handleDownloadDocument)
			r.With(s.perm(rbac.PermDocumentsRead)).Get("/url", s.handleDocumentURL)
			r.With(s.perm(rbac.PermDocumentsArchive)).Delete("/", s.handleArchiveDocument)
		})
	})
}

// documentURLResponse is a time-limited download link.
type documentURLResponse struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}

// documentResponse is the wire shape of a document. It mirrors storage.Document
// and adds the download link so a client does not have to build it.
type documentResponse struct {
	storage.Document
	DownloadURL string `json:"download_url"`
}

func toDocumentResponse(d storage.Document) documentResponse {
	return documentResponse{
		Document:    d,
		DownloadURL: "/api/v1/documents/" + d.ID.String() + "/download",
	}
}

// uploadParts is the parsed multipart body. The file part is spooled to a
// temporary file rather than buffered, so a large upload does not sit in memory
// while the remaining parts are read.
type uploadParts struct {
	input    storage.UploadInput
	spool    *os.File
	spooled  bool
	filename string
}

// parseUploadParts reads every part of the multipart body before the upload is
// attempted.
//
// The parts are read to completion before the file is stored because their order
// is not defined: a client may send the metadata before or after the file, and the
// previous code returned as soon as it saw the file part, silently discarding
// every part that followed it. The frontend sends the file first, so every
// attachment it uploaded lost its title and its entity reference and landed
// unattached. Reading the whole body first makes the outcome independent of order.
//
// The file is spooled to a temporary file so that a large body is not held in
// memory; the caller removes it. A body with no file part is refused.
func (s *Server) parseUploadParts(r *http.Request, maxBytes int64) (*uploadParts, error) {
	reader, err := r.MultipartReader()
	if err != nil {
		return nil, httpx.ValidationError(map[string]string{
			"file": "the request must be multipart/form-data with a file part"})
	}
	out := &uploadParts{}
	defer func() {
		if !out.spooled && out.spool != nil {
			out.spool.Close()
			os.Remove(out.spool.Name())
			out.spool = nil
		}
	}()
	for {
		part, err := reader.NextPart()
		if err != nil {
			break
		}
		switch part.FormName() {
		case "file":
			if out.spool != nil {
				// Only one file part is accepted; a second is refused rather than
				// overwriting the first, which would make the stored name and the
				// bytes disagree.
				return nil, httpx.ValidationError(map[string]string{
					"file": "the request must contain exactly one file part"})
			}
			f, err := os.CreateTemp("", "forgecrm-upload-*")
			if err != nil {
				return nil, fmt.Errorf("spool the upload: %w", err)
			}
			out.spool = f
			out.filename = part.FileName()
			out.input.Filename = part.FileName()
			out.input.ContentType = part.Header.Get("Content-Type")
			// The size is bounded again here: Content-Length can lie, and the limit
			// must hold for the bytes actually read.
			written, err := io.Copy(f, io.LimitReader(part, maxBytes+1))
			if err != nil {
				return nil, fmt.Errorf("read the uploaded file: %w", err)
			}
			if written > maxBytes {
				return nil, httpx.NewError(413, httpx.CodePayloadTooLarge, fmt.Sprintf(
					"the upload exceeds the maximum of %d bytes", maxBytes))
			}
			out.input.Size = written
		case "account_id", "contact_id", "deal_id", "ticket_id", "lead_id", "task_id":
			id, err := uuid.Parse(readPartString(part))
			if err != nil {
				return nil, httpx.ValidationError(map[string]string{
					part.FormName(): "must be a UUID"})
			}
			setUploadEntity(&out.input, part.FormName(), id)
		case "title":
			out.input.Title = readPartString(part)
		}
	}
	if out.spool == nil {
		return nil, httpx.ValidationError(map[string]string{
			"file": "no file part was found in the request"})
	}
	if _, err := out.spool.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("rewind the uploaded file: %w", err)
	}
	out.spooled = true
	return out, nil
}

// close removes the spooled file. It is safe to call on a nil receiver and more
// than once.
func (u *uploadParts) close() {
	if u == nil || u.spool == nil {
		return
	}
	name := u.spool.Name()
	u.spool.Close()
	os.Remove(name)
	u.spool = nil
}

// handleUploadDocument accepts a multipart upload.
//
// The body is streamed: the multipart part is never buffered whole, so a large
// file cannot exhaust the process, and the size limit is enforced twice — once on
// the declared Content-Length before a byte is read, and again on the bytes
// actually stored.
func (s *Server) handleUploadDocument(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	if !s.deps.Storage.Enabled() {
		httpx.WriteError(w, r, httpx.ErrDisabled)
		return
	}

	maxBytes := s.deps.Storage.MaxObjectBytes()
	if r.ContentLength > maxBytes {
		httpx.WriteError(w, r, httpx.NewError(413, httpx.CodePayloadTooLarge, fmt.Sprintf(
			"the upload exceeds the maximum of %d bytes", maxBytes)))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)

	parts, err := s.parseUploadParts(r, maxBytes)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	defer parts.close()

	doc, err := s.deps.Storage.Upload(r.Context(), p, parts.input, parts.spool)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSONStatus(w, http.StatusCreated, toDocumentResponse(*doc))
}

// setUploadEntity assigns the parsed id to the matching field. The mapping is a
// fixed switch, so a form field cannot name an arbitrary column.
func setUploadEntity(in *storage.UploadInput, form string, id uuid.UUID) {
	switch form {
	case "account_id":
		in.AccountID = &id
	case "contact_id":
		in.ContactID = &id
	case "deal_id":
		in.DealID = &id
	case "ticket_id":
		in.TicketID = &id
	case "lead_id":
		in.LeadID = &id
	case "task_id":
		in.TaskID = &id
	}
}

// readPartString reads a small form part, bounded so a large non-file part cannot
// be used to allocate memory.
func readPartString(part io.Reader) string {
	buf, err := io.ReadAll(io.LimitReader(part, 4096))
	if err != nil {
		return ""
	}
	return string(buf)
}

func (s *Server) handleListDocuments(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	includeArchived := r.URL.Query().Get("include_archived") == "true"
	docs, err := s.deps.Storage.List(r.Context(), p, includeArchived)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	out := make([]documentResponse, 0, len(docs))
	for _, d := range docs {
		out = append(out, toDocumentResponse(d))
	}
	httpx.WriteJSONList(w, out)
}

func (s *Server) handleGetDocument(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	doc, err := s.deps.Storage.Get(r.Context(), p, id)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, toDocumentResponse(*doc))
}

// handleDownloadDocument streams a file.
//
// The object is streamed through the server rather than redirected to a presigned
// URL, so the permission check happens on every request and the response carries
// the filename. The presigned-URL endpoint exists for a client that wants to
// fetch directly from the store.
func (s *Server) handleDownloadDocument(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	doc, body, err := s.deps.Storage.Open(r.Context(), p, id)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	defer body.Close()

	w.Header().Set("Content-Type", doc.ContentType)
	w.Header().Set("Content-Length", strconv.FormatInt(doc.SizeBytes, 10))
	w.Header().Set("Content-Disposition", contentDisposition(doc.Filename))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if _, err := io.Copy(w, body); err != nil {
		// The status is already written, so the only recourse is to log. A truncated
		// download is visible to the client as a short body.
		s.deps.Log.Warn("a document download was interrupted",
			"document_id", id, "error", err)
	}
}

// handleDocumentURL returns a time-limited presigned download URL.
func (s *Server) handleDocumentURL(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	u, expires, err := s.deps.Storage.PresignDownload(r.Context(), p, id)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, map[string]any{
		"url":        u.String(),
		"expires_at": expires.UTC(),
	})
}

func (s *Server) handleArchiveDocument(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	if err := s.deps.Storage.Archive(r.Context(), p, id); err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// contentDisposition builds a safe Content-Disposition header. The filename is
// quoted and any quote or backslash is escaped, so a name containing a quote
// cannot break out of the header value.
func contentDisposition(filename string) string {
	safe := make([]rune, 0, len(filename))
	for _, r := range filename {
		switch {
		case r == '"' || r == '\\':
			safe = append(safe, '\\', r)
		case r < 0x20 || r == 0x7f:
			// Control characters are dropped; a newline in a header is a response-splitting
			// attempt.
			safe = append(safe, '_')
		default:
			safe = append(safe, r)
		}
	}
	return `attachment; filename="` + string(safe) + `"`
}
