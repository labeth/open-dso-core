// ENGMODEL-OWNER-UNIT: FU-APP-LCD

package render

import (
	"bytes"
	"image"
	"image/png"
	"unsafe"

	"github.com/labeth/open-dso-core/hal"
)

// Color is a colour as the renderer names it, 0xAARRGGBB: 8 bits each of
// alpha, red, green and blue. A canvas converts it to its pixel format; an
// RGB565 canvas keeps each channel's top bits.
type Color uint32

// Transparent is no colour: on an ARGB8888 canvas the plane under the UI
// shows through it; elsewhere it is black.
const Transparent Color = 0

// RGB is an opaque colour.
func RGB(r, g, b uint8) Color {
	return 0xff000000 | Color(r)<<16 | Color(g)<<8 | Color(b)
}

// rgb is RGB.
// TRLC-LINKS: REQ-SDS-021
func rgb(r, g, b uint8) Color { return RGB(r, g, b) }

// channels are c's red, green and blue.
func (c Color) channels() (r, g, b uint8) {
	return uint8(c >> 16), uint8(c >> 8), uint8(c) //nolint:gosec // a byte each
}

// c565 is an RGB565 word as a colour, each channel's bits repeated into its
// low bits: packing it again gives the word back.
// TRLC-LINKS: REQ-SDS-021
func c565(p uint16) Color {
	r, g, b := uint8(p>>11), uint8(p>>5)&0x3f, uint8(p)&0x1f //nolint:gosec // 5 and 6 bits
	return RGB(r<<3|r>>2, g<<2|g>>4, b<<3|b>>2)
}

// pack565 is c in RGB565: each channel's top bits.
// TRLC-LINKS: REQ-SDS-021
func pack565(c Color) uint16 {
	return uint16(c>>8)&0xf800 | uint16(c>>5)&0x07e0 | uint16(c>>3)&0x001f //nolint:gosec // masked to 16 bits
}

// Canvas is what the renderer draws on (spec 07 §1.3): the same renderer
// runs pixel-identically against a board's framebuffer and an in-memory
// canvas. Its pixel format decides how a Color is stored.
// TRLC-LINKS: REQ-SDS-021
type Canvas interface {
	Format() hal.PixelFormat
	Metrics() *Metrics          // the screen's layout table; its W and H are the canvas's size
	SetPixel(x, y int, c Color) // out-of-range ignored
	Fill(c Color)
	At(x, y int) Color // out-of-range returns Transparent
}

// Layer is a memory canvas the persistence afterglow draws its traces on:
// it decays a step a frame and is composited over the fresh picture.
type Layer interface {
	Canvas
	FadeToBlack()
	BlitBright(src Canvas) // src's bright pixels onto this canvas
}

// Image565 is an in-memory RGB565 canvas: a display's back buffer (exactly
// one page) and the test canvas.
// TRLC-LINKS: REQ-SDS-021
type Image565 struct {
	Pix []byte // RGB565 little-endian, W*H*2 bytes
	pix []uint16
	m   *Metrics
}

// NewImage565 returns a blank (black) RGB565 page of screen m's size.
// TRLC-LINKS: REQ-SDS-021
func NewImage565(m *Metrics) *Image565 {
	pix := make([]uint16, m.W*m.H)
	return &Image565{Pix: unsafe.Slice((*byte)(unsafe.Pointer(&pix[0])), 2*len(pix)), pix: pix, m: m} //nolint:gosec // the words' own bytes, little-endian as the panel takes them
}

// Format is RGB565.
func (m *Image565) Format() hal.PixelFormat { return hal.RGB565 }

// Metrics is the canvas's screen.
func (m *Image565) Metrics() *Metrics { return m.m }

// SetPixel sets one pixel; one off the page is ignored.
// TRLC-LINKS: REQ-SDS-021
func (m *Image565) SetPixel(x, y int, c Color) {
	if uint(x) >= uint(m.m.W) || uint(y) >= uint(m.m.H) {
		return
	}
	m.pix[y*m.m.W+x] = pack565(c)
}

// px is the page's pixels as RGB565 words (little-endian, native on ARM and
// amd64).
// TRLC-LINKS: REQ-SDS-021
func (m *Image565) px() []uint16 { return m.pix }

// Fill sets every pixel of the page to c.
// TRLC-LINKS: REQ-SDS-021
func (m *Image565) Fill(c Color) {
	p := pack565(c)
	if p == 0 { // black: a memclr, not the row copies
		clear(m.pix)
		return
	}
	fillPx(m.pix, m.m.W, m.m.H, 0, 0, m.m.W, m.m.H, p)
}

