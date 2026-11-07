// ENGMODEL-OWNER-UNIT: FU-DSO-PANEL

package panel

// Hardware is what the UI model asks of the scope's acquisition hardware
// without changing anything: how a timebase is recorded, the limits views
// and bus searches are planned against, how a decoded stream runs, and the
// firmware line the SYS tab names.
type Hardware interface {
	// Record is the record a capture at tdivS takes: its sample interval
	// and its decimation (log2 of the full-rate samples one point stands
	// for; 0 at the full rate).
	Record(tdivS float64) (sampleS float64, decimLog2 int)
	// RollMinTdivS is the timebase from which the live view rolls.
	RollMinTdivS() float64
	// SearchSamples is the samples of the live view a bus search reads.
	SearchSamples() int
	// EnvelopePoints is the points of a live envelope frame.
	EnvelopePoints() int
	// StreamMode is how a decoded stream of protocol proto (the engine's
	// serial protocol number) at baud runs: "gap-free", "captures", or ""
	// for one that does not stream.
	StreamMode(proto, baud int, overdrive bool) string
	// Product is the firmware's name, version and build.
	Product() string
}

// fullRateTdiv is the slowest of tdivs whose record is sampled at the
// fastest rate (0 for none): where BUS detection looks again when a record
// hid the bus (decimated, or too short).
// TRLC-LINKS: REQ-DSO-214
func fullRateTdiv(hw Hardware, tdivs []float64) float64 {
	if len(tdivs) == 0 {
		return 0
	}
	full, _ := hw.Record(minOf(tdivs))
	best := 0.0
	for _, t := range tdivs {
		if s, _ := hw.Record(t); s <= full && t > best {
			best = t
		}
	}
	return best
}

// minOf is the least of vs (vs not empty).
// TRLC-LINKS: REQ-DSO-214
func minOf(vs []float64) float64 {
	m := vs[0]
	for _, v := range vs[1:] {
		m = min(m, v)
	}
	return m
}

// recordSampleS is the sample interval of the record a capture at tdivS
// takes.
// TRLC-LINKS: REQ-DSO-214
func (c *Controller) recordSampleS(tdivS float64) float64 {
	s, _ := c.hw.Record(tdivS)
	return s
}
