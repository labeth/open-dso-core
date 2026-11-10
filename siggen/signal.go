// Package siggen is a signal generator: the voltage at a scope input as a
// function of time, for a simulated board and its tests. A signal is
// written as text, so a page, a test or a profile can set it:
//
//	kind,freq,vpp,offset[,key=value...]
//
// Waveforms (freq = repetition rate in Hz, vpp peak-to-peak, offset the mid
// level in V):
//
//	dc        offset only
//	sine      a sine wave
//	square    square, duty = high fraction (default 0.5)
//	pulse     pulse, width = high time in s (or duty; default duty 0.1)
//	triangle  triangle, duty = rising fraction (default 0.5)
//	ramp      sawtooth rising over the period (duty < 1 gives a falling flank of 1-duty)
//	noise     Gaussian noise, sigma = vpp/6 (freq ignored)
//
// Waveform options: duty=F, width=S, rise=S (10-90 % edge of square, pulse
// and protocol lines, linear), fall=S (default rise), noise=VRMS (Gaussian
// noise added), glitch=EVERY:WIDTH:AMP (a pulse of AMP volts and WIDTH s in
// the middle of the low part of every EVERY-th period), burst=ON:OFF (ON
// periods of signal, then OFF periods at the low or mid level),
// stretch=EVERY:S (a square's or pulse's high part S longer in every
// EVERY-th period: a width violation that adds no edge), inv=1 (invert).
//
// Protocol lines (freq = bit rate; vpp and offset place the logic levels:
// low = offset − vpp/2, high = offset + vpp/2): the frame repeats every
// frame + idle, starting at t = 0.
//
//	uart          TX line: data=HEX bits=5..9 parity=n|e|o|m|s stop=1|1.5|2 gap=S lsb=1 (default)
//	spi.clk|spi.mosi|spi.miso|spi.cs  mode=0..3 data=HEX (MOSI) miso=HEX bits=8 lsb=0 csgap=S
//	i2c.scl|i2c.sda  addr=0x50 rw=w|r data=HEX (a write sends data; a read returns it, NACKed last)
//	can, can.h, can.l  id=0x123 ext=0|1 data=HEX (≤ 8 bytes) rtr=0|1: classic CAN with stuffing
//	              and CRC-15, ACKed; can = logic RX (dominant low), can.h/can.l = the bus wires
//	              (recessive at offset, dominant at offset ± vpp/2)
//	lin           id=0x10 data=HEX checksum=enh|cls: break, sync, PID, data, checksum (LIN 2.x)
//	manchester    data=HEX: IEEE 802.3 convention (1 = rising mid-bit), MSB first, idle low, lsb=1
//	prbs|prbs.clk order=7: a maximal-length sequence as NRZ data and its bit clock
//
//	bus.PROTO.ROLE  a line of any bus the core decodes (synth.go)
//
// Common protocol option: idle=S (gap after the frame, default 20 bit
// times), data=HEX (pairs of hex digits, ':' allowed between bytes).
//
// Noise is a function of time and the signal's seed, not a random stream:
// the same signal sampled at the same instants reads the same values, so a
// stored record read twice is the same record.
package siggen

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// Signal is one input's signal.
type Signal struct {
	Kind   string  // see the package comment
	Freq   float64 // Hz (the frame repetition rate for protocol lines)
	Vpp    float64 // V peak-to-peak
	Offset float64 // V (mid level)

	Duty, Width, Rise, Fall, Noise float64
	GlitchEvery                    int
	GlitchW, GlitchA               float64
	StretchEvery                   int
	StretchS                       float64
	BurstOn, BurstOff              int
	Inv                            bool
	// BitRate is a protocol line's bit rate (0 for a waveform).
	BitRate float64
	// Seed makes this signal's noise independent of another's.
	Seed uint64

	spec string    // the text the signal was parsed from
	line *lineWave // a protocol line, or a square or pulse over one period
}

// lineWave is one line over one repetition period: level lv[i] (0 or 1)
// from t[i] to t[i+1] (t[0] = 0), the last level up to period.
type lineWave struct {
	t, lv  []float64
	period float64
}

