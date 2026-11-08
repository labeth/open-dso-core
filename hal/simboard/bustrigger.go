package simboard

import (
	"math"
	"time"

	dec "github.com/labeth/open-dso-core/decode"
	"github.com/labeth/open-dso-core/engine"
	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/hal/prototrig"
)

// The protocol trigger plays a scope's hardware receiver: it reads the
// bus's lines as they pass, sliced at the trigger's levels, through the
// core's decoder for the request, and fires where the request's match
// begins (a frame start, a unit, a sequence). A record never decides it:
// the lines are sampled for the trigger alone, finer than any record.

// busCode is a sliced line's two codes on the decoders' 8-bit view: the
// threshold the request slices at is busMid between them.
const (
	busLow  = 28
	busMid  = 128
	busHigh = 228
)

// The receiver's pacing: it reads at most busEvals samples of each line a
// look, a look every busPollEvery, so a fast bus with no match (a clock
// that never pauses) cannot take the thread the whole product shares in a
// browser; what passes meanwhile is missed, as a receiver re-arming misses
// it.
const (
	busEvals     = 32768
	busPollEvery = 20 * time.Millisecond
)

// busBack bounds how far before the scan point the receiver reads again,
// so a frame that began before it is read whole: one repetition of a
// generated bus, else busBackDefault.
const (
	busBackMin     = 100e-6
	busBackMax     = 200e-3
	busBackDefault = 20e-3
)

// busOf is the protocol trigger a stage asks for.
// TRLC-LINKS: REQ-DSO-013, REQ-DSO-1001
func busOf(p hal.ProtoTrigger) (*prototrig.Bus, bool) {
	switch b := p.(type) {
	case *prototrig.Bus:
		return b, b != nil
	case prototrig.Bus:
		return &b, true
	}
	return nil, false
}

// busBack is how far back the receiver reads for the bus on lines a, b.
// TRLC-LINKS: REQ-DSO-013
func busBack(a, b view) float64 {
	back := 0.0
	for _, v := range []view{a, b} {
		if v.sig.BitRate > 0 && v.sig.Freq > 0 {
			back = max(back, 1/v.sig.Freq)
		}
	}
	if back == 0 {
		return busBackDefault
	}
	return min(max(back, busBackMin), busBackMax)
}

// busStep is the receiver's sample interval: a sixteenth of a generated
// bus's bit or of the request's own rate, else the record's samples.
// TRLC-LINKS: REQ-DSO-013
func busStep(a, b view, p prototrig.SerialParams, sampleS float64) float64 {
	step := math.Inf(1)
	for _, v := range []view{a, b} {
		if v.sig.BitRate > 0 {
			step = min(step, 1/v.sig.BitRate/16)
		}
	}
	rates := []int{p.SPIClockHz, p.I2CClockHz}
	if p.Proto != engine.SerSPI && p.Proto != engine.SerI2C { // a clocked bus keeps a baud it does not use
		rates = append(rates, p.Baud)
	}
	for _, hz := range rates {
		if hz > 0 {
			step = min(step, 1/float64(hz)/16)
		}
	}
	if math.IsInf(step, 1) {
		step = sampleS
	}
	return max(step, 1e-12)
}

