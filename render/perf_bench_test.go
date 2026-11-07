// ENGMODEL-OWNER-UNIT: FU-APP-LCD

package render

import (
	"math"
	"testing"

	"github.com/labeth/open-dso-core/framecalc"
	"github.com/labeth/open-dso-core/streamwire"
)

// TRLC-LINKS: REQ-SDS-021
func BenchmarkDrawText(b *testing.B) {
	sf := NewImage565(m800)
	for b.Loop() {
		DrawText(sf, 10, 100, "Time/div 200us  Trigger C1 Rise 1.65V", c565(0xffff), 1)
		DrawText(sf, 10, 200, "AUTO: UART 115200 8N1", c565(0xffff), 2)
		DrawText(sf, 790, 470, "clip", c565(0xffff), 2) // off the edge: clipped
	}
}

// benchPattern fills a surface like a persistence layer: mostly black, a
// band of trace pixels at varying brightness.
// TRLC-LINKS: REQ-SDS-021
func benchPattern() *Image565 {
	m := NewImage565(m800)
	for y := range H {
		for x := range W {
			if (x*7+y*3)%11 < 3 {
				m.SetPixel(x, y, c565(uint16(x*131+y*17)))
			}
		}
	}
	return m
}

// TRLC-LINKS: REQ-SDS-021
func BenchmarkFadeToBlack(b *testing.B) {
	m := benchPattern()
	src := append([]byte(nil), m.Pix...)
	for b.Loop() {
		copy(m.Pix, src)
		m.FadeToBlack()
	}
}

// TRLC-LINKS: REQ-SDS-021
func BenchmarkBlitBright(b *testing.B) {
	src := benchPattern()
	d := NewImage565(m800)
	for b.Loop() {
		d.BlitBright(src)
	}
}

// TRLC-LINKS: REQ-SDS-021
func BenchmarkEncodePNG(b *testing.B) {
	m := benchPattern()
	for b.Loop() {
		EncodePNG(m)
	}
}

// TRLC-LINKS: REQ-SDS-021
func BenchmarkDrawTraceSparse(b *testing.B) {
	sig := make([]uint8, 1000)
	for i := range sig {
		sig[i] = uint8(128 + 90*((i/37)%2*2-1) + i%13)
	}
	sf := NewImage565(m800)
	for b.Loop() {
		drawTrace(sf, sig, 400, 500, false, c565(0xffe0), 0.5)
		drawTrace(sf, sig, 300, 500, true, c565(0x07ff), 0.5)
	}
}

// TRLC-LINKS: REQ-SDS-021
func BenchmarkFmtLabels(b *testing.B) {
	for b.Loop() {
		_ = vdivLabel(0.2, 10)
		_ = vdivLabel(0.05, 1)
		_ = fmtTdiv(-999.9e-9)
		_ = fmtFreq(1234567)
	}
}

// TRLC-LINKS: REQ-SDS-021
func BenchmarkDrawLoading(b *testing.B) {
	sf := NewImage565(m800)
	for b.Loop() {
		DrawLoading(sf, "Shifting image", 1234567, 2000000)
	}
}

// TRLC-LINKS: REQ-SDS-018
func BenchmarkStreamStatus(b *testing.B) {
	v := streamwire.View{Label: "UART 115200", State: streamwire.Streaming, Units: 123456, UnitsPerSecond: 11520, Errors: 2, Lost: 1, Mode: "hex"}
	for b.Loop() {
		_ = streamStatus(v)
	}
}

// TRLC-LINKS: REQ-SDS-214
func BenchmarkDrawMatchEditor(b *testing.B) {
	toks := make([]string, 32)
	for i := range toks {
		toks[i] = [...]string{"48", "65", "6C", "?F", "??", "ERR", "20"}[i%7]
	}
	m := MatchView{
		Open: true, Set: "Hex", Proto: "UART", Toks: toks, Cur: 30,
		Keys: [][]string{{"0", "1", "2", "3", "4", "5"}, {"6", "7", "8", "9", "A", "B"}, {"C", "D", "E", "F", "?", "??"}}, Row: 1, Col: 4,
	}
	sf := NewImage565(m800)
	for b.Loop() {
		drawMatchEditor(sf, m)
	}
}

// TRLC-LINKS: REQ-SDS-068
func BenchmarkSpectrogramPush(b *testing.B) {
	sg := NewSpectrogram()
	x := sgSine(4096, 25e6, 2e-9)
	for b.Loop() {
		sg.Push(x, 0, 0.5/2e-9)
	}
}

// The waterfall in a plot narrower than the screen (beside the rail): its
// columns are scaled.
// TRLC-LINKS: REQ-SDS-068
func BenchmarkSpectrogramPaneBlit(b *testing.B) {
	sg := NewSpectrogram()
	for r := range 40 {
		sg.Push(sgSine(4096, 10e6+1e5*float64(r), 2e-9), 0, 0.5/2e-9)
	}
	m := NewImage565(m800)
	p := plotArea{w: W - 70, y0: 40, h: H - 40, top: 48, bot: H - 8, m: m800, cpd: defaultCodesPerDiv, span: 8 * defaultCodesPerDiv}
	for b.Loop() {
		drawSpectrogramMark(m, p, sg, true, nil)
	}
}

// TRLC-LINKS: REQ-SDS-070
func BenchmarkDrawSpectrum(b *testing.B) {
	const n = 4096
	x := make([]float64, n)
	for i := range x {
		x[i] = math.Round(128 + 90*math.Sin(2*math.Pi*205*float64(i)/n) + 5*math.Sin(2*math.Pi*1500*float64(i)/n))
	}
	mags, peak, _ := framecalc.Mags(x)
	sf := NewImage565(m800)
	for b.Loop() {
		drawSpectrum(sf, fullPlot, mags, peak, colC1, 10e6, 1, len(mags)-1)
	}
}

// TRLC-LINKS: REQ-SDS-021
func BenchmarkDrawEnvelope(b *testing.B) {
	const cols = 800
	mn, mx := make([]uint8, cols), make([]uint8, cols)
	for i := range mn {
		a := 40 + 25*math.Sin(2*math.Pi*3*float64(i)/cols)
		mn[i], mx[i] = uint8(128-a), uint8(128+a)
	}
	sf := NewImage565(m800)
	for b.Loop() {
		drawEnvelope(sf, fullPlot, mn, mx, cols, colC1, 1)
		drawEnvelope(sf, fullPlot, mn, mx, cols, colC2, 1)
	}
}

// A live fast-signal frame: 7000 samples over the full plot, two channels,
// each frame new (no column cache hit).
// TRLC-LINKS: REQ-SDS-021
func BenchmarkDrawDenseTrace(b *testing.B) {
	sig := make([]uint8, 7000)
	for i := range sig {
		sig[i] = uint8(128 + 90*((i/9)%2*2-1) + i%7)
	}
	sf := NewImage565(m800)
	for b.Loop() {
		drawDenseTrace(sf, fullPlot, traceID{0, 0, 0, false}, sig, nil, 0, len(sig), c565(0xffe0), 1)
		drawDenseTrace(sf, fullPlot, traceID{0, 1, 0, false}, sig, nil, 0, len(sig), c565(0x07ff), 0.5)
	}
}

// Render clears the back buffer every frame.
// TRLC-LINKS: REQ-SDS-021
func BenchmarkFillBlack(b *testing.B) {
	m := NewImage565(m800)
	for b.Loop() {
		m.Fill(0)
	}
}
