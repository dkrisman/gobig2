#!/usr/bin/env bash
#
# Regenerate cmd/gobig2/default.pgo from a run across every
# testdata/perf fixture. Go's PGO uses default.pgo
# automatically when present in the main package directory;
# the file should be regenerated after material codec changes
# so the inliner sees the current hot paths.
#
# Steps:
#
#   1. Build a non-PGO binary with --cpuprofile capture.
#   2. Run it once per fixture, dumping a per-fixture pprof.
#      A handful of warm-up iterations per fixture amortize
#      Go runtime startup so the profile reflects decode work.
#   3. Merge the per-fixture profiles into one combined pprof.
#   4. Drop the result at cmd/gobig2/default.pgo.
#
# The resulting file is checked in so fresh clones pick up
# the PGO build automatically (`go build ./cmd/gobig2`
# auto-detects default.pgo). Don't bypass this script - hand-
# rolled profiles drift from the workload mix the matrix
# represents and the inliner's hints stop matching reality.

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
CORPUS="${ROOT}/testdata/perf"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

WARMUP="${WARMUP:-1}"      # iters discarded before profile starts

cd "$ROOT"

echo "==> building cpuprofile-capable binary (-pgo=off, GOAMD64=v3)"
# Match the release build target so the captured profile maps
# to the same codegen the released binary uses.
GOAMD64=v3 go build -pgo=off -o "${WORK}/gobig2" ./cmd/gobig2

# Per-fixture iteration counts balance sample weight across
# regimes. Big 600 dpi pages (~170 ms decode each) dominate
# sample count fast; small 300 dpi symbol fixtures (~10 ms
# decode) contribute disproportionately little. Without
# rebalancing the resulting profile is ~90 % generic-mode work
# and PGO mis-tunes the symbol-mode call sites. Aiming for
# roughly equal wall-time per fixture by giving small fixtures
# more iterations.
fixture_iters() {
  case "$1" in
    sparse-600-generic-tpgd|sparse-600-symbol)        echo 16 ;;
    mono-300-*|sans-300-*|serif-300-*|dithered-300-*) echo 12 ;;
    *symbol*)                                         echo 8 ;;
    *)                                                echo 4 ;;
  esac
}

echo "==> capturing per-fixture profiles"
profiles=()
for jb2 in "${CORPUS}"/*.jb2; do
  name="$(basename "${jb2}" .jb2)"
  prof="${WORK}/${name}.pprof"
  iters="$(fixture_iters "$name")"
  for ((i = 0; i < iters; i++)); do
    out="${WORK}/out-${name}-${i}.pbm"
    if [ "$i" -lt "$WARMUP" ]; then
      # Warmup runs without profile collection.
      "${WORK}/gobig2" --format=pbm "${jb2}" "${out}" >/dev/null 2>&1
    else
      "${WORK}/gobig2" --cpuprofile="${prof}.${i}" \
        --format=pbm "${jb2}" "${out}" >/dev/null 2>&1
    fi
  done
  # Merge this fixture's per-run profiles.
  if compgen -G "${prof}.*" >/dev/null; then
    go tool pprof -proto -output="${prof}" "${prof}".* >/dev/null
    profiles+=("${prof}")
  fi
done

echo "==> merging ${#profiles[@]} per-fixture profiles"
go tool pprof -proto -output="${WORK}/default.pgo" "${profiles[@]}" >/dev/null

# Sanity: confirm the merged profile is reasonably-sized and
# contains the expected hot functions.
echo "==> top-5 by flat in merged profile"
go tool pprof -top -flat -nodecount=5 "${WORK}/default.pgo" \
  | sed -n '/flat%/,/^$/p' | head -10

mv "${WORK}/default.pgo" "${ROOT}/cmd/gobig2/default.pgo"
echo "==> wrote $(stat -c '%s' "${ROOT}/cmd/gobig2/default.pgo") bytes to cmd/gobig2/default.pgo"
echo "==> rebuild gobig2 to pick up the new PGO profile (go auto-detects default.pgo)"
