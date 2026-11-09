// ENGMODEL-OWNER-UNIT: FU-DSO-ENGINE

package engine

import (
	"math"
	"time"

	"github.com/labeth/open-dso-core/decode"
	"github.com/labeth/open-dso-core/instrument"
	"github.com/labeth/open-dso-core/sample"
)

// A serial request that leaves a rate to the decoder's own measurement (an
// SPI or I2C clock, a UART's bit rate, a SENT tick) has it measured on the
// bus's first line from the records read, as the SDS1000CML+'s engine
// measures it; the board's matcher takes the request with the rate filled
// in (ADR-HAL-SERIAL-RATES). An ARINC 429 request takes its slicer from a
// sampled record. A measured rate that moves more than serialRateMove
// places the trigger again.

// serialRateMove is the share a measured rate may move before the trigger
// is programmed again with it.
const serialRateMove = 0.01

// halSerial is what the records taught of a bus's rates.
type halSerial struct {
	spiHz, sclHz, baud, sentNs float64
	sentNib                    int
	spiQuiet                   time.Time // since when a learned SPI clock has not triggered
	spiProbe                   bool      // measure the SPI clock again on the next record
	arinc                      [4]float64
	arincHave                  bool
	arincFor                   SerialParams // the request the slicer was learned for
}

// learnSerial measures the serial request's rates on frame f's bus line.
// TRLC-LINKS: REQ-DSO-013
func (c *HALCycle) learnSerial(f *sample.Frame, triggered bool) {
	s := &c.ser
	p := c.cur.Serial
	if c.cur.SerialMode == 0 || f == nil || f.Len() < 64 {
		return
	}
	line := view8Line(f, p.ChA)
	if line == nil {
		return
	}
	peak := f.Content == sample.PeakPairs
	moved := func(was, now float64) bool { return !(was > 0) || math.Abs(now-was) > serialRateMove*was }
	switch {
	case p.Proto == SerSPI && p.SPIClockHz <= 0:
		if triggered {
			s.spiQuiet = time.Time{}
		}
		if hz := ClockRate(line, f.SampleS, peak); hz > 0 && ResolvesClock(s.spiHz, f) {
			if moved(s.spiHz, hz) || s.spiProbe {
				c.replace = c.replace || moved(s.spiHz, hz)
				s.spiHz = hz
			}
		}
		if s.spiProbe {
			s.spiProbe, s.spiQuiet = false, time.Now()
			c.replace = true
		}
		c.spiQuietCheck()
	case p.Proto == SerI2C && p.I2CClockHz <= 0:
		if hz := ClockRate(line, f.SampleS, peak); hz > 0 && ResolvesClock(s.sclHz, f) && moved(s.sclHz, hz) {
			s.sclHz, c.replace = hz, true
		}
	case p.Proto == SerUART && p.Baud <= 0:
		if b := UARTBaud(line, f.SampleS); b > 0 && int(b) != int(s.baud) && moved(s.baud, b) {
			s.baud, c.replace = b, true
		}
	case p.Proto == SerSENT && (p.TickNs <= 0 || p.Nibbles <= 0):
		if tick, nib := SentShape(line, f.SampleS); tick > 0 && (moved(s.sentNs, tick) || nib != s.sentNib) {
			s.sentNs, s.sentNib, c.replace = tick, nib, true
		}
	case p.Proto == SerARINC:
		c.learnARINC(f, line)
	}
}

// spiQuietCheck: a learned SPI clock the board has not triggered on for
// SPIQuietNs is measured again from the next record (the bus may have
// slowed: a gap planned for the faster clock splits every byte).
// TRLC-LINKS: REQ-DSO-013, REQ-DSO-229
func (c *HALCycle) spiQuietCheck() {
	s := &c.ser
	if !(s.spiHz > 0) {
		s.spiQuiet = time.Time{}
		return
	}
	switch {
	case s.spiQuiet.IsZero():
		s.spiQuiet = time.Now()
	case time.Since(s.spiQuiet) > time.Duration(SPIQuietNs):
		s.spiProbe = true
	}
}

