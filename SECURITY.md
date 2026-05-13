# Security Policy

`gobig2` decodes an attacker-influenced byte stream - JBIG2
segments embedded in PDFs, fax documents, and scan archives.
Hardening the decoder against malformed and adversarial input
is the project's core security commitment.

## Reporting a vulnerability

Email **gobig2@krisman.dev** with a description, a reproducer
if you have one, and any proposed patch. Expect a first
response within a few business days.

Please don't open a public GitHub issue for a security report
until a fix is available. GitHub's private vulnerability
reporting is also fine if you prefer that channel.

## In scope

- Decoder crashes (panic, runtime fault) on attacker-controlled
  JBIG2 bytes.
- Memory exhaustion that bypasses [`Limits`](limits.go) when
  the caller has configured caps per the
  [README](README.md#resource-budgets) and the
  [`Limits.Apply`](limits.go) doc.
- Out-of-bounds reads or writes, integer-overflow-driven
  allocation, decode loops that fail to honor
  [`Decoder.DecodeContext`](jbig2.go) cancellation.
- Misclassified errors that route malformed input to a path the
  caller would treat as success.

## Out of scope

- Resource exhaustion when the caller has explicitly opted out
  of the [`Limits`](limits.go) caps. A bare `Limits{}` literal
  sets every field to zero, which means "no cap" - supported as
  a permissive profile for fuzz / test harnesses. Outside that
  case, callers should start from
  [`DefaultLimits`](limits.go); see the documented footgun on
  [`Limits.Apply`](limits.go).
- Conformance gaps on inputs that no production encoder emits.
  The decoder rejects spec-deviating shapes from the ITU-T T.88
  Annex A sample encoder by design; see
  [docs/design/ITU-SPEC-PROBLEMS.md](docs/design/ITU-SPEC-PROBLEMS.md).
- Bugs in third-party libraries linked by callers. `gobig2` has
  zero runtime dependencies, and the bundled CLIs only link the
  standard library and `gobig2` itself.

## Supported versions

The latest tagged release is the supported version. Pre-1.0,
fixes land on `main` and ship in the next tag; the project does
not maintain release branches.
