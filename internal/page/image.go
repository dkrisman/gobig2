// Package page holds the bi-level Image type that region
// decoders write into and the document orchestrator stitches
// to form the final page bitmap. Image owns its packed
// MSB-first byte buffer, exposes pixel get/set/compose, and
// converts to image/color.Gray for the public API.
package page

import (
	"image"
)

// MaxImagePixels caps total pixels of a single JBIG2 bitmap
// allocated by NewImage. Without it, adversarial inputs
// (absurd width/height) drive multi-GB allocs and OOM. 256 MP
// (= 32 MB packed) fits every real scanned page (1200-DPI A4
// is ~140 MP) while blocking the catastrophic case. Override
// at startup for unusual content; 0 or negative disables.
var MaxImagePixels int64 = DefaultMaxImagePixels

// DefaultMaxImagePixels is the stock cap for [MaxImagePixels].
// Initializes the package var and seeds gobig2.DefaultLimits().
// Constant (not the mutable var) keeps DefaultLimits() stable
// after Apply() of custom limits.
const DefaultMaxImagePixels int64 = 256 * 1024 * 1024

// ComposeOp is a bitmap composition operator.
type ComposeOp int

const (
	// ComposeOr is the OR operator.
	ComposeOr ComposeOp = 0
	// ComposeAnd is the AND operator.
	ComposeAnd ComposeOp = 1
	// ComposeXor is the XOR operator.
	ComposeXor ComposeOp = 2
	// ComposeXnor is the XNOR operator.
	ComposeXnor ComposeOp = 3
	// ComposeReplace is the REPLACE operator.
	ComposeReplace ComposeOp = 4
)

// Image is a 1-bit-per-pixel image.
type Image struct {
	width  int32
	height int32
	stride int32
	data   []byte
	// shiftedRows caches pre-right-shifted copies of `data` for
	// non-byte-aligned ComposeTo. shiftedRows[k] holds the
	// image with every row shifted right by k bits; the
	// shifted stride is `stride + 1` (one extra byte per row
	// to catch bits spilled off the right edge of the source).
	// shiftedRows[0] stays nil - shift 0 is `data` itself.
	// shiftedReady is a bitmask: bit k set iff shiftedRows[k]
	// is populated. Lazy build on first ComposeTo use lets
	// non-source bitmaps (page bitmap, intermediate region
	// targets) skip the alloc entirely.
	//
	// Concrete win: a symbol-dict glyph used N times on a
	// page builds each shift once and reuses for every
	// placement at that dx % 8 alignment. Compose then
	// degenerates to byte-aligned OR of `shifted[k]` into
	// `dst.data` - no per-byte gather, no per-byte shift /
	// mask, no per-byte branch in the inner loop.
	shiftedRows  [8][]byte
	shiftedReady uint8
}

// NewImage creates a new image.
// Parameters: width the image width, height the image height.
// Returns: *Image the new image; nil if the requested area
// exceeds MaxImagePixels or would overflow int32.
func NewImage(width, height int32) *Image {
	if width <= 0 || height <= 0 {
		return nil
	}
	stride := (width + 7) / 8
	if stride <= 0 || height > 2147483647/stride {
		return nil
	}
	if MaxImagePixels > 0 && int64(width)*int64(height) > MaxImagePixels {
		return nil
	}
	size := stride * height
	data := make([]byte, size)
	return &Image{
		width:  width,
		height: height,
		stride: stride,
		data:   data,
	}
}

// Width returns the width.
// Returns: int32 the width.
func (i *Image) Width() int32 { return i.width }

// Height returns the height.
// Returns: int32 the height.
func (i *Image) Height() int32 { return i.height }

// Stride returns the stride in bytes.
// Returns: int32 the stride.
func (i *Image) Stride() int32 { return i.stride }

// Data returns the underlying byte slice.
// Returns: []byte the data slice.
func (i *Image) Data() []byte { return i.data }

