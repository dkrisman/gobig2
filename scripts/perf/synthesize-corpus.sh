#!/usr/bin/env bash
#
# Synthesize large JBIG2 fixtures for cmd/perf-cross.
#
# Bundled SerenityOS fixtures decode in 1-10 ms, which puts
# wall-clock benchmarks deep in subprocess-startup territory -
# the C reference jbig2dec wins on binary-load time, PDFBox
# loses on JVM startup, and gobig2's actual decode hot path
# barely runs. The fixtures this script produces are ~600 dpi
# A4 pages encoded multiple ways, targeting ~100-300 ms decode
# time on jbig2dec so JVM startup is at most ~5x the decode
# cost rather than ~250x.
#
# Output layout under $1 (default ./tmp/perf-corpus/), one triple
# per fixture:
#
#   <name>.jb2           T.88 Annex E standalone form
#                        (consumed by gobig2 + jbig2dec)
#   <name>-embedded.jb2  headerless segment-stream form
#                        (consumed by mutool / pdfimages / pdfbox
#                        after cmd/perf-cross wraps it in a PDF)
#   <name>.txt           dimensions sidecar in the same grammar
#                        as testdata/pdf-embedded/serenityos/*.txt
#
# A fixture without an `<name>-embedded.jb2` (e.g. symbol-mode
# encoding which would need /JBIG2Globals plumbing we haven't
# wired into pdfwrap.go yet) gets skipped for the PDF decoders
# cleanly.
#
# Tool prerequisites (all apt-installable on Ubuntu 24.04):
#
#   - imagemagick + fonts-dejavu-core (text rendering -> PBM)
#   - jbig2                            (Ubuntu binary package for
#                                       agl/jbig2enc; provides the
#                                       `jbig2` CLI used below.
#                                       universe repo on noble.)
#
# CI installs these in .github/workflows/perf-linux.yml.
#
# ImageMagick policy on noble blocks the `@file` syntax under the
# `path` domain to defang historical MSL/MVG read-arbitrary-file
# exploits. We work around it by tiling a small text tile across
# the page canvas instead of streaming a long lorem-ipsum body
# through -annotate @file - tile rendering uses no file paths and
# the tile itself is generated from inline string args.

set -euo pipefail

OUT="${1:-./tmp/perf-corpus}"
mkdir -p "$OUT"

# 600 dpi A4 portrait. Tested target: jbig2dec decode time in
# the 100-300 ms range on ubuntu-24.04 hosted runners.
W=4960
H=7016

need_tool() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "synthesize-corpus.sh: missing required tool: $1" >&2
    exit 1
  }
}
need_tool convert
need_tool jbig2

WORK="$(cd "$OUT" && pwd)"
cd "${WORK}"

# Step 1: render a small text tile. Short string passed inline
# (no @file, no policy violation). Tile size 400x80 px @ 28 pt
# fits "Lorem ipsum dolor sit amet, consectetur adipiscing." on
# two lines with margin - text content is incidental, what matters
# is that we get glyph-shaped repeating black-on-white structure
# the JBIG2 encoder can find symbols in.
TILE="${WORK}/text-tile.pbm"
convert \
  -size 400x80 \
  -background white \
  -fill black \
  -font 'DejaVu-Sans-Mono' \
  -pointsize 28 \
  -gravity Center \
  caption:'Lorem ipsum dolor sit amet, consectetur adipiscing.' \
  -monochrome \
  "${TILE}"

# Step 2: tile the small text image across the full A4 canvas.
# `tile:<path>` repeats the input to fill `-size`; -monochrome
# pins output to 1-bit which jbig2enc requires.
PBM="${WORK}/text-page.pbm"
convert \
  -size "${W}x${H}" \
  "tile:${TILE}" \
  -monochrome \
  "${PBM}"

# Generic-region only (no -s) - emits both forms without globals.
jbig2 text-page.pbm > perf-text-generic.jb2

# `jbig2 -p` segment-stream emission varies by build: some
# versions write to `output.0000` in the cwd (and silent stdout),
# others stream the payload straight to stdout (no file). We
# capture stdout regardless and probe both landing spots; if the
# stdout capture is the populated one, the cwd-file branch is a
# no-op. Without the redirect, the version-that-writes-to-stdout
# floods the CI log with raw JBIG2 bytes.
rm -f output.0000 output.sym  # paranoia: previous run leftovers
jbig2 -p text-page.pbm > jbig2-p-stdout.bin
if [[ -s output.0000 ]]; then
  mv output.0000 perf-text-generic-embedded.jb2
  rm -f jbig2-p-stdout.bin
elif [[ -s jbig2-p-stdout.bin ]]; then
  mv jbig2-p-stdout.bin perf-text-generic-embedded.jb2
else
  echo "synthesize-corpus.sh: jbig2 -p produced no segment stream (neither output.0000 nor stdout)" >&2
  exit 1
fi
rm -f output.sym

cat > perf-text-generic.txt <<EOF
source: synthesize-corpus.sh
content: tiled text canvas (400x80 tile across ${W}x${H})
encoder: jbig2enc (generic-region only, no symbol dictionary)
dimensions: ${W}x${H}
EOF

# Symbol-mode encoding (-s) - text region + symbol dictionary
# path.
#
# Standalone form: straight `jbig2 -s` emits an Annex E file with
# the symbol dict inline as a regular segment.
#
# Embedded form: `jbig2 -s -p` produces two files - `output.0000`
# (page segment stream) and `output.sym` (the symbol dictionary
# segment). Concatenating them gives a self-contained segment
# stream with the dict inline ahead of the page, which PDF
# readers consume verbatim through /JBIG2Decode (no /JBIG2Globals
# plumbing required on our PDF wrapper side). When this build of
# jbig2enc emits the streams to stdout instead of files we
# capture stdout and assume it already contains the same dict +
# page concatenation.
jbig2 -s text-page.pbm > perf-text-symbol.jb2

rm -f output.0000 output.sym jbig2-sp-stdout.bin
jbig2 -s -p text-page.pbm > jbig2-sp-stdout.bin
case "$([[ -s output.sym ]] && echo F)$([[ -s output.0000 ]] && echo F)$([[ -s jbig2-sp-stdout.bin ]] && echo S)" in
  FF*)
    cat output.sym output.0000 > perf-text-symbol-embedded.jb2
    ;;
  *S)
    mv jbig2-sp-stdout.bin perf-text-symbol-embedded.jb2
    ;;
  *)
    echo "synthesize-corpus.sh: jbig2 -s -p produced neither output.sym+output.0000 nor a non-empty stdout stream" >&2
    exit 1
    ;;
esac
rm -f output.0000 output.sym jbig2-sp-stdout.bin

cat > perf-text-symbol.txt <<EOF
source: synthesize-corpus.sh
content: tiled text canvas (400x80 tile across ${W}x${H})
encoder: jbig2enc (symbol mode)
dimensions: ${W}x${H}
EOF

rm -f text-tile.pbm text-page.pbm

echo "perf-corpus written to ${WORK}:"
ls -la "${WORK}"
