// ENGMODEL-OWNER-UNIT: FU-APP-LCD

package rendertest

import (
	"fmt"
	"math"

	"github.com/labeth/open-dso-core/render"
	"github.com/labeth/open-dso-core/sample"
	"github.com/labeth/open-dso-core/streamwire"
	"github.com/labeth/open-dso-core/uimodel/wsview"
)

// Every fixture is fully synthetic and deterministic: fixed waveform math (no
// randomness, no clock), and Frame.Seq left at 0 — a hand-built frame bypasses
// the time-based measurement cache in the renderer, so the HUD/MEASURE readouts are
// recomputed from the fixture on every render instead of possibly serving
// another test's cached Result within the measRefresh window. On-screen text
// that could be environment-dependent (the device URL) is pinned in the HUD.

// Clamp converts a generator value to an ADC code.
// TRLC-LINKS: REQ-SDS-021
func Clamp(v float64) uint8 {
	if v < 0 {
		v = 0
	}
	if v > 255 {
		v = 255
	}
	return uint8(v)
}

// Frame builds a render-ready dual-channel frame from two per-sample
// generators, windowed across the whole record and centred on its middle.
// TRLC-LINKS: REQ-SDS-021
func Frame(n int, g1, g2 func(i int) float64) *sample.Frame {
	f := &sample.Frame{
		WinPoints: n,
		TriggerAt: float64(n) / 2,
		SourcePtp: 144,
		SampleS:   800e-9,
		Snapshot:  sample.Settings{TdivS: 500e-6},
		Status:    sample.Status{Triggered: true},
		Traces:    []sample.Trace{{Codes8: make([]uint8, n), Valid: n}, {Ch: 1, Codes8: make([]uint8, n), Valid: n}},
	}
	for i := range n {
		f.Codes8(0)[i] = Clamp(g1(i))
		f.Codes8(1)[i] = Clamp(g2(i))
	}
	return f
}

// YTFrame is the bread-and-butter Y-T fixture: a square wave on C1 with
// a rising edge exactly at EdgeX (=n/2), and a sine on C2.
// TRLC-LINKS: REQ-SDS-021
func YTFrame() *sample.Frame {
	const n = 2048
	return Frame(n,
		func(i int) float64 { // 512-sample period square, rising edge at 1024
			if (i/256)%2 == 0 {
				return 200
			}
			return 56
		},
		func(i int) float64 { return 128 + 60*math.Sin(2*math.Pi*float64(i-n/2)/512) })
}

// YTHUD is the matching HUD: both channels shown, distinct V/div, real
// offsets (ground arrows off-centre), trigger level + position markers, and
// the fixed device URL on the top bar.
// TRLC-LINKS: REQ-SDS-021
func YTHUD() render.HUD {
	h := DefaultHUD()
	h.Ch[0].Show, h.Ch[1].Show = true, true
	h.Ch[1].VdivV = 0.5
	h.TrigLvlDiv = 1.2
	h.TrigLvlV = 0.2 // 1.2 div at 1 V/div less the 1 V offset
	h.TrigPosFrac = 0.5
	h.Ch[0].OffV, h.Ch[1].OffV = 1.0, -0.25
	h.Trigd = true
	h.URL = "http://192.168.1.42:8080"
	return h
}

// sceneYTDual pins the core dual-trace Y-T screen: graticule, both
// traces, trigger level line + position pointer, per-channel ground arrows,
// top-bar readouts (V/div, t/div, trigger state, URL) and the bottom
// Vpp/frequency line.
// TRLC-LINKS: REQ-SDS-021
func sceneYTDual() Scene {
	return Scene{Name: "yt_dual", Frame: YTFrame(), HUD: YTHUD()}
}

// UARTWave synthesizes an 8N1 LSB-first UART record (idle-high, codes
// 40/210, spb samples per bit), padded with trailing idle to length n — the
// same shape the decode package's own tests use.
// TRLC-LINKS: REQ-SDS-021
func UARTWave(bytes []int, spb, n int) []uint8 {
	w := make([]uint8, 0, n)
	push := func(bit, k int) {
		v := uint8(40)
		if bit == 1 {
			v = 210
		}
		for range k {
			w = append(w, v)
		}
	}
	push(1, spb*8) // lead idle
	for _, b := range bytes {
		push(0, spb) // start
		for c := range 8 {
			push((b>>c)&1, spb) // LSB first
		}
		push(1, spb) // stop
		push(1, spb) // inter-byte idle
	}
	for len(w) < n { // trail idle
		w = append(w, 210)
	}
	return w[:n]
}

// sceneYTDecodeUART pins the Y-T screen with the decode strip active:
// drawDecode runs the real internal/decode UART decoder on the fixture wave
// ("OK!" at an explicit baud), so the golden pins the span bars, byte text and
// the "UART  3 bytes" label produced by real decode spans.
// TRLC-LINKS: REQ-SDS-021, REQ-SDS-018
func sceneYTDecodeUART() Scene {
	const n, spb = 1024, 16
	const sampleS = 1e-6
	f := &sample.Frame{
		WinPoints: n,
		TriggerAt: float64(n) / 2,
		SourcePtp: 170,
		SampleS:   sampleS,
		Snapshot:  sample.Settings{TdivS: 100e-6},
		Status:    sample.Status{Triggered: true},
		Traces:    []sample.Trace{{Codes8: UARTWave([]int{0x4F, 0x4B, 0x21}, spb, n), Valid: n}, {Ch: 1, Codes8: make([]uint8, n), Valid: n}},
	}
	for i := range f.Codes8(1) {
		f.Codes8(1)[i] = 210 // idle-high second channel
	}
	h := DefaultHUD()
	h.TdivS, h.SampleS = 100e-6, sampleS
	// This fixture places the trigger at the center of the display.
	h.TrigPosFrac = 0.5
	h.DecProto = 2                       // UART
	h.DecBaud = int(1 / (spb * sampleS)) // 62500: explicit, no auto-inference
	h.DecChA, h.DecFormat = 0, 0         // C1, hex
	return Scene{Name: "yt_decode_uart", Frame: f, HUD: h}
}

// sceneFFTPeaks pins the FFT view with peak markers: three tones placed
// on exact FFT bins (205/614/1024 cycles over the 4096-sample record) so the
// spectrum has clean local maxima above the -40 dBc mark floor, plus the
// per-channel peak-frequency labels and the band label.
// TRLC-LINKS: REQ-SDS-021, REQ-SDS-070
func sceneFFTPeaks() Scene {
	const n = 4096
	const sampleS = 50e-9 // Nyquist 10 MHz: tones land at ~1/3/5 MHz
	f := Frame(n,
		func(i int) float64 {
			th := 2 * math.Pi * float64(i) / n
			return 128 + 70*math.Sin(205*th) + 18*math.Sin(614*th) + 7*math.Sin(1024*th)
		},
		func(i int) float64 { return 128 + 50*math.Sin(2*math.Pi*410*float64(i)/n) })
	f.Snapshot.TdivS, f.SampleS = 20e-6, sampleS
	h := DefaultHUD()
	h.TdivS, h.SampleS = 20e-6, sampleS
	h.ViewMode = 2
	return Scene{Name: "fft_peaks", Frame: f, HUD: h}
}

