# RULES

## Versioned Dependencies

When adding or updating any versioned library, package, GitHub Action,
container image, or other versioned artifact, **always check upstream for
the latest stable release first**. "Stable" means a non-prerelease tag
on the upstream's official release page/github/etc.

Default to the latest stable. If you pin to anything other than the
latest stable, document reason incomment alongside the pin so
next reader knows it was deliberate.

Examples of acceptable reasons to pin off-latest:

- Regression in latest version that affects this project (link issue).
- Breaking change that requires migration work scheduled for later.
- Newer non-stable / RC / beta version needed for a specific fix
  or feature (note that "newer non-stable" is also a deliberate
  off-default and needs the same comment).

Examples that are **not** acceptable reasons:

- "It was the version I had handy."
- "I didn't check."
- "The example in some doc used this version."

This applies equally to directives in [.github/workflows/](.github/workflows/), any language package files, Docker base images, and any other
versioned reference the project holds. When bumping a pin, also bump
the comment if the reason has changed.

## Locale

America/New York locale.

- Spellings: American English always unless used in a third-party Library APIs
- Dates: ISO 8601 numeric (`2026-04-28`) or American long-form (`April 28, 2026`)
- Units: US Imperial. Exception: scientific measurements (SI), image measurements (pixels)