// GetPixel returns a pixel value.
// Parameters: x the column, y the row.
// Returns: int the pixel value.
func (i *Image) GetPixel(x, y int32) int {
	if x < 0 || x >= i.width || y < 0 || y >= i.height {
		return 0
	}
	byteIdx := y*i.stride + (x >> 3)
	bitIdx := 7 - (x & 7)
	return int((i.data[byteIdx] >> bitIdx) & 1)
}

// SetPixel sets a pixel value.
// Parameters: x the column, y the row, v the pixel value.
func (i *Image) SetPixel(x, y int32, v int) {
	if x < 0 || x >= i.width || y < 0 || y >= i.height {
		return
	}
	byteIdx := y*i.stride + (x >> 3)
	bitIdx := 7 - (x & 7)
	mask := byte(1 << bitIdx)
	if v != 0 {
		i.data[byteIdx] |= mask
	} else {
		i.data[byteIdx] &^= mask
	}
}

// Fill fills the image with a uniform value.
// Parameters: v the fill value.
func (i *Image) Fill(v bool) {
	var val byte
	if v {
		val = 0xFF
	}
	for idx := range i.data {
		i.data[idx] = val
	}
}

// Invert inverts every pixel.
func (i *Image) Invert() {
	for idx := range i.data {
		i.data[idx] = ^i.data[idx]
	}
}

// ComposeTo composes this image onto dst at (x, y) under op.
// Clips the source rect against dst once, then dispatches: Or /
// Xor / Xnor / Replace use the byte-batched [composeFastRow];
// And uses [composeToSlow] because its byte semantics differ
// outside the source rect (AND'ing zeroed padding would clear
// pixels we must preserve).
func (i *Image) ComposeTo(dst *Image, x, y int32, op ComposeOp) {
	if i == nil || dst == nil {
		return
	}
	sx0, sy0 := int32(0), int32(0)
	sx1, sy1 := i.width, i.height
	if x < 0 {
		sx0 = -x
	}
	if y < 0 {
		sy0 = -y
	}
	if x+sx1 > dst.width {
		sx1 = dst.width - x
	}
	if y+sy1 > dst.height {
		sy1 = dst.height - y
	}
	if sx0 >= sx1 || sy0 >= sy1 {
		return
	}

	switch op {
	case ComposeOr, ComposeXor, ComposeXnor, ComposeReplace:
		// Pre-shifted source fast path. Symbol-mode placement
		// is overwhelmingly ComposeOr at sx0 == 0 (the full
		// glyph), with dx (= x + sx0) at varying bit
		// alignments per placement. Pre-shifting the source
		// once per (shift, image) lets the per-row inner loop
		// be a byte-aligned OR / XOR / etc, skipping the
		// gatherBitsMSB + chunkMask pair per dst byte.
		//
		// Predicate: sx0 byte-aligned (otherwise the cache's
		// row layout doesn't help) and dst x byte-aligned in
		// the destination (handled below by the shift = 0
		// branch).
		if sx0&7 == 0 {
			dShift := uint((x + sx0) & 7)
			if dShift == 0 {
				// Source-aligned and dst-aligned: pure byte
				// OR / XOR / etc. No cache needed.
				bitW := sx1 - sx0
				fullBytes := int(bitW >> 3)
				tailBits := uint(bitW & 7)
				for sy := sy0; sy < sy1; sy++ {
					srcRow := i.data[sy*i.stride+sx0>>3:]
					dstRow := dst.data[(y+sy)*dst.stride+(x+sx0)>>3:]
					composeAlignedRow(dstRow, srcRow, fullBytes, tailBits, op)
				}
			} else {
				shifted := i.shiftedRowFor(dShift)
				// In the shifted buffer, row r starts at
				// offset r * (i.stride + 1). The width of
				// shifted row equals the original (bitW), now
				// offset by dShift bits into byte 0. The dst
				// region spans (bitW + dShift) bits starting
				// at byte (x+sx0)>>3 of dst row.
				bitW := sx1 - sx0
				shiftedStride := int(i.stride) + 1
				totalBits := bitW + int32(dShift)
				fullBytes := int(totalBits >> 3)
				tailBits := uint(totalBits & 7)
				for sy := sy0; sy < sy1; sy++ {
					srcRow := shifted[int(sy)*shiftedStride+int(sx0>>3):]
					dstRow := dst.data[(y+sy)*dst.stride+(x+sx0)>>3:]
					composeShiftedRow(dstRow, srcRow, fullBytes, tailBits, dShift, op)
				}
			}
			return
		}
		for sy := sy0; sy < sy1; sy++ {
			composeFastRow(
				dst.data[(y+sy)*dst.stride:],
				i.data[sy*i.stride:],
				sx0, sx1, x+sx0, op,
			)
		}
	default:
		i.composeToSlow(dst, x, y, sx0, sy0, sx1, sy1, op)
	}
}

