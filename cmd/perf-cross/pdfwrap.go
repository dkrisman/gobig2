// pdfwrap builds a minimal single-page PDF that embeds a raw
// JBIG2 segment stream as a /JBIG2Decode-filtered image XObject,
// feeding the PDF-only decoders (mutool, pdfimages, PDFBox) the
// same payload the standalone decoders consume.
//
// Constraints:
//
//   - No /JBIG2Globals plumbing: the wrapped streams must
//     include their dict inline (synthesize-corpus.sh emits the
//     symbol-mode fixture this way by cat output.sym +
//     output.0000).
//   - 1-bit DeviceGray, no ColorSpace alternates.
//   - MediaBox = pixel count in user units so `mutool draw`
//     renders at 1:1; extract-only decoders ignore it.
//
// PDF object layout:
//
//	1 0 obj Catalog
//	2 0 obj Pages tree (Count 1)
//	3 0 obj Page (MediaBox W x H, refs Im0 + Contents)
//	4 0 obj Image XObject (Width W, Height H, /JBIG2Decode)
//	5 0 obj Content stream (paints Im0 at full page)
//
// Cross-reference table per ISO 32000-1 §7.5.4: each entry is
// exactly 20 bytes including a 2-byte EOL marker.
package main

import (
	"bytes"
	"fmt"
	"io"
)

// wrapJBIG2InPDF writes a minimal PDF embedding jb2 as a single
// image XObject of width x height pixels.
//
// jb2 must be JBIG2 segment-stream form (no T.88 Annex E header,
// no EOP / EOF segments). width and height must match the JBIG2
// page-information segment; mismatches trip strict PDF readers
// in the renderer path.
func wrapJBIG2InPDF(w io.Writer, jb2 []byte, width, height int) error {
	if width <= 0 || height <= 0 {
		return fmt.Errorf("pdfwrap: invalid dimensions %dx%d", width, height)
	}
	if len(jb2) == 0 {
		return fmt.Errorf("pdfwrap: empty jbig2 payload")
	}

	// Content stream first - its length goes in the dict below.
	contentStream := fmt.Sprintf("q %d 0 0 %d 0 0 cm /Im0 Do Q\n", width, height)

	// Buffer the whole PDF so xref offsets can be computed
	// without seeking.
	var buf bytes.Buffer

	// %PDF-1.5 + binary marker (4 high-bit bytes; PDF §7.5.2).
	buf.WriteString("%PDF-1.5\n")
	buf.WriteString("%\xE2\xE3\xCF\xD3\n")

	// offsets[N] = byte offset of "N 0 obj" header. Index 0 is
	// the conventional free entry.
	offsets := make([]int, 6)

	writeObj := func(n int, body string) {
		offsets[n] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", n, body)
	}

	// 1: Catalog
	writeObj(1, "<</Type /Catalog /Pages 2 0 R>>")

	// 2: Pages tree
	writeObj(2, "<</Type /Pages /Kids [3 0 R] /Count 1>>")

	// 3: Page. MediaBox in pixel units so mutool draw is 1:1.
	page := fmt.Sprintf("<</Type /Page /Parent 2 0 R /MediaBox [0 0 %d %d] "+
		"/Resources <</XObject <</Im0 4 0 R>>>> /Contents 5 0 R>>",
		width, height)
	writeObj(3, page)

	// 4: Image XObject. Written without writeObj's format
	// string so jb2 bytes go through verbatim, no escaping.
	offsets[4] = buf.Len()
	fmt.Fprintf(&buf, "4 0 obj\n")
	fmt.Fprintf(&buf, "<</Type /XObject /Subtype /Image /Width %d /Height %d "+
		"/BitsPerComponent 1 /ColorSpace /DeviceGray "+
		"/Filter /JBIG2Decode /Length %d>>\n",
		width, height, len(jb2))
	buf.WriteString("stream\n")
	buf.Write(jb2)
	buf.WriteString("\nendstream\nendobj\n")

	// 5: Content stream (paints the image at full page).
	offsets[5] = buf.Len()
	fmt.Fprintf(&buf, "5 0 obj\n<</Length %d>>\nstream\n%sendstream\nendobj\n",
		len(contentStream), contentStream)

	// xref table. PDF §7.5.4: each entry is exactly 20 bytes
	// including a 2-byte EOL (we use SP LF).
	xrefOffset := buf.Len()
	buf.WriteString("xref\n")
	buf.WriteString("0 6\n")
	// Free entry: offset 0, generation 65535, 'f'.
	fmt.Fprintf(&buf, "%010d %05d f \n", 0, 65535)
	for n := 1; n <= 5; n++ {
		fmt.Fprintf(&buf, "%010d %05d n \n", offsets[n], 0)
	}

	// Trailer + startxref + EOF.
	buf.WriteString("trailer\n")
	buf.WriteString("<</Size 6 /Root 1 0 R>>\n")
	buf.WriteString("startxref\n")
	fmt.Fprintf(&buf, "%d\n", xrefOffset)
	buf.WriteString("%%EOF\n")

	_, err := w.Write(buf.Bytes())
	return err
}
