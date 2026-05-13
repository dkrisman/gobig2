package main

import (
	"bytes"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestWrapJBIG2InPDFStructure validates wrapJBIG2InPDF output as
// a structurally-sound PDF: header, indirect-object headers at
// the byte offsets the xref table claims, startxref pointing at
// "xref\n". No external PDF parser - the test must run in CI
// with stock Go.
func TestWrapJBIG2InPDFStructure(t *testing.T) {
	jb2 := []byte("FAKEJBIG2PAYLOAD\x00\x01\x02")
	var buf bytes.Buffer
	if err := wrapJBIG2InPDF(&buf, jb2, 399, 400); err != nil {
		t.Fatalf("wrap: %v", err)
	}
	data := buf.Bytes()

	if !bytes.HasPrefix(data, []byte("%PDF-1.5\n")) {
		t.Fatalf("missing PDF header: %q", data[:8])
	}
	if !bytes.HasSuffix(bytes.TrimRight(data, "\n"), []byte("%%EOF")) {
		t.Fatalf("missing %%EOF marker; tail=%q", tail(data, 32))
	}

	// JBIG2 payload must appear verbatim inside the image
	// stream - escape / encoding would corrupt it.
	if !bytes.Contains(data, jb2) {
		t.Fatalf("jbig2 payload not present verbatim in PDF")
	}

	// Image dict must reference our dimensions.
	for _, want := range []string{"/Width 399", "/Height 400", "/Filter /JBIG2Decode", "/BitsPerComponent 1"} {
		if !bytes.Contains(data, []byte(want)) {
			t.Fatalf("image dict missing %q; PDF:\n%s", want, data)
		}
	}

	// MediaBox should be pixel-sized so a 1:1 render is possible.
	if !bytes.Contains(data, []byte("/MediaBox [0 0 399 400]")) {
		t.Fatalf("MediaBox not at pixel size")
	}

	// startxref offset must point at the byte where "xref\n"
	// starts. Parse the trailer fields and check.
	startIx := bytes.LastIndex(data, []byte("startxref\n"))
	if startIx < 0 {
		t.Fatal("no startxref marker")
	}
	rest := data[startIx+len("startxref\n"):]
	nl := bytes.IndexByte(rest, '\n')
	if nl < 0 {
		t.Fatal("no newline after startxref offset")
	}
	off, err := strconv.Atoi(string(rest[:nl]))
	if err != nil {
		t.Fatalf("startxref not an integer: %v", err)
	}
	if !bytes.HasPrefix(data[off:], []byte("xref\n")) {
		t.Fatalf("startxref points at offset %d but data there is %q (want \"xref\\n\")",
			off, data[off:min(off+8, len(data))])
	}

	// Each "N 0 obj\n" offset declared in xref must match where
	// that header actually lives in the buffer.
	xrefBody := data[off+len("xref\n"):]
	// First line of xref body is "0 6\n".
	if !bytes.HasPrefix(xrefBody, []byte("0 6\n")) {
		t.Fatalf("xref subsection header wrong: %q", xrefBody[:8])
	}
	xrefBody = xrefBody[len("0 6\n"):]
	// 6 entries, each exactly 20 bytes including EOL.
	for n := 0; n < 6; n++ {
		entry := xrefBody[n*20 : n*20+20]
		if len(entry) != 20 {
			t.Fatalf("xref entry %d wrong length: %d", n, len(entry))
		}
		// Skip the free entry (object 0).
		if n == 0 {
			if string(entry[:18]) != "0000000000 65535 f" {
				t.Fatalf("free entry wrong: %q", entry)
			}
			continue
		}
		fields := strings.Fields(string(entry))
		if len(fields) < 3 {
			t.Fatalf("entry %d malformed: %q", n, entry)
		}
		declared, err := strconv.Atoi(fields[0])
		if err != nil {
			t.Fatalf("entry %d offset not numeric: %q", n, entry)
		}
		want := []byte(strconv.Itoa(n) + " 0 obj")
		if !bytes.HasPrefix(data[declared:], want) {
			t.Fatalf("xref entry %d claims offset %d, but bytes there are %q (want prefix %q)",
				n, declared, data[declared:min(declared+12, len(data))], want)
		}
	}

	// Sanity: only one xref table, one trailer dict.
	if c := bytes.Count(data, []byte("\nxref\n")); c != 1 {
		t.Fatalf("expected 1 xref table, got %d", c)
	}
	if c := bytes.Count(data, []byte("\ntrailer\n")); c != 1 {
		t.Fatalf("expected 1 trailer, got %d", c)
	}
}

// TestWrapJBIG2InPDFRejectsBadInputs pins the argument-error
// returns for zero dims / empty payload.
func TestWrapJBIG2InPDFRejectsBadInputs(t *testing.T) {
	cases := []struct {
		name string
		jb2  []byte
		w, h int
		want string
	}{
		{"zero width", []byte{0x01}, 0, 10, "invalid dimensions"},
		{"zero height", []byte{0x01}, 10, 0, "invalid dimensions"},
		{"empty payload", nil, 10, 10, "empty jbig2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := wrapJBIG2InPDF(&buf, tc.jb2, tc.w, tc.h)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want error containing %q, got %q", tc.want, err)
			}
		})
	}
}