// sceneXY pins the X-Y (Lissajous) view: C1 and C2 at a 1:2 frequency
// ratio trace a figure-eight, plus the "X:C1  Y:C2" hint and the X-Y top-bar
// label.
// TRLC-LINKS: REQ-SDS-021
func sceneXY() Scene {
	const n = 2048
	f := Frame(n,
		func(i int) float64 { return 128 + 90*math.Sin(2*math.Pi*float64(i)/1024) },
		func(i int) float64 { return 128 + 90*math.Sin(4*math.Pi*float64(i)/1024) })
	h := DefaultHUD()
	h.ViewMode = 1
	return Scene{Name: "xy_lissajous", Frame: f, HUD: h}
}

// sceneMeasurePanel pins the MEASURE overlay with BOTH channel boxes: the
// square/sine fixture gives every voltage row plus the timing rows (freq,
// period, duty) deterministic values. Seq=0 keeps measFor cache-free.
// TRLC-LINKS: REQ-SDS-021
func sceneMeasurePanel() Scene {
	h := YTHUD()
	h.ShowMeas = true
	return Scene{Name: "measure_panel", Frame: YTFrame(), HUD: h}
}

// sceneEnvelope pins the envelope-band rendering (IsEnv + Env arrays):
// an amplitude-modulated C1 band around centre and a thin C2 band near the
// bottom rail, so both fills and their shapes are pinned.
// TRLC-LINKS: REQ-SDS-021
func sceneEnvelope() Scene {
	const cols = 800
	f := &sample.Frame{
		WinPoints: cols,
		TriggerAt: -1,
		SourcePtp: 130,
		SampleS:   1e-3,
		Flags:     sample.Envelope,
		Snapshot:  sample.Settings{TdivS: 100e-3},
		Traces:    []sample.Trace{{Codes8: make([]uint8, cols), Valid: cols, Min8: make([]uint8, cols)[:cols], Max8: make([]uint8, cols)[:cols]}, {Ch: 1, Codes8: make([]uint8, cols), Valid: cols, Min8: make([]uint8, cols)[:cols], Max8: make([]uint8, cols)[:cols]}},
	}
	for i := range cols {
		amp := 40 + 25*math.Sin(2*math.Pi*3*float64(i)/cols) // C1: AM band
		f.Min8(0)[i] = Clamp(128 - amp)
		f.Max8(0)[i] = Clamp(128 + amp)
		a2 := 6 + 4*math.Sin(2*math.Pi*5*float64(i)/cols) // C2: thin low band
		f.Min8(1)[i] = Clamp(40 - a2)
		f.Max8(1)[i] = Clamp(40 + a2)
		f.Codes8(0)[i], f.Codes8(1)[i] = 128, 128 // benign codes: no CLIP flag, flat readouts
	}
	h := DefaultHUD()
	h.TdivS, h.SampleS = 100e-3, 1e-3
	return Scene{Name: "envelope_band", Frame: f, HUD: h}
}

// sceneBode pins the Bode/FRA view: a single-pole low-pass response
// (fc = 30 kHz) sampled at 25 log-spaced points over 1 kHz..1 MHz — decade
// gridlines, dB/phase axes, both traces with point dots, and the point count.
// The Bode view renders purely from the HUD arrays; no frame is needed.
// TRLC-LINKS: REQ-SDS-021
func sceneBode() Scene {
	h := DefaultHUD()
	h.ViewMode = 3
	const fc = 30e3
	for i := range 25 {
		f := 1e3 * math.Pow(10, 3*float64(i)/24) // 1 kHz .. 1 MHz
		h.BodeFreq = append(h.BodeFreq, f)
		h.BodeGain = append(h.BodeGain, -10*math.Log10(1+(f/fc)*(f/fc)))
		h.BodePhase = append(h.BodePhase, -math.Atan(f/fc)*180/math.Pi)
	}
	return Scene{Name: "bode", Frame: nil, HUD: h}
}

// sceneSpectrogram pins the spectrogram view: a deterministic frequency
// staircase (8 tones × 24 rows, oldest/lowest at the bottom) pushed through
// the real Push path, then blitted with the frequency axis, dB key and
// new/old arrows. The waterfall content depends only on the pushed frames;
// the untouched region below the staircase pins the scroll behaviour too.
// TRLC-LINKS: REQ-SDS-021, REQ-SDS-068
func sceneSpectrogram() Scene {
	const n = 4096
	const dt = 2e-9 // Nyquist 250 MHz: 4096 samples are under MaxFFT, so the FFT view's spectrum and the waterfall span it all
	sg := render.NewSpectrogram()
	for s := range 8 {
		f := 10e6 + 15e6*float64(s) // 10..115 MHz staircase
		// two rows a 4096-sample record: 24 a step
		for range 12 {
			sg.Push(Sine(n, f, dt), 0, 0.5/dt)
		}
	}
	h := DefaultHUD()
	h.ViewMode = 4
	h.Spect = sg
	return Scene{Name: "spectrogram", Frame: nil, HUD: h}
}

// sceneCursorsTime pins the time-cursor overlay: two dashed vertical
// cursors (active A highlighted) over the Y-T trace plus the dt / 1/dt
// readout box.
// TRLC-LINKS: REQ-SDS-021
func sceneCursorsTime() Scene {
	h := YTHUD()
	h.CurOn, h.CurType, h.CurSel = true, 0, 0
	h.CurX = [2]float64{0.3, 0.7}
	return Scene{Name: "cursors_time", Frame: YTFrame(), HUD: h}
}

// sceneCursorsVolts pins the volts-cursor variant: two dashed horizontal
// cursors (active B highlighted) and the per-channel ΔV readout.
// TRLC-LINKS: REQ-SDS-021
func sceneCursorsVolts() Scene {
	h := YTHUD()
	h.CurOn, h.CurType, h.CurSel = true, 1, 1
	h.CurY = [2]float64{0.25, 0.7}
	return Scene{Name: "cursors_volts", Frame: YTFrame(), HUD: h}
}

// Golden coverage for the task-workspace UI (ADR-TASK-WORKSPACES): the rail,
// each workspace's pane, a settings sheet, the byte picker, the match editor
// and the eye. The views are hand-built snapshots of what the panel
// produces (a board's panel tests drive the real controller through the same
// renderer).

// BusFrame is a UART capture: two messages repeating on C1, C2 idle.
// TRLC-LINKS: REQ-SDS-214
func BusFrame() *sample.Frame {
	const n, spb = 4096, 8
	var bytes []int
	for range 6 {
		for _, b := range []byte("Hi\x55\xAA") {
			bytes = append(bytes, int(b))
		}
	}
	f := &sample.Frame{
		WinPoints: n,
		TriggerAt: n / 2,
		SourcePtp: 170,
		SampleS:   1e-6 / spb * 4,
		Snapshot:  sample.Settings{TdivS: 100e-6},
		Status:    sample.Status{Triggered: true},
		Traces:    []sample.Trace{{Codes8: UARTWave(bytes, spb, n), Valid: n}, {Ch: 1, Codes8: make([]uint8, n), Valid: n}},
	}
	for i := range f.Codes8(1) {
		f.Codes8(1)[i] = 210
	}
	return f
}

