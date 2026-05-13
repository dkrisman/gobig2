# CONVENTIONS

Detailed conventions for project.

CONVENTIONS.md "is more what you'd call 'guidelines' than actual rules."

Generic Go style is covered by [.golangci.yml](../.golangci.yml).
This file captures conventions specific to gobig2 - the kind a
fresh contributor would otherwise have to reverse-engineer from
the existing code.

## Documentation style

Public types and functions carry **prose** godoc, not bullet-list
godoc. Multi-paragraph package docs explain the *why* (where the
caller sits in a PDF reader, what the spec calls this region,
what the failure modes mean) - not just the parameter list. Match
the density of the existing surface in [jbig2.go](../jbig2.go),
[limits.go](../limits.go), and the internal package headers.

Reference spec sections inline - `T.88 §6.2`, `T.88 Annex B`,
`ISO/IEC 14492 §7.4.7` - so a reader chasing a behavior question
can land in the right place in the standard.

## Spec deviations

When `gobig2` diverges from a strict reading of T.88 - to match
real-world producer output, to harden against adversarial bytes,
or because the ITU sample encoder itself is non-spec - document
the deviation **inline at the divergence point** *and* link
[docs/design/ITU-SPEC-PROBLEMS.md](design/ITU-SPEC-PROBLEMS.md)
if it belongs in the corpus narrative. Silent deviations are bugs
waiting to be re-introduced.

## Errors

Every parser-side failure wraps one of the three sentinels in
[errors.go](../errors.go). Use `fmt.Errorf("... %w", errs.ErrXxx)`
inside `internal/` packages - they import [internal/errs](../internal/errs/),
not the root, to avoid an import cycle. The public package
re-exports `errs.ErrMalformed` / `errs.ErrResourceBudget` /
`errs.ErrUnsupported` as the supported `errors.Is` targets.

Errors carry decode context (segment number, parser stage)
because the CLI's `--inspect` mode and the test harness both
match on substrings. Don't strip context to make a message terse.

## Resource caps

Any new attacker-controlled allocation (count derived from input
bytes, dimension product, dictionary size) needs a cap in
[limits.go](../limits.go) gated **at parse time, before the
allocation**. A bare `make([]T, n)` where `n` came off the wire
without a `Limits` check is a CVE waiting to happen.

When adding a new cap:

1. Document the attacker shape it blocks in the `Limits` struct
   godoc.
2. Wire it through `Limits.Apply` so the cap is process-wide
   (decoder packages read mutable package-level variables, not
   the `Limits` struct directly).
3. Add a pathological-input regression in
   [internal/gobig2test/pathological_test.go](../internal/gobig2test/pathological_test.go).

## Package boundaries

`internal/` packages mirror the JBIG2 spec's section layout (see
[QUICKSTART.md](../QUICKSTART.md#code-map)) and stay leaves where
possible - only `internal/segment` (the orchestrator) imports the
decoder packages. Cross-cutting enums live in `internal/state` to
break the cycle that would otherwise form.

Public API is **only** the root [jbig2.go](../jbig2.go) +
[errors.go](../errors.go) + [limits.go](../limits.go). Internal
types surfaced to the public API (e.g. `Document`, `Result`) are
re-exported via type aliases, not copies - see the alias block
in `jbig2.go`.

## Test data

The SerenityOS corpus in [testdata/serenityos/](../testdata/serenityos/)
ships in-tree and always runs (`task test:conformance`). The
ITU-T T.88 Annex A corpus is too large to commit and is env-gated
on `JBIG2_CONFORMANCE_DIR`. Fuzz seeds land under
`internal/<pkg>/testdata/fuzz/<TestName>/<hash>`; commit failing
inputs as regression seeds. Never default either corpus env var
to a maintainer-local path - fresh clones would then silently
skip.
