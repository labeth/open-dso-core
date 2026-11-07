// ENGMODEL-OWNER-UNIT: FU-DSO-PANEL

package panel

import (
	"testing"

	"github.com/labeth/open-dso-core/instrument"

	"github.com/labeth/open-dso-core/engine"
	"github.com/labeth/open-dso-core/sample"
)

// fireEng: AcqLog reports SawTrig only while the committed level code sits
// inside [fireLo, fireHi] — a comparator with a band the volts fit misses.
// TRLC-LINKS: REQ-DSO-137
type fireEng struct {
	fakeEng
	level          uint16
	fireLo, fireHi uint16
}

// TRLC-LINKS: REQ-DSO-137
func (f *fireEng) SetTrigLevelCode(c uint16) uint16 { f.level = c; return c }

// TRLC-LINKS: REQ-DSO-137
func (f *fireEng) AcqLog(int) ([]engine.AcqSample, float64) {
	fired := f.level >= f.fireLo && f.level <= f.fireHi
	return []engine.AcqSample{{SawTrig: fired}, {SawTrig: fired}, {SawTrig: fired}}, 0
}

// The computed code misses the band → the scan must land the level inside it.
// TRLC-LINKS: REQ-DSO-137
func TestAutosetVerifyTrigLevelScansIntoBand(t *testing.T) {
	fe := &fireEng{fireLo: 30200, fireHi: 33400}
	c := &Controller{eng: fe, lv: newBoardLevels(fe, nil)}
	fe.level = 29000 // the (wrong) computed commit
	stop := make(chan struct{})
	if !c.verifyTrigLevel(stop, trigLevelVolts(29000)) {
		t.Fatal("verify reported cancelled")
	}
	if fe.level < fe.fireLo || fe.level > fe.fireHi {
		t.Fatalf("scan left level %d outside firing band [%d,%d]", fe.level, fe.fireLo, fe.fireHi)
	}
}

// No band fires anywhere (quiet input) → the computed code must be kept.
// TRLC-LINKS: REQ-DSO-137
func TestAutosetVerifyTrigLevelKeepsComputedWhenNothingFires(t *testing.T) {
	fe := &fireEng{fireLo: 1, fireHi: 2} // outside the DAC range → never fires
	c := &Controller{eng: fe, lv: newBoardLevels(fe, nil)}
	fe.level = 31000
	stop := make(chan struct{})
	if !c.verifyTrigLevel(stop, trigLevelVolts(31000)) {
		t.Fatal("verify reported cancelled")
	}
	if fe.level != 31000 {
		t.Fatalf("quiet input must keep the computed code, got %d", fe.level)
	}
}

// TRLC-LINKS: REQ-DSO-137
func TestAutosetSRAMUsesPublishedTriggerEvidence(t *testing.T) {
	e := &fakeEng{stats: instrument.Stats{BandKind: "sram"}}
	c := &Controller{eng: e}
	f := &sample.Frame{Status: sample.Status{Coherent: true, Triggered: true}, Traces: []sample.Trace{{}, {Ch: 1}}}
	c.SetFrameSource(func(fn func(*sample.Frame)) { fn(f) })
	if !c.trigFiring() {
		t.Fatal("ignored SRAM frame trigger")
	}
	f.Status.Triggered = false
	if c.trigFiring() {
		t.Fatal("forced SRAM frame counted as triggered")
	}
}