// At reads one pixel (Transparent off the page).
// TRLC-LINKS: REQ-SDS-021
func (m *Image565) At(x, y int) Color {
	if uint(x) >= uint(m.m.W) || uint(y) >= uint(m.m.H) {
		return Transparent
	}
	return c565(m.pix[y*m.m.W+x])
}

// FadeToBlack dims every pixel toward black by 1/4 (the persistence decay
// step). Pixels already black are skipped. Traces drawn onto this layer at
// full brightness thus glow and fade over ~8 frames.
// TRLC-LINKS: REQ-SDS-021
func (m *Image565) FadeToBlack() {
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
func (m *Image565) BlitBright(src Canvas) {
	sm, ok := src.(*Image565)
	if !ok {
		return
	}
	d, s := m.px(), sm.px()
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

// EncodePNG renders the canvas to a PNG — the device-screen view for the web
// /api/screen.png endpoint. Pixels are expanded straight into the image and
// compressed at BestSpeed: the default level cost ~10x the time for a
// screen grab the browser decodes to the same pixels.
// TRLC-LINKS: REQ-SDS-021
func EncodePNG(m *Image565) []byte {
	img := image.NewRGBA(image.Rect(0, 0, m.m.W, m.m.H))
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

// pixel is a memory canvas's pixel word.
type pixel interface{ ~uint16 | ~uint32 }

// fillPx fills rows [y0,y1) of columns [x0,x1) of a w×h page of pixels px,
// clipped: the sheets, panes and the F-key column fill most of the screen,
// which pixel by pixel cost most of a frame.
// TRLC-LINKS: REQ-SDS-021
func fillPx[P pixel](px []P, w, h, x0, y0, x1, y1 int, c P) {
	x0, x1 = max(x0, 0), min(x1, w)
	y0, y1 = max(y0, 0), min(y1, h)
	if x0 >= x1 || y0 >= y1 {
		return
	}
	row := px[y0*w+x0 : y0*w+x1]
	for i := range row {
		row[i] = c
	}
	for y := y0 + 1; y < y1; y++ {
		copy(px[y*w+x0:y*w+x1], row)
	}
}

// vlinePx draws column x from y0 to y1 inclusive of a page w pixels wide,
// already clipped to it: x in [0,w), 0 <= y0, y1 < h. Walking a slice of
// exactly the column's extent lets the compiler drop the per-pixel bounds
// check.
// TRLC-LINKS: REQ-SDS-021
func vlinePx[P pixel](px []P, w, x, y0, y1 int, c P) {
	if y0 > y1 {
		return
	}
	col := px[y0*w+x : y1*w+x+1]
	for o := 0; o < len(col); o += w {
		col[o] = c
	}
}

// fillSpan fills rows [y0,y1) of columns [x0,x1), clipped, straight into a
// memory canvas's pixels, and reports whether sf was one.
// TRLC-LINKS: REQ-SDS-021
func fillSpan(sf Canvas, x0, y0, x1, y1 int, c Color) bool {
	switch m := sf.(type) {
	case *Image565:
		fillPx(m.pix, m.m.W, m.m.H, x0, y0, x1, y1, pack565(c))
		return true
	}
	return false
}

// vline draws column x from y0 to y1 inclusive: straight into a memory
// canvas's pixels (a dense trace fills most of the screen height every
// frame), else pixel by pixel.
// TRLC-LINKS: REQ-SDS-021
func vline(sf Canvas, x, y0, y1 int, c Color) {
	m, ok := sf.(*Image565)
	if !ok {
		for y := y0; y <= y1; y++ {
			sf.SetPixel(x, y, c)
		}
		return
	}
	w, h := m.m.W, m.m.H
	if x < 0 || x >= w {
		return
	}
	y0, y1 = max(y0, 0), min(y1, h-1)
	if y0 > y1 {
		return
	}
	vlinePx(m.pix, w, x, y0, y1, pack565(c))
}

// atPrecision is c as a canvas of format f holds it, its low bits clear: the
// shades and dimmed colours are worked out from what the pixels can show.
// TRLC-LINKS: REQ-SDS-021
func atPrecision(f hal.PixelFormat, c Color) Color {
	if f == hal.RGB565 {
		return c &^ 0x070307
	}
	return c
}
