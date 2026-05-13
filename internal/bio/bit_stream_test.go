package bio

import (
	"math"
	"strings"
	"testing"
)

// TestReadNBitsOverflowReject pins: widths past uint32 rejected,
// widths near math.MaxUint32 don't wrap past remaining-bits check.
func TestReadNBitsOverflowReject(t *testing.T) {
	cases := []struct {
		name string
		bits uint32
	}{
		{"33-bits one past uint32 width", 33},
		{"64-bits double width", 64},
		{"math.MaxUint32", math.MaxUint32},
		{"math.MaxUint32 - 1", math.MaxUint32 - 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			bs := NewBitStream([]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF}, 0)
			_, err := bs.ReadNBits(c.bits)
			if err == nil {
				t.Fatalf("ReadNBits(%d) accepted oversize width; want error", c.bits)
			}
			if !strings.Contains(err.Error(), "width exceeds") &&
				!strings.Contains(err.Error(), "truncated") {
				t.Errorf("err = %q, want 'width exceeds' or 'truncated'", err)
			}
		})
	}
}

// TestReadNBitsTruncationGuard pins: truncation check fires for
// legal widths past EOS instead of uint32 wrap masking it.
func TestReadNBitsTruncationGuard(t *testing.T) {
	bs := NewBitStream([]byte{0xFF}, 0) // 8 bits available
	if _, err := bs.ReadNBits(8); err != nil {
		t.Fatalf("8 bits should fit: %v", err)
	}
	if _, err := bs.ReadNBits(1); err == nil {
		t.Fatalf("9th bit should error on 8-bit input")
	}
}

// TestReadNBitsZeroPadOverflowReject pins the matching ZeroPad
// overflow contract.
func TestReadNBitsZeroPadOverflowReject(t *testing.T) {
	bs := NewBitStream([]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF}, 0)
	if _, _, err := bs.ReadNBitsZeroPad(33); err == nil {
		t.Fatal("ReadNBitsZeroPad(33) accepted oversize width; want error")
	}
	if _, _, err := bs.ReadNBitsZeroPad(math.MaxUint32); err == nil {
		t.Fatal("ReadNBitsZeroPad(MaxUint32) accepted oversize width; want error")
	}
}

// TestReadNBitsZeroPadEndOfStream pins documented behavior:
// requests past EOD right-zero-pad vs error; `consumed` reflects
// real bits taken.
// TestReadNBitsInt32SignExtension pins sub-32-bit signed field
// returns two's-complement sign-extended int32. Naive int32(val)
// from ReadNBits yields unsigned cast: 5-bit 11111 -> 31 not -1.
func TestReadNBitsInt32SignExtension(t *testing.T) {
	cases := []struct {
		name      string
		input     []byte
		bits      uint32
		wantValue int32
	}{
		{"5-bit 11111 = -1", []byte{0xF8}, 5, -1},
		{"5-bit 10000 = -16", []byte{0x80}, 5, -16},
		{"5-bit 01111 = 15", []byte{0x78}, 5, 15},
		{"5-bit 00000 = 0", []byte{0x00}, 5, 0},
		{"1-bit 1 = -1", []byte{0x80}, 1, -1},
		{"1-bit 0 = 0", []byte{0x00}, 1, 0},
		{"16-bit 0xFFFF = -1", []byte{0xFF, 0xFF}, 16, -1},
		{"16-bit 0x8000 = -32768", []byte{0x80, 0x00}, 16, -32768},
		{"16-bit 0x7FFF = 32767", []byte{0x7F, 0xFF}, 16, 32767},
		{"32-bit 0xFFFFFFFF = -1", []byte{0xFF, 0xFF, 0xFF, 0xFF}, 32, -1},
		{"32-bit 0x80000000 = MinInt32", []byte{0x80, 0x00, 0x00, 0x00}, 32, -2147483648},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			bs := NewBitStream(c.input, 0)
			got, err := bs.ReadNBitsInt32(c.bits)
			if err != nil {
				t.Fatalf("ReadNBitsInt32(%d): %v", c.bits, err)
			}
			if got != c.wantValue {
				t.Errorf("ReadNBitsInt32(%d) on %v = %d, want %d",
					c.bits, c.input, got, c.wantValue)
			}
		})
	}
}

func TestReadNBitsZeroPadEndOfStream(t *testing.T) {
	bs := NewBitStream([]byte{0xA0}, 0) // 10100000 = 8 bits
	// Skip 4 (1010) -> 4 real bits remain (0000).
	if _, err := bs.ReadNBits(4); err != nil {
		t.Fatalf("seed read failed: %v", err)
	}
	// Ask 8 -> read 4 real, right-pad 4 zeros.
	v, consumed, err := bs.ReadNBitsZeroPad(8)
	if err != nil {
		t.Fatalf("ReadNBitsZeroPad past end: %v", err)
	}
	if consumed != 4 {
		t.Errorf("consumed = %d, want 4", consumed)
	}
	// 4 real 0000 + 4 pad = 0.
	if v != 0 {
		t.Errorf("value = %#x, want 0 (4 zero bits + 4 zero pad)", v)
	}
}