// scanBus looks for the armed protocol trigger from the scan point up to
// now: it decodes the lines from a frame's length before the scan point
// and fires on the first match beginning at or after it.
// TRLC-LINKS: REQ-DSO-013, REQ-DSO-1001
func (b *Board) scanBus(q *acquisition, bus *prototrig.Bus, now float64) {
	p := bus.Request
	chA, chB := p.ChA, p.ChB
	if chB < 0 || chB >= b.profile.Channels {
		chB = chA
	}
	va, vb := b.viewOf(chA, q.set.channels[chA]), b.viewOf(chB, q.set.channels[chB])
	va.rep, vb.rep = q.replay, q.replay
	back := busBack(va, vb)
	step := busStep(va, vb, p, q.set.plan.SampleS)
	from := q.scanFrom
	if now-from < 2*step {
		return
	}
	// a scan fallen far behind skips ahead, as a receiver misses what passes
	// while it is not armed
	if (now-from+back)/step > busEvals {
		from = now - busEvals*step + back
	}
	t0 := from - back
	n := int((now - t0) / step)
	if n < 32 {
		return
	}
	slice := func(v view, level float64, out []uint8) {
		for i := range out {
			if v.volts(t0+float64(i)*step) >= level {
				out[i] = busHigh
			} else {
				out[i] = busLow
			}
		}
	}
	a := make([]uint8, n)
	slice(va, bus.Levels[0], a)
	bb := a
	if chB != chA {
		bb = make([]uint8, n)
		slice(vb, bus.Levels[1], bb)
	}
	req := p
	req.HaveThr, req.Threshold, req.HaveThrB, req.ThresholdB = true, busMid, true, busMid
	lo := int(math.Ceil((from - t0) / step))
	if d, ok := engine.LookupSerialDecoder(req.Proto); ok {
		if res := d.Decode(a, bb, step, req); res.OK {
			spans := markWordGaps(res.Spans, req.Proto)
			if ok, at := engine.SerialMatchSpans(spans, req, engine.AnchorSpan{Target: lo, Lo: lo, Hi: n - 1}); ok && at >= lo {
				q.found, q.trigT = true, t0+float64(at)*step
				return
			}
		}
	}
	// nothing begins in the frame's length before now that is not still
	// being read: the next scan starts there
	q.scanFrom = max(from, now-back)
}

// busStage checks a protocol stage: a bus the core decodes, on the board's
// analog channels.
// TRLC-LINKS: REQ-DSO-013, REQ-DSO-1001
func (b *Board) busStage(st hal.TrigStage) error {
	bus, ok := busOf(st.Proto)
	if !ok {
		return hal.Refuse("protocol trigger", "the simulated board's receiver takes a decoded bus's request only")
	}
	p := bus.Request
	if _, ok := engine.LookupSerialDecoder(p.Proto); !ok {
		return hal.Refuse("protocol trigger", "the core has no decoder for this bus")
	}
	if p.ChA < 0 || p.ChA >= b.profile.Channels {
		return hal.Refuse("trigger source", "the simulated board triggers on its analog channels only")
	}
	return nil
}

// wordGap is the idle, in a word's clocks, after which an SPI receiver
// framing by the clock alone (no chip select) starts a new frame: a
// scope's matcher ends a word after a pause of 1.5 clock periods; the
// decoder's word spans end and begin at sampling edges, a clock and a
// half apart inside a frame, so the pause between frames is looked for
// past two.
const wordGap = 2.0

// spanFrameStart is the decoders' span kind of a frame's opening.
const spanFrameStart = "start"

// markWordGaps marks a frame start before each SPI word that follows an idle clock
// (and before the first), as the receiver frames words: the decoder marks
// no frame start on a bus with no chip select. Other buses' spans are
// returned as they are.
// TRLC-LINKS: REQ-DSO-013
func markWordGaps(spans []dec.Span, proto int) []dec.Span {
	if proto != engine.SerSPI {
		return spans
	}
	out := make([]dec.Span, 0, len(spans)+len(spans)/2)
	prevEnd := -1
	for _, sp := range spans {
		if sp.Kind == engine.SpanGap {
			prevEnd = -1
		}
		if sp.Kind == engine.SpanData {
			clock := float64(sp.I1-sp.I0) / 8
			if prevEnd < 0 || float64(sp.I0-prevEnd) > wordGap*clock {
				out = append(out, dec.Span{I0: sp.I0, I1: sp.I0, Kind: spanFrameStart, Text: "SOF"})
			}
			prevEnd = sp.I1
		}
		out = append(out, sp)
	}
	return out
}
