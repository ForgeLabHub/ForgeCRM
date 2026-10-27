package safe

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func testLimits() Limits {
	l := DefaultLimits()
	l.MaxTotalBytes = 1 << 20
	l.MaxEntryBytes = 1 << 18
	l.MaxEntries = 20
	l.MaxDepth = 4
	l.MaxCompressRatio = 50
	return l
}

func writeTemp(t *testing.T, name string, data []byte) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return p
}

func makeZip(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, data := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create zip entry: %v", err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatalf("write zip entry: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

func TestExtractZipRoundTrip(t *testing.T) {
	data := makeZip(t, map[string][]byte{
		"contacts.csv":     []byte("name,email\nAda,ada@example.com\n"),
		"nested/deals.csv": []byte("title,amount\nDeal,100\n"),
	})
	src := writeTemp(t, "data.zip", data)

	ex := NewExtractor(testLimits())
	res, err := ex.Extract(t.TempDir(), src, FormatZIP, int64(len(data)))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	defer res.Cleanup()

	if len(res.Files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(res.Files))
	}
	// Content must match.
	for _, f := range res.Files {
		if _, err := os.Stat(f.AbsPath); err != nil {
			t.Fatalf("extracted file missing: %v", err)
		}
	}
}

func TestExtractZipRejectsPathTraversal(t *testing.T) {
	for _, name := range []string{
		"../escape.csv",
		"../../etc/passwd",
		"nested/../../escape.csv",
		"/absolute/path.csv",
	} {
		t.Run(name, func(t *testing.T) {
			src := writeTemp(t, "evil.zip", makeZip(t, map[string][]byte{name: []byte("x")}))
			ex := NewExtractor(testLimits())
			_, err := ex.Extract(t.TempDir(), src, FormatZIP, 100)
			if err == nil {
				t.Fatalf("expected rejection for %q", name)
			}
		})
	}
}

func TestExtractZipRejectsTooManyEntries(t *testing.T) {
	entries := map[string][]byte{}
	for i := 0; i < 30; i++ {
		entries[filepath.Join("f", string(rune('a'+i%26))+string(rune('0'+i/26))+".csv")] = []byte("x")
	}
	src := writeTemp(t, "many.zip", makeZip(t, entries))
	ex := NewExtractor(testLimits())
	if _, err := ex.Extract(t.TempDir(), src, FormatZIP, 1000); err == nil {
		t.Fatal("expected too-many-entries rejection")
	}
}

func TestExtractZipRejectsDeepNesting(t *testing.T) {
	deep := "a/b/c/d/e/f/g/deep.csv"
	src := writeTemp(t, "deep.zip", makeZip(t, map[string][]byte{deep: []byte("x")}))
	ex := NewExtractor(testLimits())
	if _, err := ex.Extract(t.TempDir(), src, FormatZIP, 100); err == nil {
		t.Fatal("expected depth rejection")
	}
}

func TestExtractTarRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	content := []byte("title,amount\nDeal,100\n")
	if err := tw.WriteHeader(&tar.Header{
		Name: "deals.csv", Mode: 0o600, Size: int64(len(content)), Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	src := writeTemp(t, "data.tar", buf.Bytes())

	ex := NewExtractor(testLimits())
	res, err := ex.Extract(t.TempDir(), src, FormatTAR, int64(buf.Len()))
	if err != nil {
		t.Fatalf("extract tar: %v", err)
	}
	defer res.Cleanup()
	if len(res.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(res.Files))
	}
	got, err := os.ReadFile(res.Files[0].AbsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("content mismatch: %q", got)
	}
}

func TestExtractTarRejectsSymlink(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{
		Name: "link", Linkname: "/etc/passwd", Mode: 0o777, Typeflag: tar.TypeSymlink,
	}); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	src := writeTemp(t, "link.tar", buf.Bytes())
	ex := NewExtractor(testLimits())
	if _, err := ex.Extract(t.TempDir(), src, FormatTAR, int64(buf.Len())); err == nil {
		t.Fatal("expected symlink rejection")
	}
}

