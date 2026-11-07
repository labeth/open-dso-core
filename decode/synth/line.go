// ENGMODEL-OWNER-UNIT: FU-DSO-DECODE

// Package synth lays out the buses the decoders read as logic lines: each
// protocol's frames written as levels over time, spec-correct, with the
// result a decoder must read from them. The decoders' own tests (round
// trip, break, parity, oracle, detection and the AUTO fuzzing) build their
// captures here, and so can anything that needs a bus on a wire: a
// simulated scope's signal generator, a demonstration, a bench source.
//
// A Line is written in order (Add a level for a duration) and keeps both
// views of what was written: the level changes in continuous time (Steps,
// for a signal model that samples at any rate or shapes the edges) and,
// when made with a sample rate, the samples themselves (Bits), cut where a
// real capture would cut them, so a bit of a non-integer number of samples
// accumulates as it does on a scope.
//
// The package depends on no decoder: its CRCs, parities and tables are its
// own, written from the protocols' specifications, so a decoder checked
// against it is checked against an independent writer.
package synth

// Logic levels as two-level ADC codes (Codes): well apart around the
// decoders' default threshold of 128.
const (
	CodeLo = uint8(56)  // logic 0
	CodeHi = uint8(200) // logic 1
	// CodeMid is an idle channel's code: no edges, mid-scale.
	CodeMid = uint8(128)
)

// Step is a level change: the line holds Level from At (seconds from the
// line's start) until the next step.
type Step struct {
	At    float64
	Level byte
}

// Line is one logic line written in time order.
type Line struct {
	SR    float64 // samples per second; 0: no samples, the steps only
	T     float64 // the end of what was written, seconds
	Bits  []byte  // the samples at SR (0 or 1)
	Steps []Step  // the level changes, the first at 0
	// Levels is the number of levels the line takes: 0 or 2 for a logic
	// line (0 low, 1 high), 3 for a bipolar one (0 low, 1 null, 2 high:
	// ARINC 429).
	Levels int
}

// NewLine starts a line sampled at sr (0: steps only).
// TRLC-LINKS: REQ-DSO-018
func NewLine(sr float64) *Line { return &Line{SR: sr} }

// Add holds level for d seconds.
// TRLC-LINKS: REQ-DSO-018
func (w *Line) Add(level byte, d float64) {
	if n := len(w.Steps); d > 0 && (n == 0 || w.Steps[n-1].Level != level) {
		w.Steps = append(w.Steps, Step{At: w.T, Level: level})
	}
	w.T += d
	if w.SR > 0 {
		for len(w.Bits) < int(w.T*w.SR) {
			w.Bits = append(w.Bits, level)
		}
	}
}

// Idle holds the line high (a UART's and most buses' idle) for d seconds.
// TRLC-LINKS: REQ-DSO-018
func (w *Line) Idle(d float64) { w.Add(1, d) }

// Last is the level the line ends at: its last sample when sampled (0
// before the first), else its last step's.
// TRLC-LINKS: REQ-DSO-018
func (w *Line) Last() byte {
	if w.SR > 0 {
		if k := len(w.Bits); k > 0 {
			return w.Bits[k-1]
		}
		return 0
	}
	if k := len(w.Steps); k > 0 {
		return w.Steps[k-1].Level
	}
	return 0
}

// Level is the line's level at t seconds (the first level before 0, the
// last after T).
// TRLC-LINKS: REQ-DSO-018
func (w *Line) Level(t float64) byte {
	lo, hi := 0, len(w.Steps)
	if hi == 0 {
		return 0
	}
	for lo+1 < hi {
		m := (lo + hi) / 2
		if w.Steps[m].At <= t {
			lo = m
		} else {
			hi = m
		}
	}
	return w.Steps[lo].Level
}

// Codes is the line's samples as two-level ADC codes.
// TRLC-LINKS: REQ-DSO-018
func (w *Line) Codes() []uint8 {
	if w.Levels == 3 {
		out := make([]uint8, len(w.Bits))
		for i, b := range w.Bits {
			out[i] = [3]uint8{CodeLo, CodeMid, CodeHi}[min(b, 2)]
		}
		return out
	}
	return BitsToCodes(w.Bits)
}

// Fraction is a level's place between the line's lowest (0) and highest
// (1): what a signal model scales to its swing.
// TRLC-LINKS: REQ-DSO-018
func (w *Line) Fraction(level byte) float64 {
	if w.Levels > 2 {
		return float64(level) / float64(w.Levels-1)
	}
	return float64(min(level, 1))
}

// BitsToCodes maps logic samples to CodeLo and CodeHi.
// TRLC-LINKS: REQ-DSO-018
func BitsToCodes(bits []byte) []uint8 {
	out := make([]uint8, len(bits))
	for i, b := range bits {
		if b != 0 {
			out[i] = CodeHi
		} else {
			out[i] = CodeLo
		}
	}
	return out
}

// Pair is two lines written in step: a clock and its data, or the two
// wires of a two-wire bus.
type Pair struct{ A, B *Line }

// NewPair starts two lines sampled at sr.
// TRLC-LINKS: REQ-DSO-018
func NewPair(sr float64) Pair { return Pair{NewLine(sr), NewLine(sr)} }

// Add holds la on A and lb on B for d seconds.
// TRLC-LINKS: REQ-DSO-018
func (p Pair) Add(la, lb byte, d float64) {
	p.A.Add(la, d)
	p.B.Add(lb, d)
}

// lvl is bit 0 of b as a level.
func lvl(b int) byte {
	if b&1 != 0 {
		return 1
	}
	return 0
}
