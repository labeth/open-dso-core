// ENGMODEL-OWNER-UNIT: FU-APP-LCD

package render

import (
	"bytes"
	"image"
	"image/png"
	"unsafe"
)

const (
	// W is the panel's width in pixels.
	W = 800
	// H is the panel's height in pixels.
	H         = 480
	pageBytes = W * H * 2
)

// RGB565 packing: pixel = (R>>3)<<11 | (G>>2)<<5 | (B>>3).
// TRLC-LINKS: REQ-SDS-021
func rgb(r, g, b uint8) uint16 {
	return uint16(r>>3)<<11 | uint16(g>>2)<<5 | uint16(b>>3)
}

// Surface is the only drawing contract the renderer sees (spec 07 §1.3):
// the same renderer must run byte-identically against /dev/fb0 and an
// in-memory test surface.
// TRLC-LINKS: REQ-SDS-021
type Surface interface {
	SetPixel(x, y int, c uint16) // out-of-range ignored
	Fill(c uint16)
	At(x, y int) uint16 // out-of-range returns 0
}

// MemSurface is the back buffer (exactly one page) and the test surface.
// TRLC-LINKS: REQ-SDS-021
type MemSurface struct {
	Pix []byte // RGB565 little-endian, W*H*2 bytes
}

// NewMemSurface returns a blank (black) in-memory page.
// TRLC-LINKS: REQ-SDS-021
func NewMemSurface() *MemSurface { return &MemSurface{Pix: make([]byte, pageBytes)} }

// SetPixel sets one pixel; one off the page is ignored.
// TRLC-LINKS: REQ-SDS-021
func (m *MemSurface) SetPixel(x, y int, c uint16) {
	if x < 0 || x >= W || y < 0 || y >= H {
		return
	}
	o := (y*W + x) * 2
	m.Pix[o] = byte(c & 0xff)
	m.Pix[o+1] = byte(c >> 8)
}

// vline draws column x from y0 to y1 inclusive: straight into a MemSurface's
// pixels (a dense trace fills most of the screen height every frame), else
// pixel by pixel.
// TRLC-LINKS: REQ-SDS-021
func vline(sf Surface, x, y0, y1 int, c uint16) {
	m, ok := sf.(*MemSurface)
	if !ok {
		for y := y0; y <= y1; y++ {
			sf.SetPixel(x, y, c)
		}
		return
	}
	if x < 0 || x >= W {
		return
	}
	y0, y1 = max(y0, 0), min(y1, H-1)
	if y0 > y1 {
		return
	}
	vlinePx(m.px(), x, y0, y1, c)
}

// vlinePx is vline on pixels already clipped to the screen: x in [0,W),
// 0 <= y0, y1 < H. Walking a slice of exactly the column's extent lets the
// compiler drop the per-pixel bounds check.
// TRLC-LINKS: REQ-SDS-021
func vlinePx(px []uint16, x, y0, y1 int, c uint16) {
	if y0 > y1 {
		return
	}
	col := px[y0*W+x : y1*W+x+1]
	for o := 0; o < len(col); o += W {
		col[o] = c
	}
}

// px is the surface's pixels as RGB565 words (little-endian, native on ARM
// and amd64).
// TRLC-LINKS: REQ-SDS-021
//
// An array-pointer conversion, not unsafe.Slice: the latter's size check is a
// runtime division on this ARM, and px is taken per drawn column.
func (m *MemSurface) px() []uint16 {
	_ = m.Pix[pageBytes-1]
	return (*[W * H]uint16)(unsafe.Pointer(&m.Pix[0]))[:] //nolint:gosec // Pix is W*H RGB565 words, length checked above
}

// fillSpan fills rows [y0,y1) of columns [x0,x1) of a MemSurface, clipped:
// the sheets, panes and the F-key column fill most of the screen, which
// pixel by pixel cost most of a frame.
// TRLC-LINKS: REQ-SDS-021
func (m *MemSurface) fillSpan(x0, y0, x1, y1 int, c uint16) {
	x0, x1 = max(x0, 0), min(x1, W)
	y0, y1 = max(y0, 0), min(y1, H)
	if x0 >= x1 || y0 >= y1 {
		return
	}
	px := m.px()
	row := px[y0*W+x0 : y0*W+x1]
	for i := range row {
		row[i] = c
	}
	for y := y0 + 1; y < y1; y++ {
		copy(px[y*W+x0:y*W+x1], row)
	}
}

// Fill sets every pixel of the page to c.
// TRLC-LINKS: REQ-SDS-021
func (m *MemSurface) Fill(c uint16) {
	lo, hi := byte(c&0xff), byte(c>>8)
	row := m.Pix[:W*2]
	for x := range W {
		row[x*2], row[x*2+1] = lo, hi
	}
	for y := 1; y < H; y++ {
		copy(m.Pix[y*W*2:(y+1)*W*2], row)
	}
}

// At reads one pixel (0 off the page).
// TRLC-LINKS: REQ-SDS-021
func (m *MemSurface) At(x, y int) uint16 {
	if x < 0 || x >= W || y < 0 || y >= H {
		return 0
	}
	o := (y*W + x) * 2
	return uint16(m.Pix[o]) | uint16(m.Pix[o+1])<<8
}

// FadeToBlack dims every RGB565 pixel toward black by 1/4 (the persistence decay
// step). Pixels already black are skipped. Traces drawn onto this layer at full
// brightness thus glow and fade over ~8 frames.
// TRLC-LINKS: REQ-SDS-021
func (m *MemSurface) FadeToBlack() {
	p := m.px()
	for i, c := range p {
		if c == 0 {
			continue
		}
		r := ((c >> 11) & 0x1f) * 3 / 4
		g := ((c >> 5) & 0x3f) * 3 / 4
		b := (c & 0x1f) * 3 / 4
		p[i] = r<<11 | g<<5 | b
	}
}

// BlitBright copies the non-black (bright enough) pixels of src onto m — the
// persistence trace layer composited over the fresh graticule.
// TRLC-LINKS: REQ-SDS-021
func (m *MemSurface) BlitBright(src *MemSurface) {
	d, s := m.px(), src.px()
	for i, c := range s {
		if c == 0 {
			continue
		}
		if (c>>11)&0x1f+(c>>6)&0x1f+c&0x1f < 3 { // too dim → let the graticule show
			continue
		}
		d[i] = c
	}
}

// EncodePNG renders the surface (RGB565) to a PNG — the device-screen view for
// the web /api/screen.png endpoint. Pixels are expanded straight into the
// image and compressed at BestSpeed: the default level cost ~10x the time
// for a screen grab the browser decodes to the same pixels.
// TRLC-LINKS: REQ-SDS-021
func EncodePNG(m *MemSurface) []byte {
	img := image.NewRGBA(image.Rect(0, 0, W, H))
	d := img.Pix
	for i, c := range m.px() {
		o := i * 4
		d[o] = uint8((c>>11)&0x1f) << 3
		d[o+1] = uint8((c>>5)&0x3f) << 2
		d[o+2] = uint8(c&0x1f) << 3
		d[o+3] = 255
	}
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := enc.Encode(&buf, img); err != nil {
		return nil // not reached: a fixed-size RGBA image into a memory buffer
	}
	return buf.Bytes()
}

// StatusSlotRows are the rows the status slot spans (RedrawStatusSlot).
const StatusSlotRows = 11
