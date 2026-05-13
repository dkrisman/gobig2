package segment

// JBIG2 segment-type registry (ITU-T T.88 / ISO/IEC 14492
// Annex H, Table H.1).
//
// One canonical table used by:
//
//   - probe.ValidSegmentType - embedded-stream first-segment
//     sniff (rejects reserved/undefined before parser stalls).
//   - cmd/gobig2.segmentTypeName - human label for --inspect.
//   - Document.parseSegmentDataInner - dispatcher; absent
//     types fall through to ErrMalformed.
//
// Decentralized lists drift: hand-maintained allowlists easily
// include reserved code 17 (halftone is 20/22/23, not 17) or
// omit refinement codes 42/43, and dispatcher defaults silently
// skip reserved payloads. Single source of truth here keeps
// all three sites honest.

// TypeKind classifies a JBIG2 segment-type code.
type TypeKind int

const (
	// TypeKindReserved: every code byte (0..63) not assigned by
	// T.88 Annex H, plus the type-17 hole. Dispatcher treats
	// these as ErrMalformed past the first segment.
	TypeKindReserved TypeKind = iota
	TypeKindSymbolDict
	TypeKindTextRegion
	TypeKindPatternDict
	TypeKindHalftoneRegion
	TypeKindGenericRegion
	TypeKindGenericRefinementRegion
	TypeKindPageInfo
	TypeKindEndOfPage
	TypeKindEndOfStripe
	TypeKindEndOfFile
	TypeKindProfiles
	TypeKindTables
	TypeKindExtension
)

// TypeInfo reports the spec classification and label for a
// segment-type byte. Returns (TypeKindReserved, "") for
// unassigned values per T.88 Annex H.
func TypeInfo(t byte) (TypeKind, string) {
	switch t {
	case 0:
		return TypeKindSymbolDict, "symbol dictionary"
	case 4:
		return TypeKindTextRegion, "intermediate text region"
	case 6:
		return TypeKindTextRegion, "immediate text region"
	case 7:
		return TypeKindTextRegion, "immediate lossless text region"
	case 16:
		return TypeKindPatternDict, "pattern dictionary"
	case 20:
		return TypeKindHalftoneRegion, "intermediate halftone region"
	case 22:
		return TypeKindHalftoneRegion, "immediate halftone region"
	case 23:
		return TypeKindHalftoneRegion, "immediate lossless halftone region"
	case 36:
		return TypeKindGenericRegion, "intermediate generic region"
	case 38:
		return TypeKindGenericRegion, "immediate generic region"
	case 39:
		return TypeKindGenericRegion, "immediate lossless generic region"
	case 40:
		return TypeKindGenericRefinementRegion, "intermediate generic refinement region"
	case 42:
		return TypeKindGenericRefinementRegion, "immediate generic refinement region"
	case 43:
		return TypeKindGenericRefinementRegion, "immediate lossless generic refinement region"
	case 48:
		return TypeKindPageInfo, "page information"
	case 49:
		return TypeKindEndOfPage, "end of page"
	case 50:
		return TypeKindEndOfStripe, "end of stripe"
	case 51:
		return TypeKindEndOfFile, "end of file"
	case 52:
		return TypeKindProfiles, "profiles"
	case 53:
		return TypeKindTables, "tables"
	case 62:
		return TypeKindExtension, "extension"
	}
	return TypeKindReserved, ""
}

// IsDefinedType reports whether t is assigned by T.88 Annex H.
// Use TypeInfo for the full label.
func IsDefinedType(t byte) bool {
	kind, _ := TypeInfo(t)
	return kind != TypeKindReserved
}
