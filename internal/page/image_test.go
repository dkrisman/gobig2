package page

import "testing"

// TestNewImageRejects pins NewImage allocation guards:
// non-positive dims, overflow shapes, MaxImagePixels rejections
// return nil instead of panicking or allocating.
func TestNewImageRejects(t *testing.T) {
	cases := []struct {
		name string
		w, h int32
	}{
		{"zero width", 0, 100},
		{"zero height", 100, 0},
		{"negative width", -1, 100},
		{"negative height", 100, -1},
		// stride = ceil(MaxInt32/8) * 8 = MaxInt32-aligned. height *
		// stride wraps int32. NewImage's `height > MaxInt32/stride`
		// guard should reject before allocation.
		{"int32 overflow", 0x7FFFFFFF, 0x7FFFFFFF},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if img := NewImage(c.w, c.h); img != nil {
				t.Errorf("NewImage(%d, %d) accepted bad dims", c.w, c.h)
			}
		})
	}
}

// TestExpandOverflowGuard: Image.Expand rejects heights that
// overflow int32 `stride * height` packed-buffer math, even
// with MaxImagePixels disabled (cap == 0). Without guard,
// call panics on negative-product alloc or allocates buffer
// whose length mismatches recorded height.
func TestExpandOverflowGuard(t *testing.T) {
	// Save + restore the cap so disabling it doesn't leak.
	prev := MaxImagePixels
	MaxImagePixels = 0
	defer func() { MaxImagePixels = prev }()

	// Build image with stride > 1 so stride*height overflows
	// well below MaxInt32. width=16 -> stride=2; threshold =
	// MaxInt32/2 = ~1.07 billion. height = MaxInt32 wraps
	// stride*height negative - without guard, panic on neg-size
	// alloc or buffer length mismatches recorded height.
	img := NewImage(16, 1)
	if img == nil {
		t.Fatal("setup NewImage(16,1) failed")
	}
	img.Expand(0x7FFFFFFF, false)
	if img.Height() != 1 {
		t.Errorf("Expand should be no-op on int32 overflow; height = %d, want 1", img.Height())
	}
}

// TestExpandRespectsMaxImagePixels pins the existing
// MaxImagePixels guard.
func TestExpandRespectsMaxImagePixels(t *testing.T) {
	prev := MaxImagePixels
	MaxImagePixels = 4 // very tight cap
	defer func() { MaxImagePixels = prev }()

	img := NewImage(1, 4) // 4 pixels = at cap exactly
	if img == nil {
		t.Fatal("setup NewImage at cap failed")
	}
	img.Expand(100, false) // 100 pixels > 4 cap
	if img.Height() != 4 {
		t.Errorf("Expand past MaxImagePixels should be no-op; height = %d, want 4", img.Height())
	}
}

// TestExpandShorterIsNoOp pins the documented behavior: an
// Expand request shorter than the current height does nothing.
func TestExpandShorterIsNoOp(t *testing.T) {
	img := NewImage(10, 100)
	if img == nil {
		t.Fatal("setup failed")
	}
	img.Expand(50, false)
	if img.Height() != 100 {
		t.Errorf("Expand shorter than current should be no-op; height = %d, want 100", img.Height())
	}
}

// composeReference is the per-pixel oracle for
// TestComposeToMatchesReference: same op semantics as ComposeTo,
// implemented via GetPixel / SetPixel. ComposeTo's byte-batched
// path must produce identical pixels.
func composeReference(src, dst *Image, x, y int32, op ComposeOp) {
	if src == nil || dst == nil {
		return
	}
	for h := int32(0); h < src.height; h++ {
		for w := int32(0); w < src.width; w++ {
			dx := x + w
			dy := y + h
			if dx < 0 || dx >= dst.width || dy < 0 || dy >= dst.height {
				continue
			}
			srcBit := src.GetPixel(w, h)
			dstBit := dst.GetPixel(dx, dy)
			var resBit int
			switch op {
			case ComposeOr:
				resBit = dstBit | srcBit
			case ComposeAnd:
				resBit = dstBit & srcBit
			case ComposeXor:
				resBit = dstBit ^ srcBit
			case ComposeXnor:
				if dstBit == srcBit {
					resBit = 1
				}
			case ComposeReplace:
				resBit = srcBit
			default:
				resBit = dstBit
			}
			dst.SetPixel(dx, dy, resBit)
		}
	}
}

