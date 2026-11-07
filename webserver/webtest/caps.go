// ENGMODEL-OWNER-UNIT: FU-APP-WEB

package webtest

import (
	"math"
	"strconv"

	"github.com/labeth/open-dso-core/sample"
	"github.com/labeth/open-dso-core/webserver"
	"github.com/labeth/open-dso-core/webserver/wire"
)

// decade is m·10^e, rounded as its decimal reads.
// TRLC-LINKS: REQ-SDS-164
func decade(m float64, e int) float64 {
	if e < 0 {
		return m / math.Pow10(-e)
	}
	return m * math.Pow10(e)
}

// tdivLadder is a 1-2-5 timebase ladder from 1 ns to 50 s.
// TRLC-LINKS: REQ-SDS-164
func tdivLadder() []float64 {
	var out []float64
	for e := -9; e <= 1; e++ {
		for _, m := range []float64{1, 2, 5} {
			out = append(out, decade(m, e))
		}
	}
	return out
}

// vdivLadder is a 1-2-5 V/div ladder from lo to 10 V.
// TRLC-LINKS: REQ-SDS-164
func vdivLadder(lo float64) []float64 {
	var out []float64
	for e := -4; e <= 1; e++ {
		for _, m := range []float64{1, 2, 5} {
			if v := decade(m, e); v >= lo*(1-1e-9) && v <= 10 {
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
	c.Controls = []wire.CapsControl{
		{Name: "vdiv", Unit: "V", PerChannel: true, Ladder: "vertical.vdivs"},
		{Name: "offset", Unit: "V", PerChannel: true},
		{Name: "probe", PerChannel: true, Ladder: "vertical.probes"},
		{Name: "coupling", PerChannel: true, Ladder: "vertical.couplings"},
		{Name: "bwlimit", PerChannel: true, Ladder: "vertical.bw_limits"},
		{Name: "invert", PerChannel: true},
		{Name: "triglevel", Unit: "V"},
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

// Status is the status document of the fields the shared page reads, for
// four channels.
// TRLC-LINKS: REQ-SDS-164
func (w *Wide) Status() any {
	w.mu.Lock()
	defer w.mu.Unlock()
	st := map[string]any{
		"running": w.running, "seq": w.seq, "tdiv_s": w.tdivS, "fps": 20.0, "band": "sim", "version": "wide",
		"tdivs": tdivLadder(), "vdivs": vdivLadder(0.0005), "trig_source": 0, "trig_rising": true,
		"trig_volts": 0.0, "trig_pos_frac": 0.5, "mem_depth": fakeN, "cal_source": "fake", "mmap_drain": true, "mask_gen": w.maskGen,
		"win_cols": 1000,
	}
	for n := 1; n <= 4; n++ {
		k := strconv.Itoa(n)
		st["vdiv"+k], st["probe"+k], st["cpl"+k], st["off"+k+"_v"], st["zoom"+k] = 0.5, 1, 0, 0.0, 1
		st["bwl"+k], st["inv"+k] = 0, 0
	}
	return st
}

// Sound is the Fake's stream, its two lanes carrying the tuned channel and
// C1 (C2 when C1 is tuned), named in its scales: a scope of more channels
// than lanes.
// TRLC-LINKS: REQ-SDS-265
func (w *Wide) Sound() (webserver.Sound, bool) { return wideSound{fakeSound{w.Fake}}, true }

// SoundControls is the Fake's SOUND settings.
// TRLC-LINKS: REQ-SDS-274
func (w *Wide) SoundControls() (webserver.SoundControls, bool) {
	return wideSound{fakeSound{w.Fake}}, true
}

// wideSound is the Fake's sound with its lanes named.
type wideSound struct{ fakeSound }

// WideLanes is the channels (0-based) the four-channel fake's two sound
// lanes carry when channel ch is tuned.
// TRLC-LINKS: REQ-SDS-265
func WideLanes(ch int) []int {
	if ch == 0 {
		return []int{0, 1}
	}
	return []int{ch, 0}
}

// Read is the Fake's block with the lanes named in its scales.
// TRLC-LINKS: REQ-SDS-265
func (s wideSound) Read(from uint64, limit int) wire.SoundBlock {
	b := s.fakeSound.Read(from, limit)
	lanes := WideLanes(s.SoundSettings().Ch)
	for i := range b.Scales {
		b.Scales[i].Chans = lanes
	}
	return b
}

// Take is the Fake's take with the lanes named.
// TRLC-LINKS: REQ-SDS-275
func (s wideSound) Take() *wire.SoundTake {
	return &wire.SoundTake{SoundBlock: s.Read(0, 16), ID: 1, Why: "stop"}
}

// Twin is the Fake serving Caps2x8 and its frames in wire v2 too: the
// two-channel 8-bit scope with caps, which the page must show exactly as
// it shows the Fake without them.
type Twin struct {
	*Fake
}

// NewTwin is a running two-channel fake with caps.
// TRLC-LINKS: REQ-SDS-164
func NewTwin() *Twin { return &Twin{Fake: NewFake(false)} }

// Caps is Caps2x8.
// TRLC-LINKS: REQ-SDS-164
func (t *Twin) Caps() wire.Caps { return Caps2x8() }

// CapsGen is Caps2x8's generation.
// TRLC-LINKS: REQ-SDS-164
func (t *Twin) CapsGen() uint64 { return 1 }

// Frames serves the Fake's frames in either wire.
// TRLC-LINKS: REQ-SDS-163
func (t *Twin) Frames() webserver.Frames { return twinFrames{fakeFrames{t.Fake}} }

// twinFrames serve v2 from the Fake's v1 replies.
type twinFrames struct{ fakeFrames }

// FrameV2 is the Fake's v1 reply as v2.
// TRLC-LINKS: REQ-SDS-163
func (fr twinFrames) FrameV2(q webserver.FrameQueryV2, dst *wire.Frame) {
	if !q.Raw {
		dst.FromReply(fr.Frame(q.FrameQuery))
		return
	}
	raw := fr.RawFrame(q.Since)
	rep := raw.Header
	if !raw.Unchanged {
		n := len(raw.Payload) / 2
		for i, b := range raw.Payload {
			if i < n {
				rep.C1 = append(rep.C1, int16(b))
			} else {
				rep.C2 = append(rep.C2, int16(b))
			}
		}
	}
	dst.FromReply(rep)
	dst.Header.Raw = true
}
