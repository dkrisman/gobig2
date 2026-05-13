package gobig2test

import (
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"io"
	"os"
)

// referenceBitmap is test-side normalized bi-level image from
// reference file (BMP / PBM / PNG). 1 = ink, 0 = paper. Row-major
// MSB-first packed, matching codec bitmap layout, so tests compare
// raw byte slices instead of per-pixel loops.
type referenceBitmap struct {
	width, height int
	stride        int
	data          []byte
}

// pixel returns the bit at (x,y); 1 = ink. Out-of-range coordinates return 0.
func (r *referenceBitmap) pixel(x, y int) int {
	if x < 0 || x >= r.width || y < 0 || y >= r.height {
		return 0
	}
	return int(r.data[y*r.stride+(x>>3)]>>(7-uint(x&7))) & 1
}

// hammingDistance counts pixel mismatches in overlap rect. Returns
// sizes too so callers can apply per-pixel tolerance or report
// mismatched dimensions verbatim.
func (r *referenceBitmap) hammingDistance(other *referenceBitmap) (mismatches, comparedPixels int) {
	if r == nil || other == nil {
		return 0, 0
	}
	w := r.width
	if other.width < w {
		w = other.width
	}
	h := r.height
	if other.height < h {
		h = other.height
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if r.pixel(x, y) != other.pixel(x, y) {
				mismatches++
			}
		}
	}
	return mismatches, w * h
}

// loadReference reads BMP or PBM, normalizes to ink=1 packed
// MSB-first. Errors on unsupported formats so caller fails loud
// rather than silently passing comparison.
func loadReference(path string) (*referenceBitmap, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	head := make([]byte, 2)
	if _, err := io.ReadFull(f, head); err != nil {
		return nil, err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	switch {
	case head[0] == 'B' && head[1] == 'M':
		return readBMP(f)
	case head[0] == 'P' && (head[1] == '1' || head[1] == '4'):
		return readPBM(f)
	}
	return nil, fmt.Errorf("unsupported reference format: % x", head)
}

// readBMP handles uncompressed 1bpp Windows BMP (BITMAPINFOHEADER,
// variant produced by JBIG2 conformance corpus). Other variants
// rejected - suite never uses them.
func readBMP(f *os.File) (*referenceBitmap, error) {
	const fileHdrLen = 14
	const infoHdrV3Len = 40
	hdr := make([]byte, fileHdrLen+infoHdrV3Len)
	if _, err := io.ReadFull(f, hdr); err != nil {
		return nil, fmt.Errorf("bmp header: %w", err)
	}
	if hdr[0] != 'B' || hdr[1] != 'M' {
		return nil, errors.New("bmp: bad signature")
	}
	bfOffBits := binary.LittleEndian.Uint32(hdr[10:14])
	biSize := binary.LittleEndian.Uint32(hdr[14:18])
	if biSize < infoHdrV3Len {
		return nil, fmt.Errorf("bmp: unsupported info header size %d", biSize)
	}
	width := int32(binary.LittleEndian.Uint32(hdr[18:22]))
	height := int32(binary.LittleEndian.Uint32(hdr[22:26]))
	bitCount := binary.LittleEndian.Uint16(hdr[28:30])
	compression := binary.LittleEndian.Uint32(hdr[30:34])
	if bitCount != 1 {
		return nil, fmt.Errorf("bmp: only 1bpp supported, got %d bpp", bitCount)
	}
	if compression != 0 {
		return nil, fmt.Errorf("bmp: only BI_RGB supported, got compression %d", compression)
	}
	flip := height > 0 // bottom-up when positive
	if height < 0 {
		height = -height
	}
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("bmp: bad dimensions %d x %d", width, height)
	}

	// Read 2-entry palette to learn which color is ink.
	// Palette sits between info header and bfOffBits.
	if _, err := f.Seek(int64(fileHdrLen+biSize), io.SeekStart); err != nil {
		return nil, err
	}
	palette := make([]byte, 8)
	if _, err := io.ReadFull(f, palette); err != nil {
		return nil, fmt.Errorf("bmp: palette: %w", err)
	}
	// Palette entries BGR0. File bit value = palette index, not
	// ink/paper. Index 0 = ink when darker entry at offset 0 - then
	// file 0-bits mean ink, 1-bits mean paper, inverse of codec's
	// 1=ink. Flip in that case.
	lum := func(off int) int { return int(palette[off]) + int(palette[off+1]) + int(palette[off+2]) }
	inkIsZero := lum(0) <= lum(4)

	// BMP rows are padded to 4-byte boundaries.
	rowBytes := ((int(width) + 31) / 32) * 4
	stride := (int(width) + 7) / 8
	if _, err := f.Seek(int64(bfOffBits), io.SeekStart); err != nil {
		return nil, err
	}
	rows := make([][]byte, height)
	row := make([]byte, rowBytes)
	for y := int32(0); y < height; y++ {
		if _, err := io.ReadFull(f, row); err != nil {
			return nil, fmt.Errorf("bmp: row %d: %w", y, err)
		}
		out := make([]byte, stride)
		copy(out, row[:stride])
		if inkIsZero {
			for i := range out {
				out[i] ^= 0xFF
			}
		}
		// Mask padding bits in trailing byte so they don't pollute
		// byte-equality comparisons regardless of polarity branch.
		if rem := uint(int(width) & 7); rem != 0 {
			out[stride-1] &^= byte(0xFF >> rem)
		}
		rows[y] = out
	}

	data := make([]byte, stride*int(height))
	for y := int32(0); y < height; y++ {
		src := rows[y]
		var dst int
		if flip {
			dst = (int(height) - 1 - int(y)) * stride
		} else {
			dst = int(y) * stride
		}
		copy(data[dst:dst+stride], src)
	}
	return &referenceBitmap{
		width:  int(width),
		height: int(height),
		stride: stride,
		data:   data,
	}, nil
}

