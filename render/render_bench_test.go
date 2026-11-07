// ENGMODEL-OWNER-UNIT: FU-APP-LCD

package render_test

import (
	"math"
	"testing"

	"github.com/labeth/open-dso-core/render"
	"github.com/labeth/open-dso-core/render/rendertest"
	"github.com/labeth/open-dso-core/sample"
	"github.com/labeth/open-dso-core/uimodel/wsview"
)

// The LCD draws 20 frames a second on the device's single core; these
// measure a whole frame for the plain trace and the workspace screens, so a
// change that makes one of them much dearer than the trace shows up.
// go test ./render -run - -bench render.Render

// benchRender renders f with h b.N times.
// TRLC-LINKS: REQ-SDS-021
func benchRender(b *testing.B, f *sample.Frame, h render.HUD) {
	b.Helper()
	sf := render.NewImage565(&render.Metrics800x480)
	b.ReportAllocs()
	for range b.N {
		render.Render(sf, f, h, true)
	}
}

// TRLC-LINKS: REQ-SDS-021
func BenchmarkRenderTrace(b *testing.B) { benchRender(b, rendertest.YTFrame(), rendertest.YTHUD()) }

// TRLC-LINKS: REQ-SDS-211
func BenchmarkRenderScopeRail(b *testing.B) {
	v := wsview.View{WS: wsview.Scope, Layout: rendertest.PaneLayout(0), Rail: true, Scope: &wsview.ScopePane{AdjustTag: "Adjust: Level 200mV"}}
	benchRender(b, rendertest.YTFrame(), rendertest.UIHUD(v))
}

// TRLC-LINKS: REQ-SDS-212
func BenchmarkRenderSheet(b *testing.B) {
	keys := &wsview.Keys{Title: "LEVEL", Top: 30, Items: [5]wsview.Key{{Label: "Down"}, {Label: "Up"}}}
	v := wsview.View{WS: wsview.Scope, Layout: wsview.Layout{MenuW: 116, Sheet: true, HideTraces: true, HideBottom: true}, Sheet: rendertest.Sheet(), Keys: keys}
	benchRender(b, rendertest.YTFrame(), rendertest.UIHUD(v))
}

// TRLC-LINKS: REQ-SDS-215
func BenchmarkRenderBusEye(b *testing.B) {
	const cols, rows = 384, 166
	grid, mx := rendertest.EyeGrid(cols, rows)
	e := &wsview.EyePane{
		Pane: wsview.Pane{Title: "BUS"}, Grid: grid, Cols: cols, Rows: rows, Max: mx, HasBox: true,
		Box: [4]int{110, 20, 270, 145}, SampleCol: cols / 2, ThreshRow: rows / 2, TimeLabel: "TIME",
	}
	v := wsview.View{WS: wsview.Bus, Layout: wsview.Layout{MenuW: 30, PlotTop: 136, HideTraces: true, HideBottom: true}, Rail: true, Eye: e}
	h := rendertest.UIHUD(v)
	h.TdivS = 100e-6
	benchRender(b, rendertest.BusFrame(), h)
}

// TRLC-LINKS: REQ-SDS-221
func BenchmarkDrawSaver(b *testing.B) {
	sf := render.NewImage565(&render.Metrics800x480)
	f := rendertest.YTFrame()
	h := rendertest.YTHUD()
	h.MaskMode = 1
	h.MaskPass, h.MaskFail = 1234, 5
	step := 0
	for b.Loop() {
		render.DrawSaver(sf, f, h, step)
		step++
	}
}

// TRLC-LINKS: REQ-SDS-021
func BenchmarkRenderBode(b *testing.B) {
	h := rendertest.DefaultHUD()
	h.ViewMode = 3
	const fc = 30e3
	for i := range 25 {
		f := 1e3 * math.Pow(10, 3*float64(i)/24)
		h.BodeFreq = append(h.BodeFreq, f)
		h.BodeGain = append(h.BodeGain, -10*math.Log10(1+(f/fc)*(f/fc)))
		h.BodePhase = append(h.BodePhase, -math.Atan(f/fc)*180/math.Pi)
	}
	benchRender(b, nil, h)
}

// TRLC-LINKS: REQ-SDS-070
func BenchmarkRenderFFT(b *testing.B) {
	const n = 4096
	f := rendertest.Frame(n,
		func(i int) float64 {
			th := 2 * math.Pi * float64(i) / n
			return 128 + 70*math.Sin(205*th) + 18*math.Sin(614*th) + 7*math.Sin(1024*th)
		},
		func(i int) float64 { return 128 + 50*math.Sin(2*math.Pi*410*float64(i)/n) })
	f.Snapshot.TdivS, f.SampleS = 20e-6, 50e-9
	h := rendertest.DefaultHUD()
	h.TdivS, h.SampleS = 20e-6, 50e-9
	h.ViewMode = 2
	benchRender(b, f, h)
}

