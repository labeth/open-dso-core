// ENGMODEL-OWNER-UNIT: FU-DSO-ENGINE

package engine

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/labeth/open-dso-core/instrument"
	"github.com/labeth/open-dso-core/sample"
)

// The Bode mode states (SetBodeMode), and the accumulation limits.
const (
	BodeOff = 0 // frames are not measured for the frequency response
	BodeOn  = 1 // every frame adds a point to the frequency response

	BodeBinsPerDecade = 30   // log-frequency accumulation resolution
	BodeMaxPoints     = 4096 // hard cap on the accumulated set
	BodeMinCycles     = 4.0  // need at least this many cycles in the record
)

// BodePoint is one accumulated transfer-function sample.
// TRLC-LINKS: REQ-DSO-066
type BodePoint struct {
	FreqHz   float64
	GainDB   float64
	PhaseDeg float64
	Seq      uint64 // capture ordinal of the frame that set this bin (freshness)
}

// BodeState is the frequency-response sweep's accumulated points and the
// channels it compares.
// TRLC-LINKS: REQ-DSO-066
type BodeState struct {
	Mu    sync.Mutex
	RefCh int // the DUT input reference's channel, 0 = C1
	DutCh int // the DUT output's channel
	Bins  map[int]BodePoint
	Cap   uint64 // capture ordinal

	// last live point (for the status readout), guarded by mu
	Live      BodePoint
	LiveValid bool
	Why       string            // why the last capture gave no point ("" when it did)
	Scale     func(int) float64 // input-referred volts/div, including probe factor
}

// FundamentalHz estimates the fundamental frequency of a periodic record by
// mean-level crossing spacing (works for square or sine stimuli; no FFT). The
// single-bin DFT that follows only needs this as an f0 estimate — the gain/
// phase come from the X2/X1 ratio, which is robust to a small f0 error.
// TRLC-LINKS: REQ-DSO-066
func FundamentalHz(sig []uint8, sampleS float64) float64 {
	n := len(sig)
	if n < 8 {
		return 0
	}
	sum := 0 // an integer sum: the float one was exact, every step
	for _, v := range sig {
		sum += int(v)
	}
	mean := float64(sum) / float64(n)
	// rising crossings of the mean with a small hysteresis to reject noise
	const hyst = 2.0
	// codes are integers: v < m is v < ⌈m⌉, v >= m is v >= ⌈m⌉
	armBelow, at := int(math.Ceil(mean-hyst)), int(math.Ceil(mean))
	var firstX, lastX float64
	crossings := 0
	armed := false
	firstX, lastX = -1, -1
	for i := 1; i < n; i++ {
		ai, bi := int(sig[i-1]), int(sig[i])
		if !armed && bi < armBelow {
			armed = true
		}
		if armed && ai < at && bi >= at {
			a, b := float64(ai), float64(bi)
			// linear-interpolated crossing position
			x := float64(i-1) + (mean-a)/(b-a)
			if firstX < 0 {
				firstX = x
			}
			lastX = x
			crossings++
			armed = false
		}
	}
	if crossings < 2 || lastX <= firstX {
		return 0
	}
	periodSamples := (lastX - firstX) / float64(crossings-1)
	if periodSamples <= 1 {
		return 0
	}
	return 1.0 / (periodSamples * sampleS)
}

// SingleBinDFT returns the real/imag parts of the DFT coefficient at frequency
// f0: Σ (v[n]−mean)·exp(−j·2π·f0·n·dt). Mean-subtracted so the DC term does not
// leak into the fundamental.
// TRLC-LINKS: REQ-DSO-066
func SingleBinDFT(sig []uint8, f0, sampleS float64) (re, im float64) {
	n := len(sig)
	var sum float64
	for _, v := range sig {
		sum += float64(v)
	}
	mean := sum / float64(n)
	w := 2 * math.Pi * f0 * sampleS // radians per sample
	// incremental cos/sin via rotation to avoid a trig call per sample
	c, s := math.Cos(w), math.Sin(w)
	cosk, sink := 1.0, 0.0
	for i := range n {
		v := float64(sig[i]) - mean
		re += v * cosk
		im -= v * sink
		cosk, sink = cosk*c-sink*s, sink*c+cosk*s
	}
	return re, im
}