// shiftedRowFor returns a buffer where every row of i.data has
// been shifted right by `shift` bits (shift in [1, 7]). Layout
// is `(i.stride + 1) * i.height` bytes with stride
// `i.stride + 1`: each row has one extra byte to catch bits
// shifted past the right edge of the source. Built lazily on
// first call per shift; subsequent calls return the cached
// buffer.
//
// Shift 0 is the identity; callers must check before calling.
// Used by [Image.ComposeTo]'s symbol-placement fast path -
// see the predicate there.
func (i *Image) shiftedRowFor(shift uint) []byte {
	if i.shiftedReady&(1<<shift) != 0 {
		return i.shiftedRows[shift]
	}
	newStride := int(i.stride) + 1
	buf := make([]byte, newStride*int(i.height))
	leftShift := 8 - shift
	for r := int32(0); r < i.height; r++ {
		srcOff := int(r) * int(i.stride)
		dstOff := int(r) * newStride
		var prev byte
		for c := int32(0); c < i.stride; c++ {
			cur := i.data[srcOff+int(c)]
			buf[dstOff+int(c)] = (prev << leftShift) | (cur >> shift)
			prev = cur
		}
		// Final spilled byte: trailing pad bits of source are
		// zero by NewImage invariant, so this byte has the
		// last (shift) bits of the row's MSB-first content
		// and the rest zero.
		buf[dstOff+int(i.stride)] = prev << leftShift
	}
	i.shiftedRows[shift] = buf
	i.shiftedReady |= 1 << shift
	return buf
}

// composeAlignedRow emits a byte-aligned compose of srcRow
// into dstRow under op. fullBytes is the number of full source
// bytes; tailBits is the bit count of the partial last byte
// (0..7); op is one of ComposeOr / Xor / Xnor / Replace.
// Caller has already clipped to dst bounds and confirmed both
// source and dst start on byte boundaries.
func composeAlignedRow(dstRow, srcRow []byte, fullBytes int, tailBits uint, op ComposeOp) {
	switch op {
	case ComposeOr:
		for b := 0; b < fullBytes; b++ {
			dstRow[b] |= srcRow[b]
		}
		if tailBits != 0 {
			mask := byte(0xFF) << (8 - tailBits)
			dstRow[fullBytes] |= srcRow[fullBytes] & mask
		}
	case ComposeXor:
		for b := 0; b < fullBytes; b++ {
			dstRow[b] ^= srcRow[b]
		}
		if tailBits != 0 {
			mask := byte(0xFF) << (8 - tailBits)
			dstRow[fullBytes] ^= srcRow[fullBytes] & mask
		}
	case ComposeXnor:
		for b := 0; b < fullBytes; b++ {
			diff := dstRow[b] ^ srcRow[b]
			dstRow[b] = ^diff
		}
		if tailBits != 0 {
			mask := byte(0xFF) << (8 - tailBits)
			diff := (dstRow[fullBytes] ^ srcRow[fullBytes]) & mask
			dstRow[fullBytes] = (dstRow[fullBytes] &^ mask) | (mask &^ diff)
		}
	case ComposeReplace:
		copy(dstRow[:fullBytes], srcRow[:fullBytes])
		if tailBits != 0 {
			mask := byte(0xFF) << (8 - tailBits)
			dstRow[fullBytes] = (dstRow[fullBytes] &^ mask) | (srcRow[fullBytes] & mask)
		}
	}
}

