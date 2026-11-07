// ENGMODEL-OWNER-UNIT: FU-APP-WEB

package webtest

import (
	"math"

	"github.com/labeth/open-dso-core/sample"
	"github.com/labeth/open-dso-core/webserver"
	"github.com/labeth/open-dso-core/webserver/wire"
)

// tdivLadder is a 1-2-5 timebase ladder from 1 ns to 50 s.
// TRLC-LINKS: REQ-SDS-164
func tdivLadder() []float64 {
	var out []float64
	for e := -9; e <= 1; e++ {
		for _, m := range []float64{1, 2, 5} {
			out = append(out, m*math.Pow10(e))
		}
	}
	return append(out, 50)
}

// vdivLadder is a 1-2-5 V/div ladder from lo to 10 V.
// TRLC-LINKS: REQ-SDS-164
func vdivLadder(lo float64) []float64 {
	var out []float64
	for e := -4; e <= 1; e++ {
		for _, m := range []float64{1, 2, 5} {
			if v := m * math.Pow10(e); v >= lo*(1-1e-9) && v <= 10 {
				out = append(out, v)
			}
		}
	}
	return out
}

// Caps2x8 is the capabilities of a two-channel 8-bit scope that draws 25
// codes a division about code 128 with Q8.8 precision words: exactly what
// the shared page assumes of a scope that serves no caps, so a scope that
// serves these sees no change in the page.
// TRLC-LINKS: REQ-SDS-164
func Caps2x8() wire.Caps {
	return wire.Caps{
		Gen:      1,
		Identity: wire.Identity{Model: "fake two-channel 8-bit"},
		Channels: []wire.CapsChannel{{Ch: 0, Name: "C1", Color: "c1"}, {Ch: 1, Name: "C2", Color: "c2"}},
		Codes: wire.CapsCodes{Bits: 8, Storage: "u8", Min: 0, Max: 255, Mid: 128, CodesPerDiv: 25,
			FracBitsPrecision: 8},
		Timebase: wire.CapsTimebase{Tdivs: tdivLadder(), FullRateTdivS: 2e-7, FullRateS: 2e-9,
			Depths: []int{1024, 4096, 20480}, Positions: []float64{0, 1}},
		Vertical: wire.CapsVertical{Vdivs: vdivLadder(0.002), Probes: []float64{1, 10, 100},
			Couplings: []string{"DC", "AC", "GND"}, BWLimits: []string{"full", "20M"}, Impedances: []string{"1M"}},
		Trigger: wire.CapsTrigger{Types: []string{"edge", "pulse", "slope", "video"}, Sources: []string{"C1", "C2"},
			Modes: []string{"auto", "normal", "single"}, HoldoffS: [2]float64{0, 10}},
		Acquisition: wire.CapsAcquisition{Modes: []string{"normal", "average", "eres", "peak", "precision"},
			Content: []string{"raw-full-rate", "raw-decimated", "peak-pairs"}},
		Features: map[string]wire.Feature{"decode": {OK: true}, "measure": {OK: true}},
		Screen:   wire.CapsScreen{W: 800, H: 480, Format: "RGB565"},
		UI:       wire.CapsUI{Remote: true, FKeys: 5},
		Limits:   wire.CapsLimits{FrameCols: [2]int{64, 4096}, LiveClients: 1},
	}
}

// Caps4x12 is the capabilities of a four-channel scope whose 12-bit codes
// sit left-aligned in 16-bit words about 0x8000, 7680 stored units a
// division, with a trigger on any channel and only edge triggers.
// TRLC-LINKS: REQ-SDS-164
func Caps4x12() wire.Caps {
	c := Caps2x8()
	c.Identity = wire.Identity{Model: "fake four-channel 12-bit"}
	c.Channels = []wire.CapsChannel{
		{Ch: 0, Name: "C1", Color: "c1"}, {Ch: 1, Name: "C2", Color: "c2"},
		{Ch: 2, Name: "C3", Color: "c3"}, {Ch: 3, Name: "C4", Color: "c4"},
	}
	c.Codes = wide12
	c.Vertical.Vdivs = vdivLadder(0.0005)
	c.Vertical.Probes = []float64{0.1, 1, 10, 100, 1000}
	c.Trigger = wire.CapsTrigger{Types: []string{"edge"}, Sources: []string{"C1", "C2", "C3", "C4"},
		Modes: []string{"auto", "normal", "single"}, HoldoffS: [2]float64{0, 10}}
	c.Acquisition = wire.CapsAcquisition{Modes: []string{"normal"}, Content: []string{"raw-full-rate", "raw-decimated"}}
	c.Features = map[string]wire.Feature{
		"decode":  {OK: true},
		"measure": {OK: false, Why: "measurements of 12-bit codes are not served yet"},
		"eye":     {OK: false, Why: "needs full-rate raw records"},
	}
	c.Screen = wire.CapsScreen{W: 1024, H: 600, Format: "RGB565", Touch: true}
	return c
}

