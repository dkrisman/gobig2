// Package errs holds sentinel error values re-exported by
// gobig2 for `errors.Is` classification. Internal packages
// can't import gobig2 (root consumes internal/, not vice
// versa), so sentinels live here.
//
// Three categories:
//
//   - ErrMalformed: input not legal JBIG2 (truncation, bad
//     header, OOB ref, missing segment). PDF apps map to "skip
//     this image".
//   - ErrResourceBudget: tripped a Limits cap. Wrapped error
//     names the cap. PDF apps map to "skip + maybe raise cap"
//     or treat as DoS.
//   - ErrUnsupported: legal JBIG2 but feature not implemented.
//     PDF apps may route to fallback decoder.
package errs

import "errors"

var (
	// ErrMalformed: input does not conform to JBIG2
	// (T.88 / ISO 14492). Wrap with fmt.Errorf("%w: ...",
	// ErrMalformed) at every parser failure site that's not a
	// budget breach or true unsupported-feature.
	ErrMalformed = errors.New("malformed input")

	// ErrResourceBudget: input exceeded a configured Limits cap.
	// Wrap at the cap-check site to name the cap that fired.
	ErrResourceBudget = errors.New("resource budget exceeded")

	// ErrUnsupported: valid JBIG2 but feature not implemented
	// (codec extension or real-encoder deviation; see
	// docs/ITU-SPEC-PROBLEMS.md).
	ErrUnsupported = errors.New("unsupported feature")
)
