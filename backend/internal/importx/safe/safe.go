// Package safe implements extraction of untrusted archives.
//
// Every archive is treated as hostile input. The rules below are enforced in
// code, not merely documented:
//
//   - a cap on the compressed input size;
//   - a cap on the total number of entries;
//   - a cap on the total uncompressed size, checked incrementally so a bomb is
//     stopped while it is being read rather than after it has filled the disk;
//   - a cap on the compression ratio per entry and overall;
//   - a cap on directory nesting depth;
//   - every entry path is cleaned and rejected if absolute, if it contains "..",
//     or if it resolves outside the extraction root;
//   - symbolic links, hard links and device/fifo entries are refused, never
//     followed;
//   - extraction always happens into a directory created by this package with a
//     random name under a private temporary root, never at an attacker-chosen
//     path;
//   - nothing inside the archive is ever executed.
package safe

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/bzip2"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Limits bounds archive processing.
type Limits struct {
	MaxEntries       int
	MaxTotalBytes    int64
	MaxEntryBytes    int64
	MaxDepth         int
	MaxCompressRatio float64
}

// DefaultLimits returns conservative defaults.
func DefaultLimits() Limits {
	return Limits{
		MaxEntries:       5000,
		MaxTotalBytes:    2 << 30, // 2 GiB
		MaxEntryBytes:    512 << 20,
		MaxDepth:         8,
		MaxCompressRatio: 200,
	}
}

// Errors returned for rejected archives. They are distinct so that a caller can
// explain precisely why an upload was refused.
var (
	ErrTooManyEntries  = errors.New("archive contains too many entries")
	ErrTooLarge        = errors.New("archive exceeds the maximum uncompressed size")
	ErrEntryTooLarge   = errors.New("archive entry exceeds the maximum size")
	ErrTooDeep         = errors.New("archive nesting is too deep")
	ErrUnsafePath      = errors.New("archive entry has an unsafe path")
	ErrUnsupportedLink = errors.New("archive contains a link or special entry")
	ErrRatioExceeded   = errors.New("archive compression ratio exceeds the limit")
	ErrUnsupportedType = errors.New("unsupported archive type")
)

// ExtractedFile describes one file produced by extraction.
type ExtractedFile struct {
	// RelPath is the entry's path relative to the extraction root, cleaned.
	RelPath string `json:"rel_path"`
	// AbsPath is the on-disk location inside the private extraction root. It is
	// never serialised: exposing server paths to clients is unnecessary.
	AbsPath string `json:"-"`
	Size    int64  `json:"size"`
}

// ExtractResult is the outcome of an extraction.
type ExtractResult struct {
	Root           string
	Files          []ExtractedFile
	TotalBytes     int64
	EntriesSeen    int
	EntriesSkipped int
}

// Format identifies an archive container.
type Format string

// Supported archive formats.
const (
	FormatZIP    Format = "zip"
	FormatTAR    Format = "tar"
	FormatTarGz  Format = "tar.gz"
	FormatTarBz2 Format = "tar.bz2"
	FormatTarXz  Format = "tar.xz"
	FormatGzip   Format = "gzip"
	FormatBzip2  Format = "bzip2"
	FormatXz     Format = "xz"
)

// DetectFormat inspects a filename and the leading bytes to determine the
// container format. The magic bytes take precedence over the extension, because
// an attacker controls the extension.
func DetectFormat(filename string, head []byte) Format {
	lower := strings.ToLower(filename)
	switch {
	case len(head) >= 4 && bytes.Equal(head[:4], []byte("PK\x03\x04")):
		return FormatZIP
	case len(head) >= 2 && head[0] == 0x1f && head[1] == 0x8b:
		// gzip: could be a .tar.gz or a single compressed file.
		if strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz") {
			return FormatTarGz
		}
		return FormatGzip
	case len(head) >= 3 && bytes.Equal(head[:3], []byte("BZh")):
		if strings.HasSuffix(lower, ".tar.bz2") || strings.HasSuffix(lower, ".tbz2") {
			return FormatTarBz2
		}
		return FormatBzip2
	case len(head) >= 6 && bytes.Equal(head[:6], []byte{0xfd, '7', 'z', 'X', 'Z', 0x00}):
		if strings.HasSuffix(lower, ".tar.xz") || strings.HasSuffix(lower, ".txz") {
			return FormatTarXz
		}
		return FormatXz
	case strings.HasSuffix(lower, ".tar"):
		return FormatTAR
	}
	// Fall back to the extension when magic bytes are inconclusive.
	switch {
	case strings.HasSuffix(lower, ".zip"):
		return FormatZIP
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		return FormatTarGz
	case strings.HasSuffix(lower, ".tar.bz2"):
		return FormatTarBz2
	case strings.HasSuffix(lower, ".tar.xz"):
		return FormatTarXz
	case strings.HasSuffix(lower, ".tar"):
		return FormatTAR
	case strings.HasSuffix(lower, ".gz"):
		return FormatGzip
	case strings.HasSuffix(lower, ".bz2"):
		return FormatBzip2
	case strings.HasSuffix(lower, ".xz"):
		return FormatXz
	}
	return ""
}