// UIHUD is the HUD the workspace goldens share.
// TRLC-LINKS: REQ-SDS-211
func UIHUD(v wsview.View) render.HUD {
	h := YTHUD()
	h.UI = v
	h.Notice = v.Notice
	return h
}

// PaneLayout is a workspace layout: the rail and a pane above the plot.
// TRLC-LINKS: REQ-SDS-211
func PaneLayout(top int) wsview.Layout { return wsview.Layout{MenuW: 30, PlotTop: top} }

// sceneScopeRail pins SCOPE: the full-height trace beside the
// pictogram rail (SCOPE active, inverted amber), the label after F1, the
// workspace's name in the top bar and the Adjust tag.
// TRLC-LINKS: REQ-SDS-211, REQ-SDS-213
func sceneScopeRail() Scene {
	v := wsview.View{
		WS: wsview.Scope, Layout: PaneLayout(0), Rail: true,
		RailNote: &wsview.Note{WS: wsview.Scope, Head: "F1 SCOPE", Text: "again: Y-T / X-Y"},
		Scope:    &wsview.ScopePane{AdjustTag: "Adjust: Level 200mV", Hot: true},
	}
	return Scene{Name: "ws_scope", Frame: YTFrame(), HUD: UIHUD(v), Prefill: true}
}

// sceneBusPackets pins BUS: the packet list as a hex dump with its
// string box, the selected packet, the match column, the tool line, and
// the strip's packet box under the trace.
// TRLC-LINKS: REQ-SDS-214
func sceneBusPackets() Scene {
	line := func(num int, sel bool) wsview.BusLine {
		return wsview.BusLine{
			Num: num, Sel: sel, Hex: []string{"48", "69", "55", "AA"}, Err: make([]bool, 4), Str: "Hi..",
			Bytes: 4, Match: "55 AA",
		}
	}
	b := &wsview.BusPane{Pane: wsview.Pane{
		Title: "BUS", Caption: "UART 31250 8N1 on C1  -  6 packets", Right: "match 55 AA: 6 packets",
		Tool:  []wsview.Seg{{Key: "Adjust", Job: "packet"}, {Key: "push", Job: "pick bytes"}, {Key: "Position", Job: "trigger position"}, {Key: "push", Job: "next match"}, {Key: "Zoom", Job: "on packet"}},
		Again: "F2 again: Eye diagram",
	}}
	for i := 1; i <= 6; i++ {
		b.Lines = append(b.Lines, line(i, i == 2))
	}
	b.Lines[3].Err[1], b.Lines[3].Hex[1] = true, "ER"
	b.Marks = []wsview.PacketMark{{I0: 800, I1: 1150, Sel: true, Match: true}, {I0: 1700, I1: 2050, Match: true}}
	b.SelNum = 2
	v := wsview.View{WS: wsview.Bus, Layout: PaneLayout(136), Rail: true, Bus: b}
	h := UIHUD(v)
	h.TdivS, h.SampleS, h.DecProto, h.DecBaud = 100e-6, 0.5e-6, 2, 250000
	return Scene{Name: "ws_bus_packets", Frame: BusFrame(), HUD: h, Prefill: true}
}

// sceneBusPicker pins the byte picker: the pane framed in amber, the
// first byte filled, the next three underlined, a bar at a message start,
// and the F-key column of choices.
// TRLC-LINKS: REQ-SDS-214
func sceneBusPicker() Scene {
	var toks []wsview.PickTok
	for range 3 {
		for i, b := range []string{"48", "69", "55", "AA"} {
			toks = append(toks, wsview.PickTok{Text: b, Start: i == 0})
		}
	}
	toks[6].Text = "ERR"
	pk := &wsview.Picker{
		Caption: "to trigger on  (12 decoded on screen)", Toks: toks, Cur: 4,
		Lines: []wsview.Span{
			{Text: "| = idle gap (a message starts).  Underlined: the bytes the F-keys would add.", Col: wsview.Dim},
			{Text: "Turn Adjust or Position: the first byte.  Push Adjust (or F1): trigger on it; F2-F4: on 2-4 bytes.", Col: wsview.Text},
			{Text: "F5 or Menu On/Off: back, the trigger as it was.", Col: wsview.Dim},
		},
		Now: "Now: any byte",
	}
	keys := &wsview.Keys{Title: "PICK BYTES", Top: 12, Items: [5]wsview.Key{
		{Label: "Trig on 1", Value: "48"},
		{Label: "Trig on 2", Value: "48 69"},
		{Label: "Trig on 3", Value: "48 69 ERR"},
		{Label: "Trig on 4", Value: "48 69 ERR AA"},
		{Label: "Cancel", Value: "keep the match"},
	}}
	v := wsview.View{WS: wsview.Bus, Layout: wsview.Layout{MenuW: 116, PlotTop: 136}, Picker: pk, Keys: keys}
	h := UIHUD(v)
	h.TdivS, h.SampleS = 100e-6, 0.5e-6
	return Scene{Name: "ws_bus_picker", Frame: BusFrame(), HUD: h, Prefill: true}
}

// EyeGrid is a synthetic eye: the two levels as rails and the
// crossings at 0.5 and 1.5 UI as X-shaped edges with a little jitter.
// TRLC-LINKS: REQ-SDS-215
func EyeGrid(cols, rows int) ([]uint32, uint32) {
	g := make([]uint32, cols*rows)
	add := func(r, c int, n uint32) {
		if r >= 0 && r < rows && c >= 0 && c < cols {
			g[r*cols+c] += n
		}
	}
	hi, lo := rows/10, rows-rows/10-1
	for c := range cols {
		for d := -1; d <= 1; d++ {
			add(hi+d, c, uint32(60-20*d*d))
			add(lo+d, c, uint32(60-20*d*d))
		}
	}
	for _, x0 := range []int{cols / 4, 3 * cols / 4} {
		for j := -6; j <= 6; j++ { // jitter: the edges spread over a few columns
			w := uint32(13 - 2*int(math.Abs(float64(j))))
			for r := hi; r <= lo; r++ {
				dx := (r - (hi+lo)/2) * cols / 16 / (lo - hi)
				add(r, x0+j+dx, w)
				add(r, x0+j-dx, w)
			}
		}
	}
	var mx uint32
	for _, v := range g {
		mx = max(mx, v)
	}
	return g, mx
}

