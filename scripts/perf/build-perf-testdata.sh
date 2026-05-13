#!/usr/bin/env bash
#
# Build the testdata/perf/ fixture matrix consumed by the
# in-process bench at
# internal/gobig2test/perf_corpus_bench_test.go.
#
# Output layout under $1 (default <repo>/testdata/perf), one
# pair per fixture:
#
#   <name>.jb2   T.88 Annex E standalone form
#                (consumed via gobig2.NewDecoder)
#   <name>.txt   sidecar: source bitmap, encoder flags,
#                dimensions
#
# Matrix shape (28 fixtures):
#
#   - 6 text-bearing source bitmaps spanning size (300/600 dpi
#     A4), font family (DejaVu Sans-Mono / Sans / Serif), and
#     density (dense / sparse / mixed text+shapes). Three
#     baseline encoder combos each (generic / generic+TPGD /
#     symbol) = 18 fixtures.
#   - 1 inverted-canvas variant (mono-600-inv) under generic
#     and generic+TPGD - flips the white/black pixel ratio so
#     the MQ coder walks different Qe states. Symbol mode is
#     omitted; jbig2enc's connected-component step treats the
#     inverted background as one giant symbol that trips
#     Limits.MaxSymbolPixels on decode.
#   - 1 ordered-dither source - no repeating symbols; under
#     generic + generic+TPGD. Stresses the arith coder's
#     per-pixel context cost rather than the symbol classifier.
#   - 5 classifier-tuning specials on the canonical mono-600
#     source: -s -t {0.85, 0.97}, -s -w {0.3, 0.7}, -s -a.
#
# Total 27. Symbol + refinement (-s -r) is omitted entirely
# because jbig2enc's refinement encoder is broken in the
# universe build. Refinement decode is still covered by the
# SerenityOS bitmap-composite-*-refine.jbig2 fixtures.
#
# Tool prerequisites match scripts/perf/synthesize-corpus.sh:
#
#   - imagemagick + fonts-dejavu-core (text -> PBM)
#   - jbig2 (Ubuntu binary for agl/jbig2enc; universe repo)
#
# This script is the testdata-folder analog of
# synthesize-corpus.sh - that one writes to ./tmp/perf-corpus/
# for the cross-decoder wall-clock bench
# ([cmd/perf-cross](../../cmd/perf-cross/)); this one writes
# checked-in fixtures for the Go in-process bench.

set -euo pipefail

ROOT_DEFAULT="$(cd "$(dirname "$0")/../.." && pwd)/testdata/perf"
ROOT="${1:-$ROOT_DEFAULT}"
mkdir -p "$ROOT"

need_tool() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "build-perf-testdata.sh: missing required tool: $1" >&2
    exit 1
  }
}
need_tool convert
need_tool jbig2

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

# ---- step 1: render source PBMs --------------------------------------------

# Tile a small caption across a canvas. Inline caption keeps
# us clear of ImageMagick's @file path-domain block on noble.
render_tiled() {
  local out="$1" font="$2" pt="$3" w="$4" h="$5"
  local tile="$WORK/${out}-tile.pbm"
  convert \
    -size 400x80 \
    -background white \
    -fill black \
    -font "$font" \
    -pointsize "$pt" \
    -gravity Center \
    caption:'Lorem ipsum dolor sit amet, consectetur adipiscing.' \
    -monochrome \
    "$tile"
  convert \
    -size "${w}x${h}" \
    "tile:${tile}" \
    -monochrome \
    "$WORK/${out}.pbm"
}

# 600 dpi A4 dense mono - canonical "lots of text on a big
# page" shape. Same source as the legacy perf-text-* fixtures.
render_tiled mono-600  DejaVu-Sans-Mono 28 4960 7016

# 300 dpi A4 dense mono - half-linear-resolution variant,
# ~1/4 the pixel work but same coding shape.
render_tiled mono-300  DejaVu-Sans-Mono 16 2480 3508

# 300 dpi A4 sans - same dimensions as mono-300, variable-
# width glyph shapes; produces a differently-sized symbol
# dictionary under -s.
render_tiled sans-300  DejaVu-Sans 16 2480 3508

# 300 dpi A4 serif - serifed glyphs encode larger; more
# arith work per symbol under -s.
render_tiled serif-300 DejaVu-Serif 16 2480 3508

# 600 dpi A4 sparse - single block of large text on a mostly-
# white canvas. Inverts the white/black pixel ratio relative
# to the dense sources; arith coder sits in different Qe
# states.
convert \
  -size 4960x7016 \
  xc:white \
  -fill black \
  -font DejaVu-Sans \
  -pointsize 72 \
  -gravity North -annotate +0+600 'Sparse heading text sample' \
  -gravity Center -annotate +0+0 'Lorem ipsum dolor sit amet' \
  -gravity South -annotate +0+600 'Footer text content area' \
  -monochrome \
  "$WORK/sparse-600.pbm"