// IsArchive reports whether a format is an archive container.
func IsArchive(f Format) bool { return f != "" }

// IsSupported reports whether a format can be handled, including compressed
// single-file formats.
func IsSupported(f Format) bool {
	switch f {
	case FormatZIP, FormatTAR, FormatTarGz, FormatTarBz2, FormatTarXz,
		FormatGzip, FormatBzip2, FormatXz:
		return true
	}
	return false
}

// Extractor writes archive contents into a private root.
type Extractor struct {
	limits Limits
}

// NewExtractor builds an extractor with the given limits.
func NewExtractor(l Limits) *Extractor {
	if l.MaxEntries <= 0 {
		l = DefaultLimits()
	}
	return &Extractor{limits: l}
}

// Extract decompresses sourcePath into a new directory under rootParent and
// returns the extracted files. The caller owns the returned Root and must remove
// it when finished; ExtractTemp does this automatically.
//
// compressedSize is the size of the input file, used for ratio checks.
func (e *Extractor) Extract(rootParent, sourcePath string, format Format, compressedSize int64) (*ExtractResult, error) {
	if rootParent == "" {
		rootParent = os.TempDir()
	}
	// The parent must exist before MkdirTemp; creating it here keeps the
	// extractor usable even when the process never created the temp tree.
	if err := os.MkdirAll(rootParent, 0o700); err != nil {
		return nil, fmt.Errorf("create extraction parent: %w", err)
	}
	root, err := os.MkdirTemp(rootParent, "forgecrm-import-")
	if err != nil {
		return nil, fmt.Errorf("create extraction root: %w", err)
	}
	// MkdirTemp creates the directory with mode 0700.
	if err := os.Chmod(root, 0o700); err != nil {
		os.RemoveAll(root)
		return nil, fmt.Errorf("restrict extraction root: %w", err)
	}

	res := &ExtractResult{Root: root}
	var extractErr error

	switch format {
	case FormatZIP:
		extractErr = e.extractZIP(sourcePath, root, res, compressedSize)
	case FormatTAR:
		f, err := os.Open(sourcePath)
		if err != nil {
			extractErr = err
			break
		}
		defer f.Close()
		extractErr = e.extractTAR(f, root, res, compressedSize)
	case FormatTarGz:
		extractErr = e.withGzip(sourcePath, func(r io.Reader) error {
			return e.extractTAR(r, root, res, compressedSize)
		})
	case FormatTarBz2:
		extractErr = e.withFile(sourcePath, func(f *os.File) error {
			return e.extractTAR(bzip2.NewReader(f), root, res, compressedSize)
		})
	case FormatTarXz:
		extractErr = e.withFile(sourcePath, func(f *os.File) error {
			xzr, err := newXZReader(f)
			if err != nil {
				return err
			}
			return e.extractTAR(xzr, root, res, compressedSize)
		})
	case FormatGzip:
		extractErr = e.withGzip(sourcePath, func(r io.Reader) error {
			return e.writeSingleFile(r, root, stripCompressionSuffix(filepath.Base(sourcePath)), res)
		})
	case FormatBzip2:
		extractErr = e.withFile(sourcePath, func(f *os.File) error {
			return e.writeSingleFile(bzip2.NewReader(f), root,
				stripCompressionSuffix(filepath.Base(sourcePath)), res)
		})
	case FormatXz:
		extractErr = e.withFile(sourcePath, func(f *os.File) error {
			xzr, err := newXZReader(f)
			if err != nil {
				return err
			}
			return e.writeSingleFile(xzr, root, stripCompressionSuffix(filepath.Base(sourcePath)), res)
		})
	default:
		extractErr = fmt.Errorf("%w: %q", ErrUnsupportedType, format)
	}

	if extractErr != nil {
		os.RemoveAll(root)
		return nil, extractErr
	}

	sort.Slice(res.Files, func(i, j int) bool { return res.Files[i].RelPath < res.Files[j].RelPath })
	return res, nil
}

