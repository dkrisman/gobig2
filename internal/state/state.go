// Package state holds cross-cutting enums and constants shared
// by JBIG2 decoders and the document orchestrator. Split from
// internal/segment to break the import cycle that forms once
// the orchestrator imports each decoder and each decoder
// returns one of these values.
package state

// SegmentState is a segment-parsing state.
type SegmentState int

const (
	HeaderUnparsed SegmentState = 0
	DataUnparsed   SegmentState = 1
	ParseComplete  SegmentState = 2
	Error          SegmentState = 4
)

// ResultType identifies the kind of result attached to a segment.
type ResultType int

const (
	VoidPointer         ResultType = 0
	ImagePointer        ResultType = 1
	SymbolDictPointer   ResultType = 2
	PatternDictPointer  ResultType = 3
	HuffmanTablePointer ResultType = 4
)

// Cross-cutting JBIG2 limits and sentinels.
const (
	// OOB is the out-of-bound marker from Huffman/arithmetic-
	// integer decoders meaning "no in-range value."
	OOB = 1
	// MaxPatternIndex caps GRAYMAX in halftone pattern dicts.
	MaxPatternIndex = 65535
	// MaxImageSize caps any single bitmap dimension. JBIG2 §6.6
	// regions are 32-bit; no real doc needs 64K pixels per side.
	// Cap blocks pathological inputs.
	MaxImageSize = 65535
)

// Rect is an axis-aligned rectangle in pixel space.
type Rect struct {
	Left   int32
	Top    int32
	Right  int32
	Bottom int32
}