// TestParseDimensions exercises the sidecar parser directly.
// Regression here would zero PDF /Width and /Height.
func TestParseDimensions(t *testing.T) {
	cases := []struct {
		in   string
		w, h int
		err  bool
	}{
		{"source: foo\nobject: 5\ndimensions: 399x400\n", 399, 400, false},
		{"dimensions: 1x1\n", 1, 1, false},
		{"dimensions:  4096 x 4096 \n", 4096, 4096, false},
		{"no dims here\n", 0, 0, true},
		{"dimensions: 100\n", 0, 0, true},
		{"dimensions: badxgood\n", 0, 0, true},
	}
	for _, tc := range cases {
		w, h, err := parseDimensions(tc.in)
		if (err != nil) != tc.err {
			t.Errorf("parseDimensions(%q) err=%v, wantErr=%v", tc.in, err, tc.err)
			continue
		}
		if !tc.err && (w != tc.w || h != tc.h) {
			t.Errorf("parseDimensions(%q) = (%d, %d), want (%d, %d)", tc.in, w, h, tc.w, tc.h)
		}
	}
}

// TestFormatCellAndSpeedup pins cell-format policy: skip ->
// "skip", failure -> "FAIL", missing speedup -> "-".
func TestFormatCellAndSpeedup(t *testing.T) {
	if got := formatCell(measurement{Skipped: true, SkipReason: "not installed"}); got != "skip" {
		t.Errorf("formatCell skip: %q", got)
	}
	if got := formatCell(measurement{}); got != "FAIL" {
		t.Errorf("formatCell !OK: %q", got)
	}
	if got := formatCell(measurement{OK: true, BestMs: 12.345}); got != "12.35" {
		t.Errorf("formatCell ok: %q", got)
	}
	base := measurement{OK: true, BestMs: 10}
	other := measurement{OK: true, BestMs: 5}
	if got := formatSpeedup(base, other); got != "0.50x" {
		t.Errorf("formatSpeedup gobig2 slower: %q", got)
	}
	if got := formatSpeedup(base, measurement{Skipped: true}); got != "-" {
		t.Errorf("formatSpeedup skipped: %q", got)
	}
}

// tail returns the last n bytes of b (or all of b if shorter).
func tail(b []byte, n int) []byte {
	if len(b) <= n {
		return b
	}
	return b[len(b)-n:]
}

// Canary that the literal Markdown table separators stay
// regex-parseable.
var _ = regexp.MustCompile(`^\| --- \|( ---: \|)+$`)

// TestLoadExtraCorpus seeds a fake corpus dir with three
// shapes - full triple, no embedded form, orphan
// `-embedded.jb2` - and pins how loadExtraCorpus classifies each.
func TestLoadExtraCorpus(t *testing.T) {
	dir := t.TempDir()
	mustWrite := func(name, body string) {
		t.Helper()
		if err := writeFile(dir+"/"+name, []byte(body)); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}
	mustWrite("perf-text-generic.jb2", "STANDALONE")
	mustWrite("perf-text-generic-embedded.jb2", "EMBEDDED")
	mustWrite("perf-text-generic.txt", "dimensions: 4960x7016\n")

	mustWrite("perf-text-symbol.jb2", "STANDALONE-SYMBOL")
	mustWrite("perf-text-symbol.txt", "dimensions: 4960x7016\n")
	// No embedded form for the symbol variant - PDF decoders skip.

	// Stray *-embedded.jb2 with no standalone counterpart.
	// Loader should warn (to stderr) and not produce a fixture.
	mustWrite("orphan-embedded.jb2", "ORPHAN")

	fixtures, err := loadExtraCorpus(dir)
	if err != nil {
		t.Fatalf("loadExtraCorpus: %v", err)
	}
	if len(fixtures) != 2 {
		t.Fatalf("expected 2 fixtures, got %d: %+v", len(fixtures), fixtures)
	}
	// Lexicographic order: generic then symbol.
	if fixtures[0].Name != "perf-text-generic" {
		t.Fatalf("fixture[0].Name=%q", fixtures[0].Name)
	}
	if fixtures[0].EmbeddedPath == "" {
		t.Errorf("generic fixture should have embedded form: %+v", fixtures[0])
	}
	if fixtures[0].Width != 4960 || fixtures[0].Height != 7016 {
		t.Errorf("generic fixture dims: %dx%d", fixtures[0].Width, fixtures[0].Height)
	}
	if fixtures[1].Name != "perf-text-symbol" {
		t.Fatalf("fixture[1].Name=%q", fixtures[1].Name)
	}
	if fixtures[1].EmbeddedPath != "" {
		t.Errorf("symbol fixture should NOT have embedded form: %+v", fixtures[1])
	}
}

// writeFile is a one-line os.WriteFile alias for the seed loop.
func writeFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0o600)
}