// sceneBusEye pins the eye diagram: the density in four shades, the UI
// grid and labels, the opening box, the two cursors, and the figures beside
// a small time trace.
// TRLC-LINKS: REQ-SDS-215
func sceneBusEye() Scene {
	const cols, rows = 384, 166
	grid, mx := EyeGrid(cols, rows)
	e := &wsview.EyePane{
		Pane: wsview.Pane{
			Title: "BUS", Caption: "EYE  UART 31250 8N1 on C1", Right: "212 UI folded from 9 captures",
			Tool:  []wsview.Seg{{Key: "Adjust", Job: "threshold"}, {Key: "push", Job: "clear eye"}, {Key: "Position", Job: "sample point"}, {Key: "push", Job: "trigger position"}, {Key: "Zoom", Job: "finer samples"}},
			Again: "F2 again: Packets",
		},
		Figures: [3][4][2]string{
			{{"BIT RATE", "31.25 kbit/s"}, {"UI", "32us"}, {"CROSSING", "50 %"}, {"FOLDED", "212 UI"}},
			{{"EYE HEIGHT", "6.4V"}, {"EYE WIDTH", "0.84 UI"}, {"RISE", "120ns"}, {"FALL", "118ns"}},
			{{"TIE RMS", "410ns"}, {"TIE PK-PK", "2.1us"}, {"SAMPLES/UI", "64.0"}, {"dt", "500ns"}},
		},
		Cursor: "SAMPLE PT +0.00 UI: opening 6.4V     THRESHOLD 0V: opening 0.84 UI",
		Grid:   grid, Cols: cols, Rows: rows, Max: mx,
		Box: [4]int{cols/4 + 14, rows/10 + 3, 3*cols/4 - 14, rows - rows/10 - 4}, HasBox: true,
		SampleCol: cols / 2, ThreshRow: rows / 2, TimeLabel: "TIME",
	}
	v := wsview.View{WS: wsview.Bus, Layout: wsview.Layout{MenuW: 30, PlotTop: 136, HideTraces: true, HideBottom: true}, Rail: true, Eye: e}
	h := UIHUD(v)
	h.TdivS, h.SampleS = 100e-6, 0.5e-6
	return Scene{Name: "ws_bus_eye", Frame: BusFrame(), HUD: h, Prefill: true}
}

// sceneBusStream pins BUS's Stream view: the stream's state, what stops
// it and the controls in the pane, the transcript filling the plot.
// TRLC-LINKS: REQ-SDS-018, REQ-SDS-228
func sceneBusStream() Scene {
	b := &wsview.BusPane{
		Pane: wsview.Pane{
			Title: "BUS", Caption: "STREAM  UART 115200 8N1 on C1", Right: "stops on a decode error",
			Tool: []wsview.Seg{
				{Key: "Adjust", Job: "stops on error"},
				{Key: "push", Job: "stop"},
				{Key: "Position", Job: "trigger position"},
				{Key: "push", Job: "transcript"},
			},
			Again: "F2 again: Packets",
		}, Off: true,
		Msg: []wsview.Span{
			{Text: "STREAMING", Col: wsview.Text},
			{Text: "37961 units  8.78k/s  errors 0  lost 0", Col: wsview.Text},
			{Text: "Run/Stop or push Adjust: stop - the record goes on screen", Col: wsview.Dim},
			{Text: "Adjust: what stops it (an error, the Stream value, by hand)", Col: wsview.Dim},
		},
	}
	v := wsview.View{WS: wsview.Bus, Layout: PaneLayout(136), Rail: true, Bus: b}
	h := UIHUD(v)
	h.TdivS, h.SampleS = 100e-6, 0.5e-6
	h.StreamOn, h.StreamList = true, true
	h.Stream = streamwire.View{State: streamwire.Streaming, Label: "UART 115200 8N1 on C1", Units: 37961, UnitsPerSecond: 8778, Lines: 2373, TriggerLine: -1}
	pg := streamwire.Page{Top: 2340, Total: 2373, TriggerLine: -1, Header: "         t ms        hex                                              text"}
	for i := range 30 {
		pg.Lines = append(pg.Lines, fmt.Sprintf("%13.4f        48 69 20 55 AA 0F F0 0A 48 69 20 55 AA 0F F0 0A  Hi U....Hi U....", 4270.3+1.8227*float64(i)))
	}
	h.StreamPage = pg
	return Scene{Name: "ws_bus_stream", Frame: BusFrame(), HUD: h, Prefill: true}
}

// sceneSpectrumPeaks pins SPECTRUM: the FFT under its pane with the
// peak table, readout, marker line and its label in the plot.
// TRLC-LINKS: REQ-SDS-216
func sceneSpectrumPeaks() Scene {
	const n = 4096
	f := Frame(n,
		func(i int) float64 {
			th := 2 * math.Pi * float64(i) / n
			return 128 + 70*math.Sin(205*th) + 18*math.Sin(615*th) + 7*math.Sin(1025*th)
		},
		func(i int) float64 { return 128 + 50*math.Sin(2*math.Pi*float64(i)*410/n) })
	f.SampleS = 50e-9
	s := &wsview.SpecPane{
		Pane: wsview.Pane{
			Title: "SPECTRUM", Caption: "FFT C1  span 0-10MHz  RBW 4.88kHz  floor -71dBV", Right: "S/DIV: span",
			Tool:  []wsview.Seg{{Key: "Adjust", Job: "peak"}, {Key: "push", Job: "delta"}, {Key: "Position", Job: "trigger position"}, {Key: "push", Job: "fine marker"}},
			Again: "F3 again: Spectrogram",
		},
		Head: [4]string{"FREQ", "LEVEL", "VS TOP", "HARM"},
		Rows: []wsview.SpecRow{
			{Num: 1, Freq: "1MHz", Level: "8.9dBV", Rel: "0dB", Harm: "H1", Sel: true},
			{Num: 2, Freq: "3MHz", Level: "-2.9dBV", Rel: "-11.8dB", Harm: "H3"},
			{Num: 3, Freq: "5MHz", Level: "-11.1dBV", Rel: "-20dB", Harm: "H5"},
		},
		Readout:   []wsview.Span{{Text: "top 1MHz", Col: wsview.Text}, {Text: "    8.9dBV", Col: wsview.Text}, {Text: "THD 27.4% (2h)", Col: wsview.Text}},
		Marker:    "MKR 1MHz  8.9dBV  H1  (largest peak)",
		TimeLabel: "TIME", HasMark: true, MarkHz: 1.001e6, MarkRel: 0, MarkText: "M 1MHz 8.9dBV", BodeIdx: -1, BodeRef: -1,
	}
	v := wsview.View{WS: wsview.Spectrum, Layout: wsview.Layout{MenuW: 30, PlotTop: 150, HideViewCaptions: true, HideBottom: true}, Rail: true, Spec: s}
	h := UIHUD(v)
	h.ViewMode, h.SampleS = 2, 50e-9
	return Scene{Name: "ws_spectrum", Frame: f, HUD: h, Prefill: true}
}

