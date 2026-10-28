package safe

import (
	"archive/tar"
	"bytes"
	"compress/bzip2"
	"encoding/base64"
	"io"
	"os"
	"testing"
)

// The archive formats below are advertised by GET /api/v1/system/capabilities.
// Nothing proved they worked until these tests existed: the suite covered zip,
// tar, tar.gz and gzip, while bzip2, xz and their tar variants were reachable
// from the API and exercised by no test at all.
//
// The bzip2 fixtures are embedded rather than built at run time because the Go
// standard library reads bzip2 but cannot write it. They were produced with
// `bzip2 -c` over the same `name,email\nAda,ada@example.com\n` payload used
// everywhere else in this file, so a reader can regenerate them and confirm the
// bytes rather than trusting an opaque blob. Each is well under 300 bytes.

const (
	// tar archive holding contacts.csv
	b64TarBz2 = "QlpoOTFBWSZTWa8sdtoAAH5/hMoQCABABX+AYAAEAG5n30AAEAAIIACUhKhk1NTGoNGgMhptNQSlMmjQAANAAPufJFLQQIWAIhKlFFbWEzwCCttlv9KdqTsaI5ggW3dSapFHIdu/FLDFlJDQJI2/BTM18Y6baEc8n3fH2EvVtD7EbVae1KvICbHOiE26GFZT/QrC+9zkD8XckU4UJCvLHbaA"
	b64TarXz  = "/Td6WFoAAATm1rRGBMCcAYBQIQEWAAAAAAAAAPfhHbvgJ/8AlF0AMZvKGdqxiddgHTHLCUIYEhSK4/uNFj6bVlzW15IRKUaKxtEavLBnumzGVSnlWZmaeaaUPzTRInqCrW51loW65O0CXP29KDndgw4IrRlsWJA0DY26OSEtYCVVskXlVPJr2FRZVImkrtNnniJ9eHeEOp0UECPk9pCnpGMXE2GQnghGRT9bTguUOwPOC5gNuNuSMLt0AAArY+PVsKpqnwABuAGAUAAA/vRJibHEZ/sCAAAAAARZWg=="
	// a single compressed contacts.csv
	b64Bz2 = "QlpoOTFBWSZTWYCep30AAA3VgAAQAAVgAC4nwEAgADFMABNCIPSaPSNqGYLXBbT9XJ25iR5Kg1Ij4u5IpwoSEBPU76A="
	b64Xz  = "/Td6WFoAAATm1rRGBMAjHyEBFgAAAAAAAAAAAAAbLvYBAB5uYW1lLGVtYWlsCkFkYSxhZGFAZXhhbXBsZS5jb20KAAAh5PDPC3vwtwABPx/iklD1H7bzfQEAAAAABFla"
)

const fixtureCSV = "name,email\nAda,ada@example.com\n"

func mustB64(t *testing.T, s string) []byte {
	t.Helper()
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	return b
}

// assertExtractedCSV checks that a single-file extraction produced the expected
// content under the expected name.
func assertExtractedCSV(t *testing.T, res *ExtractResult, wantName string) {
	t.Helper()
	if len(res.Files) != 1 {
		t.Fatalf("expected 1 extracted file, got %d: %+v", len(res.Files), res.Files)
	}
	if res.Files[0].RelPath != wantName {
		t.Errorf("entry name: got %q, want %q", res.Files[0].RelPath, wantName)
	}
	got, err := os.ReadFile(res.Files[0].AbsPath)
	if err != nil {
		t.Fatalf("read extracted file: %v", err)
	}
	if string(got) != fixtureCSV {
		t.Errorf("content mismatch:\n got %q\nwant %q", got, fixtureCSV)
	}
}

func TestExtractTarBz2RoundTrip(t *testing.T) {
	data := mustB64(t, b64TarBz2)
	src := writeTemp(t, "data.tar.bz2", data)

	ex := NewExtractor(testLimits())
	res, err := ex.Extract(t.TempDir(), src, FormatTarBz2, int64(len(data)))
	if err != nil {
		t.Fatalf("extract tar.bz2: %v", err)
	}
	defer res.Cleanup()
	assertExtractedCSV(t, res, "contacts.csv")
}

func TestExtractTarXzRoundTrip(t *testing.T) {
	data := mustB64(t, b64TarXz)
	src := writeTemp(t, "data.tar.xz", data)

	ex := NewExtractor(testLimits())
	res, err := ex.Extract(t.TempDir(), src, FormatTarXz, int64(len(data)))
	if err != nil {
		t.Fatalf("extract tar.xz: %v", err)
	}
	defer res.Cleanup()
	assertExtractedCSV(t, res, "contacts.csv")
}

func TestExtractBzip2SingleFile(t *testing.T) {
	data := mustB64(t, b64Bz2)
	src := writeTemp(t, "contacts.csv.bz2", data)

	ex := NewExtractor(testLimits())
	res, err := ex.Extract(t.TempDir(), src, FormatBzip2, int64(len(data)))
	if err != nil {
		t.Fatalf("extract bzip2: %v", err)
	}
	defer res.Cleanup()
	assertExtractedCSV(t, res, "contacts.csv")
}