// composeShiftedRow emits a compose of `srcRow` (a pre-shifted
// row from [Image.shiftedRowFor]) into `dstRow` under `op`.
// `fullBytes` and `tailBits` count the total dst span
// (`originalWidth + dShift` bits). The first byte of dstRow
// receives the source bits at positions `dShift..7` -
// preserved bits `0..dShift-1` need a head mask. Tail handling
// mirrors [composeAlignedRow].
func composeShiftedRow(dstRow, srcRow []byte, fullBytes int, tailBits, dShift uint, op ComposeOp) {
	// Head mask: the shifted source's first byte has zero in
	// bits 0..dShift-1 (we shifted right), so OR / XOR don't
	// need explicit masking for them. Replace / Xnor still
	// need to preserve those bits in dst.
	headMask := byte(0xFF) >> dShift // bits dShift..7 = 1, rest 0
	tailLen := tailBits
	// When tailLen != 0 the final byte is partial and handled
	// per-op below; otherwise the OR/XOR/etc loop covers it
	// directly with no special masking.
	switch op {
	case ComposeOr:
		// Shifted source has zeros in head bits already; OR is
		// safe to apply byte-wise across the whole span.
		for b := 0; b < fullBytes; b++ {
			dstRow[b] |= srcRow[b]
		}
		if tailLen != 0 {
			mask := byte(0xFF) << (8 - tailLen)
			dstRow[fullBytes] |= srcRow[fullBytes] & mask
		}
	case ComposeXor:
		// Same as OR for the head: shifted-zero XOR x = x.
		for b := 0; b < fullBytes; b++ {
			dstRow[b] ^= srcRow[b]
		}
		if tailLen != 0 {
			mask := byte(0xFF) << (8 - tailLen)
			dstRow[fullBytes] ^= srcRow[fullBytes] & mask
		}
	case ComposeXnor:
		// First byte: mask to bits dShift..7 only.
		if fullBytes > 0 {
			diff := (dstRow[0] ^ srcRow[0]) & headMask
			dstRow[0] = (dstRow[0] &^ headMask) | (headMask &^ diff)
			for b := 1; b < fullBytes; b++ {
				diff := dstRow[b] ^ srcRow[b]
				dstRow[b] = ^diff
			}
		}
		if tailLen != 0 {
			mask := byte(0xFF) << (8 - tailLen)
			if fullBytes == 0 {
				mask &= headMask
			}
			diff := (dstRow[fullBytes] ^ srcRow[fullBytes]) & mask
			dstRow[fullBytes] = (dstRow[fullBytes] &^ mask) | (mask &^ diff)
		}
	case ComposeReplace:
		// First byte: replace only bits dShift..7.
		if fullBytes > 0 {
			dstRow[0] = (dstRow[0] &^ headMask) | (srcRow[0] & headMask)
			copy(dstRow[1:fullBytes], srcRow[1:fullBytes])
		}
		if tailLen != 0 {
			mask := byte(0xFF) << (8 - tailLen)
			if fullBytes == 0 {
				mask &= headMask
			}
			dstRow[fullBytes] = (dstRow[fullBytes] &^ mask) | (srcRow[fullBytes] & mask)
		}
	}
}

// composeFastRow folds source bits [sx0, sx1) of one row into
// dstRow starting at column dx, under op. Dispatches to a
// per-op specialized inner loop so the per-byte body has no
// switch on op.
//
// Handles arbitrary bit alignments via [gatherBitsMSB]: a
// source-byte chunk lands in one or two destination bytes via
// shift / mask. Byte-aligned case (dx & 7 == 0 == sx0 & 7)
// falls out of the same arithmetic.
func composeFastRow(dstRow, srcRow []byte, sx0, sx1, dx int32, op ComposeOp) {
	w := sx1 - sx0
	if w <= 0 {
		return
	}
	switch op {
	case ComposeOr:
		composeFastRowOr(dstRow, srcRow, sx0, sx1, dx)
	case ComposeXor:
		composeFastRowXor(dstRow, srcRow, sx0, sx1, dx)
	case ComposeXnor:
		composeFastRowXnor(dstRow, srcRow, sx0, sx1, dx)
	case ComposeReplace:
		composeFastRowReplace(dstRow, srcRow, sx0, sx1, dx)
	}
}