# 600 dpi A4 mixed - dense text tile plus drawn rules and
# filled rectangles. Text-region + generic-region hybrid;
# matches what real PDF pages tend to look like.
render_tiled mixed-600-base DejaVu-Sans-Mono 28 4960 7016
convert \
  "$WORK/mixed-600-base.pbm" \
  -fill black \
  -draw "rectangle 400,300 1200,500" \
  -draw "rectangle 3700,300 4500,500" \
  -draw "rectangle 400,6500 4500,6700" \
  -draw "line 100,1000 4860,1000" \
  -draw "line 100,3500 4860,3500" \
  -draw "line 100,5500 4860,5500" \
  -monochrome \
  "$WORK/mixed-600.pbm"

# 600 dpi A4 mono inverted - black background, white text. No
# coding-shape change vs mono-600, but the MQ coder's
# per-context Qe walks land elsewhere.
convert \
  "$WORK/mono-600.pbm" \
  -negate \
  -monochrome \
  "$WORK/mono-600-inv.pbm"

# 300 dpi A4 ordered-dither - gradient ramped through an 8x8
# dither matrix yields a high-entropy bi-level field with no
# repeating shapes; symbol coder degenerates to mostly
# generic-region behavior. Stresses the arith coder's
# per-pixel context cost rather than the symbol classifier.
convert \
  -size 2480x3508 \
  gradient:black-white \
  -ordered-dither o8x8 \
  -monochrome \
  "$WORK/dithered-300.pbm"

# ---- step 2: encode matrix -------------------------------------------------

# Emit one (.jb2, .txt) pair. `flags` is the verbatim
# jbig2enc arg vector for the sidecar; `${@:5}` is what we
# actually pass to the binary.
emit() {
  local name="$1" src="$2" dims="$3" flags="$4"
  shift 4
  local out="$ROOT/${name}.jb2"
  jbig2 "$@" "$WORK/${src}.pbm" > "$out"
  cat > "$ROOT/${name}.txt" <<EOF
source: ${src}.pbm
encoder: jbig2enc ${flags}
dimensions: ${dims}
EOF
}

# Per-source baseline matrix (generic, generic+TPGD, symbol).
# One source per row, three encoder combos each.
for row in \
  "mono-600 4960x7016" \
  "mono-300 2480x3508" \
  "sans-300 2480x3508" \
  "serif-300 2480x3508" \
  "sparse-600 4960x7016" \
  "mixed-600 4960x7016"
do
  src="${row% *}"
  dims="${row#* }"

  emit "${src}-generic"      "$src" "$dims" "(default)"
  emit "${src}-generic-tpgd" "$src" "$dims" "-d (TPGD duplicate-line removal)" -d
  emit "${src}-symbol"       "$src" "$dims" "-s (symbol mode)"                 -s
done

# Inverted-canvas variant - generic paths only. Symbol mode
# is skipped because jbig2enc treats the inverted background
# as one giant connected component and emits a symbol bitmap
# that trips Limits.MaxSymbolPixels on decode.
emit "mono-600-inv-generic"      "mono-600-inv" "4960x7016" "(default)"
emit "mono-600-inv-generic-tpgd" "mono-600-inv" "4960x7016" "-d (TPGD)"        -d

# Dithered source: only generic + generic+TPGD make sense.
emit "dithered-300-generic"      "dithered-300" "2480x3508" "(default)"
emit "dithered-300-generic-tpgd" "dithered-300" "2480x3508" "-d (TPGD)"        -d

# Classifier-tuning specials on the canonical mono-600
# source. Covers the symbol-mode tunables not otherwise
# exercised: classification threshold (-t), weight (-w),
# auto-threshold (-a).
emit "mono-600-symbol-thresh-low"  mono-600 4960x7016 "-s -t 0.85 (looser, larger dict)"   -s -t 0.85
emit "mono-600-symbol-thresh-high" mono-600 4960x7016 "-s -t 0.97 (tighter, smaller dict)" -s -t 0.97
emit "mono-600-symbol-weight-low"  mono-600 4960x7016 "-s -w 0.3 (low classifier weight)"  -s -w 0.3
emit "mono-600-symbol-weight-high" mono-600 4960x7016 "-s -w 0.7 (high classifier weight)" -s -w 0.7
emit "mono-600-symbol-auto"        mono-600 4960x7016 "-s -a (auto-threshold)"             -s -a

echo "perf testdata written to ${ROOT}:"
ls -la "$ROOT" | awk 'NR==1 || /\.jb2$/'
