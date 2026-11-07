package panel

import (
	"math"

	"github.com/labeth/open-dso-core/decode"
	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/instrument"
)

// The tests drive the UI model over a two-channel 8-bit board whose levels
// are DAC codes and whose front end has a twelve-step V/div ladder, with
// the laws and ladders of the SDS1000CML+ (its numbers are what the tests
// were written against). These are test doubles of the board, not the
// board: the scope's own package proves its implementation.

// testTdivs is the timebase ladder, 1 ns to 50 s a division.
var testTdivs = []float64{
	1e-09, 2e-09, 5e-09, 1e-08, 2.5e-08, 5e-08, 1e-07, 2e-07, 5e-07, 1e-06, 2e-06, 5e-06, 1e-05, 2e-05,
	5e-05, 0.0001, 0.0002, 0.0005, 0.001, 0.002, 0.005, 0.01, 0.02, 0.05, 0.1, 0.2, 0.5, 1, 2, 5, 10, 20, 50,
}

// testCaptureNs is the register path's capture interval at each step of
// testTdivs.
var testCaptureNs = []float64{
	5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 10, 10, 10, 40, 80, 200, 400, 800, 2000, 4000, 230410, 229890, 229890,
	244140, 370000, 370000, 370000, 370000, 370000, 370000, 370000, 370000, 370000,
}

// testBand is a timebase step as the board resolved it.
type testBand struct{ TdivS, capNs float64 }

// CaptureIntervalNs is the interval the band captures at.
// TRLC-LINKS: REQ-DSO-010
func (b testBand) CaptureIntervalNs() float64 { return b.capNs }

// Timebase is the band as the controls report it.
// TRLC-LINKS: REQ-DSO-010
func (b testBand) Timebase() instrument.Timebase {
	return instrument.Timebase{TdivS: b.TdivS, SampleS: b.capNs * 1e-9}
}

// planTdiv is the ladder's step at t (1e-6 relative).
// TRLC-LINKS: REQ-DSO-010
func planTdiv(t float64) (testBand, bool) {
	for i, s := range testTdivs {
		if math.Abs(s-t) <= s*1e-6 {
			return testBand{s, testCaptureNs[i]}, true
		}
	}
	return testBand{}, false
}

// The trigger comparator's DAC: its codes, and the global fit code =
// trigZeroFit - trigCPVFit·V.
const (
	trigCodeMin = 1
	trigCodeMax = 65535
	trigZeroFit = 31437.0
	trigCPVFit  = 911.0
)

// trigLevelVolts is the volts a level code holds on the global fit.
// TRLC-LINKS: REQ-DSO-098
func trigLevelVolts(code uint16) float64 { return (trigZeroFit - float64(code)) / trigCPVFit }

// detent is one V/div step of the front end's ladder: the volts a division
// shown, the attenuated range, and the display zoom of the steps under the
// 10 mV range.
type detent struct {
	VdivV float64
	Atten bool
	Zoom  int
}

// detents is the front end's ladder.
var detents = []detent{
	{0.002, false, 5}, {0.005, false, 2}, {0.010, false, 1}, {0.020, false, 1}, {0.050, false, 1}, {0.100, false, 1},
	{0.200, false, 1}, {0.500, true, 1}, {1.0, true, 1}, {2.0, true, 1}, {5.0, true, 1}, {10.0, true, 1},
}

// detentRange is the V/div of the analog range a step's codes are on.
// TRLC-LINKS: REQ-DSO-015
func detentRange(i int) float64 { return detents[i].VdivV * float64(detents[i].Zoom) }

// planVdiv is the ladder's step at v (1e-6 relative).
// TRLC-LINKS: REQ-DSO-015
func planVdiv(v float64) (int, bool) {
	for i, d := range detents {
		if math.Abs(d.VdivV-v) <= d.VdivV*1e-6 {
			return i, true
		}
	}
	return 0, false
}

// The offset DAC's law without a front end: 100 codes a volt about the
// boot zero on the boot step's ±40 V tier.
const (
	offsetZeroFallback = 10223.0
	offsetCodesPerVolt = 100.0
	offsetTierV        = 40.0
)