// composeFastRowOr is the [composeFastRow] specialization for
// [ComposeOr]. Hot path on symbol-mode text placement: every
// glyph compose lands here. The OR semantics mean we don't
// need the chunkMask at all - source-zero bits leave dst
// untouched, source-one bits force dst to 1 within the
// chunk's range, and bits outside the chunk are zero in the
// gathered chunk byte.
func composeFastRowOr(dstRow, srcRow []byte, sx0, sx1, dx int32) {
	dxEnd := dx + (sx1 - sx0)
	for d := dx; d < dxEnd; {
		dByteIdx := d >> 3
		dBitInByte := uint(d & 7)
		bitsThisChunk := uint(8) - dBitInByte
		if int32(bitsThisChunk) > dxEnd-d {
			bitsThisChunk = uint(dxEnd - d)
		}
		s := sx0 + (d - dx)
		srcChunk := gatherBitsMSB(srcRow, s, bitsThisChunk, dBitInByte)
		dstRow[dByteIdx] |= srcChunk
		d += int32(bitsThisChunk)
	}
}

// composeFastRowXor is the [composeFastRow] specialization for
// [ComposeXor]. Source-zero bits leave dst untouched (XOR
// identity); source-one bits flip the corresponding dst bit.
// Like Or, no chunkMask needed - gathered chunk byte is zero
// outside [dBitInByte, dBitInByte+bitsThisChunk).
func composeFastRowXor(dstRow, srcRow []byte, sx0, sx1, dx int32) {
	dxEnd := dx + (sx1 - sx0)
	for d := dx; d < dxEnd; {
		dByteIdx := d >> 3
		dBitInByte := uint(d & 7)
		bitsThisChunk := uint(8) - dBitInByte
		if int32(bitsThisChunk) > dxEnd-d {
			bitsThisChunk = uint(dxEnd - d)
		}
		s := sx0 + (d - dx)
		srcChunk := gatherBitsMSB(srcRow, s, bitsThisChunk, dBitInByte)
		dstRow[dByteIdx] ^= srcChunk
		d += int32(bitsThisChunk)
	}
}

// composeFastRowXnor is the [composeFastRow] specialization
// for [ComposeXnor]. xor is 1 where src != dst; xnor = mask &^
// xor on in-range bits, with original dest preserved outside.
// Needs the mask so we don't corrupt bits past the chunk.
func composeFastRowXnor(dstRow, srcRow []byte, sx0, sx1, dx int32) {
	dxEnd := dx + (sx1 - sx0)
	for d := dx; d < dxEnd; {
		dByteIdx := d >> 3
		dBitInByte := uint(d & 7)
		bitsThisChunk := uint(8) - dBitInByte
		if int32(bitsThisChunk) > dxEnd-d {
			bitsThisChunk = uint(dxEnd - d)
		}
		s := sx0 + (d - dx)
		srcChunk := gatherBitsMSB(srcRow, s, bitsThisChunk, dBitInByte)
		dByte := dstRow[dByteIdx]
		mask := chunkMask(dBitInByte, bitsThisChunk)
		diff := (dByte ^ srcChunk) & mask
		dstRow[dByteIdx] = (dByte &^ mask) | (mask &^ diff)
		d += int32(bitsThisChunk)
	}
}

// composeFastRowReplace is the [composeFastRow] specialization
// for [ComposeReplace]. Writes src bits, preserves dst outside
// the chunk via mask.
func composeFastRowReplace(dstRow, srcRow []byte, sx0, sx1, dx int32) {
	dxEnd := dx + (sx1 - sx0)
	for d := dx; d < dxEnd; {
		dByteIdx := d >> 3
		dBitInByte := uint(d & 7)
		bitsThisChunk := uint(8) - dBitInByte
		if int32(bitsThisChunk) > dxEnd-d {
			bitsThisChunk = uint(dxEnd - d)
		}
		s := sx0 + (d - dx)
		srcChunk := gatherBitsMSB(srcRow, s, bitsThisChunk, dBitInByte)
		dByte := dstRow[dByteIdx]
		mask := chunkMask(dBitInByte, bitsThisChunk)
		dstRow[dByteIdx] = (dByte &^ mask) | (srcChunk & mask)
		d += int32(bitsThisChunk)
	}
}