// readPBM handles ASCII (P1) and raw (P4) PBM. PBM spec: 1 = ink,
// matches codec, no inversion required.
func readPBM(f *os.File) (*referenceBitmap, error) {
	all, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	if len(all) < 2 {
		return nil, errors.New("pbm: too short")
	}
	if all[0] != 'P' || (all[1] != '1' && all[1] != '4') {
		return nil, fmt.Errorf("pbm: bad magic % q", all[:2])
	}
	pos := 2
	skipPBMWS := func() {
		for pos < len(all) {
			c := all[pos]
			switch c {
			case ' ', '\t', '\r', '\n':
				pos++
			case '#':
				for pos < len(all) && all[pos] != '\n' {
					pos++
				}
			default:
				return
			}
		}
	}
	readInt := func() (int, error) {
		skipPBMWS()
		start := pos
		for pos < len(all) && all[pos] >= '0' && all[pos] <= '9' {
			pos++
		}
		if pos == start {
			return 0, errors.New("pbm: expected integer")
		}
		v := 0
		for _, c := range all[start:pos] {
			v = v*10 + int(c-'0')
		}
		return v, nil
	}
	width, err := readInt()
	if err != nil {
		return nil, err
	}
	height, err := readInt()
	if err != nil {
		return nil, err
	}
	if all[1] == '4' {
		// raw: skip exactly one whitespace byte then read packed pixels
		if pos >= len(all) {
			return nil, errors.New("pbm: truncated")
		}
		pos++
		stride := (width + 7) / 8
		need := stride * height
		if pos+need > len(all) {
			return nil, errors.New("pbm: pixel data truncated")
		}
		data := make([]byte, need)
		copy(data, all[pos:pos+need])
		return &referenceBitmap{width: width, height: height, stride: stride, data: data}, nil
	}
	// ASCII: each pixel is '0' or '1'
	stride := (width + 7) / 8
	data := make([]byte, stride*height)
	idx := 0
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			skipPBMWS()
			if pos >= len(all) {
				return nil, errors.New("pbm: pixel data truncated")
			}
			c := all[pos]
			pos++
			if c == '1' {
				data[idx+(x>>3)] |= 1 << (7 - uint(x&7))
			} else if c != '0' {
				return nil, fmt.Errorf("pbm: bad pixel %q", c)
			}
		}
		idx += stride
	}
	return &referenceBitmap{width: width, height: height, stride: stride, data: data}, nil
}

// imageToReference normalizes codec output (*Image or image.Image)
// to packed MSB-first ink=1 layout used by reference bitmaps for
// byte-compare. Returns nil on nil/empty input.
func imageToReference(img image.Image) *referenceBitmap {
	if img == nil {
		return nil
	}
	b := img.Bounds()
	width := b.Dx()
	height := b.Dy()
	if width <= 0 || height <= 0 {
		return nil
	}
	stride := (width + 7) / 8
	data := make([]byte, stride*height)
	// Use Gray bit-trick: any pixel below midpoint is ink.
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			r, g, blue, _ := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			lum := r + g + blue
			if lum < (3 * 0x8000) {
				data[y*stride+(x>>3)] |= 1 << (7 - uint(x&7))
			}
		}
	}
	return &referenceBitmap{width: width, height: height, stride: stride, data: data}
}
