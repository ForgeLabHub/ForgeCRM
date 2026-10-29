package objectstore

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

// These tests cover the pure, security-relevant helpers. They live with the
// package so the extracted module carries its own proof, independent of the
// application layer that also calls them.

func TestObjectKeyIsTenantScopedAndIgnoresTheClientName(t *testing.T) {
	org := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	doc := uuid.MustParse("22222222-2222-2222-2222-222222222222")

	// A hostile filename must not change the key beyond a safe extension.
	key := ObjectKey(org, doc, "../../../etc/passwd")
	if !strings.HasPrefix(key, "org/"+org.String()+"/documents/") {
		t.Fatalf("key %q is not under the tenant prefix", key)
	}
	if strings.Contains(key, "..") || strings.Contains(key, "passwd") {
		t.Fatalf("key %q leaked the client filename", key)
	}
	if !strings.HasSuffix(key, doc.String()) {
		t.Fatalf("key %q does not end in the document id", key)
	}
}

func TestObjectKeyKeepsASafeExtension(t *testing.T) {
	org := uuid.New()
	doc := uuid.New()
	key := ObjectKey(org, doc, "report.PDF")
	if !strings.HasSuffix(key, ".pdf") {
		t.Fatalf("key %q did not keep the lowercased extension", key)
	}
	if got := ObjectKey(org, doc, "x.averylongextensionname"); strings.Contains(got, "averylong") {
		t.Fatalf("key %q kept an unsafe extension", got)
	}
}

func TestIsSafeExtension(t *testing.T) {
	for _, ext := range []string{".txt", ".pdf", ".tar", ".gz"} {
		if !IsSafeExtension(ext) {
			t.Errorf("IsSafeExtension(%q) = false, want true", ext)
		}
	}
	for _, ext := range []string{"", ".", "x", ".a/b", ".UPPER", ".a-b", "..", ".averylongextension"} {
		if IsSafeExtension(ext) {
			t.Errorf("IsSafeExtension(%q) = true, want false", ext)
		}
	}
}

func TestIsAllowedContentType(t *testing.T) {
	// With no allow-list, executables are refused and ordinary types pass.
	if IsAllowedContentType("application/x-msdownload", nil) {
		t.Error("an executable was accepted with no allow-list")
	}
	if !IsAllowedContentType("image/png", nil) {
		t.Error("a png was refused with no allow-list")
	}
	// With an allow-list, only listed types pass.
	allow := []string{"image/png"}
	if IsAllowedContentType("text/plain", allow) {
		t.Error("a type outside the allow-list was accepted")
	}
	if !IsAllowedContentType("image/png", allow) {
		t.Error("an allow-listed type was refused")
	}
	// The comparison is case-insensitive, matching the previous behaviour.
	if !IsAllowedContentType("IMAGE/PNG", allow) {
		t.Error("a type differing only in case was refused")
	}
}

func TestNormaliseContentType(t *testing.T) {
	if got := NormaliseContentType("text/plain; charset=utf-8", "x.txt"); got != "text/plain" {
		t.Errorf("declared type = %q, want text/plain", got)
	}
	if got := NormaliseContentType("", "x.png"); got != "image/png" {
		t.Errorf("type from extension = %q, want image/png", got)
	}
	if got := NormaliseContentType("", "x.unknownext"); got != "application/octet-stream" {
		t.Errorf("fallback type = %q, want application/octet-stream", got)
	}
}