// gatherBitsMSB returns `count` bits from src starting at
// MSB-first bit position startBit, packed into a byte with the
// first source bit at MSB-first position alignBit. Result bits
// outside [alignBit, alignBit+count) are zero. Reads at most two
// source bytes; the srcByteIdx+1 bounds check covers the last-
// byte window-straddle case.
func gatherBitsMSB(src []byte, startBit int32, count, alignBit uint) byte {
	if count == 0 {
		return 0
	}
	srcByteIdx := int(startBit >> 3)
	srcShift := uint(startBit & 7)
	hi := uint16(src[srcByteIdx]) << 8
	if srcByteIdx+1 < len(src) {
		hi |= uint16(src[srcByteIdx+1])
	}
	aligned := byte(hi >> (8 - srcShift))
	if count < 8 {
		aligned &= byte(0xFF) << (8 - count)
	}
	return aligned >> alignBit
}

// chunkMask returns a byte mask with `count` bits set starting
// at MSB-first position alignBit. E.g. (alignBit=2, count=4) ->
// 0b0011_1100. Used by Replace / Xnor to preserve dest bits
// outside the source chunk.
func chunkMask(alignBit, count uint) byte {
	if count == 0 {
		return 0
	}
	return byte(0xFF<<(8-count)) >> alignBit
}

// composeToSlow is the per-pixel fallback for ComposeAnd. Called
// after [ComposeTo] has clipped the rect, so GetPixel /
// SetPixel bounds checks fire but always miss inside the loop.
func (i *Image) composeToSlow(dst *Image, x, y, sx0, sy0, sx1, sy1 int32, op ComposeOp) {
	for sy := sy0; sy < sy1; sy++ {
		dy := y + sy
		for sx := sx0; sx < sx1; sx++ {
			dx := x + sx
			srcBit := i.GetPixel(sx, sy)
			dstBit := dst.GetPixel(dx, dy)
			var resBit int
			switch op {
			case ComposeAnd:
				resBit = dstBit & srcBit
			default:
				resBit = dstBit
			}
			dst.SetPixel(dx, dy, resBit)
		}
	}
}

// ComposeFrom composes a source image onto this image.
// Parameters: x the destination column, y the destination row, src the source image, op the composition operator.
func (i *Image) ComposeFrom(x, y int32, src *Image, op ComposeOp) {
	if src != nil {
		src.ComposeTo(i, x, y, op)
	}
}

// SubImage returns a sub-image copy.
// Parameters: x the source column, y the source row, w the width, h the height.
// Returns: *Image the sub-image.
//
// Memory note: SubImage allocates a new packed bitmap and
// copies pixels - not a view. Two call sites use this:
//
//   - parseGenericRefinementRegion (no referred segment) uses
//     d.page.SubImage(...) for GRREFERENCE, briefly holding
//     page + copied window + decoded refinement.
//   - SDDProc.DecodeHuffman pops height-class collective
//     bitmap (BHC) and populates each symbol via
//     BHC.SubImage(...); symbol buffer sum ~= BHC pixel area
//     while BHC live. Existing image/symbol Limits bound peak;
//     height-class decode briefly approaches 2x packed-bitmap
//     footprint for that class.
//
// Both O(area) copies, bounded by MaxImagePixels /
// MaxSymbolPixels / MaxSymbolDictPixels. Zero-copy view would
// avoid duplicate but needs careful changes to pixel access,
// bounds, and owned-buffer assumption downstream.
func (i *Image) SubImage(x, y, w, h int32) *Image {
	if w <= 0 || h <= 0 {
		return nil
	}
	sub := NewImage(w, h)
	if sub == nil {
		return nil
	}
	// NewImage zero-inits; no Fill(false) needed.
	for r := int32(0); r < h; r++ {
		for c := int32(0); c < w; c++ {
			sub.SetPixel(c, r, i.GetPixel(x+c, y+r))
		}
	}
	return sub
}