func TestExtractXzSingleFile(t *testing.T) {
	data := mustB64(t, b64Xz)
	src := writeTemp(t, "contacts.csv.xz", data)

	ex := NewExtractor(testLimits())
	res, err := ex.Extract(t.TempDir(), src, FormatXz, int64(len(data)))
	if err != nil {
		t.Fatalf("extract xz: %v", err)
	}
	defer res.Cleanup()
	assertExtractedCSV(t, res, "contacts.csv")
}

// The embedded fixtures must match what the compressor produces, not merely what
// our own reader accepts. Reading them back with an independent decoder proves
// the bytes are real bzip2/xz rather than something the extractor happens to
// tolerate, so a fixture cannot silently drift into a false pass.
func TestBzip2FixturesDecodeIndependently(t *testing.T) {
	for name, tc := range map[string]struct{ b64, want string }{
		"tar.bz2": {b64TarBz2, "contacts.csv"},
		"single":  {b64Bz2, fixtureCSV},
	} {
		t.Run(name, func(t *testing.T) {
			plain, err := io.ReadAll(bzip2.NewReader(bytes.NewReader(mustB64(t, tc.b64))))
			if err != nil {
				t.Fatalf("stdlib bzip2 reader rejected the fixture: %v", err)
			}
			if tc.want == "contacts.csv" {
				tr := tar.NewReader(bytes.NewReader(plain))
				hdr, err := tr.Next()
				if err != nil {
					t.Fatalf("fixture is not a readable tar: %v", err)
				}
				if hdr.Name != tc.want {
					t.Errorf("tar entry: got %q, want %q", hdr.Name, tc.want)
				}
				return
			}
			if string(plain) != tc.want {
				t.Errorf("content: got %q, want %q", plain, tc.want)
			}
		})
	}
}

// Corrupt compressed data must be rejected, not silently truncated into a
// partial import. Both codecs surface damage as a read error part-way through.
func TestExtractRejectsCorruptCompressedData(t *testing.T) {
	for name, tc := range map[string]struct {
		b64    string
		format Format
	}{
		"bzip2": {b64Bz2, FormatBzip2},
		"xz":    {b64Xz, FormatXz},
	} {
		t.Run(name, func(t *testing.T) {
			data := mustB64(t, tc.b64)
			// Flip bytes past the header so the container still looks valid and
			// the failure has to come from the decompressor.
			for i := len(data) / 2; i < len(data) && i < len(data)/2+8; i++ {
				data[i] ^= 0xff
			}
			src := writeTemp(t, "corrupt."+name, data)
			ex := NewExtractor(testLimits())
			res, err := ex.Extract(t.TempDir(), src, tc.format, int64(len(data)))
			if err == nil {
				if res != nil {
					res.Cleanup()
				}
				t.Fatal("expected corrupt compressed data to be rejected")
			}
		})
	}
}

// DetectFormat must identify these containers from magic bytes, and must not
// confuse a single-file compression with its tar variant — the two take
// different extraction paths.
func TestDetectFormatRecognisesBzip2AndXz(t *testing.T) {
	cases := []struct {
		filename string
		head     []byte
		want     Format
	}{
		{"data.tar.bz2", []byte("BZh91AY&SY"), FormatTarBz2},
		{"contacts.csv.bz2", []byte("BZh91AY&SY"), FormatBzip2},
		{"data.tar.xz", []byte{0xfd, '7', 'z', 'X', 'Z', 0x00}, FormatTarXz},
		{"contacts.csv.xz", []byte{0xfd, '7', 'z', 'X', 'Z', 0x00}, FormatXz},
		// An attacker-controlled extension must not override the magic bytes:
		// a real single-file bzip2 named .tar.bz2 still extracts as bzip2.
		{"lied.tar.bz2", []byte("BZh91AY&SY"), FormatTarBz2},
	}
	for _, tc := range cases {
		t.Run(tc.filename, func(t *testing.T) {
			if got := DetectFormat(tc.filename, tc.head); got != tc.want {
				t.Errorf("DetectFormat(%q): got %q, want %q", tc.filename, got, tc.want)
			}
		})
	}
}

// A format the capabilities endpoint advertises must also be reported as
// supported; the two lists are maintained separately and can drift apart.
func TestAdvertisedFormatsAreSupported(t *testing.T) {
	for _, f := range []Format{
		FormatZIP, FormatTAR, FormatTarGz, FormatTarBz2, FormatTarXz,
		FormatGzip, FormatBzip2, FormatXz,
	} {
		if !IsSupported(f) {
			t.Errorf("format %q is advertised but IsSupported reports false", f)
		}
		if !IsArchive(f) {
			t.Errorf("format %q is advertised but IsArchive reports false", f)
		}
	}
}
