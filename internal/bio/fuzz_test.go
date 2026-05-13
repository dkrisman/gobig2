package bio

import (
	"testing"
)

// FuzzBitStream exercises BitStream API on random bytes. Contract:
// no panic, including reads past declared end / seeks past start.
//
// Seeds: boundary cases (empty, 1 byte, 2 bytes) + bit patterns
// catching off-by-one in arith-coder renormalization above.
func FuzzBitStream(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0x00})
	f.Add([]byte{0xFF})
	f.Add([]byte{0x80, 0x40, 0x20})
	f.Add([]byte{0xAA, 0x55, 0xAA, 0x55})
	f.Add([]byte{0x97, 0x4A, 0x42, 0x32, 0x0D, 0x0A, 0x1A, 0x0A}) // JBIG2 magic
	f.Fuzz(func(t *testing.T, data []byte) {
		// New + every method taking no further input. No panic
		// regardless of buffer size. Drive ReadNBits widths 1..32
		// to hit bit-position arithmetic.
		bs := NewBitStream(data, 0)
		bs.SetLittleEndian(false)
		_ = bs.GetCurByte()
		_ = bs.GetCurByteArith()
		_ = bs.GetNextByteArith()
		_ = bs.GetOffset()
		_ = bs.GetByteLeft()
		_ = bs.GetLength()
		_ = bs.GetPointer()
		_ = bs.GetKey()
		_ = bs.GetBitPos()
		_ = bs.IsInBounds()

		// Read a series of values of escalating widths.
		_, _ = bs.Read1Bit()
		_, _ = bs.Read1BitBool()
		_, _ = bs.Read1Byte()
		_, _ = bs.ReadNBits(7)
		_, _ = bs.ReadNBits(13)
		_, _ = bs.ReadNBits(32)
		// ReadNBitsZeroPad: MMR codeword-window helper has
		// different EOS semantics (zero-pad vs error) and own
		// bounds arithmetic.
		_, _, _ = bs.ReadNBitsZeroPad(7)
		_, _, _ = bs.ReadNBitsZeroPad(24)
		_, _, _ = bs.ReadNBitsZeroPad(32)
		_, _ = bs.ReadNBitsInt32(16)
		_, _ = bs.ReadShortInteger()
		_, _ = bs.ReadInteger()

		// Seek operations that can OOB on adversarial offsets.
		bs.SetOffset(0)
		bs.SetOffset(uint32(len(data)) + 1)
		bs.AddOffset(0xFFFFFFFF)
		bs.SetBitPos(0)
		bs.SetBitPos(0xFFFFFFFF)
		bs.AlignByte()
		bs.IncByteIdx()

		// Little-endian path.
		bs2 := NewBitStream(data, 0)
		bs2.SetLittleEndian(true)
		_, _ = bs2.ReadInteger()
		_, _ = bs2.ReadShortInteger()
	})
}
