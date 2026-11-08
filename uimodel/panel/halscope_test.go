package panel

import (
	"image"
	"strings"
	"testing"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/hal/halfake"
	"github.com/labeth/open-dso-core/instrument"
	"github.com/labeth/open-dso-core/render"
	"github.com/labeth/open-dso-core/uimodel"
)

// halRig is the UI model over a HAL board through the core's instrument
// state, as a scope without its own engine runs it.
func halRig(t *testing.T, chans int) (*Controller, *instrument.Model, *halfake.Board) {
	t.Helper()
	b := halfake.New(halfake.Config{Channels: chans})
	ch := hal.Channel{On: true, VoltsDiv: 1, Probe: 1, Impedance: 1e6}
	st := instrument.State{TimeDivS: 1e-3, Running: true}
	for range chans {
		st.Channels = append(st.Channels, ch)
	}
	m := instrument.New(st, nil)
	s := NewHALScope(m, b, "open-dso test scope")
	m.SetTimebases(s.Tdivs())
	c := New(s.Engine(), s.Analog(), s.Levels(), s.Hardware(), s.Tdivs(), st.TimeDivS, t.Logf)
	return c, m, b
}

// The UI model drives a HAL scope through the instrument state: the keys
// and knobs change the Model's settings and run control, and the lamps
// follow.
// TRLC-LINKS: REQ-DSO-135, REQ-DSO-008
func TestHALScopeKeysAndKnobs(t *testing.T) {
	c, m, _ := halRig(t, 4)
	in := &lampInput{leds: &ledRecorder{}}
	c.SetInput(in)
	c.Input(knob(KnobTdiv, 1))
	if got := m.State().TimeDivS; got != 1e-6 {
		t.Fatalf("TIME/DIV clockwise: %g", got)
	}
	v0 := m.State().Channels[0].VoltsDiv
	c.Input(knob(KnobCh1Vdiv, -1))
	if got := m.State().Channels[0].VoltsDiv; !(got > v0) {
		t.Fatalf("CH1 V/DIV counter-clockwise: %g from %g", got, v0)
	}
	c.Input(knob(KnobCh1Pos, 5))
	if got := m.State().Channels[0].OffsetV; !(got > 0) {
		t.Fatalf("CH1 position up: offset %g", got)
	}
	c.Input(knob(KnobTrigLevel, 3))
	if got := m.State().Trigger.LevelV(); !(got > 0) {
		t.Fatalf("level up: %g", got)
	}
	c.Input(key(KeySingle))
	if st := m.State(); !st.Single || !st.Running || st.Sweep != hal.NormalSweep {
		t.Fatalf("SINGLE: %+v", st)
	}
	c.Input(key(KeyRunStop))
	if st := m.State(); st.Running || st.Single {
		t.Fatalf("RUN/STOP after SINGLE: %+v", st)
	}
	c.Input(key(KeyRunStop))
	if st := m.State(); !st.Running || st.Sweep != hal.Auto {
		t.Fatalf("RUN: %+v", st)
	}
	if in.word&ledRun == 0 {
		t.Fatalf("lamps %#04x", in.word)
	}
}

// ledRecorder takes lamp words.
type ledRecorder struct{ w []uint16 }

// TRLC-LINKS: REQ-DSO-135
func (r *ledRecorder) SetLEDs(w uint16) { r.w = append(r.w, w) }

// Every row the page drives is on a HAL scope's sheets, in use or with why.
// TRLC-LINKS: REQ-DSO-219, REQ-DSO-220
func TestHALScopeMeetsTheRowContract(t *testing.T) {
	c, _, _ := halRig(t, 4)
	if p := uimodel.CheckRows(c.Sheets()); p != nil {
		t.Fatalf("rows: %v", p)
	}
	if r := c.SheetSet("time.tdiv", 1e-6, ""); !r.OK {
		t.Fatalf("time.tdiv: %+v", r)
	}
}

// The display of a HAL scope draws at 1024×600 into ARGB over the hardware
// waveform plane: the plane's area stays transparent, the UI around it is
// opaque.
// TRLC-LINKS: REQ-DSO-168, REQ-DSO-021
func TestHALScopeDrawsOverTheWaveformPlane(t *testing.T) {
	c, _, _ := halRig(t, 4)
	c.SetScreen(1024, 600)
	img := render.NewImage32(&render.Metrics1024x600, hal.ARGB8888)
	area := image.Rect(12, 60, 1012, 540)
	img.SetUnder(area)
	render.Render(img, nil, c.HUD(4), true)
	if a := img.At(500, 300) >> 24; a != 0 {
		t.Fatalf("the waveform plane's area: alpha %#x", a)
	}
	if a := img.At(1020, 5) >> 24; a == 0 {
		t.Fatal("the UI beside the plane is transparent")
	}
	press(t, c, "menu")
	render.Render(img, nil, c.HUD(4), true)
	if !c.PanelStatus().Sheet.Open {
		t.Fatal("the menu key opened no sheet")
	}
}

// On a HAL scope the TRIG tab's Type row lists the trigger types the
// board runs, and picking one stages it in the instrument state; the
// pulse rows reach the pulse type's stage.
// TRLC-LINKS: REQ-DSO-135, REQ-DSO-011, REQ-DSO-212
func TestHALScopeTrigTypes(t *testing.T) {
	c, m, b := halRig(t, 2)
	m.SetTrigCaps(b.Trigger().Caps)
	var opts []string
	for _, tab := range c.Sheets().Tabs {
		for _, r := range tab.Rows {
			if r.ID == "trig.type" {
				for _, o := range r.Options {
					opts = append(opts, o.Text)
				}
			}
		}
	}
	if strings.Join(opts, ",") != "Edge,Pulse" {
		t.Fatalf("Type options %v (the fake board has an edge and a pulse node)", opts)
	}
	if res := c.SheetSet("trig.type", nil, "Pulse"); !res.OK {
		t.Fatalf("Pulse: %+v", res)
	}
	if st := m.State(); st.Trigger.Type != instrument.TrigPulse || st.TriggerSpec().Stages[0].Node != hal.NodePulse {
		t.Fatalf("pulse: %+v", st.Trigger.Type)
	}
	// The pulse rows' settings are the UI model's, placed on the learned
	// swing by the owner.
	m.SetPulseParams(.5, 100, 1000, 3)
	if st := m.State(); st.Qual.PulseMinNs != 100 || st.Qual.PulseMaxNs != 1000 || st.Qual.PulseCond != 3 ||
		!st.Trigger.Active().Placed {
		t.Fatalf("pulse settings: %+v", st.Qual)
	}
	if err := m.SetTrigTypeOf(instrument.TrigVideo); err == nil || !strings.Contains(err.Error(), "no video node") {
		t.Fatalf("video on a board without: %v", err)
	}
}
