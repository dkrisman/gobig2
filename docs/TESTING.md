# TESTING

## Test Driven Development

Tests always high priority - feature not done if no test. Coverage >70% is goal. Perfect enemy of good, don't make code worse to chase 100% coverage but that doesn't mean skimp on tests

Test files should not live in root of repo except when necessary. Keep repo tidy and tests green

Every app that makes it past MVP should have all:

## Unit Tests

Quick tests to verify code works and doesn't regress

## Smoke tests

Quick tests to verify system works as a whole

## Bench Testing

Quick tests to verify performance characteristics on smallest meaningful pieces

## Performance Testing

Long tests to verify full performance metrics of the system.
gobig2 ships one performance comparison harness today:

- `task bench:cross` (also wired into `.github/workflows/perf-linux.yml`)
  runs `cmd/perf-cross` over a curated SerenityOS subset against
  jbig2dec, mutool, pdfimages, and PDFBox. Treat the absolute
  numbers as noisy on shared CI runners; the value is relative
  ordering drift, not bps targets.

## Fuzz Testing

Long tests with semi-random data to identify code flaws spanning performance, reliability, correctness and security