// sceneMeasureStats pins MEASURE: the statistics table, the cursor
// block and the trace below with the time cursors.
// TRLC-LINKS: REQ-SDS-217
func sceneMeasureStats() Scene {
	m := &wsview.MeasPane{
		Pane: wsview.Pane{
			Title: "MEASURE", Caption: "statistics  N 48  over 2.4 s", Right: "Measure key: choose",
			Tool:  []wsview.Seg{{Key: "Adjust", Job: "cursor A"}, {Key: "push", Job: "cursors Volts"}, {Key: "Position", Job: "trigger position"}, {Key: "push", Job: "cursor B"}},
			Again: "F4 again: All values",
		},
		Stats: []wsview.StatRow{
			{Name: "C1 Vpp", Col: wsview.C1, Now: "5.68V", Mean: "5.68V", Min: "5.6V", Max: "5.76V", Sdev: "40mV", N: "48"},
			{Name: "C1 Freq", Col: wsview.C1, Now: "1kHz", Mean: "1kHz", Min: "999Hz", Max: "1kHz", Sdev: "0.4Hz", N: "48"},
			{Name: "C2 Vpp", Col: wsview.C2, Now: "4.8V", Mean: "4.8V", Min: "4.76V", Max: "4.84V", Sdev: "20mV", N: "48"},
		},
		Cursor: []wsview.Span{
			{Text: "CURSORS TIME", Col: wsview.Amber},
			{Text: "A -1.5ms  B 1.5ms", Col: wsview.Text},
			{Text: "dt 3ms  1/dt 333Hz", Col: wsview.Text},
			{Text: "C1 A 2.8V  B -2.8V", Col: wsview.C1},
		},
	}
	v := wsview.View{WS: wsview.Measure, Layout: wsview.Layout{MenuW: 30, PlotTop: 142, HideMeas: true}, Rail: true, Meas: m}
	h := UIHUD(v)
	h.CurOn, h.CurType, h.CurSel, h.CurX = true, 0, 0, [2]float64{0.35, 0.65}
	return Scene{Name: "ws_measure", Frame: YTFrame(), HUD: h, Prefill: true}
}

// sceneCompare pins COMPARE: the five rows with the stacking bar on
// MASTER, the selected row's action, and the plot below.
// TRLC-LINKS: REQ-SDS-218
func sceneCompare() Scene {
	c := &wsview.CmpPane{Pane: wsview.Pane{
		Title: "COMPARE", Caption: "trigger source C1: averaging",
		Tool:  []wsview.Seg{{Key: "Adjust", Job: "row"}, {Key: "push", Job: "row action"}, {Key: "Position", Job: "trigger position"}, {Key: "push", Job: "centre"}},
		Again: "F5 again: cancel stack",
	}}
	c.Rows[0] = wsview.CmpRow{
		Name: "MASTER", Info: "STACK 40/64 of C1", InfoCol: wsview.Amber, Sel: true, Action: "F5: cancel",
		Bar: true, BarN: 40, BarOf: 64, BarNote: "averaging - then REF A + mask, test raw", BarNoteCol: wsview.Dim,
	}
	c.Rows[1] = wsview.CmpRow{Name: "REF A", Info: "C1 1V 500us  shown", Extra: "max|d| 61.9mV  rms 18.4mV  100% in 0.5div"}
	c.Rows[2] = wsview.CmpRow{Name: "REF B", Info: "empty", InfoCol: wsview.Dim}
	c.Rows[3] = wsview.CmpRow{Name: "MASK", Info: "counting", Extra: "pass 91  fail 0  (0.0% fail)"}
	c.Rows[4] = wsview.CmpRow{Name: "MATH", Info: "Off", InfoCol: wsview.Dim}
	v := wsview.View{WS: wsview.Compare, Layout: PaneLayout(104), Rail: true, Cmp: c}
	return Scene{Name: "ws_compare", Frame: YTFrame(), HUD: UIHUD(v), Prefill: true}
}

// Sheet is the TRIG tab as SCOPE's sheet opens it: the tab strip with
// the workspace's own tabs first, enum rows written out, number rows with
// gauges, a header, an action row, the side panel and the hint line.
// TRLC-LINKS: REQ-SDS-212
func Sheet() *wsview.Sheet {
	opts := func(sel int, names ...string) []wsview.Opt {
		var o []wsview.Opt
		for i, n := range names {
			o = append(o, wsview.Opt{Text: n, Sel: i == sel})
		}
		return o
	}
	sh := &wsview.Sheet{
		WSName: "SCOPE", ThumbView: 0,
		Hint: wsview.Span{Text: "S/DIV: tab   Adjust: row   F1-F5: pick   Trig Menu or Menu On/Off: back to SCOPE   Help: guide", Col: wsview.Dim},
	}
	for i, n := range []string{"TRIG", "CH1", "CH2", "TIME", "ACQ", "VIEW", "MEAS", "CURS", "MATH/REF", "BUS", "FILE", "SYS"} {
		sh.Tabs = append(sh.Tabs, wsview.Tab{Name: n, Own: i < 5, Active: i == 0})
	}
	sh.Rows = []wsview.Row{
		{Kind: wsview.Enum, Label: "Source/slope", Rel: true, Opts: opts(0, "C1 Rise", "C1 Fall", "C2 Rise", "C2 Fall")},
		{Kind: wsview.Number, Label: "Level", Rel: true, Focus: true, Value: "200mV", Gauge: 0.53},
		{Kind: wsview.Enum, Label: "Mode", Rel: true, Opts: opts(0, "Auto", "Normal")},
		{Kind: wsview.Enum, Label: "Type", Rel: true, Opts: opts(0, "Edge", "Pulse", "Slope", "Video", "Serial")},
		{Kind: wsview.Number, Label: "Holdoff", Rel: true, Value: "Off", Gauge: 0},
		{Kind: wsview.Action, Label: "Force trigger", Rel: true, Acts: []wsview.Act{{Text: "Force now"}}},
		{Kind: wsview.Header, Header: "PULSE  (used when Type = Pulse)"},
		{Kind: wsview.Enum, Label: "Pulse when", Opts: []wsview.Opt{{Text: "Any", Sel: true, Quiet: true}, {Text: "< min"}, {Text: "> max"}, {Text: "In range"}}},
		{Kind: wsview.Number, Label: "Min width", Value: "100ns", Gauge: 0.2},
	}
	sh.Status = []wsview.Span{{Text: "Status: Trig'd", Col: wsview.Dim}}
	sh.Side = wsview.Side{
		Label: "Level", Value: "200mV", Big: true,
		Lines: []wsview.Span{
			{Text: "Voltage the trigger source must", Col: wsview.Dim},
			{Text: "cross.", Col: wsview.Dim},
			{Text: "Range -6.4V .. 6.4V", Col: wsview.Dim},
			{Text: "Also the Level knob (any time);", Col: wsview.Text},
		},
		Hints: []wsview.Span{{Text: "F1..F5: pick", Col: wsview.Dim}, {Text: "Push Adjust: edit", Col: wsview.Dim}},
	}
	return sh
}

// sceneSheet pins a full-screen settings sheet with its F-key column
// (Down, Up and presets for a number row) and the live mini view.
// TRLC-LINKS: REQ-SDS-212
func sceneSheet() Scene {
	keys := &wsview.Keys{Title: "LEVEL", Top: 30, Items: [5]wsview.Key{
		{Label: "Down", Value: "one step"},
		{Label: "Up", Value: "one step"},
		{Label: "0 V"},
		{Label: "50 %"},
	}}
	v := wsview.View{WS: wsview.Scope, Layout: wsview.Layout{MenuW: 116, Sheet: true, HideTraces: true, HideBottom: true}, Sheet: Sheet(), Keys: keys}
	return Scene{Name: "ws_sheet", Frame: YTFrame(), HUD: UIHUD(v), Prefill: true}
}

