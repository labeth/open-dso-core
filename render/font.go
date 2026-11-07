// ENGMODEL-OWNER-UNIT: FU-APP-LCD

package render

import "github.com/labeth/open-dso-core/design"

// glyphs is the shared display font (design.Glyphs).
var glyphs = design.Glyphs

// TextWidth is len(runes) × 6 × scale.
// TRLC-LINKS: REQ-SDS-021
func TextWidth(s string, scale int) int {
	n := 0
	for range s {
		n++
	}
	return n * 6 * scale
}

// DrawText renders each set bit as a scale×scale block; advance 6·scale.
// Bit 7 is a descender row: g, j, p, q and y drop below the baseline, so g
// does not read as 9 ("Sp9m", "phase de9").
// TRLC-LINKS: REQ-SDS-021
func DrawText(sf Canvas, x, y int, s string, c Color, scale int) {
	if m, ok := sf.(*Image565); ok {
		textPx(sf, m.px(), W, H, x, y, s, pack565(c), c, scale)
		return
	}
	textPx[uint16](sf, nil, 0, 0, x, y, s, 0, c, scale)
}

// textPx is DrawText on a w×h page of pixels px, n being c in its format;
// glyphs off the page, or every glyph without pixels, go through sf.
// TRLC-LINKS: REQ-SDS-021
func textPx[P pixel](sf Canvas, px []P, w, h, x, y int, s string, n P, c Color, scale int) {
	mem := px != nil
	for _, r := range s {
		var g [5]byte
		ok := false
		if r >= 0 && r < 128 {
			g, ok = asciiGlyphs[r], asciiHave[r]
		} else {
			g, ok = glyphs[r]
		}
		switch {
		case !ok:
		case mem && scale == 1 && x >= 0 && y >= 0 && x+5 <= w && y+8 <= h:
			// Most of the HUD: one pixel a set bit, a column at a time.
			glyphPx(px, w, y*w+x, &g, n)
		case mem && x >= 0 && y >= 0 && x+5*scale <= w && y+8*scale <= h:
			// The glyph's box is on the screen: its pixels written
			// directly, as SetPixel would write them.
			glyphPix(px, w, x, y, &g, scale, n)
		default:
			glyphAt(sf, x, y, &g, scale, c)
		}
		x += 6 * scale
	}
}

// glyphPx writes a glyph at scale 1 into a page w pixels wide from offset o
// (its top-left pixel): one pixel a set bit, a column at a time.
// TRLC-LINKS: REQ-SDS-021
func glyphPx[P pixel](px []P, w, o int, g *[5]byte, c P) {
	for col := range 5 {
		p := o + col
		for bits := g[col]; bits != 0; bits >>= 1 {
			if bits&1 != 0 {
				px[p] = c
			}
			p += w
		}
	}
}

// glyphPix writes a scaled glyph whose box is on a page w pixels wide
// straight into its pixels, as SetPixel would write them.
// TRLC-LINKS: REQ-SDS-021
func glyphPix[P pixel](px []P, w, x, y int, g *[5]byte, scale int, c P) {
	for col := range 5 {
		bits := g[col]
		for row := range 8 {
			if bits&(1<<uint(row)) == 0 {
				continue
			}
			for dy := range scale {
				o := (y+row*scale+dy)*w + x + col*scale
				for dx := range scale {
					px[o+dx] = c
				}
			}
		}
	}
}

// glyphAt draws a glyph pixel by pixel through the surface, clipped by it.
// TRLC-LINKS: REQ-SDS-021
func glyphAt(sf Canvas, x, y int, g *[5]byte, scale int, c Color) {
	for col := range 5 {
		bits := g[col]
		for row := range 8 { // row 7: descenders (g j p q y)
			if bits&(1<<uint(row)) == 0 {
				continue
			}
			for dy := range scale {
				for dx := range scale {
					sf.SetPixel(x+col*scale+dx, y+row*scale+dy, c)
				}
			}
		}
	}
}

// asciiGlyphs and asciiHave are the glyph table's ASCII part as arrays: a
// lookup a character without the map.
var asciiGlyphs, asciiHave = func() (g [128][5]byte, have [128]bool) {
	for r, v := range glyphs {
		if r >= 0 && r < 128 {
			g[r], have[r] = v, true
		}
	}
	return g, have
}()

// DrawTextRight right-aligns: draws ending at xr.
// TRLC-LINKS: REQ-SDS-021
func DrawTextRight(sf Canvas, xr, y int, s string, c Color, scale int) {
	DrawText(sf, xr-TextWidth(s, scale), y, s, c, scale)
}
