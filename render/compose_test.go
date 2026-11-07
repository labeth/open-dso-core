package render

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/labeth/open-dso-core/hal"
)

// The UI layer goes over the read-back plane with its straight alpha: an
// opaque pixel is the UI's, a transparent one the plane's, a half one
// between; a canvas without alpha, or of another size, is encoded alone.
// TRLC-LINKS: REQ-DSO-021
func TestEncodePNGOver(t *testing.T) {
	m := &Metrics800x480
	ui := NewImage32(m, hal.ARGB8888)
	ui.SetPixel(1, 0, RGB(0x10, 0x20, 0x30))
	ui.pix[2] = 0x80ff0000 // red at half alpha
	under := image.NewRGBA(image.Rect(0, 0, m.W, m.H))
	for i := 0; i < len(under.Pix); i += 4 {
		copy(under.Pix[i:], []byte{0, 0, 0xff, 0xff})
	}
	img, err := png.Decode(bytes.NewReader(EncodePNGOver(ui, under)))
	if err != nil {
		t.Fatal(err)
	}
	at := func(x int) color.RGBA {
		r, g, b, a := img.At(x, 0).RGBA()
		return color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), uint8(a >> 8)}
	}
	if got := at(0); got != (color.RGBA{0, 0, 0xff, 0xff}) {
		t.Errorf("a transparent pixel %v, want the plane's", got)
	}
	if got := at(1); got != (color.RGBA{0x10, 0x20, 0x30, 0xff}) {
		t.Errorf("an opaque pixel %v, want the UI's", got)
	}
	if got := at(2); got != (color.RGBA{0x80, 0, 0x7f, 0xff}) {
		t.Errorf("a half pixel %v", got)
	}
	if !bytes.Equal(EncodePNGOver(ui, image.NewRGBA(image.Rect(0, 0, 2, 2))), EncodePNG(ui)) {
		t.Error("a read-back of another size was used")
	}
	c565 := NewImage565(m)
	if !bytes.Equal(EncodePNGOver(c565, under), EncodePNG(c565)) {
		t.Error("a canvas without alpha was composited")
	}
}