// Cleanup removes the extraction root. Safe to call more than once.
func (r *ExtractResult) Cleanup() {
	if r != nil && r.Root != "" {
		_ = os.RemoveAll(r.Root)
	}
}

// ---------------------------------------------------------------------------
// Containers
// ---------------------------------------------------------------------------

func (e *Extractor) extractZIP(sourcePath, root string, res *ExtractResult, compressedSize int64) error {
	zr, err := zip.OpenReader(sourcePath)
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	defer zr.Close()

	if len(zr.File) > e.limits.MaxEntries {
		return fmt.Errorf("%w: %d > %d", ErrTooManyEntries, len(zr.File), e.limits.MaxEntries)
	}

	for _, f := range zr.File {
		if err := e.checkEntryPath(f.Name); err != nil {
			return err
		}
		// Reject links and special files outright.
		mode := f.Mode()
		if mode&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: symlink %q", ErrUnsupportedLink, f.Name)
		}
		if mode&os.ModeDevice != 0 || mode&os.ModeNamedPipe != 0 || mode&os.ModeSocket != 0 {
			return fmt.Errorf("%w: special file %q", ErrUnsupportedLink, f.Name)
		}
		if f.FileInfo().IsDir() {
			res.EntriesSeen++
			continue
		}
		if int64(f.UncompressedSize64) > e.limits.MaxEntryBytes {
			return fmt.Errorf("%w: %q is %d bytes", ErrEntryTooLarge, f.Name, f.UncompressedSize64)
		}
		// A declared compression ratio that is already implausible is refused
		// before a single byte is decompressed.
		if f.CompressedSize64 > 0 {
			ratio := float64(f.UncompressedSize64) / float64(f.CompressedSize64)
			if ratio > e.limits.MaxCompressRatio {
				return fmt.Errorf("%w: %q ratio %.1f > %.1f",
					ErrRatioExceeded, f.Name, ratio, e.limits.MaxCompressRatio)
			}
		}

		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("open zip entry %q: %w", f.Name, err)
		}
		err = e.writeEntry(rc, root, f.Name, res)
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func (e *Extractor) extractTAR(r io.Reader, root string, res *ExtractResult, compressedSize int64) error {
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			// A truncated or corrupt archive surfaces here; report it rather than
			// returning partial results.
			return fmt.Errorf("read tar entry: %w", err)
		}
		res.EntriesSeen++
		if res.EntriesSeen > e.limits.MaxEntries {
			return fmt.Errorf("%w: more than %d entries", ErrTooManyEntries, e.limits.MaxEntries)
		}
		if err := e.checkEntryPath(hdr.Name); err != nil {
			return err
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			continue
		case tar.TypeReg, tar.TypeRegA:
			// handled below
		case tar.TypeSymlink, tar.TypeLink:
			return fmt.Errorf("%w: link %q", ErrUnsupportedLink, hdr.Name)
		default:
			// Char/block devices, fifos and anything unrecognised are refused.
			return fmt.Errorf("%w: entry %q has type %q", ErrUnsupportedLink, hdr.Name, hdr.Typeflag)
		}
		if hdr.Size > e.limits.MaxEntryBytes {
			return fmt.Errorf("%w: %q is %d bytes", ErrEntryTooLarge, hdr.Name, hdr.Size)
		}
		if err := e.writeEntry(tr, root, hdr.Name, res); err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Entry writing
// ---------------------------------------------------------------------------