// TestComposeToMatchesReference sweeps every op x source size x
// (x, y) alignment combination against composeReference. Dest is
// pre-filled with a non-uniform pattern so Replace / Xnor catch
// any leak of bits outside the source rect.
func TestComposeToMatchesReference(t *testing.T) {
	ops := []struct {
		name string
		op   ComposeOp
	}{
		{"Or", ComposeOr},
		{"And", ComposeAnd},
		{"Xor", ComposeXor},
		{"Xnor", ComposeXnor},
		{"Replace", ComposeReplace},
	}

	srcWidths := []int32{1, 3, 7, 8, 9, 15, 16, 17, 23}
	srcHeights := []int32{1, 3, 8}
	dstW := int32(40)
	dstH := int32(20)
	xOffsets := []int32{0, 1, 3, 7, 8, 13, 25, 32}
	yOffsets := []int32{0, 1, 7, 10}

	for _, opc := range ops {
		for _, sw := range srcWidths {
			for _, sh := range srcHeights {
				for _, x := range xOffsets {
					for _, y := range yOffsets {
						src := patternImage(sw, sh, 0xA5) // 1010_0101 pattern
						dstFast := patternImage(dstW, dstH, 0x6C)
						dstSlow := patternImage(dstW, dstH, 0x6C)
						src.ComposeTo(dstFast, x, y, opc.op)
						composeReference(src, dstSlow, x, y, opc.op)
						if !equalPixels(dstFast, dstSlow) {
							t.Errorf("%s sw=%d sh=%d x=%d y=%d: fast path diverges from oracle",
								opc.name, sw, sh, x, y)
						}
					}
				}
			}
		}
	}
}

// TestComposeToClipping exercises every direction the source
// rect can overhang dst, plus fully-outside cases. The clip math
// in ComposeTo's preamble must trim rows / columns or the byte
// writes would land outside dst.
func TestComposeToClipping(t *testing.T) {
	cases := []struct {
		name string
		x, y int32
	}{
		{"left overhang", -3, 0},
		{"top overhang", 0, -2},
		{"right overhang", 35, 0},
		{"bottom overhang", 0, 18},
		{"corner overhang", -5, -3},
		{"fully outside left", -100, 0},
		{"fully outside right", 100, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := patternImage(10, 5, 0xFF)
			dstFast := patternImage(40, 20, 0x00)
			dstSlow := patternImage(40, 20, 0x00)
			src.ComposeTo(dstFast, c.x, c.y, ComposeOr)
			composeReference(src, dstSlow, c.x, c.y, ComposeOr)
			if !equalPixels(dstFast, dstSlow) {
				t.Errorf("%s (x=%d, y=%d): fast path diverges", c.name, c.x, c.y)
			}
		})
	}
}

// TestToGoImageInkPaper pins the public color convention (ink ->
// Y 0, paper -> Y 255). Width 11 covers both the unpack8 full-
// byte path and the tail-bit loop.
func TestToGoImageInkPaper(t *testing.T) {
	src := NewImage(11, 3)
	if src == nil {
		t.Fatal("setup")
	}
	// Set a known pixel pattern: ink at (0,0), (3,0), (10,0),
	// (7,1), (1,2), (8,2). Everything else paper.
	for _, p := range [][2]int32{{0, 0}, {3, 0}, {10, 0}, {7, 1}, {1, 2}, {8, 2}} {
		src.SetPixel(p[0], p[1], 1)
	}
	im := src.ToGoImage()
	if im == nil {
		t.Fatal("ToGoImage returned nil")
	}
	for y := int32(0); y < 3; y++ {
		for x := int32(0); x < 11; x++ {
			want := byte(255) // paper
			if src.GetPixel(x, y) != 0 {
				want = 0 // ink
			}
			got := im.At(int(x), int(y))
			// image.Gray.At returns color.Gray; the Y field is
			// the byte we set.
			gray, ok := got.(interface{ RGBA() (r, g, b, a uint32) })
			if !ok {
				t.Fatalf("(%d, %d): expected gray color", x, y)
			}
			r, _, _, _ := gray.RGBA()
			gotY := byte(r >> 8)
			if gotY != want {
				t.Errorf("(%d, %d): got Y=%d, want %d", x, y, gotY, want)
			}
		}
	}
}

// patternImage returns a w x h image filled with byte `pat`,
// with each row's unused tail bits masked to zero.
func patternImage(w, h int32, pat byte) *Image {
	img := NewImage(w, h)
	if img == nil {
		return nil
	}
	for i := range img.data {
		img.data[i] = pat
	}
	if w%8 != 0 {
		mask := byte(0xFF << (8 - uint(w%8)))
		for y := int32(0); y < h; y++ {
			img.data[y*img.stride+img.stride-1] &= mask
		}
	}
	return img
}

// equalPixels returns true iff a and b match bit-for-bit over
// their declared width / height. Unused tail bits are ignored.
func equalPixels(a, b *Image) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.width != b.width || a.height != b.height {
		return false
	}
	for y := int32(0); y < a.height; y++ {
		for x := int32(0); x < a.width; x++ {
			if a.GetPixel(x, y) != b.GetPixel(x, y) {
				return false
			}
		}
	}
	return true
}
