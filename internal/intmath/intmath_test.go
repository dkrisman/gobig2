package intmath

import "testing"

func TestCeilLog2U32(t *testing.T) {
	cases := []struct {
		in   uint32
		want uint8
	}{
		{0, 0},
		{1, 0},
		{2, 1},
		{3, 2},
		{4, 2},
		{5, 3},
		{7, 3},
		{8, 3},
		{9, 4},
		{1023, 10},
		{1024, 10},
		{1025, 11},
		{1 << 16, 16},
		{(1 << 16) + 1, 17},
		{1 << 20, 20},
		{1 << 30, 30},
		{1 << 31, 31},
		{(1 << 31) + 1, 32},
		{0xFFFF_FFFF, 32},
	}
	for _, c := range cases {
		got := CeilLog2U32(c.in)
		if got != c.want {
			t.Errorf("CeilLog2U32(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}
