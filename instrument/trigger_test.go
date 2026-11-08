package instrument

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/hal/prototrig"
)

func fourChannels() State {
	ch := hal.Channel{On: true, VoltsDiv: 1, Probe: 1, Impedance: 1e6}
	return State{Channels: []hal.Channel{ch, ch, ch, ch}, TimeDivS: 1e-3, Running: true}
}

// sampleValue is a value of setting p different from its default.
func sampleValue(p TrigParam) ParamValue {
	switch p.Kind { //nolint:exhaustive // default: a number
	case ParamChoice:
		return ParamValue{Text: p.Choices[len(p.Choices)-1]}
	case ParamBool:
		return ParamValue{Num: 1}
	case ParamSource:
		return ParamValue{Text: "C3"}
	case ParamStates:
		return ParamValue{Text: "L,H,X,H"}
	case ParamInt:
		return ParamValue{Num: 3, Ch: 2}
	}
	return ParamValue{Num: 0.25e-6, Ch: 2}
}

// Every setting of every type reads back what was set, edits only its
// own type, and moves the model's generation once.
// TRLC-LINKS: REQ-DSO-011
func TestTrigParamsRoundTrip(t *testing.T) {
	for tt := range NumTrigTypes {
		for _, p := range TrigParams(tt) {
			m := New(fourChannels(), nil)
			before := m.State()
			g := m.Generation()
			v := sampleValue(p)
			if cur, _ := before.TrigParam(tt, p.ID, v.Ch); p.Kind == ParamBool && cur.Num != 0 {
				v.Num = 0
			} else if p.Kind == ParamChoice && cur.Text == v.Text {
				v.Text = p.Choices[0]
			}
			if err := m.Change(func(s *State) error { return s.SetTrigParam(tt, p.ID, v) }); err != nil {
				t.Fatalf("%s %s: %v", tt, p.ID, err)
			}
			got, err := m.State().TrigParam(tt, p.ID, v.Ch)
			if err != nil {
				t.Fatalf("%s %s: read: %v", tt, p.ID, err)
			}
			switch p.Kind { //nolint:exhaustive // default: a number
			case ParamChoice, ParamSource:
				if got.Text != v.Text {
					t.Errorf("%s %s: %q, want %q", tt, p.ID, got.Text, v.Text)
				}
			case ParamStates:
				if !strings.HasPrefix(got.Text, "L,H,X,H") {
					t.Errorf("%s %s: %q", tt, p.ID, got.Text)
				}
			default:
				if got.Num != v.Num {
					t.Errorf("%s %s: %g, want %g", tt, p.ID, got.Num, v.Num)
				}
			}
			after := m.State()
			for o := range NumTrigTypes {
				if o == tt {
					continue
				}
				var a, b Trigger
				a.Configs[o], b.Configs[o] = before.Trigger.Configs[o], after.Trigger.Configs[o]
				if !a.Equal(b) {
					t.Errorf("%s %s changed the %s configuration", tt, p.ID, o)
				}
			}
			if m.Generation() != g+1 {
				t.Errorf("%s %s: generation %d, want %d", tt, p.ID, m.Generation(), g+1)
			}
		}
	}
}

// A clone is deep: an edit of a clone's pattern term, gate, time condition
// or protocol data never reaches the original, and Equal sees each edit.
// TRLC-LINKS: REQ-DSO-008
func TestTriggerCloneIsDeep(t *testing.T) {
	s := fourChannels()
	s.Trigger.Fill(4)
	edits := map[string]func(tr *Trigger){
		"pattern term": func(tr *Trigger) { tr.Configs[TrigPattern].Stages[0].Pattern.Terms[1].State = hal.Low },
		"gate":         func(tr *Trigger) { tr.Configs[TrigQualified].Stages[0].Gate.Level = 1 },
		"time":         func(tr *Trigger) { tr.Configs[TrigPulse].Stages[0].Qual.MaxS = 1 },
		"video":        func(tr *Trigger) { tr.Configs[TrigVideo].Stages[0].Video.Line = 9 },
		"i2c data": func(tr *Trigger) {
			p, _ := tr.Configs[TrigI2C].Stages[0].Proto.(prototrig.I2C)
			p.Data = append(p.Data, 1)
			tr.Configs[TrigI2C].Stages[0].Proto = p
		},
		"spi bits": func(tr *Trigger) {
			p, _ := tr.Configs[TrigSPI].Stages[0].Proto.(prototrig.SPI)
			p.Bits[0] = hal.High
		},
		"holdoff": func(tr *Trigger) { tr.Configs[TrigEdge].HoldoffS = 1e-3 },
		"type":    func(tr *Trigger) { tr.Type = TrigRunt },
	}
	for name, edit := range edits {
		c := s.Clone()
		edit(&c.Trigger)
		if c.Equal(s) {
			t.Errorf("%s: the edit is not seen", name)
		}
		if !s.Equal(s.Clone()) {
			t.Fatalf("%s: the original changed", name)
		}
	}
}