// SetChannels sets the reference and DUT channels. A fresh pair does not
// clear the accumulated curve (the operator may re-arm mid-sweep).
// TRLC-LINKS: REQ-DSO-066
func (b *BodeState) SetChannels(refCh, dutCh int) {
	b.Mu.Lock()
	b.RefCh, b.DutCh = refCh, dutCh
	if b.Bins == nil {
		b.Bins = make(map[int]BodePoint)
	}
	b.Mu.Unlock()
}

// Clear empties the accumulated curve.
// TRLC-LINKS: REQ-DSO-066
func (b *BodeState) Clear() {
	b.Mu.Lock()
	b.Bins = make(map[int]BodePoint)
	b.LiveValid = false
	b.Mu.Unlock()
}

// Points returns the accumulated curve sorted by frequency.
// TRLC-LINKS: REQ-DSO-066
func (b *BodeState) Points() []BodePoint {
	b.Mu.Lock()
	defer b.Mu.Unlock()
	out := make([]BodePoint, 0, len(b.Bins))
	for _, p := range b.Bins {
		out = append(out, p)
	}
	slices.SortFunc(out, func(a, b BodePoint) int { return cmp.Compare(a.FreqHz, b.FreqHz) })
	return out
}

// Invalidate marks the live point stale, saying why the capture gave none.
// TRLC-LINKS: REQ-DSO-066
func (b *BodeState) Invalidate(why string) {
	b.Mu.Lock()
	b.LiveValid, b.Why = false, why
	b.Mu.Unlock()
}

// Eval computes the transfer-function point of frame f's first valid
// samples (its 8-bit view, any channels) and, if it is one, updates its
// log-frequency bin. vdiv is each channel's input-referred volts a division
// when Scale is unset; a channel's codes a division are its trace's. One
// point a locked frame: the fundamental from the reference, the magnitude
// ratio in dB and the phase difference in degrees from a single-bin DFT.
// TRLC-LINKS: REQ-DSO-066
func (b *BodeState) Eval(f *sample.Frame, valid int, sampleS float64, vdiv func(int) float64) {
	if sampleS <= 0 || valid < 8 {
		b.Invalidate("no samples")
		return
	}
	b.Mu.Lock()
	refCh, dutCh, scale := b.RefCh, b.DutCh, b.Scale
	b.Mu.Unlock()
	ref, dut := f.Codes8(refCh), f.Codes8(dutCh)
	if len(ref) < valid || len(dut) < valid {
		return
	}
	ref, dut = ref[:valid], dut[:valid]

	noTone := fmt.Sprintf("no tone on the reference C%d", refCh+1)
	f0 := FundamentalHz(ref, sampleS)
	if f0 <= 0 {
		b.Invalidate(noTone)
		return
	}
	cyc := f0 * float64(valid) * sampleS
	if cyc < BodeMinCycles {
		b.Invalidate(fmt.Sprintf("%.1f cycles on screen, %g needed: S/DIV slower", cyc, BodeMinCycles))
		return
	}
	if f0 >= 0.5/sampleS {
		b.Invalidate("the tone is too fast for this sample rate: S/DIV faster")
		return
	}
	re1, im1 := SingleBinDFT(ref, f0, sampleS)
	re2, im2 := SingleBinDFT(dut, f0, sampleS)
	mag1 := math.Hypot(re1, im1)
	mag2 := math.Hypot(re2, im2)
	// noise floor: the reference must carry real signal (its DFT bin well above
	// the per-sample quantisation noise scaled by record length).
	floor := 0.5 * math.Sqrt(float64(valid))
	if mag1 < floor {
		b.Invalidate(noTone)
		return
	}
	var refScale, dutScale float64
	switch {
	case scale != nil:
		refScale, dutScale = scale(refCh), scale(dutCh)
	case vdiv != nil:
		refScale, dutScale = vdiv(refCh), vdiv(dutCh)
	}
	if !(refScale > 0) {
		refScale = 1
	}
	if !(dutScale > 0) {
		dutScale = 1
	}
	// volts a code: a channel's scale over its trace's codes a division
	// (the same on every channel of a scope, so a two-channel 8-bit scope's
	// ratio is its scales' alone)
	if rc, dc := traceCPD(f, refCh), traceCPD(f, dutCh); rc > 0 && dc > 0 && rc != dc {
		refScale, dutScale = refScale/rc, dutScale/dc
	}
	gainDB := 20 * math.Log10((mag2*dutScale)/(mag1*refScale))
	if math.IsInf(gainDB, 0) || math.IsNaN(gainDB) {
		b.Invalidate(fmt.Sprintf("no output on the DUT C%d", dutCh+1))
		return
	}
	// phase of X2·conj(X1) = arg(X2) − arg(X1), wrapped to (−180, 180]
	pr := re2*re1 + im2*im1
	pi := im2*re1 - re2*im1
	phaseDeg := math.Atan2(pi, pr) * 180 / math.Pi

	p := BodePoint{FreqHz: f0, GainDB: gainDB, PhaseDeg: phaseDeg}
	b.Mu.Lock()
	p.Seq = b.Cap + 1
	b.Cap = p.Seq
	if b.Bins == nil {
		b.Bins = make(map[int]BodePoint)
	}
	bin := int(math.Round(math.Log10(f0) * BodeBinsPerDecade))
	if _, ok := b.Bins[bin]; len(b.Bins) < BodeMaxPoints || ok {
		b.Bins[bin] = p
	}
	b.Live, b.LiveValid, b.Why = p, true, ""
	b.Mu.Unlock()
}

