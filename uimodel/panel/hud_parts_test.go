// ENGMODEL-OWNER-UNIT: FU-DSO-PANEL

package panel

import (
	"testing"
	"time"

	"github.com/labeth/open-dso-core/engine"
	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/hal/halfake"
	"github.com/labeth/open-dso-core/instrument"
)

// The trigger readout writes the armed match as the BUS table does.
// TRLC-LINKS: REQ-DSO-168
func TestSerialMatchText(t *testing.T) {
	if got := SerialMatchText(engine.SerialParams{Bytes: []int{0xAA, 0x0F}}); got != "AA 0F" {
		t.Fatalf("bytes: %q", got)
	}
	p := engine.SerialParams{Pattern: []engine.PatternElem{{Kind: 2, Value: 0xA0, Mask: 0xF0}, {Kind: 4}, {Kind: 2, Value: 0x55, Mask: 0xFF}}}
	if got := SerialMatchText(p); got != "A? ERR 55" {
		t.Fatalf("pattern: %q", got)
	}
	sof := engine.SerialParams{Pattern: []engine.PatternElem{{Kind: 1}, {Kind: 2}}}
	if got := SerialMatchText(sof); got != "SOF ??" {
		t.Fatalf("frame start: %q", got)
	}
	cmd := engine.SerialParams{Pattern: []engine.PatternElem{{Kind: 2, Value: engine.MILCommand, Mask: engine.MILCommand}}}
	if got := SerialMatchText(cmd); got != "CMD" {
		t.Fatalf("MIL-1553 command word: %q", got)
	}
}

// On a HAL scope the HUD says a NORMAL capture waits once its cycle
// published nothing for a second or two and a half screens, and the BODE
// view arms the sweep while it shows and disarms it as it is left.
// TRLC-LINKS: REQ-DSO-168, REQ-DSO-011
func TestHUDWaitAndBodeOnAHALScope(t *testing.T) {
	b := halfake.New(halfake.Config{Channels: 2})
	ch := hal.Channel{On: true, VoltsDiv: 1, Probe: 1, Impedance: 1e6}
	m := instrument.New(instrument.State{TimeDivS: 1e-3, Running: true, Channels: []hal.Channel{ch, ch}}, nil)
	s := NewHALScope(m, b, "t").WithCycle(fixedCycle(3 * time.Second))
	m.SetTimebases(s.Tdivs())
	c := New(s.Engine(), s.Analog(), s.Levels(), s.Hardware(), s.Tdivs(), 1e-3, t.Logf)
	if h := c.HUD(2); h.Waiting || h.WaitS != 3 {
		t.Fatalf("AUTO on an edge: waiting %v after %gs", h.Waiting, h.WaitS)
	}
	m.SetNorm(true)
	if h := c.HUD(2); !h.Waiting {
		t.Fatal("NORMAL, nothing published for 3 s: not waiting")
	}
	c.mu.Lock()
	c.viewMode = bodeView
	c.mu.Unlock()
	if c.HUD(2); c.eng.Snapshot().BodeMode == 0 {
		t.Fatal("the BODE view did not arm the sweep")
	}
	c.mu.Lock()
	c.viewMode = viewYT
	c.mu.Unlock()
	if c.HUD(2); c.eng.Snapshot().BodeMode != 0 {
		t.Fatal("leaving the BODE view left the sweep armed")
	}
}
