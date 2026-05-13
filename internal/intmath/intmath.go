// Package intmath holds small integer helpers shared across the
// decoder. Own package (not internal/state, not internal/bio) so
// helpers unit-test in isolation without dragging parser or
// bit-stream surface.
package intmath

// CeilLog2U32 returns smallest n s.t. (uint32(1) << n) >= count.
//
// Edge cases (easy to mis-inline):
//
//   - CeilLog2U32(0) == 0. Zero counts need no bits; empty index
//     range encodes as zero-width field.
//   - CeilLog2U32(1) == 0. Single value needs zero index bits.
//   - CeilLog2U32(2) == 1, (3) == 2, (4) == 2.
//   - CeilLog2U32(1 << 31) == 31, ((1 << 31) + 1) == 32.
//   - count > (1 << 31) returns 32 (uint32 shift width). Callers
//     allocating `1 << n` arrays should treat 32 as "too large"
//     and reject as resource-budget vs overflow into adversarial
//     allocation.
//
// No error returned; callers carry own cap policy
// (MaxSymbolsPerDict, MaxIaidCodeLen, etc.) before invoking.
// Contract: math identity above + explicit clamp at 32.
func CeilLog2U32(count uint32) uint8 {
	if count <= 1 {
		return 0
	}
	// Walk powers of 2 until count covered. n bounded by 32: 1
	// << 32 wraps to zero in uint32, re-triggering loop forever.
	// Clamp doubles as ceiling for callers w/ disabled caps.
	var n uint8
	for n < 32 {
		if (uint32(1) << n) >= count {
			return n
		}
		n++
	}
	return 32
}
