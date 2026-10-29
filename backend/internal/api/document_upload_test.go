package api

import (
	"bytes"
	"mime/multipart"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

// buildMultipart assembles a multipart body with the parts in the given order.
// Each part is a (name, filename, value) triple; filename is empty for a plain
// field.
type part struct {
	name     string
	filename string
	value    string
}

func buildMultipart(t *testing.T, parts ...part) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for _, p := range parts {
		if p.filename != "" {
			fw, err := w.CreateFormFile(p.name, p.filename)
			if err != nil {
				t.Fatalf("create file part: %v", err)
			}
			if _, err := fw.Write([]byte(p.value)); err != nil {
				t.Fatalf("write file part: %v", err)
			}
			continue
		}
		if err := w.WriteField(p.name, p.value); err != nil {
			t.Fatalf("write field: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	return &body, w.FormDataContentType()
}

// parseRequest runs the handler's part parser against a body and returns the
// parsed input.
func parseRequest(t *testing.T, parts ...part) (*uploadParts, error) {
	t.Helper()
	body, contentType := buildMultipart(t, parts...)
	req := httptest.NewRequest("POST", "/api/v1/documents", body)
	req.Header.Set("Content-Type", contentType)
	s := &Server{}
	out, err := s.parseUploadParts(req, 8<<20)
	if out != nil {
		t.Cleanup(out.close)
	}
	return out, err
}

// TestUploadPartsKeepsMetadataAfterTheFile is the regression guard for the upload
// ordering bug.
//
// The handler used to return as soon as it saw the file part, so every part that
// followed it was silently dropped. The frontend appends the file first and the
// title and entity id after it, so every attachment made through the UI landed
// untitled and unattached — the upload succeeded, the file was stored, and the
// link to the record the user attached it to was thrown away. The parser must now
// read the whole body, so the result does not depend on part order.
func TestUploadPartsKeepsMetadataAfterTheFile(t *testing.T) {
	dealID := uuid.New()
	got, err := parseRequest(t,
		part{name: "file", filename: "proposal.pdf", value: "the bytes"},
		part{name: "title", value: "Signed proposal"},
		part{name: "deal_id", value: dealID.String()},
	)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.input.Title != "Signed proposal" {
		t.Fatalf("title = %q, want the value that followed the file part", got.input.Title)
	}
	if got.input.DealID == nil || *got.input.DealID != dealID {
		t.Fatalf("deal_id = %v, want the id that followed the file part", got.input.DealID)
	}
	if got.input.Filename != "proposal.pdf" {
		t.Fatalf("filename = %q", got.input.Filename)
	}
	if got.input.Size != int64(len("the bytes")) {
		t.Fatalf("size = %d, want the bytes actually read", got.input.Size)
	}
	buf := make([]byte, got.input.Size)
	if _, err := got.spool.Read(buf); err != nil {
		t.Fatalf("read spool: %v", err)
	}
	if string(buf) != "the bytes" {
		t.Fatalf("spooled bytes = %q", buf)
	}
}

// TestUploadPartsAcceptsMetadataBeforeTheFile checks the other order still works,
// so the fix does not simply trade one order for the other.
func TestUploadPartsAcceptsMetadataBeforeTheFile(t *testing.T) {
	got, err := parseRequest(t,
		part{name: "title", value: "First"},
		part{name: "file", filename: "a.txt", value: "aaa"},
	)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.input.Title != "First" {
		t.Fatalf("title = %q", got.input.Title)
	}
	if got.input.Size != 3 {
		t.Fatalf("size = %d, want 3", got.input.Size)
	}
}

// TestUploadPartsRejectsABodyWithNoFile checks the missing-file case is still an
// error and not a zero-byte upload.
func TestUploadPartsRejectsABodyWithNoFile(t *testing.T) {
	_, err := parseRequest(t, part{name: "title", value: "no file here"})
	if err == nil {
		t.Fatal("a body with no file part was accepted")
	}
}

// TestUploadPartsRejectsASecondFile checks that a second file part is refused
// rather than silently replacing the first, which would store bytes that do not
// match the name.
func TestUploadPartsRejectsASecondFile(t *testing.T) {
	_, err := parseRequest(t,
		part{name: "file", filename: "first.txt", value: "first"},
		part{name: "file", filename: "second.txt", value: "second"},
	)
	if err == nil {
		t.Fatal("a body with two file parts was accepted")
	}
}

// TestUploadPartsRejectsAnOversizeFile checks the byte limit is enforced on the
// bytes read, not just the declared Content-Length.
func TestUploadPartsRejectsAnOversizeFile(t *testing.T) {
	body, contentType := buildMultipart(t, part{name: "file", filename: "big.bin", value: "0123456789"})
	req := httptest.NewRequest("POST", "/api/v1/documents", body)
	req.Header.Set("Content-Type", contentType)
	s := &Server{}
	got, err := s.parseUploadParts(req, 4)
	if got != nil {
		got.close()
	}
	if err == nil {
		t.Fatal("a file larger than the limit was accepted")
	}
}