// traceCPD is channel ch's trace's codes a division in f, 0 for none stated.
// TRLC-LINKS: REQ-DSO-066
func traceCPD(f *sample.Frame, ch int) float64 {
	if ch < 0 || ch >= len(f.Traces) {
		return 0
	}
	return f.Traces[ch].CodesPerDiv
}

// Fill writes the sweep's channels, curve size and live point into st.
// TRLC-LINKS: REQ-DSO-066
func (b *BodeState) Fill(st *instrument.Stats) {
	b.Mu.Lock()
	defer b.Mu.Unlock()
	st.BodeRef, st.BodeDut = b.RefCh, b.DutCh
	st.BodePoints = len(b.Bins)
	st.BodeValid = b.LiveValid
	if b.LiveValid {
		st.BodeFreqHz = b.Live.FreqHz
		st.BodeGainDB = b.Live.GainDB
		st.BodePhaseDeg = b.Live.PhaseDeg
	}
	st.BodeWhy = b.Why
}

// Bode is a frequency-response sweep the owner loop runs on its captures
// (HALConfig.Bode): armed or not, and the accumulated state.
type Bode struct {
	BodeState
	on atomic.Bool
}

// NewBode is a sweep with its reference on C1 and its DUT on C2.
// TRLC-LINKS: REQ-DSO-066
func NewBode() *Bode {
	b := &Bode{}
	b.SetChannels(0, 1)
	return b
}

// SetBodeMode arms or disarms the sweep and sets its channels.
// TRLC-LINKS: REQ-DSO-066
func (b *Bode) SetBodeMode(on bool, refCh, dutCh int) {
	b.SetChannels(refCh, dutCh)
	b.on.Store(on)
}

// ClearBode empties the accumulated curve.
// TRLC-LINKS: REQ-DSO-066
func (b *Bode) ClearBode() { b.Clear() }

// BodePoints returns the accumulated curve sorted by frequency.
// TRLC-LINKS: REQ-DSO-066
func (b *Bode) BodePoints() []BodePoint { return b.Points() }

// Armed reports whether the sweep takes frames.
// TRLC-LINKS: REQ-DSO-066
func (b *Bode) Armed() bool { return b.on.Load() }

// FillStats writes the sweep's mode, channels and live point into st.
// TRLC-LINKS: REQ-DSO-066
func (b *Bode) FillStats(st *instrument.Stats) {
	if b.on.Load() {
		st.BodeMode = BodeOn
	}
	b.Fill(st)
}

// Frame measures a capture (its 8-bit view) while armed, triggered or
// not: its gain and phase are the ratio of two channels sampled together,
// and asking for a trigger would stop the sweep, unsaid, with the level off
// the signal. An envelope or a roll gives no point.
// TRLC-LINKS: REQ-DSO-066
func (b *Bode) Frame(f *sample.Frame, vdiv func(int) float64) {
	if !b.on.Load() || f == nil || f.Flags.Has(sample.Envelope) || f.Content == sample.PeakPairs || f.Flags.Has(sample.Roll) {
		return
	}
	valid := min(f.Len(), f.Codes8Len())
	b.Mu.Lock()
	refCh, dutCh := b.RefCh, b.DutCh
	b.Mu.Unlock()
	for _, ch := range [2]int{refCh, dutCh} {
		if len(f.Codes8(ch)) < valid {
			b.Invalidate(fmt.Sprintf("no trace on C%d: switch it on", ch+1))
			return
		}
	}
	b.Eval(f, valid, f.SampleS, vdiv)
}
