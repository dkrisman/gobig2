# QUICKSTART

## Doc map

- [RULES.md](RULES.md) - project-specific rules
- [docs/CONVENTIONS.md](docs/CONVENTIONS.md) - gobig2-specific style
- [docs/LIBRARIES.md](docs/LIBRARIES.md) - dependency policy
- [docs/TESTING.md](docs/TESTING.md) - test categories and bar
- [docs/TOOLS.md](docs/TOOLS.md) - Taskfile-centric workflow

Project info:
- [README.md](README.md) - public-facing overview
- [docs/design/](docs/design/) - design notes (ITU spec deviations, etc.)

## What codebase is

Pure-Go decoder for ITU-T T.88 / ISO/IEC 14492 (**JBIG2**)
bi-level image streams. Two on-the-wire forms supported:
standalone `.jb2` / `.jbig2` files (T.88 Annex E header) and
PDF-embedded segment streams (the bytes a PDF reader's
`/JBIG2Decode` filter delivers). No cgo. No third-party runtime
deps.

Public API lives in three files at repo root:

- [jbig2.go](jbig2.go) - `Decoder`, three constructors, `Decode` /
  `DecodeContext` / `DecodeAll`, `DecodeConfig`, `ParseGlobals`,
  `image.Decode` registration.
- [errors.go](errors.go) - sentinel taxonomy (`ErrMalformed`,
  `ErrResourceBudget`, `ErrUnsupported`).
- [limits.go](limits.go) - `Limits` struct, `DefaultLimits`, the
  process-wide `Apply` cap propagator.

Two CLI binaries under [cmd/](cmd/).

The internal packages mirror the JBIG2 spec's section layout -
each one owns a single decoding procedure or piece of shared
plumbing. The orchestrator is `internal/segment`; everything
else is a leaf the orchestrator dispatches into.

## Code map

### Public API (repo root)
- [jbig2.go](jbig2.go) - package doc, `Decoder`, constructors, decode loop, multi-page handling, `image.Decode` registration.
- [errors.go](errors.go) - `ErrMalformed`, `ErrResourceBudget`, `ErrUnsupported` sentinels.
- [limits.go](limits.go) - `Limits` struct, nine resource caps, `DefaultLimits`, `Apply` (process-wide).
- [example_pdf_test.go](example_pdf_test.go) - runnable godoc example of the PDF-embedded flow.

### CLI binaries
- [cmd/gobig2/](cmd/gobig2/) - decode a `.jb2` / PDF-embedded stream to PNG; exit codes mirror the sentinel taxonomy.
- [cmd/extract-jbig2/](cmd/extract-jbig2/) - walk a PDF, dump every `/JBIG2Decode` image XObject (and `/JBIG2Globals`) as `.jb2` fixture files.
- [cmd/perf-cross/](cmd/perf-cross/) - dev / CI tool: wall-clock bench gobig2 against jbig2dec, mutool, pdfimages, and PDFBox over the bundled SerenityOS subset plus optional `-extra-corpus-dir` synthesized fixtures; emits Markdown + JSON. Driven by `task bench:cross` and the `.github/workflows/perf-linux.yml` workflow.

### Dev / CI scripts
- [scripts/perf/synthesize-corpus.sh](scripts/perf/synthesize-corpus.sh) - generates ~600 dpi A4 JBIG2 fixtures (generic-region + symbol-mode encodings) via `jbig2enc` + ImageMagick, dropped under `./tmp/perf-corpus/` for `cmd/perf-cross` to consume. Wrapped by `task bench:corpus`; CI runs it cached behind `actions/cache`.

### Internal packages - bit / arithmetic plumbing
- [internal/bio/](internal/bio/) - `BitStream` over a byte buffer; the single bit-I/O primitive every higher decoder reaches for.
- [internal/arith/](internal/arith/) - JBIG2 MQ arithmetic coder (T.88 Annex E) plus integer / IAID adapters; inner loop of every context-coded region.
- [internal/huffman/](internal/huffman/) - T.88 Annex B standard tables (B.1-B.15) and user-defined-table parser.
- [internal/mmr/](internal/mmr/) - CCITT Group 4 / T.6 (MMR) bitmap decoding (the non-arithmetic generic-region path).
- [internal/intmath/](internal/intmath/) - small integer helpers shared across decoders, isolated for unit-testability.

### Internal packages - region / dictionary decoders
- [internal/generic/](internal/generic/) - generic-region decoding procedure (T.88 §6.2); workhorse coding mode, also exposes the MMR entry point.
- [internal/refinement/](internal/refinement/) - generic refinement region (T.88 §6.3); delta-context re-rendering used by aggregate symbols and standalone refinement segments.
- [internal/symbol/](internal/symbol/) - symbol dictionary (`sdd.go`, T.88 §6.5) and text region (`trd.go`, T.88 §6.4) decoding.
- [internal/halftone/](internal/halftone/) - pattern dictionary (T.88 §6.7, type-16) and halftone region (T.88 §6.6, type-22/23); coupled because the halftone region indexes into the pattern dict.

### Internal packages - orchestration
- [internal/segment/](internal/segment/) - segment table, header parsing, `Document` orchestrator that dispatches per-segment Procs and stitches results onto the page bitmap.
- [internal/page/](internal/page/) - bi-level `Image` type; packed MSB-first byte buffer with get/set/compose primitives and `image/color.Gray` conversion.
- [internal/state/](internal/state/) - cross-cutting enums and constants shared between decoders and orchestrator; broken out to avoid import cycles.

### Internal packages - input / classification
- [internal/probe/](internal/probe/) - file-header magic detection, organization-mode classification, embedded-stream sniff + endianness heuristic.
- [internal/input/](internal/input/) - bounded `io.Reader` slurp (`MaxBytes` cap), globals sanity check.
- [internal/errs/](internal/errs/) - sentinel error values the public package re-exports; lives here so internal packages can wrap without importing the root.

### Test harness
- [internal/gobig2test/](internal/gobig2test/) - public-API contract tests, SerenityOS corpus runner, ITU-T T.88 Annex A conformance runner (env-gated), fuzz targets, pathological-input regressions, limits enforcement, benchmarks.
- [testdata/serenityos/](testdata/serenityos/) - bundled SerenityOS JBIG2 corpus (always runs).
- [testdata/pdf-embedded/](testdata/pdf-embedded/) - sample PDF-extracted stream for the godoc example.
- [testdata/perf/](testdata/perf/) - 27-fixture perf matrix (300/600 dpi A4 pages under jbig2enc's generic / generic+TPGD / symbol-mode flag combos, plus classifier-tuning specials). Consumed by `BenchmarkPerfCorpus` (`task bench:perf`). Regen via [scripts/perf/build-perf-testdata.sh](scripts/perf/build-perf-testdata.sh) (`task bench:perf-rebuild`).
