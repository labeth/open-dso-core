// ENGMODEL-OWNER-UNIT: FU-DSO-ENGINE

package engine

import (
	"errors"

	"github.com/labeth/open-dso-core/decode"
)

// ParamsFromDecode completes p, the hardware decoder's settings from the
// decoder settings, with what a decode r of a frame of colTimeS samples with
// those settings measured: the threshold it sliced at, an SPI clock rate,
// SENT's tick, a CAN bit rate the settings leave open. The stream then starts
// with exactly what the display decodes. ARINC 429's slicer levels are the
// engine's, learned from a sampled record while the settings name ARINC 429.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-013, REQ-DSO-222
func ParamsFromDecode(p SerialParams, r decode.Result, colTimeS float64) (SerialParams, error) {
	if !r.OK {
		return SerialParams{}, errors.New("the bus does not decode on screen: nothing to stream")
	}
	if !(colTimeS > 0) || !(r.Thr > 0 && r.Thr < 255) {
		return SerialParams{}, errors.New("the decode on screen has no usable threshold or sample time")
	}
	p.Threshold, p.HaveThr = r.Thr, true
	rate := 0.0
	if r.SPB > 0 {
		rate = 1 / (r.SPB * colTimeS)
	}
	switch r.Proto {
	case "spi":
		p.SPIClockHz = int(rate + 0.5)
	case "sent":
		p.TickNs = r.SPB * colTimeS * 1e9
	case "canfd":
		if p.Baud == 0 {
			p.Baud = int(rate + 0.5)
		}
	}
	return p, nil
}