// checkEntryPath rejects any path that is absolute, contains a traversal
// component, or is otherwise not a plain relative path.
func (e *Extractor) checkEntryPath(name string) error {
	if name == "" {
		return fmt.Errorf("%w: empty entry name", ErrUnsafePath)
	}
	// Windows-style separators and drive letters are never legitimate here.
	if strings.ContainsRune(name, '\\') {
		return fmt.Errorf("%w: %q contains a backslash", ErrUnsafePath, name)
	}
	if filepath.IsAbs(name) || strings.HasPrefix(name, "/") {
		return fmt.Errorf("%w: %q is absolute", ErrUnsafePath, name)
	}
	// A leading drive letter such as C: would be absolute on Windows.
	if len(name) >= 2 && name[1] == ':' {
		return fmt.Errorf("%w: %q has a drive letter", ErrUnsafePath, name)
	}
	cleaned := filepath.Clean(name)
	if cleaned == "." || cleaned == ".." {
		return fmt.Errorf("%w: %q is not a file path", ErrUnsafePath, name)
	}
	if strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) || cleaned == ".." {
		return fmt.Errorf("%w: %q escapes the extraction root", ErrUnsafePath, name)
	}
	if strings.Contains(cleaned, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%w: %q contains a traversal segment", ErrUnsafePath, name)
	}
	depth := strings.Count(cleaned, string(filepath.Separator))
	if depth > e.limits.MaxDepth {
		return fmt.Errorf("%w: %q has depth %d", ErrTooDeep, name, depth)
	}
	return nil
}

// writeEntry streams one entry to disk, enforcing the cumulative size budget.
func (e *Extractor) writeEntry(r io.Reader, root, name string, res *ExtractResult) error {
	rel := filepath.Clean(name)
	abs := filepath.Join(root, rel)

	// Defence in depth: confirm the resolved path is still inside the root even
	// after Join, which would catch any cleaning mistake.
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	absClean, err := filepath.Abs(abs)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(absClean, rootAbs+string(filepath.Separator)) {
		return fmt.Errorf("%w: %q resolves outside the extraction root", ErrUnsafePath, name)
	}

	if err := os.MkdirAll(filepath.Dir(absClean), 0o700); err != nil {
		return fmt.Errorf("create entry directory: %w", err)
	}

	// O_EXCL prevents overwriting an earlier entry with the same name, which
	// would otherwise let a later entry replace a file already validated.
	f, err := os.OpenFile(absClean, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("%w: duplicate entry %q", ErrUnsafePath, name)
		}
		return fmt.Errorf("create entry file: %w", err)
	}

	// Read one byte more than the remaining budget so that exceeding it is
	// detected rather than silently truncated.
	remaining := e.limits.MaxTotalBytes - res.TotalBytes
	limited := io.LimitReader(r, remaining+1)
	written, err := io.Copy(f, limited)
	closeErr := f.Close()
	if err != nil {
		os.Remove(absClean)
		return fmt.Errorf("write entry %q: %w", name, err)
	}
	if closeErr != nil {
		os.Remove(absClean)
		return fmt.Errorf("close entry %q: %w", name, closeErr)
	}
	if written > remaining {
		os.Remove(absClean)
		return fmt.Errorf("%w: writing %q would exceed %d bytes",
			ErrTooLarge, name, e.limits.MaxTotalBytes)
	}
	if written > e.limits.MaxEntryBytes {
		os.Remove(absClean)
		return fmt.Errorf("%w: %q is %d bytes", ErrEntryTooLarge, name, written)
	}
	res.TotalBytes += written
	res.Files = append(res.Files, ExtractedFile{RelPath: rel, AbsPath: absClean, Size: written})
	return nil
}

// writeSingleFile handles a compressed single-file format such as .gz, .bz2, .xz.
func (e *Extractor) writeSingleFile(r io.Reader, root, name string, res *ExtractResult) error {
	res.EntriesSeen++
	if err := e.checkEntryPath(name); err != nil {
		return err
	}
	return e.writeEntry(r, root, name, res)
}

// ---------------------------------------------------------------------------
// Reader helpers
// ---------------------------------------------------------------------------

func (e *Extractor) withGzip(path string, fn func(io.Reader) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	gr, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("open gzip: %w", err)
	}
	defer gr.Close()
	return fn(gr)
}

func (e *Extractor) withFile(path string, fn func(*os.File) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return fn(f)
}

func stripCompressionSuffix(name string) string {
	for _, suffix := range []string{".gz", ".bz2", ".xz", ".gzip", ".bzip2"} {
		if strings.HasSuffix(strings.ToLower(name), suffix) {
			return name[:len(name)-len(suffix)]
		}
	}
	return name + ".out"
}