// at returns the line's level at time t with linear edges of the given
// rise and fall times.
// TRLC-LINKS: REQ-DSO-298
func (w *lineWave) at(t, rise, fall float64) float64 {
	if w.period <= 0 || len(w.t) == 0 {
		return 0
	}
	t -= math.Floor(t/w.period) * w.period
	i := sort.SearchFloat64s(w.t, t)
	if i == len(w.t) || w.t[i] > t {
		i--
	}
	i = max(i, 0)
	v := w.lv[i]
	if rise <= 0 && fall <= 0 {
		return v
	}
	n := len(w.t)
	prev := w.lv[(i+n-1)%n]
	next := w.lv[(i+1)%n]
	tEnd := w.period
	if i+1 < n {
		tEnd = w.t[i+1]
	}
	// half of the edge before and after each transition
	edge := func(from, to float64) float64 {
		if to > from {
			return rise
		}
		return fall
	}
	if e := edge(prev, v); prev != v && e > 0 && t-w.t[i] < e/2 {
		return prev + (v-prev)*(0.5+(t-w.t[i])/e)
	}
	if e := edge(v, next); next != v && e > 0 && tEnd-t < e/2 {
		return v + (next-v)*(0.5-(tEnd-t)/e)
	}
	return v
}

// IsLine reports whether the signal is a protocol line.
// TRLC-LINKS: REQ-DSO-298
func (s Signal) IsLine() bool { return s.line != nil && s.BitRate > 0 }

// CounterFrequency returns the ideal mean same-polarity edge rate. Protocol
// lines can have many edges per packet; their packet repetition rate is not
// the frequency a gated hardware counter measures. Noise and comparator
// threshold effects are outside this ideal signal model.
// TRLC-LINKS: REQ-DSO-298
func (s Signal) CounterFrequency() float64 {
	if !s.IsLine() {
		return s.Freq
	}
	rises := 0
	for i, v := range s.line.lv {
		prev := s.line.lv[(i+len(s.line.lv)-1)%len(s.line.lv)]
		if v > prev {
			rises++
		}
	}
	return float64(rises) / s.line.period
}

// V returns the voltage at time t (s).
// TRLC-LINKS: REQ-DSO-298
func (s Signal) V(t float64) float64 {
	v := s.v(t)
	if s.Noise > 0 {
		v += gauss(s.Seed, t) * s.Noise
	}
	return v
}

// Sample fills dst with the signal at t0, t0+dt, t0+2·dt, ...
// TRLC-LINKS: REQ-DSO-298
func (s Signal) Sample(dst []float64, t0, dt float64) {
	for i := range dst {
		dst[i] = s.V(t0 + float64(i)*dt)
	}
}

// v is the signal without its added noise.
// TRLC-LINKS: REQ-DSO-298
func (s Signal) v(t float64) float64 {
	a := s.Vpp / 2
	if s.BitRate > 0 {
		lv := s.line.at(t, s.Rise, s.fall())
		if s.Inv {
			lv = 1 - lv
		}
		return s.Offset - a + 2*a*lv
	}
	if s.Kind == "noise" {
		return s.Offset + gauss(s.Seed, t)*s.Vpp/6
	}
	if s.Freq <= 0 || s.Kind == "dc" {
		return s.Offset
	}
	cyc := t * s.Freq
	k := math.Floor(cyc)
	ph := cyc - k
	if s.BurstOn > 0 && s.BurstOff > 0 {
		n := int64(k) % int64(s.BurstOn+s.BurstOff)
		if n < 0 {
			n += int64(s.BurstOn + s.BurstOff)
		}
		if n >= int64(s.BurstOn) {
			return s.rest()
		}
	}
	v := s.shape(ph)
	if s.StretchEvery > 0 && s.StretchS > 0 && (s.Kind == kindSquare || s.Kind == kindPulse) {
		n := int64(k) % int64(s.StretchEvery)
		if n < 0 {
			n += int64(s.StretchEvery)
		}
		if d := s.duty(); n == 0 && ph >= d && ph < d+s.StretchS*s.Freq {
			v = s.Offset + s.Vpp/2 // the high part, held on
		}
	}
	if s.Inv {
		v = 2*s.Offset - v
	}
	if s.GlitchEvery > 0 && s.GlitchW > 0 {
		n := int64(k) % int64(s.GlitchEvery)
		if n < 0 {
			n += int64(s.GlitchEvery)
		}
		at := s.glitchPhase()
		if n == 0 && ph >= at && ph < at+s.GlitchW*s.Freq {
			v += s.GlitchA
		}
	}
	return v
}

// fall is the falling edge's time (the rise's when not set).
// TRLC-LINKS: REQ-DSO-298
func (s Signal) fall() float64 {
	if s.Fall > 0 {
		return s.Fall
	}
	return s.Rise
}

// rest is the level between bursts.
// TRLC-LINKS: REQ-DSO-298
func (s Signal) rest() float64 {
	switch s.Kind {
	case kindSquare, kindPulse:
		if s.Inv {
			return s.Offset + s.Vpp/2
		}
		return s.Offset - s.Vpp/2
	}
	return s.Offset
}