// Each type's default runs as the HAL maps it: the running type's stages
// and trigger-wide settings are the spec, swept as the run control says.
// TRLC-LINKS: REQ-DSO-011
func TestTriggerSpecMapsTypes(t *testing.T) {
	want := map[TrigType][]hal.TrigNode{
		TrigEdge: {hal.NodeEdge}, TrigPulse: {hal.NodePulse}, TrigSlope: {hal.NodeSlope}, TrigVideo: {hal.NodeVideo},
		TrigWindow: {hal.NodeWindow}, TrigInterval: {hal.NodeEdge}, TrigDropout: {hal.NodeTimeout}, TrigRunt: {hal.NodeRunt},
		TrigPattern: {hal.NodePattern}, TrigQualified: {hal.NodeEdge}, TrigNthEdge: {hal.NodeEdge},
		TrigDelay: {hal.NodePattern, hal.NodeEdge}, TrigSetupHold: {hal.NodeSetupHold},
		TrigI2C: {hal.NodeProtocol}, TrigSPI: {hal.NodeProtocol}, TrigUART: {hal.NodeProtocol}, TrigCAN: {hal.NodeProtocol},
		TrigLIN: {hal.NodeProtocol},
	}
	for tt, nodes := range want {
		s := fourChannels()
		s.Sweep = hal.NormalSweep
		s.Trigger.Fill(4)
		s.Trigger.Type = tt
		s.Trigger.Configs[tt].HoldoffS = 1e-6
		spec := s.TriggerSpec()
		if len(spec.Stages) != len(nodes) || spec.Sweep != hal.NormalSweep || spec.HoldoffS != 1e-6 {
			t.Fatalf("%s: %+v", tt, spec)
		}
		for i, n := range nodes {
			if spec.Stages[i].Node != n {
				t.Errorf("%s stage %d: %v, want %v", tt, i, spec.Stages[i].Node, n)
			}
		}
		if p := tt.protocol(); p != "" && spec.Stages[0].Proto.Protocol() != p {
			t.Errorf("%s: protocol %s", tt, spec.Stages[0].Proto.Protocol())
		}
	}
}

// A type the trigger's capabilities lack is refused with the reason and
// the type stays; the types list says which run and why the others do not.
// TRLC-LINKS: REQ-DSO-011
func TestTrigTypeRefusedByCaps(t *testing.T) {
	caps := hal.TrigCaps{
		Nodes: map[hal.TrigNode]hal.NodeCaps{
			hal.NodeEdge: {QualOps: []hal.QualOp{hal.Shorter}}, hal.NodePulse: {}, hal.NodeProtocol: {},
		},
		MaxStages: 1,
		Protocols: []string{"uart"},
	}
	m := New(fourChannels(), nil)
	m.SetTrigCaps(func() hal.TrigCaps { return caps })
	if err := m.SetTrigTypeOf(TrigPulse); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		t   TrigType
		why string
	}{
		{TrigVideo, "no video node"},
		{TrigQualified, "no gate input"},
		{TrigNthEdge, "counts no edges"},
		{TrigDelay, "no pattern node"},
		{TrigI2C, "no i2c matcher"},
	} {
		err := m.SetTrigTypeOf(c.t)
		var r *hal.Refusal
		if !errors.As(err, &r) || !strings.Contains(r.Reason, c.why) {
			t.Errorf("%s: %v, want a refusal %q", c.t, err, c.why)
		}
		if m.State().Trigger.Type != TrigPulse {
			t.Fatalf("%s: the type moved to %s", c.t, m.State().Trigger.Type)
		}
	}
	ok := map[TrigType]bool{}
	for _, a := range m.TrigTypes() {
		ok[a.Type] = a.OK
		if !a.OK && a.Why == "" {
			t.Errorf("%s: refused without a reason", a.Type)
		}
	}
	if !ok[TrigEdge] || !ok[TrigInterval] || !ok[TrigPulse] || !ok[TrigUART] || ok[TrigCAN] || ok[TrigSlope] {
		t.Errorf("types %v", ok)
	}
}

