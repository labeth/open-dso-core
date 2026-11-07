// ENGMODEL-OWNER-UNIT: FU-DSO-RENDER

package render

import (
	"bytes"
	"image"
	"image/png"
)

// EncodePNGOver is the PNG of a UI canvas drawn over a picture of the
// whole screen (a hardware renderer's plane read back): the canvas's alpha
// straight, as a framebuffer mixed over the plane. The result is opaque.
// A canvas without alpha covers the picture; one of another size than the
// picture is encoded alone.
// TRLC-LINKS: REQ-DSO-021
func EncodePNGOver(ui Canvas, under *image.RGBA) []byte {
	m, ok := ui.(*Image32)
	sm := ui.Metrics()
	if !ok || under == nil || under.Rect.Dx() != sm.W || under.Rect.Dy() != sm.H {
		return EncodePNG(ui)
	}
	img := image.NewRGBA(image.Rect(0, 0, sm.W, sm.H))
	d := img.Pix
	for y := range sm.H {
		src := under.Pix[y*under.Stride : y*under.Stride+4*sm.W]
		for x := range sm.W {
			c := m.pix[y*sm.W+x] | m.opaque
			o, so := (y*sm.W+x)*4, 4*x
			a := c >> 24
			for i, v := range [3]uint32{c >> 16 & 0xff, c >> 8 & 0xff, c & 0xff} {
				d[o+i] = uint8((v*a + uint32(src[so+i])*(255-a) + 127) / 255) //nolint:gosec // a blend of two bytes is a byte
			}
			d[o+3] = 255
		}
	}
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := enc.Encode(&buf, img); err != nil {
		return nil // not reached: a fixed-size image into a memory buffer
	}
	return buf.Bytes()
}