// duty is the high (rising) fraction of the period.
// TRLC-LINKS: REQ-DSO-298
func (s Signal) duty() float64 {
	d := s.Duty
	if s.Kind == kindPulse && s.Width > 0 && s.Freq > 0 {
		d = s.Width * s.Freq
	}
	if d <= 0 {
		switch s.Kind {
		case kindPulse:
			d = 0.1
		case "ramp":
			d = 1
		default:
			d = 0.5
		}
	}
	return min(1, d)
}

// glitchPhase is where in the period a glitch starts: the middle of the
// low part.
// TRLC-LINKS: REQ-DSO-298
func (s Signal) glitchPhase() float64 {
	switch s.Kind {
	case kindSquare, kindPulse:
		d := s.duty()
		return d + (1-d)/2
	}
	return 0.25
}

// shape is one period of the waveform at phase ph (0 ≤ ph < 1).
// TRLC-LINKS: REQ-DSO-298
func (s Signal) shape(ph float64) float64 {
	a := s.Vpp / 2
	o := s.Offset
	d := s.duty()
	switch s.Kind {
	case "sine":
		return o + a*math.Sin(2*math.Pi*ph)
	case kindSquare, kindPulse:
		return o - a + 2*a*s.line.at(ph/s.Freq, s.Rise, s.fall())
	case "triangle":
		if d >= 1 {
			return o - a + 2*a*ph
		}
		if ph < d {
			return o - a + 2*a*ph/d
		}
		return o + a - 2*a*(ph-d)/(1-d)
	case "ramp":
		if d >= 1 || ph < d {
			return o - a + 2*a*ph/d
		}
		return o + a - 2*a*(ph-d)/(1-d)
	}
	return o
}

// String is the text the signal was set from.
// TRLC-LINKS: REQ-DSO-298
func (s Signal) String() string {
	if s.spec != "" {
		return s.spec
	}
	return fmt.Sprintf("%s,%g,%g,%g", s.Kind, s.Freq, s.Vpp, s.Offset)
}

// kindPulse is the pulse waveform's kind.
const kindPulse = "pulse"

// kindSquare is the square waveform's kind.
const kindSquare = "square"

// waveKinds are the waveform kinds; every other kind is a protocol line.
var waveKinds = map[string]bool{"dc": true, "sine": true, kindSquare: true, kindPulse: true, "triangle": true, "ramp": true, "noise": true}

// Parse parses one input's "kind,freq,vpp,offset[,key=value...]".
// TRLC-LINKS: REQ-DSO-298
func Parse(v string) (Signal, error) {
	f := strings.Split(strings.TrimSpace(v), ",")
	if len(f) < 4 {
		return Signal{}, errors.New("want kind,freq,vpp,offset[,key=value...]")
	}
	var n [3]float64
	for i := range n {
		x, err := strconv.ParseFloat(strings.TrimSpace(f[i+1]), 64)
		if err != nil || math.IsNaN(x) || math.IsInf(x, 0) {
			return Signal{}, fmt.Errorf("%q: not a number", f[i+1])
		}
		n[i] = x
	}
	s := Signal{Kind: strings.ToLower(strings.TrimSpace(f[0])), Freq: n[0], Vpp: n[1], Offset: n[2], spec: strings.TrimSpace(v)}
	if s.Freq < 0 || s.Freq > 10e9 {
		return Signal{}, fmt.Errorf("freq %g out of range 0..10 GHz", s.Freq)
	}
	if math.Abs(s.Vpp) > 1000 || math.Abs(s.Offset) > 1000 {
		return Signal{}, errors.New("vpp/offset beyond ±1000 V")
	}
	opts := map[string]string{}
	for _, kv := range f[4:] {
		kv = strings.TrimSpace(kv)
		if kv == "" {
			continue
		}
		k, val, ok := strings.Cut(kv, "=")
		if !ok {
			return Signal{}, fmt.Errorf("option %q: want key=value", kv)
		}
		opts[strings.ToLower(k)] = val
	}
	if err := s.waveOptions(opts); err != nil {
		return Signal{}, err
	}
	if waveKinds[s.Kind] {
		if len(opts) > 0 {
			return Signal{}, fmt.Errorf("%s: unknown options %v", s.Kind, keys(opts))
		}
		if s.Kind == kindSquare || s.Kind == kindPulse {
			s.line = edgeWave(s.duty(), s.Freq)
		}
		return s, nil
	}
	if synthKind(s.Kind) {
		w, period, step, err := buildSynthLine(s.Kind, opts)
		if err != nil {
			return Signal{}, err
		}
		s.line, s.BitRate, s.Freq = w, 1/step, 1/period
		return s, nil
	}
	w, period, err := buildLine(s.Kind, s.Freq, opts)
	if err != nil {
		return Signal{}, err
	}
	s.line = w
	s.BitRate = s.Freq
	// Freq records the frame repetition rate, not the wire edge rate.
	s.Freq = 1 / period
	switch s.Kind { // the bus wires: recessive (1) at offset, dominant (0) at offset ± vpp/2
	case "can.h":
		s.Inv = !s.Inv
		s.Offset += s.Vpp / 4
		s.Vpp /= 2
	case "can.l":
		s.Offset -= s.Vpp / 4
		s.Vpp /= 2
	}
	return s, nil
}