// sceneSheetPicker pins the sheet's byte picker box under the Match
// bytes row (the BUS tab's From screen).
// TRLC-LINKS: REQ-SDS-212, REQ-SDS-214
func sceneSheetPicker() Scene {
	sh := &wsview.Sheet{WSName: "BUS", Hint: wsview.Span{Text: "Menu On/Off: close and discard   S/DIV: other tab (discard)", Col: wsview.Dim}}
	for i, n := range []string{"BUS", "TRIG", "TIME", "CH1", "CH2", "ACQ", "VIEW", "MEAS", "CURS", "MATH/REF", "FILE", "SYS"} {
		sh.Tabs = append(sh.Tabs, wsview.Tab{Name: n, Own: i < 3, Active: i == 0})
	}
	sh.Rows = []wsview.Row{
		{Kind: wsview.Enum, Label: "Protocol", Rel: true, Opts: []wsview.Opt{{Text: "Off"}, {Text: "Auto"}, {Text: "UART", Sel: true}, {Text: "I2C"}, {Text: "SPI"}}},
		{Kind: wsview.Header, Header: "SERIAL TRIGGER"},
		{Kind: wsview.Bytes, Label: "Match bytes", Rel: true, Focus: true, Value: "55 AA", Armed: true},
		{Kind: wsview.Enum, Label: "Trigger on it", Rel: true, Opts: []wsview.Opt{{Text: "Off"}, {Text: "On", Sel: true}}},
	}
	var toks []wsview.PickTok
	for range 4 {
		for i, b := range []string{"48", "69", "55", "AA"} {
			toks = append(toks, wsview.PickTok{Text: b, Start: i == 0})
		}
	}
	sh.Sub = &wsview.Sub{
		Title: "PICK FROM SCREEN  (16 bytes decoded)", Toks: toks, Cur: 6,
		Lines: []wsview.Span{
			{Text: "| = idle gap (message start).  Underlined: the next bytes.", Col: wsview.Dim},
			{Text: "F5 or push Adjust: cancel.   Now: 55 AA", Col: wsview.Text},
		},
	}
	sh.Side = wsview.Side{Title: "PICK FROM SCREEN", Lines: []wsview.Span{{Text: "Bytes decoded on screen,", Col: wsview.Text}}}
	keys := &wsview.Keys{Title: "PICK BYTES", Top: 30, Items: [5]wsview.Key{
		{Label: "Trig on 1", Value: "55"},
		{Label: "Trig on 2", Value: "55 AA"},
		{Label: "Trig on 3", Value: "55 AA 48"},
		{Label: "Trig on 4", Value: "55 AA 48 69"},
		{Label: "Cancel", Value: "keep the match"},
	}}
	v := wsview.View{WS: wsview.Bus, Layout: wsview.Layout{MenuW: 116, Sheet: true, HideTraces: true, HideBottom: true}, Sheet: sh, Keys: keys}
	h := UIHUD(v)
	h.TdivS = 100e-6
	return Scene{Name: "ws_sheet_picker", Frame: BusFrame(), HUD: h, Prefill: true}
}

// sceneMatchEditor pins the serial match keyboard over the sheet with
// its F-key column.
// TRLC-LINKS: REQ-SDS-214
func sceneMatchEditor() Scene {
	keys := &wsview.Keys{Title: "MATCH EDITOR", Top: 30, Items: [5]wsview.Key{
		{Label: "Keys", Value: "Hex"},
		{Label: "Type", Value: "A"},
		{Label: "Delete", Value: "<"},
		{Label: "OK", Value: "use these bytes"},
		{Label: "Cancel", Value: "keep the match"},
	}}
	v := wsview.View{WS: wsview.Bus, Layout: wsview.Layout{MenuW: 116, Sheet: true, HideTraces: true, HideBottom: true}, Sheet: &wsview.Sheet{WSName: "BUS"}, Keys: keys}
	h := UIHUD(v)
	h.TdivS = 100e-6
	h.Match = render.MatchView{
		Open: true, Set: "Hex", Proto: "UART", Toks: []string{"55", "A?", "ERR"}, Cur: 3,
		Keys: [][]string{{"0", "1", "2", "3", "4", "5"}, {"6", "7", "8", "9", "A", "B"}, {"C", "D", "E", "F", "?", "??"}}, Row: 1, Col: 4,
	}
	return Scene{Name: "ws_match_editor", Frame: BusFrame(), HUD: h, Prefill: true}
}

// SoundPane is a SOUND view as the panel builds it: A4 3 cents sharp
// on C1, both meters, a cycle trace with its persistence.
// TRLC-LINKS: REQ-SDS-273
func SoundPane(page string, lines []wsview.Span) *wsview.SoundPane {
	s := &wsview.SoundPane{
		Pane: wsview.Pane{
			Title: "SOUND", Caption: "gap-free stream 61.0 kSa/s", Right: "tuner C1",
			Tool:  []wsview.Seg{{Key: "Adjust", Job: "readout"}, {Key: "push", Job: "clear strip"}, {Key: "Position", Job: "cycles"}, {Key: "push", Job: "C1/C2"}},
			Again: "F3 again: FFT",
		},
		HasPitch: true, Note: "A4", Cents: 3.1, Hz: "440.79 Hz", Sub: "A4 = 440 Hz  auto", Strobe: 0.3,
		RefName: "0 dB = dBu", Page: page, PageNum: 0, Pages: 5, Lines: lines,
	}
	for i := range 80 {
		s.Strip = append(s.Strip, float32(3+2*math.Sin(float64(i)/9)))
		s.StripOK = append(s.StripOK, i > 4)
	}
	s.Meter[0] = wsview.SoundMeter{On: true, DB: -6.2, PeakDB: -3.1, Lines: []string{"-6.2 dBu", "rms 380mV", "pk 537mV", "Vpp 1.07V", "DC +2mV", "crest 3.0dB"}}
	s.Meter[1] = wsview.SoundMeter{On: true, DB: 2.4, PeakDB: 5.5, Clip: true, Lines: []string{"2.4 dBu", "rms 1.02V", "pk 1.6V", "Vpp 3.2V", "DC -10mV", "crest 3.9dB"}}
	for k := range 4 {
		tr := make([]float32, 600)
		for i := range tr {
			ph := 2 * math.Pi * 2 * float64(i) / 599
			v := 0.0
			for h := 1; h < 12; h += 2 {
				v += math.Sin(float64(h)*ph) / float64(h)
			}
			tr[i] = float32(0.5*v + 0.02*float64(k))
		}
		s.Traces = append(s.Traces, tr)
	}
	s.TraceLo, s.TraceHi = -0.55, 0.6
	s.TraceTop = []wsview.Span{
		{Text: "2 cycles, trigger: FPGA edge mark C1", Col: wsview.Dim},
		{Text: "period 2.27ms  duty 50.0%  Vpp 1.07V", Col: wsview.Text},
		{Text: "rise 98us  fall 98us  slew 0.0315 V/us", Col: wsview.Text},
	}
	return s
}

