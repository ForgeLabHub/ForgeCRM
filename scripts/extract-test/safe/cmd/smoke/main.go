// Command smoke exercises the safe-archive public API in a clean module.
//
// It builds a real ZIP in a temp directory, extracts it, and then feeds the
// extractor the two attacks the package exists to stop — a path-traversal entry
// and a zip bomb — to confirm the limits are enforced here, not only in the
// monolith that normally calls this code.
package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/geff1778/forgecrm/internal/importx/safe"
)

func main() {
	dir, err := os.MkdirTemp("", "safe-smoke-")
	if err != nil {
		fail("temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	// A benign archive round-trips: format detected from magic bytes, contents on
	// disk under a private root.
	good := filepath.Join(dir, "good.zip")
	if err := writeZip(good, map[string][]byte{"data/rows.csv": []byte("name\nAda\n")}); err != nil {
		fail("build zip: %v", err)
	}
	head, err := os.ReadFile(good)
	if err != nil {
		fail("read zip: %v", err)
	}
	if got := safe.DetectFormat("good.zip", head[:4]); got != safe.FormatZIP {
		fail("DetectFormat = %q, want zip", got)
	}
	res, err := safe.NewExtractor(safe.DefaultLimits()).Extract(dir, good, safe.FormatZIP, int64(len(head)))
	if err != nil {
		fail("extract good zip: %v", err)
	}
	defer os.RemoveAll(res.Root)
	if len(res.Files) != 1 || res.Files[0].RelPath != "data/rows.csv" {
		fail("extracted files = %+v", res.Files)
	}
	content, err := os.ReadFile(filepath.Join(res.Root, "data", "rows.csv"))
	if err != nil || !strings.Contains(string(content), "Ada") {
		fail("extracted content unreadable: %v", err)
	}
	fmt.Printf("roundtrip files=%d bytes=%d\n", len(res.Files), res.TotalBytes)

	// Path traversal is refused: the entry is never written outside the root.
	evil := filepath.Join(dir, "evil.zip")
	if err := writeZip(evil, map[string][]byte{"../escaped.txt": []byte("x")}); err != nil {
		fail("build evil zip: %v", err)
	}
	evilHead, _ := os.ReadFile(evil)
	if _, err := safe.NewExtractor(safe.DefaultLimits()).Extract(dir, evil, safe.FormatZIP, int64(len(evilHead))); err == nil {
		fail("a path-traversal archive was accepted")
	}
	if _, statErr := os.Stat(filepath.Join(dir, "escaped.txt")); statErr == nil {
		fail("path traversal escaped the extraction root")
	}
	fmt.Printf("traversal refused=1\n")

	// A zip bomb is refused by the compression-ratio limit before it is written.
	bomb := filepath.Join(dir, "bomb.zip")
	if err := writeZip(bomb, map[string][]byte{"big.txt": bytes.Repeat([]byte("A"), 8<<20)}); err != nil {
		fail("build bomb zip: %v", err)
	}
	bombHead, _ := os.ReadFile(bomb)
	limits := safe.DefaultLimits()
	limits.MaxCompressRatio = 5
	if _, err := safe.NewExtractor(limits).Extract(dir, bomb, safe.FormatZIP, int64(len(bombHead))); err == nil {
		fail("a zip bomb was accepted")
	}
	fmt.Printf("zip bomb refused=1\n")
}

func writeZip(path string, files map[string][]byte) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			return err
		}
		if _, err := w.Write(body); err != nil {
			return err
		}
	}
	return zw.Close()
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "smoke: "+format+"\n", args...)
	os.Exit(1)
}