// boardEngine is what the levels stage: DAC codes, and the state.
type boardEngine interface {
	Snapshot() instrument.Stats
	SetTrigLevelCode(code uint16) uint16
	SetOffsetDAC(ch int, code uint16)
}

// boardFE is the front end's calibration of the comparator and the offsets.
type boardFE interface {
	TrigCode(volts float64, srcCh int) float64
	TrigVolts(code uint16, srcCh int) float64
	SetOffset(ch int, volts float64) uint16
	OffsetVolts(ch int, code uint16) float64
}

// boardLevels are the board's levels: volts to DAC codes and back.
type boardLevels struct {
	eng boardEngine
	fe  boardFE // nil: the global fits
}

var _ Levels = (*boardLevels)(nil)

// newBoardLevels returns the levels over eng and fe; fe may be nil.
// TRLC-LINKS: REQ-DSO-098
func newBoardLevels(eng boardEngine, fe boardFE) *boardLevels { return &boardLevels{eng: eng, fe: fe} }

// TRLC-LINKS: REQ-DSO-098
func (l *boardLevels) trigCodeOf(v float64, src int) float64 {
	if l.fe != nil {
		return l.fe.TrigCode(v, src)
	}
	return trigZeroFit - trigCPVFit*v
}

// TRLC-LINKS: REQ-DSO-098
func (l *boardLevels) trigVoltsOf(c uint16, src int) float64 {
	if l.fe != nil {
		return l.fe.TrigVolts(c, src)
	}
	return trigLevelVolts(c)
}

// TRLC-LINKS: REQ-DSO-098
func clampCode(code float64) uint16 {
	return uint16(min(max(math.Round(code), trigCodeMin), trigCodeMax))
}

// TRLC-LINKS: REQ-DSO-098
func (l *boardLevels) SetTrigLevel(v float64, src int) float64 {
	return l.trigVoltsOf(l.eng.SetTrigLevelCode(clampCode(l.trigCodeOf(v, src))), src)
}

// TRLC-LINKS: REQ-DSO-098
func (l *boardLevels) TrigLevelIn(st instrument.Stats, src int) (float64, bool) {
	if st.TrigCode == 0 {
		return 0, false
	}
	return l.trigVoltsOf(st.TrigCode, src), true
}

// TRLC-LINKS: REQ-DSO-098
func (l *boardLevels) TrigLevelOrIn(st instrument.Stats, v float64, src int) float64 {
	if lv, ok := l.TrigLevelIn(st, src); ok {
		return lv
	}
	return l.trigVoltsOf(clampCode(l.trigCodeOf(v, src)), src)
}

// TRLC-LINKS: REQ-DSO-098
func (l *boardLevels) TrigStepsV(n int) float64 { return float64(n) / trigCPVFit }

// TRLC-LINKS: REQ-DSO-098
func (l *boardLevels) StepTrigLevel(n, src int) float64 {
	c := l.eng.Snapshot().TrigCode
	if c == 0 {
		c = clampCode(trigZeroFit)
	}
	nc := min(max(int(c)-n, trigCodeMin), trigCodeMax)
	return l.trigVoltsOf(l.eng.SetTrigLevelCode(uint16(nc)), src) //nolint:gosec // G115: clamped to the DAC's range
}

// TRLC-LINKS: REQ-DSO-098
func (l *boardLevels) TrigLevelGrid(src int) hal.Grid {
	lo, hi := l.trigVoltsOf(trigCodeMax, src), l.trigVoltsOf(trigCodeMin, src)
	return hal.Grid{Step: (hi - lo) / float64(trigCodeMax-trigCodeMin), Min: lo, Max: hi}
}

// TRLC-LINKS: REQ-DSO-098
func (l *boardLevels) SaveTrigLevel() instrument.TrigLevelSaved {
	return l.SaveTrigLevelIn(l.eng.Snapshot())
}

// TRLC-LINKS: REQ-DSO-098
func (l *boardLevels) SaveTrigLevelIn(st instrument.Stats) instrument.TrigLevelSaved {
	return instrument.TrigLevelSaved{Word: uint64(st.TrigCode)}
}