// wide12 is the four-channel fake's code format.
var wide12 = wire.CapsCodes{Bits: 12, Storage: "u16", Shift: 4, Min: 0x0800, Max: 0xF7F0, Mid: 0x8000, CodesPerDiv: 7680}

// Wide is a four-channel 12-bit scope: the Fake with capabilities and v2
// frames of four channels (a sine, a square, a triangle and a ramp, 12-bit
// codes left-aligned in 16-bit words). Its v1 frames are the Fake's.
type Wide struct {
	*Fake
}

// NewWide is a running four-channel fake with its first frame published.
// TRLC-LINKS: REQ-SDS-164
func NewWide() *Wide { return &Wide{Fake: NewFake(false)} }

// Caps is Caps4x12.
// TRLC-LINKS: REQ-SDS-164
func (w *Wide) Caps() wire.Caps { return Caps4x12() }

// CapsGen is Caps4x12's generation.
// TRLC-LINKS: REQ-SDS-164
func (w *Wide) CapsGen() uint64 { return 1 }

// Frames serves v2 frames beside the Fake's v1.
// TRLC-LINKS: REQ-SDS-163
func (w *Wide) Frames() webserver.Frames { return wideFrames{fakeFrames{w.Fake}} }

// wideFrames are the Fake's frames plus v2.
type wideFrames struct{ fakeFrames }

// WideRecord is the four-channel fake's record of n samples per channel, as
// a sample frame: the words a v2 frame of it carries.
// TRLC-LINKS: REQ-SDS-163
func WideRecord(seq uint64, n int) *sample.Frame {
	f := &sample.Frame{
		Content: sample.RawFullRate, SampleS: 1e-8, CaptureS: 1e-8, AcqDecim: 1, Depth: n, WinPoints: n / 2,
		TriggerAt: float64(n) / 2, TrigKind: sample.HardwareNode, Seq: seq,
		Status:   sample.Status{Triggered: true, Coherent: true},
		Snapshot: sample.Settings{TdivS: 1e-6, DisplayedS: 1e-6},
	}
	fmtw := sample.CodeFormat{Bits: 12, Storage: sample.U16, Shift: 4, Min: wide12.Min, Max: wide12.Max, Mid: wide12.Mid}
	for ch := range 4 {
		t := sample.Trace{Ch: ch, Format: fmtw, Codes16: make([]uint16, n), Valid: n,
			Scale: sample.Scale{Zero: 0x8000, PerCode: 0.5 / 7680}, CodesPerDiv: 7680}
		for i := range n {
			x := float64(i) / 200 // periods
			var d float64         // divisions from the centre
			switch ch {
			case 0:
				d = 2.5 * math.Sin(2*math.Pi*x)
			case 1:
				d = -1.5
				if int(x*2)%2 == 0 {
					d = 1.5
				}
			case 2:
				d = 3 * (2*math.Abs(2*(x-math.Floor(x+0.5))) - 1)
			default:
				d = 3.5 * (2*(x-math.Floor(x)) - 1)
			}
			code := math.Round(d * 480) // 12-bit codes: 480 a division
			t.Codes16[i] = uint16(0x8000 + int(code)*16)
		}
		f.Traces = append(f.Traces, t)
	}
	return f
}

// FrameV2 serves the whole record of four channels.
// TRLC-LINKS: REQ-SDS-163
func (fr wideFrames) FrameV2(q webserver.FrameQueryV2, dst *wire.Frame) {
	fr.f.mu.Lock()
	seq := fr.f.seq
	fr.f.mu.Unlock()
	if seq == q.Since {
		dst.Header = wire.FrameHeader{Seq: seq, Unchanged: true, EdgeX: -1, TriggerAt: -1}
		return
	}
	dst.FromSample(WideRecord(seq, fakeN))
	h := &dst.Header
	h.Raw = q.Raw
	h.Cols, h.Depth, h.EdgeX = fakeN, fakeN, fakeN/2
	h.ColSpanS, h.DtS = fakeN*1e-8, 1e-8
	h.EdgeFrac, h.WinFrac = 0.5, 0.5
	for i := range dst.Traces {
		dst.Traces[i].Coupling = "DC"
	}
}