// learnARINC takes ARINC 429's slicer from a sampled record that resolves
// its bits, once a request's threshold is known; an envelope or a record
// too coarse asks for one with a short lease.
// TRLC-LINKS: REQ-DSO-013, REQ-DSO-018
func (c *HALCycle) learnARINC(f *sample.Frame, line []uint8) {
	s := &c.ser
	p, why := c.planRates(c.cur.Serial)
	if why != "" || (s.arincHave && s.arincFor.Equal(&c.cur.Serial)) {
		return
	}
	resolves := f.AcqDecim <= 1 || p.Baud > 0 && f.SampleS > 0 && f.SampleS*arincBitSamples <= 1/float64(p.Baud)
	switch {
	case (f.Content == sample.PeakPairs || f.Flags.Has(sample.Envelope)) && p.Baud > 0:
		c.inst.Lease("arinc", instrument.RecordNeed{Samples: 8192}, time.Second)
		return
	case f.Content == sample.PeakPairs || f.Flags.Has(sample.Envelope) || !resolves:
		c.inst.Lease("arinc", instrument.RecordNeed{FullRate: true}, time.Second)
		return
	}
	e, why := c.swing(p.ChA)
	if why != "" {
		return
	}
	if !p.HaveThr { // sliced at the middle of the learned swing, as the trigger
		p.Threshold, p.HaveThr = (e.lo+e.hi)/2/e.voltsCode8+e.zero8, true
	}
	_, hi, lo, exitHi, exitLo, reason := decode.ARINC429Levels(line, decode.ARINC429Cfg{Bitrate: p.Baud, Threshold: p.Threshold, HaveThr: p.HaveThr})
	if reason != "" {
		return
	}
	h, l, eh, el := math.Ceil(hi), math.Floor(lo), math.Ceil(exitHi), math.Floor(exitLo)
	if !(l >= 0 && l < el && el <= eh && eh < h && h <= 255) {
		return
	}
	volts := func(code float64) float64 { return (code - e.zero8) * e.voltsCode8 }
	s.arinc = [4]float64{volts(eh), volts(el), volts(h), volts(l)}
	s.arincHave, s.arincFor, c.replace = true, c.cur.Serial, true
}

// arincBitSamples is the samples a bit a decimated record must hold for
// ARINC 429's slicer to be taken from it.
const arincBitSamples = 8

// planRates is request p with the rates it leaves to the decoder filled in
// from the records' measurements, its match bytes as the matcher's units;
// why says what is still measured ("" when p is complete).
// TRLC-LINKS: REQ-DSO-013
func (c *HALCycle) planRates(p SerialParams) (SerialParams, string) {
	s := &c.ser
	if p.Proto == SerSPI && p.SPIClockHz <= 0 {
		if !(s.spiHz > 0) || s.spiProbe {
			return p, SPIMeasuring
		}
		p.SPIClockHz = int(math.Round(s.spiHz))
	}
	if p.Proto == SerI2C && p.I2CClockHz <= 0 && s.sclHz > 0 {
		p.I2CClockHz = int(math.Round(s.sclHz))
	}
	if p.Proto == SerUART && p.Baud <= 0 {
		if !(s.baud > 0) {
			return p, "measuring the UART bit rate"
		}
		p.Baud = int(s.baud)
	}
	if p.Proto == SerSENT && (p.TickNs <= 0 || p.Nibbles <= 0) {
		if !(s.sentNs > 0) {
			return p, "measuring the SENT tick"
		}
		if p.TickNs <= 0 {
			p.TickNs = s.sentNs
		}
		if p.Nibbles <= 0 {
			p.Nibbles = s.sentNib
		}
	}
	p = JoinUnits(p)
	if p.Proto == SerARINC && len(PatternOf(p)) > 1 {
		return p, ArincOneWord
	}
	return p, ""
}

// arincSlicer is the slicer learned for request p, nil while none is.
// TRLC-LINKS: REQ-DSO-013, REQ-DSO-018
func (c *HALCycle) arincSlicer(p SerialParams) []float64 {
	if p.Proto != SerARINC || !c.ser.arincHave || !c.ser.arincFor.Equal(&c.cur.Serial) {
		return nil
	}
	return c.ser.arinc[:]
}

// view8Line is channel ch's codes of frame f on the 8-bit view, nil when f
// has no such trace.
// TRLC-LINKS: REQ-DSO-013
func view8Line(f *sample.Frame, ch int) []uint8 {
	v := sample.View8Frame(f)
	for i := range v.Traces {
		if t := &v.Traces[i]; t.Ch == ch && len(t.Codes8) >= t.Valid {
			return t.Codes8[:t.Valid]
		}
	}
	return nil
}
