PDF-embedded JBIG2 fixtures.

These streams have no JBIG2 file header - they're the bytes a PDF
reader hands the JBIG2Decode filter directly. Standalone .jb2
files keep an 8-byte magic + 1-byte flags + optional 4-byte page
count prefix; PDF 1.4 §7.4.7 strips that, plus the end-of-page
and end-of-file segments. NewDecoderEmbedded is the entry point
that handles this shape.

sample.jb2
  Source: PDF embedded image, extracted by hand. 3562x851 all-white
          page. 53-byte generic-region payload + 19-byte page-info
          segment is the entire file (94 bytes total).
  Producer: unknown PDF generator.