// TRLC-LINKS: REQ-DSO-098
func (l *boardLevels) RestoreTrigLevel(s instrument.TrigLevelSaved) {
	if s.Set() {
		l.eng.SetTrigLevelCode(uint16(s.Word)) //nolint:gosec // G115: the code SaveTrigLevelIn kept
	}
}

// TRLC-LINKS: REQ-DSO-098
func (l *boardLevels) ResetTrigLevel() { l.eng.SetTrigLevelCode(uint16(l.trigCodeOf(0, 0))) }

// TRLC-LINKS: REQ-DSO-098
func (l *boardLevels) LevelWordV(word int) float64 {
	return trigLevelVolts(uint16(min(max(word, 1), 65535))) //nolint:gosec // G115: clamped to 1..65535
}

// TRLC-LINKS: REQ-DSO-095
func (l *boardLevels) SetOffset(ch int, v float64) float64 {
	if l.fe != nil {
		return l.fe.OffsetVolts(ch, l.fe.SetOffset(ch, v))
	}
	code := math.Round(offsetZeroFallback - offsetCodesPerVolt*v)
	code = min(max(code, offsetZeroFallback-offsetCodesPerVolt*offsetTierV), offsetZeroFallback+offsetCodesPerVolt*offsetTierV)
	c := uint16(min(max(code, 0), 0xFFFF))
	l.eng.SetOffsetDAC(ch, c)
	return (offsetZeroFallback - float64(c)) / offsetCodesPerVolt
}

// TRLC-LINKS: REQ-DSO-095
func (l *boardLevels) OffsetIn(ch int, st instrument.Stats) float64 {
	code := st.OffC1
	if ch == 1 {
		code = st.OffC2
	}
	if code == 0 {
		return 0
	}
	if l.fe != nil {
		return l.fe.OffsetVolts(ch, code)
	}
	return (offsetZeroFallback - float64(code)) / offsetCodesPerVolt
}

// TRLC-LINKS: REQ-DSO-095
func (l *boardLevels) OffsetStagedIn(ch int, st instrument.Stats) float64 {
	if l.fe == nil {
		return 0
	}
	code := st.OffC1
	if ch == 1 {
		code = st.OffC2
	}
	return l.fe.OffsetVolts(ch, code)
}

// testHW is the board's acquisition facts: a 1 Mi-sample record at 2 ns a
// sample, decimated by powers of two from 32 ns when ten divisions do not
// fit; rolling from 10 ms a division.
type testHW struct{ product string }

// TRLC-LINKS: REQ-DSO-214
func (testHW) Record(tdiv float64) (float64, int) {
	samples, sampleS := float64(1<<20), 2e-9
	if !(tdiv > 0) || math.IsInf(tdiv, 0) {
		tdiv = 500e-6
	}
	if 10*tdiv <= samples*sampleS {
		return sampleS, 0
	}
	log, samples, sampleS := 4, float64(1<<19), 32e-9
	for log < 20 && 10*tdiv > samples*sampleS {
		log++
		sampleS *= 2
	}
	return sampleS, log
}

// TRLC-LINKS: REQ-DSO-214
func (testHW) RollMinTdivS() float64 { return 10e-3 }

// TRLC-LINKS: REQ-DSO-214
func (testHW) SearchSamples() int { return 32768 }

// TRLC-LINKS: REQ-DSO-214
func (testHW) EnvelopePoints() int { return 2048 }

// TRLC-LINKS: REQ-DSO-018
func (testHW) StreamMode(int, int, bool) string { return "" }

// TRLC-LINKS: REQ-DSO-212
func (h testHW) Product() string {
	if h.product != "" {
		return h.product
	}
	return "open-dso test"
}

// spiReceiverDelay is where the board's SPI receiver on the samples reads
// the data line for a delay of ns asked: sixteenths of a nanosecond from
// the clock channel's phase, within the delay register.
// TRLC-LINKS: REQ-DSO-229
func spiReceiverDelay(ns float64, clockCh int) float64 {
	phase := decode.ChannelPhase16
	if clockCh == 0 {
		phase = -decode.ChannelPhase16
	}
	d16 := max(-128, min(127, phase+int(math.Round(ns*16))))
	return float64(d16-phase) / 16
}