// soundScene is a SOUND pane in SPECTRUM.
func soundScene(name string, s *wsview.SoundPane) Scene {
	v := wsview.View{
		WS: wsview.Spectrum, Layout: wsview.Layout{MenuW: 30, PlotTop: 262, HideViewCaptions: true, HideBottom: true, HideMeas: true},
		Rail: true, Sound: s,
	}
	h := UIHUD(v)
	h.ViewMode = 5
	return Scene{Name: name, Frame: YTFrame(), HUD: h, Prefill: true}
}

// sceneSoundModulation pins the SOUND view on its modulation page:
// tuner, meters (C2 clipping), the readout and the pitch-locked trace.
// TRLC-LINKS: REQ-SDS-273, REQ-SDS-269, REQ-SDS-272
func sceneSoundModulation() Scene {
	return soundScene("ws_sound_mod", SoundPane("MODULATION", []wsview.Span{
		{Text: "TREMOLO (AM)", Col: wsview.Amber},
		{Text: "depth 30.0%  5.4 dB", Col: wsview.Text},
		{Text: "rate 5 Hz  sine", Col: wsview.Text},
		{Text: "VIBRATO (FM)", Col: wsview.Amber},
		{Text: "+-20.0 cents  +-5.09 Hz", Col: wsview.Text},
		{Text: "rate 6 Hz  sine", Col: wsview.Text},
		{Text: "W&F 0.312% wtd  wow 0.204%", Col: wsview.Text},
		{Text: "flutter 0.811%", Col: wsview.Text},
	}))
}

// sceneSoundEnvelope pins the envelope page after a capture, with no
// pitch (the tuner idle) and no cycle trace.
// TRLC-LINKS: REQ-SDS-273, REQ-SDS-268
func sceneSoundEnvelope() Scene {
	s := SoundPane("ENVELOPE", []wsview.Span{
		{Text: "trigger: FPGA edge, gate C2 1.5V", Col: wsview.Dim},
		{Text: "attack 40ms (10-90)", Col: wsview.Text},
		{Text: "  0-100 50ms", Col: wsview.Text},
		{Text: "decay 92.1ms  tau 40ms", Col: wsview.Text},
		{Text: "sustain 50%  -6.0 dB", Col: wsview.Text},
		{Text: "release 184ms  tau 80ms", Col: wsview.Text},
		{Text: "peak 4.98V  push: arm again", Col: wsview.Dim},
	})
	s.PageNum, s.HasPitch, s.Note, s.Hz, s.Strip, s.StripOK = 1, false, "--", "no pitch", nil, nil
	s.Traces, s.TraceWhy = nil, "no pitch to lock to"
	return soundScene("ws_sound_env", s)
}

// sceneSoundTake pins a SINGLE take in the plot: a bass note's onset
// on C2 under a sawtooth melody on C1, each in its lane, the trigger's
// dashed line and marker, the window's bar and the knobs' jobs.
// TRLC-LINKS: REQ-SDS-275, REQ-SDS-273
func sceneSoundTake() Scene {
	s := SoundPane("MODULATION", []wsview.Span{{Text: "TREMOLO (AM)", Col: wsview.Amber}, {Text: "none (0.4%)", Col: wsview.Text}})
	s.Caption = "STOP: the take (RUN: live)"
	s.Note, s.Hz, s.Cents = "D3", "146.80 Hz", -0.2
	s.Tool = []wsview.Seg{
		{Key: "Adjust", Job: "readout"},
		{Key: "push", Job: "clear strip"},
		{Key: "Position", Job: "scroll"},
		{Key: "push", Job: "trigger/all"},
		{Key: "S/DIV", Job: "zoom"},
	}
	const n = 780
	tk := &wsview.SoundTake{
		TrigX: 0.25, TrigCh: 1, From: 0.1, To: 0.6, Title: "SINGLE take 5.0 s  view 2.5s at -0.500 s",
		Hint: "Position: scroll  push: trigger/all  S/DIV: zoom  RUN: live", VLo: [2]float64{-1.2, -1.6}, VHi: [2]float64{1.2, 1.6},
	}
	for ch := range 2 {
		tk.Lo[ch], tk.Hi[ch] = make([]float32, n), make([]float32, n)
		for i := range n {
			var a float64
			if ch == 1 {
				a = 0.02
				if i >= n/4 {
					a = 1.5 * math.Exp(-float64(i-n/4)/900)
				}
			} else {
				a = 0.6 + 0.4*math.Abs(math.Sin(float64(i)/40))
			}
			tk.Lo[ch][i], tk.Hi[ch][i] = float32(-a), float32(a)
		}
	}
	s.Take = tk
	return soundScene("ws_sound_take", s)
}

// sceneSoundHarmonics pins the harmonics page with the stream not yet
// running.
// TRLC-LINKS: REQ-SDS-273, REQ-SDS-270
func sceneSoundHarmonics() Scene {
	s := SoundPane("HARMONICS", []wsview.Span{
		{Text: "H1 1kHz  -0.0 dBV", Col: wsview.Text},
		{Text: "H2  -40.0  H3  -30.0  ", Col: wsview.Text},
		{Text: "H4    --   H5  -52.1  ", Col: wsview.Text},
		{Text: "THD 3.32%", Col: wsview.Text},
		{Text: "even/odd -10.0 dB", Col: wsview.Text},
		{Text: "odd (symmetric)", Col: wsview.Dim},
		{Text: "hum 50 Hz -60.0 dBV", Col: wsview.Text},
	})
	s.PageNum, s.Cents, s.Note = 3, -27, "C#3"
	s.State = "starting the audio stream (the stream image loads)"
	s.Traces, s.TraceWhy = nil, "waiting for the edge marks"
	return soundScene("ws_sound_harm", s)
}

// SuperresHUD is the fixed synthetic dataset behind the super-res review
// golden: a harmonic-rich C1 stack (square-ish: 5 MHz + 3rd/5th harmonics) on
// a ×16 fine grid with a gap band, a C2 tone, review focus, and the Task-2
// device URL on the top bar — all deterministic.
// TRLC-LINKS: REQ-SDS-021
func SuperresHUD() render.HUD {
	const K, nb = 16, 4096
	const sampleS = 2e-9
	fineDt := sampleS / K
	h := SRHUD(nb, K, sampleS,
		func(b int) float64 {
			ts := float64(b) * fineDt
			v := 128.0
			for _, m := range []float64{1, 3, 5} {
				v += (55 / m) * math.Sin(2*math.Pi*5e6*m*ts)
			}
			return v
		},
		func(b int) float64 { return 128 + 70*math.Sin(2*math.Pi*2e6*float64(b)*fineDt) })
	for b := 1500; b < 1650; b++ {
		h.SRMean[b] = -1 // a gap band: the review must lift the pen, not bridge
	}
	h.SRBits = 3.2
	h.SRStatus = "done: 512 stacked"
	h.URL = "http://192.168.1.42:8080"
	return h
}