// Expand grows the image height. Silent no-op if new height
// would exceed MaxImagePixels - adversarial striped-page
// segments could otherwise grow past the cap (NewImage's
// guard fires at allocation, but Expand can grow past).
//
// Also no-op when new height overflows int32 packed-buffer
// (`stride * height`). NewImage's `height > 2147483647/stride`
// applies same guard at alloc; disabling MaxImagePixels via
// Limits.Apply means "no policy cap", not "no overflow safety".
//
// No-op preserves the existing image; downstream sees prior
// pixels. Matches NewImage's "return nil on over-budget".
func (i *Image) Expand(height int32, defaultPixel bool) {
	if height <= i.height {
		return
	}
	if i.stride <= 0 || height > 2147483647/i.stride {
		return
	}
	if MaxImagePixels > 0 && int64(i.width)*int64(height) > MaxImagePixels {
		return
	}
	newStride := i.stride
	newHeight := height
	newData := make([]byte, newStride*newHeight)
	copy(newData, i.data)
	start := i.stride * i.height
	fill := byte(0x00)
	if defaultPixel {
		fill = 0xFF
	}
	for j := start; j < int32(len(newData)); j++ {
		newData[j] = fill
	}
	i.data = newData
	i.height = newHeight
}

// Duplicate returns a copy of the image.
// Returns: *Image the duplicate.
func (i *Image) Duplicate() *Image {
	if i == nil {
		return nil
	}
	newImg := NewImage(i.width, i.height)
	if newImg != nil {
		copy(newImg.data, i.data)
	}
	return newImg
}

// CopyLine copies one scan line over another.
// Parameters: h the destination row, srcH the source row.
func (i *Image) CopyLine(h, srcH int32) {
	if h < 0 || h >= i.height || srcH < 0 || srcH >= i.height {
		return
	}
	start := h * i.stride
	end := start + i.stride
	srcStart := srcH * i.stride
	srcEnd := srcStart + i.stride
	copy(i.data[start:end], i.data[srcStart:srcEnd])
}

// ToGoImage converts to an [image.Gray] with ink (source bit 1)
// = Y 0 (black) and paper (source bit 0) = Y 255 (white). Walks
// source rows in packed bytes, mapping each full source byte
// through [unpack8] into 8 pix bytes; the tail partial byte
// falls through a per-bit loop.
func (i *Image) ToGoImage() image.Image {
	if i == nil {
		return nil
	}
	w, h := int(i.width), int(i.height)
	img := image.NewGray(image.Rect(0, 0, w, h))
	stride := int(i.stride)
	fullBytes := w >> 3
	tailBits := w & 7
	for y := 0; y < h; y++ {
		srcRowStart := y * stride
		pixRowStart := y * img.Stride
		for b := 0; b < fullBytes; b++ {
			copy(img.Pix[pixRowStart+b*8:pixRowStart+b*8+8], unpack8[i.data[srcRowStart+b]][:])
		}
		if tailBits != 0 {
			tail := i.data[srcRowStart+fullBytes]
			base := pixRowStart + fullBytes*8
			for k := 0; k < tailBits; k++ {
				if (tail>>uint(7-k))&1 != 0 {
					img.Pix[base+k] = 0
				} else {
					img.Pix[base+k] = 255
				}
			}
		}
	}
	return img
}

// unpack8 maps a packed source byte to its 8 [image.Gray] pix
// bytes (ink=0, paper=255). 2 KiB, built in init.
var unpack8 [256][8]byte

func init() {
	for b := 0; b < 256; b++ {
		for k := 0; k < 8; k++ {
			if (b>>uint(7-k))&1 != 0 {
				unpack8[b][k] = 0
			} else {
				unpack8[b][k] = 255
			}
		}
	}
}
