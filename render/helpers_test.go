// ENGMODEL-OWNER-UNIT: FU-APP-LCD

package render

import "github.com/labeth/open-dso-core/uimodel/wsview"

// uartWave is n 8-bit codes of a UART line sending bytes LSB first, spb
// samples a bit, idle high around them (rendertest.UARTWave's).
// TRLC-LINKS: REQ-SDS-021
func uartWave(bytes []int, spb, n int) []uint8 {
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

// paneLayout is a workspace layout: the rail and a pane above the plot.
// TRLC-LINKS: REQ-SDS-211
func paneLayout(top int) wsview.Layout { return wsview.Layout{MenuW: 30, PlotTop: top} }

// at565 is the RGB565 word canvas sf shows at (x, y).
// TRLC-LINKS: REQ-SDS-021
func at565(sf Canvas, x, y int) uint16 { return pack565(sf.At(x, y)) }