// The shared accessors follow the running type: a protocol's data line, a
// pattern's first term.
// TRLC-LINKS: REQ-DSO-011
func TestTriggerAccessorsFollowType(t *testing.T) {
	s := fourChannels()
	s.Trigger.Fill(4)
	s.Trigger.Type = TrigI2C
	if err := s.Trigger.SetSource(hal.Source{Kind: hal.Analog, Index: 3}, 4); err != nil {
		t.Fatal(err)
	}
	if err := s.Trigger.SetLevel(1.5, 4); err != nil {
		t.Fatal(err)
	}
	p, _ := s.Trigger.Configs[TrigI2C].Stages[0].Proto.(prototrig.I2C)
	if p.SDA.Source.Index != 3 || p.SDA.Level != 1.5 || s.Trigger.Source().Index != 3 || s.Trigger.LevelV() != 1.5 {
		t.Fatalf("i2c: %+v", p)
	}
	if st := s.Trigger.Configs[TrigI2C].Stages[0]; st.Source.Index != 3 || st.Levels.High != 1.5 {
		t.Fatalf("i2c stage: %+v", st)
	}
	s.Trigger.Type = TrigPattern
	if err := s.SetTrigParam(TrigPattern, "states", ParamValue{Text: "X,X,H,L"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Trigger.SetLevel(0.3, 4); err != nil {
		t.Fatal(err)
	}
	if s.Trigger.Source().Index != 2 || s.Trigger.LevelV() != 0.3 {
		t.Fatalf("pattern: %v %g", s.Trigger.Source(), s.Trigger.LevelV())
	}
	if err := s.SetTrigParam(TrigEdge, "slope", ParamValue{Text: "sideways"}); err == nil {
		t.Fatal("a choice the setting lacks")
	}
	if err := s.SetTrigParam(TrigEdge, "source", ParamValue{Text: "C9"}); err == nil {
		t.Fatal("a channel the scope lacks")
	}
	if err := s.SetTrigParam(TrigEdge, "gate_level", ParamValue{}); err == nil {
		t.Fatal("a setting the type lacks")
	}
}

// A stored state reads back as it was: every type's settings, the running
// type and the rest of the state, through the same settings every surface
// edits.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-011
func TestTriggerJSONRoundTrip(t *testing.T) {
	s := fourChannels()
	s.Trigger.Fill(4)
	for tt := range NumTrigTypes {
		for _, p := range TrigParams(tt) {
			v := sampleValue(p)
			if p.Kind == ParamChoice {
				v.Text = p.Choices[len(p.Choices)/2]
			}
			if err := s.SetTrigParam(tt, p.ID, v); err != nil {
				t.Fatalf("%s %s: %v", tt, p.ID, err)
			}
		}
	}
	s.Trigger.Type = TrigDelay
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var got State
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if !got.Equal(s) {
		for tt := range NumTrigTypes {
			var a, c Trigger
			a.Configs[tt], c.Configs[tt] = s.Trigger.Configs[tt], got.Trigger.Configs[tt]
			if !a.Equal(c) {
				t.Errorf("%s: %+v, want %+v", tt, c.Configs[tt], a.Configs[tt])
			}
		}
		t.Fatalf("round trip differs: %s", b)
	}
}
