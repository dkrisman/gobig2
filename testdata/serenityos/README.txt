SerenityOS LibGfx JBIG2 test inputs.

Source:
  https://github.com/SerenityOS/serenity
  Tests/LibGfx/test-inputs/jbig2/*.jbig2 (107 files)

Vendored verbatim - fixture bytes are the upstream's; gobig2 redistributes
unmodified for use as a regression suite. SerenityOS publishes these test
inputs under a permissive license (BSD 2-clause), so vendoring is allowed
provided the copyright notice + disclaimer below travel with them.

The accompanying check-jbig2-json.sh and jbig2_to_pdf.py from upstream are
SerenityOS's own test orchestration; they're not vendored here. Their JSON
sidecars (the per-fixture segment-dump references the upstream test
harness consumes) aren't vendored either - gobig2's TestSerenityOSCorpus
asserts decode-succeeds-within-budget, not byte-exact-against-segment-
dumps. Cross-decoder pixel-equivalence is verified separately via the
pdfbox-jbig2 oracle in external/pdfbox-jbig2-cli/.

What this corpus covers (per-file naming): annex-h (T.88 Annex H spec
example), bitmap-* (composite ops, halftones, refinement, text regions
under various flag combinations), generic-region-* (templates 0-3 + MMR),
mmr-* (T.6 fax encodings), pattern-dict-* (halftone pattern tables),
refinement-* (generic refinement region), symbol-* + symhuff* (symbol
dictionaries with arith and Huffman coding paths). Every file decodes
bit-exact against pdfbox-jbig2; gobig2's coverage of this corpus is the
project's "we handle real-world JBIG2" floor.

License:

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
