# LIBRARIES

All third party libraries used by codebase documented here.

## Runtime dependencies

**None.** `gobig2` depends only on the Go standard library
(`context`, `errors`, `fmt`, `image`, `image/color`, `io`,
`bytes`, `encoding/binary`, etc.). `go.mod` declares the module
path and Go toolchain; no `require` block.

This is a deliberate constraint - JBIG2 decoding is a hot path
inside PDF readers and similar pipelines, and a zero-dep import
graph keeps the codec drop-in across hostile environments
(distroless containers, embedded toolchains, security-audited
builds). New runtime deps require a strong justification and
must update this file in the same commit (see [RULES.md](../RULES.md)).

## Dev / build dependencies

Dev tool versions are pinned in [Taskfile.yml](../Taskfile.yml)
(`GOIMPORTS_VERSION`, `GOLANGCI_LINT_VERSION`,
`GOVULNCHECK_VERSION`) and installed by `task tools`. See
[docs/TOOLS.md](TOOLS.md) for the full tooling reference.