// TRLC-LINKS: REQ-SDS-068
func BenchmarkRenderSpectrogram(b *testing.B) {
	sg := render.NewSpectrogram()
	for s := range 8 {
		for range 12 {
			sg.Push(rendertest.Sine(4096, 10e6+15e6*float64(s), 2e-9), 0, 0.5/2e-9)
		}
	}
	h := rendertest.DefaultHUD()
	h.ViewMode = 4
	h.Spect = sg
	benchRender(b, nil, h)
}

// TRLC-LINKS: REQ-SDS-021
func BenchmarkRenderCursorsMeasure(b *testing.B) {
	h := rendertest.YTHUD()
	h.CurOn, h.CurType, h.CurSel = true, 0, 0
	h.CurX = [2]float64{0.3, 0.7}
	h.ShowMeas = true
	benchRender(b, rendertest.YTFrame(), h)
}

// TRLC-LINKS: REQ-SDS-217
func BenchmarkDrawMeasPanel(b *testing.B) {
	f := rendertest.YTFrame()
	f.Seq = 7 // measured once, then cached: the panel's own cost
	h := rendertest.YTHUD()
	h.MeasItems = []string{"1:pkpk", "1:freq", "1:duty", "1:rise", "2:pkpk", "2:mean", "2:period", "2:ovsh"}
	sf := render.NewImage565(&render.Metrics800x480)
	for b.Loop() {
		render.DrawMeasPanel(sf, render.FullPlot, f, h)
	}
}

// TRLC-LINKS: REQ-SDS-021
func BenchmarkMathCodes(b *testing.B) {
	c1, c2 := make([]uint8, 20000), make([]uint8, 20000)
	for i := range c1 {
		c1[i], c2[i] = uint8(i*7), uint8(i*13+5)
	}
	h := rendertest.YTHUD()
	h.OffC1V, h.OffC2V = 0.1, -0.05
	for b.Loop() {
		for mode := 1; mode <= 4; mode++ {
			h.MathMode = mode
			render.MathCodes(c1, c2, h, 25)
		}
	}
}

// TRLC-LINKS: REQ-SDS-273
func BenchmarkDrawSoundPane(b *testing.B) {
	s := rendertest.SoundPane("MODULATION", []wsview.Span{{Text: "TREMOLO (AM)", Col: wsview.Amber}, {Text: "depth 30.0%  5.4 dB", Col: wsview.Text}})
	p := render.PlotOf(&render.Metrics800x480, wsview.Layout{MenuW: 30, PlotTop: 262})
	sf := render.NewImage565(&render.Metrics800x480)
	for b.Loop() {
		render.DrawSoundPane(sf, p, s)
		render.DrawSoundTrace(sf, p, s)
	}
}

// TRLC-LINKS: REQ-SDS-021
func BenchmarkDrawSuperresTrace(b *testing.B) {
	h := rendertest.SuperresHUD()
	sf := render.NewImage565(&render.Metrics800x480)
	for b.Loop() {
		render.DrawSuperresTrace(sf, render.FullPlot, h)
	}
}

// TRLC-LINKS: REQ-SDS-021
func BenchmarkSRResample(b *testing.B) {
	h := rendertest.SuperresHUD()
	for b.Loop() {
		render.SRResampleArray(h.SRMean, 4096)
	}
}

// The saver picture is the live mini scope and its lines in the box, and
// black everywhere else, whatever the screen showed before.
// TRLC-LINKS: REQ-SDS-221
func TestDrawSaver(t *testing.T) {
	f := rendertest.YTFrame()
	hud := rendertest.YTHUD()
	hud.Running = true
	for _, step := range []int{0, 137, 1450} {
		m := render.NewImage565(&render.Metrics800x480)
		m.Fill(render.ColC1) // the workspace that was shown
		render.DrawSaver(m, f, hud, step)
		x0, y0 := render.SaverPos(&render.Metrics800x480, step)
		var c1, ok int
		for y := range render.Metrics800x480.H {
			for x := range render.Metrics800x480.W {
				c := render.At565(m, x, y)
				in := x >= x0 && x < x0+render.SaverW && y >= y0 && y < y0+render.SaverBoxH
				if c != 0 && !in {
					t.Fatalf("step %d: pixel (%d,%d) lit outside the box at (%d,%d)", step, x, y, x0, y0)
				}
				switch {
				case c == render.Pack565(render.ColC1) && y < y0+render.SaverH:
					c1++ // the trace
				case c == render.Pack565(render.ColOK) && y >= y0+render.SaverH:
					ok++ // RUN
				}
			}
		}
		if c1 < render.SaverW/2 || ok == 0 {
			t.Fatalf("step %d: %d trace pixels, %d RUN pixels", step, c1, ok)
		}
	}
}
