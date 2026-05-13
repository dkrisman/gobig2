# gobig2 CLI

`cmd/gobig2` decodes a standalone JBIG2 file (`.jb2` / `.jbig2`)
or a PDF-embedded segment stream into a PNG, PBM, or raw bitmap.
Thin wrapper around the [Go API](jbig2.go); the same flags map
1:1 to the public constructors and the [`Limits`](limits.go)
struct.

## Synopsis

```
gobig2 [flags] <input> [output]
```

- `<input>` - path to a `.jb2` / `.jbig2` file, or `-` for stdin.
- `[output]` - destination path; default is stdout in PNG.

## Flags

| Flag | Default | Description |
| --- | --- | --- |
| `--globals PATH` | (none) | Path to a `/JBIG2Globals` stream, or `-` to read it from stdin. PDF-embedded inputs use this when the dictionary lives outside the image XObject. |
| `--inspect` | off | Print the segment table and exit. Triage tool; pairs with `-v` for richer detail. |
| `--format png\|pbm\|raw` | `png` | Output bitmap encoding. `png` writes an 8-bpp Gray; `pbm` writes a P4 (binary PBM) bilevel image; `raw` writes packed MSB-first 1-bpp bytes with no header. |
| `--page N` | `1` | 1-based page number for multi-page input. `--page N` past the end exits with `exitUsage` (`2`). |
| `--max-pixels N` | `100000000` | Reject any region whose pixel count exceeds this. Hard cap on attacker-controlled `Width x Height`. |
| `--max-alloc SIZE` | `1G` | Per-decode soft memory ceiling via `runtime/debug.SetMemoryLimit`. Accepts `K` / `M` / `G` / `T` suffixes. |
| `--timeout DUR` | `10s` | Wall-clock budget for the entire decode. Accepts Go duration syntax (`100ms`, `30s`, `2m`). |
| `-v, --verbose` | off | Verbose logging on stderr. Repeat (`-v -v`) for more detail. |
| `--version` | off | Print version and exit. |
| `-h, --help` | off | Print help and exit. |

## Exit codes

Scripts branch on failure class without parsing stderr.

| Code | Constant | Meaning |
| ---: | --- | --- |
| `0` | `exitOK` | Success. |
| `1` | `exitErr` | Generic / unclassified failure (internal bug, I/O error). |
| `2` | `exitUsage` | Flag-parse error, missing required arg, or `--page` past end of stream. |
| `3` | `exitMalformed` | Input is not legal JBIG2; wraps [`gobig2.ErrMalformed`](errors.go). |
| `4` | `exitResourceBudget` | A [`Limits`](limits.go) cap fired; wraps [`gobig2.ErrResourceBudget`](errors.go). |
| `5` | `exitUnsupported` | Legal JBIG2 but uses a feature gobig2 doesn't implement; wraps [`gobig2.ErrUnsupported`](errors.go). |
| `6` | `exitTimeoutExceeded` | `--timeout` budget exhausted mid-decode. |

## Examples

```sh
# Standalone .jb2 file to PNG on stdout
gobig2 page.jbig2 > page.png

# PDF-embedded stream + globals to PBM
gobig2 --globals globals.jb2 --format=pbm image.jb2 image.pbm

# Inspect a segment table
gobig2 --inspect page.jbig2

# Tight resource budget for an untrusted source
gobig2 --max-pixels=4000000 --max-alloc=64M --timeout=5s suspect.jb2 out.png
```
