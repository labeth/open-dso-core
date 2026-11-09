package panel

import (
	"testing"
	"time"

	"github.com/labeth/open-dso-core/engine"
	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/hal/halfake"
	"github.com/labeth/open-dso-core/instrument"
)

// The FORCE key asks the owner for one forced trigger through the
// instrument state.
// TRLC-LINKS: REQ-DSO-011, REQ-DSO-212
func TestHALScopeForceKey(t *testing.T) {
	c, m, _ := halRig(t, 2)
	if m.ForcePending() {
		t.Fatal("a force before the key")
	}
	c.Input(key(KeyForce))
	if !m.ForcePending() {
		t.Fatal("FORCE asked the owner for no forced trigger")
	}
}

// fixedCycle is a cycle that published d ago.
type fixedCycle time.Duration

// TRLC-LINKS: REQ-DSO-011
func (f fixedCycle) SincePublish() time.Duration { return time.Duration(f) }

// SincePublish is the cycle's, zero without one: NORMAL waiting for a
// trigger shows on the Run/Stop lamp only with a cycle that says so.
// TRLC-LINKS: REQ-DSO-011, REQ-DSO-135
func TestHALScopeSincePublish(t *testing.T) {
	b := halfake.New(halfake.Config{Channels: 2})
	m := instrument.New(instrument.State{Channels: make([]hal.Channel, 2), TimeDivS: 1e-3}, nil)
	s := NewHALScope(m, b, "t")
	sp, ok := s.Engine().(interface{ SincePublish() time.Duration })
	if !ok || sp.SincePublish() != 0 {
		t.Fatalf("without a cycle: ok %v", ok)
	}
	s.WithCycle(fixedCycle(3 * time.Second))
	sp, ok = s.Engine().(interface{ SincePublish() time.Duration })
	if got := sp.SincePublish(); !ok || got != 3*time.Second {
		t.Fatalf("with a cycle: %v", got)
	}
}

// ChannelState answers what the front end's getters answer one by one.
// TRLC-LINKS: REQ-DSO-135, REQ-DSO-089
func TestHALScopeChannelState(t *testing.T) {
	b := halfake.New(halfake.Config{Channels: 2})
	st := instrument.State{TimeDivS: 1e-3, Channels: []hal.Channel{
		{On: true, VoltsDiv: 1, Probe: 1, OffsetV: .2, Impedance: 1e6},
		{On: true, VoltsDiv: 10, Probe: 10, OffsetV: -3, Coupling: hal.AC, BWLimit: 20e6, Impedance: 1e6},
	}}
	s := NewHALScope(instrument.New(st, nil), b, "t")
	a, ok := s.Analog().(halAnalog)
	if !ok {
		t.Fatal("the HAL front end is not halAnalog")
	}
	for ch := range 2 {
		off, cpl, p, bwl := a.ChannelState(ch)
		if off != a.OffsetReqV(ch) || cpl != a.Coupling(ch) || p != a.ProbeFactor(ch) || bwl != a.BWL(ch) {
			t.Errorf("C%d: state (%g %d %g %v), getters (%g %d %g %v)", ch+1, off, cpl, p, bwl,
				a.OffsetReqV(ch), a.Coupling(ch), a.ProbeFactor(ch), a.BWL(ch))
		}
	}
}

// monitoredBoard is a board whose platform checks its converters.
type monitoredBoard struct {
	*halfake.Board
	h hal.ConverterHealth
}

type monitoredPlatform struct {
	hal.Platform
	h hal.ConverterHealth
}

// TRLC-LINKS: REQ-DSO-231
func (p monitoredPlatform) ConverterHealth() hal.ConverterHealth { return p.h }

// TRLC-LINKS: REQ-DSO-231
func (b monitoredBoard) Platform() hal.Platform { return monitoredPlatform{b.Board.Platform(), b.h} }

// A board's converter fault reaches the status the screen's notice reads;
// a board without converter checks leaves it empty.
// TRLC-LINKS: REQ-DSO-231, REQ-DSO-135
func TestHALScopeConverterHealth(t *testing.T) {
	st := instrument.State{Channels: make([]hal.Channel, 2), TimeDivS: 1e-3}
	plain := NewHALScope(instrument.New(st, nil), halfake.New(halfake.Config{Channels: 2}), "t")
	if f := plain.Engine().Snapshot().ADCFault; f != "" {
		t.Fatalf("a board without checks: %q", f)
	}
	h := hal.ConverterHealth{Fault: "ADC fault: converter E2 not converting - restart the scope", Recoveries: 2, Recovery: "an image reload after a periodic check: E3", CheckMs: 4.5}
	mb := monitoredBoard{halfake.New(halfake.Config{Channels: 2}), h}
	got := NewHALScope(instrument.New(st, nil), mb, "t").Engine().Snapshot()
	if got.ADCFault != h.Fault || got.ADCRecoveries != h.Recoveries || got.ADCRecovery != h.Recovery || got.ADCCheckMs != h.CheckMs {
		t.Fatalf("snapshot %q %d %q %g", got.ADCFault, got.ADCRecoveries, got.ADCRecovery, got.ADCCheckMs)
	}
}

// A HAL scope over the cycle's audio stream controls its takes.
// TRLC-LINKS: REQ-DSO-275
func TestHALScopeSoundTakes(t *testing.T) {
	var _ soundTaker = (*engine.HALCycle)(nil)
	inst := instrument.New(instrument.State{Channels: make([]hal.Channel, 2), Running: true}, nil)
	_, c := engine.NewHALLoop(engine.HALConfig{Board: halfake.New(halfake.Config{}), Instrument: inst})
	s := NewHALScope(inst, halfake.New(halfake.Config{}), "test").WithSound(c)
	if _, ok := s.Engine().(soundTaker); !ok {
		t.Fatal("the engine has no SOUND takes")
	}
}