// waveOptions takes the options every kind has out of opts.
// TRLC-LINKS: REQ-DSO-298
func (s *Signal) waveOptions(opts map[string]string) error {
	num := func(k string, dst *float64) error {
		x, ok := opts[k]
		if !ok {
			return nil
		}
		delete(opts, k)
		y, err := strconv.ParseFloat(x, 64)
		if err != nil || math.IsNaN(y) || math.IsInf(y, 0) || y < 0 {
			return fmt.Errorf("%s=%q: want a number ≥ 0", k, x)
		}
		*dst = y
		return nil
	}
	for k, dst := range map[string]*float64{"duty": &s.Duty, "width": &s.Width, "rise": &s.Rise, "fall": &s.Fall, "noise": &s.Noise} {
		if err := num(k, dst); err != nil {
			return err
		}
	}
	if s.Duty > 1 {
		return fmt.Errorf("duty %g: want 0..1", s.Duty)
	}
	if g, ok := opts["glitch"]; ok {
		delete(opts, "glitch")
		p := strings.Split(g, ":")
		if len(p) != 3 {
			return fmt.Errorf("glitch=%q: want EVERY:WIDTH:AMP", g)
		}
		e, e1 := strconv.Atoi(p[0])
		w, e2 := strconv.ParseFloat(p[1], 64)
		am, e3 := strconv.ParseFloat(p[2], 64)
		if e1 != nil || e2 != nil || e3 != nil || e < 1 || w < 0 {
			return fmt.Errorf("glitch=%q: want EVERY≥1:WIDTH:AMP", g)
		}
		s.GlitchEvery, s.GlitchW, s.GlitchA = e, w, am
	}
	if g, ok := opts["stretch"]; ok {
		delete(opts, "stretch")
		every, ext, _ := strings.Cut(g, ":")
		e, e1 := strconv.Atoi(every)
		w, e2 := strconv.ParseFloat(ext, 64)
		if e1 != nil || e2 != nil || e < 1 || !(w >= 0) || math.IsInf(w, 0) {
			return fmt.Errorf("stretch=%q: want EVERY≥1:SECONDS", g)
		}
		s.StretchEvery, s.StretchS = e, w
	}
	if b, ok := opts["burst"]; ok {
		delete(opts, "burst")
		on, off, _ := strings.Cut(b, ":")
		x, e1 := strconv.Atoi(on)
		y, e2 := strconv.Atoi(off)
		if e1 != nil || e2 != nil || x < 0 || y < 0 {
			return fmt.Errorf("burst=%q: want ON:OFF periods", b)
		}
		s.BurstOn, s.BurstOff = x, y
	}
	if iv, ok := opts["inv"]; ok {
		delete(opts, "inv")
		s.Inv = iv == "1" || iv == "true"
	}
	return nil
}

// edgeWave is one period of a two-level waveform high for duty d.
// TRLC-LINKS: REQ-DSO-298
func edgeWave(d, freq float64) *lineWave {
	if freq <= 0 {
		return &lineWave{t: []float64{0}, lv: []float64{1}, period: 1}
	}
	T := 1 / freq
	if d >= 1 {
		return &lineWave{t: []float64{0}, lv: []float64{1}, period: T}
	}
	return &lineWave{t: []float64{0, d * T}, lv: []float64{1, 0}, period: T}
}

// keys is m's keys, sorted.
// TRLC-LINKS: REQ-DSO-298
func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// gauss is a standard normal value that depends only on seed and t: a
// counter-based generator (splitmix64 over the time's bits) through the
// Box-Muller transform.
// TRLC-LINKS: REQ-DSO-298
func gauss(seed uint64, t float64) float64 {
	h := mix(seed ^ math.Float64bits(t))
	u1 := (float64(h>>11) + 1) / (1 << 53) // (0, 1]
	u2 := float64(mix(h)>>11) / (1 << 53)  // [0, 1)
	return math.Sqrt(-2*math.Log(u1)) * math.Cos(2*math.Pi*u2)
}

// mix is splitmix64's finaliser.
// TRLC-LINKS: REQ-DSO-298
func mix(x uint64) uint64 {
	x += 0x9e3779b97f4a7c15
	x = (x ^ x>>30) * 0xbf58476d1ce4e5b9
	x = (x ^ x>>27) * 0x94d049bb133111eb
	return x ^ x>>31
}