// sceneSuperresReview pins the whole super-res review Y-T screen —
// graticule, stacked trace with its gap, SR status line, top-bar HUD with the
// device URL — against testdata/golden/superres_review_yt.png.
// TRLC-LINKS: REQ-SDS-021
func sceneSuperresReview() Scene {
	h := SuperresHUD()
	h.ViewMode = 0
	return Scene{Name: "superres_review_yt", Frame: nil, HUD: h} // review renders from the HUD stack; no live frame
}

// sceneSuperresReviewFFT pins the review's stacked-FFT screen (both
// channels + the fine-Nyquist axis label) — a second consumer proving the
// harness generalises across views.
// TRLC-LINKS: REQ-SDS-021, REQ-SDS-070
func sceneSuperresReviewFFT() Scene {
	h := SuperresHUD()
	h.ViewMode = 2
	// Frequency-zoom onto the low band (the tones live at 2–25 MHz; the fine
	// grid's Nyquist is 4 GHz) so the golden pins real spectral content.
	h.Zoom, h.ZoomOff = 64, -0.5
	return Scene{Name: "superres_review_fft", Frame: nil, HUD: h}
}

// SRHUD is a synthetic super-res HUD: a k× fine-grid mean built from gen(fineBin), a
// second channel from gen2, review focus, and a window covering the whole grid.
// TRLC-LINKS: REQ-SDS-021, REQ-SDS-070
func SRHUD(nb, k int, sampleS float64, gen, gen2 func(b int) float64) render.HUD {
	m1 := make([]float32, nb)
	m2 := make([]float32, nb)
	for b := range nb {
		m1[b] = float32(gen(b))
		if gen2 != nil {
			m2[b] = float32(gen2(b))
		} else {
			m2[b] = -1
		}
	}
	h := DefaultHUD()
	h.SRActive, h.SRFocus = true, 3
	h.SRMean, h.SRMean2 = m1, m2
	h.SRk, h.SRSampleS = k, sampleS
	h.SRGateLo, h.SRGateHi, h.SRN = 0, nb/k, nb/k
	h.SRWinLo, h.SRWinHi = 0, nb/k // review the whole grid
	h.SRPeriod = 0
	return h
}

// Sine is a frame of a full-scale-ish sine on both channels.
func Sine(n int, freqHz, sampleS float64) *sample.Frame {
	c := make([]uint8, n)
	for i := range n {
		v := 128 + 90*math.Sin(2*math.Pi*freqHz*float64(i)*sampleS)
		if v < 0 {
			v = 0
		}
		if v > 255 {
			v = 255
		}
		c[i] = uint8(v)
	}
	return &sample.Frame{SampleS: sampleS, Traces: []sample.Trace{{Codes8: c, Valid: n}, {Ch: 1, Codes8: c, Valid: n}}}
}

// DefaultHUD is the plain running two-channel HUD the scenes start from.
func DefaultHUD() render.HUD {
	return render.HUD{
		Ch: [render.MaxChans]render.Channel{{VdivV: 1}, {VdivV: 1}}, TdivS: 500e-6, TrigRising: true,
		Running: true, SampleS: 800e-9, NumChans: 2}
}

// Format12 is a 12-bit converter's codes left-aligned in 16-bit words (the
// SDS800X HD's), and CodesPerDiv12 its words a division: 30 codes a
// division on the 8-bit view.
var Format12 = sample.CodeFormat{Bits: 12, Storage: sample.U16, Shift: 4, Min: 0, Max: 0xfff0, Mid: 0x8000}

// CodesPerDiv12 is Format12's words a division.
const CodesPerDiv12 = 30 * 256

// Frame12 builds a frame of len(gens) channels of 12-bit codes from
// per-sample generators in divisions from the centre line, windowed across
// the whole record and centred on its middle.
// TRLC-LINKS: REQ-SDS-021
func Frame12(n int, gens ...func(i int) float64) *sample.Frame {
	f := &sample.Frame{
		WinPoints: n,
		TriggerAt: float64(n) / 2,
		SourcePtp: 144,
		SampleS:   800e-9,
		Snapshot:  sample.Settings{TdivS: 500e-6},
		Status:    sample.Status{Triggered: true},
	}
	for ch, g := range gens {
		w := make([]uint16, n)
		for i := range w {
			v := 0x8000 + g(i)*CodesPerDiv12
			w[i] = uint16(math.Min(math.Max(math.Round(v/16)*16, 0), 0xfff0))
		}
		f.Traces = append(f.Traces, sample.Trace{Ch: ch, Format: Format12, Codes16: w, Valid: n, CodesPerDiv: CodesPerDiv12})
	}
	return f
}

// FourChanFrame is four 12-bit channels: a square, a sine, a ramp and a
// small fast burst, each about its own level.
// TRLC-LINKS: REQ-SDS-021
func FourChanFrame() *sample.Frame {
	const n = 2048
	return Frame12(n,
		func(i int) float64 { // C1: 512-sample square, rising edge at 1024
			if (i/256)%2 == 0 {
				return 2.5
			}
			return 0.5
		},
		func(i int) float64 { return 1.2 * math.Sin(2*math.Pi*float64(i-n/2)/512) },
		func(i int) float64 { return -1 + 1.5*float64(i%400)/400 },
		func(i int) float64 {
			return -3 + 0.6*math.Sin(2*math.Pi*float64(i)/37)*math.Exp(-math.Abs(float64(i-n/2))/300)
		})
}

// FourChanHUD shows the four channels at their own scales and offsets.
// TRLC-LINKS: REQ-SDS-021
func FourChanHUD() render.HUD {
	h := DefaultHUD()
	h.NumChans = 4
	for ch, c := range []render.Channel{
		{VdivV: 1, OffV: -1.5, Probe: 10},
		{VdivV: 0.5},
		{VdivV: 0.2, OffV: 0.2, Cpl: 1},
		{VdivV: 0.05, OffV: 0.15, Probe: 1},
	} {
		c.Show = true
		h.Ch[ch] = c
	}
	h.ChanExplicit = true
	h.TrigLvlDiv, h.TrigLvlV = 1.5, 0.5
	h.TrigPosFrac = 0.5
	h.Trigd = true
	h.URL = "http://192.168.1.42:8080"
	return h
}

// FourChanScenes are pictures only a four-channel 12-bit scope shows: the
// Y-T screen, its MEASURE boxes and volt cursors, and SCOPE's rail.
// TRLC-LINKS: REQ-SDS-021
func FourChanScenes() []Scene {
	yt := FourChanHUD()
	meas := FourChanHUD()
	meas.ShowMeas = true
	meas.CurOn, meas.CurType, meas.CurY = true, 1, [2]float64{0.3, 0.6}
	rail := UIHUD(wsview.View{
		WS: wsview.Scope, Layout: PaneLayout(0), Rail: true,
		Scope: &wsview.ScopePane{AdjustTag: "Adjust: Level 500mV", Hot: true},
	})
	rail.Ch, rail.NumChans, rail.ChanExplicit = yt.Ch, 4, true
	return []Scene{
		{Name: "yt_4ch_12bit", Frame: FourChanFrame(), HUD: yt},
		{Name: "measure_4ch_12bit", Frame: FourChanFrame(), HUD: meas},
		{Name: "ws_scope_4ch", Frame: FourChanFrame(), HUD: rail, Prefill: true},
	}
}
