// ENGMODEL-OWNER-UNIT: FU-APP-LCD

package render

// Metrics is a screen's layout table: its size and the positions the
// renderer anchors to it (ADR-HAL-RENDER-METRICS). The renderer does not
// scale continuously: the 5×7 bitmap font keeps its size, panes and readouts
// keep their pixel sizes, and what a screen's size moves (the right column,
// the bottom readouts, the strips under the plot, the F-key rows beside the
// screen) comes from here. The 800×480 table is the SDS1000CML+'s.
type Metrics struct {
	W, H int
	// TraceTop and TraceBot are the rows the full-screen trace's +4 and -4
	// divisions map to: clear of the status bar and of the bottom edge.
	TraceTop, TraceBot int
	// MinPlotW is the narrowest the plot gets beside a menu column, and
	// MaxPlotTop the lowest a workspace pane may push the plot's top.
	MinPlotW, MaxPlotTop int
	// DecodeStripH is the decode strip's height above the bottom readout row.
	DecodeStripH int
	// SoftkeyY are the rows of the F1..F5 keys' slot centres: on the
	// SDS1000CML+ measured against the bezel (an 80 px pitch, F5 +90).
	SoftkeyY [5]int
	// ChanLabelDX is the top bar's pitch between channel labels, and
	// ReadoutDX the bottom quick readouts' pitch between channels.
	ChanLabelDX, ReadoutDX int
	// StatusSlotX is where the top bar's status slot (the loading indicator
	// or the device URL) ends, right-aligned.
	StatusSlotX int
	// SaverW and SaverH are the screen saver's mini scope.
	SaverW, SaverH int
	// DivsH and DivsV are the graticule's divisions across and down.
	DivsH, DivsV int
}

// defaultCodesPerDiv is the codes a division on the 8-bit view of a trace
// that does not state its own: an 8-bit converter's 25 (the SDS1000CML+'s).
const defaultCodesPerDiv = 25

// MaxW is the widest screen the renderer draws: its per-column scratch
// arrays hold this many columns.
const MaxW = 2048

// Metrics800x480 is the SDS1000CML+'s panel: every value is what the
// renderer's literals were before the table.
var Metrics800x480 = Metrics{
	W: 800, H: 480,
	TraceTop: 8, TraceBot: 476,
	MinPlotW: 200, MaxPlotTop: 300,
	DecodeStripH: 38,
	SoftkeyY:     [5]int{80, 160, 240, 320, 410},
	ChanLabelDX:  92, ReadoutDX: 406,
	StatusSlotX: 664,
	SaverW:      240, SaverH: 144,
	DivsH: 10, DivsV: 8,
}

// Metrics1024x600 is a 1024×600 panel (the SDS800X HD): the F-key rows
// spread over the taller screen, the strips and readouts anchored as on the
// 800×480 panel, the four channels' readouts across the width.
var Metrics1024x600 = Metrics{
	W: 1024, H: 600,
	TraceTop: 8, TraceBot: 596,
	MinPlotW: 256, MaxPlotTop: 375,
	DecodeStripH: 38,
	SoftkeyY:     [5]int{100, 200, 300, 400, 512},
	ChanLabelDX:  92, ReadoutDX: 255,
	StatusSlotX: 888,
	SaverW:      300, SaverH: 180,
	DivsH: 10, DivsV: 8,
}

// Metrics1280x800 is a 1280×800 panel (the DHO1000 series): laid out as the
// 1024×600 one.
var Metrics1280x800 = Metrics{
	W: 1280, H: 800,
	TraceTop: 8, TraceBot: 796,
	MinPlotW: 320, MaxPlotTop: 500,
	DecodeStripH: 38,
	SoftkeyY:     [5]int{133, 266, 400, 533, 683},
	ChanLabelDX:  92, ReadoutDX: 319,
	StatusSlotX: 1144,
	SaverW:      384, SaverH: 230,
	DivsH: 10, DivsV: 8,
}

// metricsOf is the table canvas sf draws with.
// TRLC-LINKS: REQ-SDS-021
func metricsOf(sf Canvas) *Metrics { return sf.Metrics() }
