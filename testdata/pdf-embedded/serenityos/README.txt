PDF-extracted JBIG2 streams from the SerenityOS corpus.

Source:
  https://github.com/SerenityOS/serenity
  Tests/LibGfx/test-inputs/jbig2/*.jbig2 wrapped in minimal
  PDFs by upstream's jbig2_to_pdf.py helper.

These bytes are the **embedded** form of the same JBIG2 streams
that live under testdata/serenityos/ - i.e., what a PDF reader's
/JBIG2Decode filter actually sees after PDF §7.4.7 strips the
8-byte file magic, the 1-byte flags, the optional 4-byte page-
count word, and the end-of-page (type 49) + end-of-file (type
51) tail markers. Each .jb2 here is roughly 24 bytes shorter
than the corresponding standalone .jbig2 in testdata/serenityos/.

Layout:

  <basename>-obj<N>.jb2          - the image XObject's stream
  <basename>-obj<N>.globals.jb2  - the /JBIG2Globals stream (when
                                    referenced via /DecodeParms)
  <basename>-obj<N>.txt          - provenance: source PDF basename,
                                    object number, dimensions

The TestPDFEmbeddedCorpus harness pairs each .jb2 with its
optional .globals.jb2 sibling and decodes via
NewDecoderEmbeddedWithGlobals (parsed once per pair). 98 image
fixtures + 1 globals stream (bitmap-p32-eof-obj6 carries
/DecodeParms /JBIG2Globals 3 0 R).

Extracted via cmd/extract-jbig2/ - see
docs/EXTRACTING.md if it ever lands; the source PDFs are too
large to commit (~190 KB total but lots of binary noise) but
extraction is reproducible from the SerenityOS corpus + their
jbig2_to_pdf.py.

License (same as testdata/serenityos/, since these are derived
from those bytes wrapped in PDF):

  Copyright (c) 2018-2024, SerenityOS contributors

  Redistribution and use in source and binary forms, with or without
  modification, are permitted provided that the following conditions are
  met:

  1. Redistributions of source code must retain the above copyright
     notice, this list of conditions and the following disclaimer.

  2. Redistributions in binary form must reproduce the above copyright
     notice, this list of conditions and the following disclaimer in the
     documentation and/or other materials provided with the distribution.

  THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS
  IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED
  TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A
  PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
  HOLDER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
  SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
  LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
  DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
  THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
  (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
  OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