func TestExtractTarGzRoundTrip(t *testing.T) {
	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)
	content := []byte("name,email\nAda,ada@example.com\n")
	if err := tw.WriteHeader(&tar.Header{
		Name: "contacts.csv", Mode: 0o600, Size: int64(len(content)), Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	var gzBuf bytes.Buffer
	gw := gzip.NewWriter(&gzBuf)
	if _, err := gw.Write(tarBuf.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	src := writeTemp(t, "data.tar.gz", gzBuf.Bytes())

	ex := NewExtractor(testLimits())
	res, err := ex.Extract(t.TempDir(), src, FormatTarGz, int64(gzBuf.Len()))
	if err != nil {
		t.Fatalf("extract tar.gz: %v", err)
	}
	defer res.Cleanup()
	if len(res.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(res.Files))
	}
	got, _ := os.ReadFile(res.Files[0].AbsPath)
	if !bytes.Equal(got, content) {
		t.Fatalf("content mismatch: %q", got)
	}
}

func TestExtractGzipSingleFile(t *testing.T) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	content := []byte("name,email\nAda,ada@example.com\n")
	if _, err := gw.Write(content); err != nil {
		t.Fatal(err)
	}
	gw.Close()
	src := writeTemp(t, "contacts.csv.gz", buf.Bytes())

	ex := NewExtractor(testLimits())
	res, err := ex.Extract(t.TempDir(), src, FormatGzip, int64(buf.Len()))
	if err != nil {
		t.Fatalf("extract gzip: %v", err)
	}
	defer res.Cleanup()
	if len(res.Files) != 1 || res.Files[0].RelPath != "contacts.csv" {
		t.Fatalf("unexpected files: %+v", res.Files)
	}
	got, _ := os.ReadFile(res.Files[0].AbsPath)
	if !bytes.Equal(got, content) {
		t.Fatalf("content mismatch")
	}
}

func TestExtractRejectsZipBomb(t *testing.T) {
	// A highly compressible payload whose declared ratio is implausible.
	big := bytes.Repeat([]byte("A"), 1<<20)
	src := writeTemp(t, "bomb.zip", makeZip(t, map[string][]byte{"bomb.txt": big}))
	ex := NewExtractor(testLimits())
	_, err := ex.Extract(t.TempDir(), src, FormatZIP, 1024)
	if err == nil {
		t.Fatal("expected zip bomb rejection")
	}
}

func TestExtractTruncatedArchiveFails(t *testing.T) {
	full := makeZip(t, map[string][]byte{"a.csv": []byte("x,y\n1,2\n")})
	truncated := full[:len(full)/2]
	src := writeTemp(t, "trunc.zip", truncated)
	ex := NewExtractor(testLimits())
	if _, err := ex.Extract(t.TempDir(), src, FormatZIP, int64(len(truncated))); err == nil {
		t.Fatal("expected failure on a truncated archive")
	}
}

func TestExtractCleansUpOnFailure(t *testing.T) {
	parent := t.TempDir()
	src := writeTemp(t, "evil.zip", makeZip(t, map[string][]byte{"../x.csv": []byte("x")}))
	ex := NewExtractor(testLimits())
	if _, err := ex.Extract(parent, src, FormatZIP, 100); err == nil {
		t.Fatal("expected rejection")
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("extraction root was not cleaned up: %v", entries)
	}
}

func TestDetectFormatUsesMagicBytesNotExtension(t *testing.T) {
	zipData := makeZip(t, map[string][]byte{"a.csv": []byte("x")})
	// A zip that claims to be a CSV must still be detected as a zip.
	if got := DetectFormat("innocent.csv", zipData[:8]); got != FormatZIP {
		t.Fatalf("expected zip detection, got %q", got)
	}
	// A tar.gz by extension.
	if got := DetectFormat("data.tar.gz", []byte{0x1f, 0x8b, 0x08}); got != FormatTarGz {
		t.Fatalf("expected tar.gz detection, got %q", got)
	}
	if got := DetectFormat("data.gz", []byte{0x1f, 0x8b, 0x08}); got != FormatGzip {
		t.Fatalf("expected gzip detection, got %q", got)
	}
}

func TestExtractRejectsBackslashPaths(t *testing.T) {
	src := writeTemp(t, "win.zip", makeZip(t, map[string][]byte{"..\\escape.csv": []byte("x")}))
	ex := NewExtractor(testLimits())
	if _, err := ex.Extract(t.TempDir(), src, FormatZIP, 100); err == nil {
		t.Fatal("expected rejection of a backslash path")
	}
}
