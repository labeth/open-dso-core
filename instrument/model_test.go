package instrument

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/sample"
)

func twoChannels() State {
	ch := hal.Channel{On: true, VoltsDiv: 1, Probe: 1, Impedance: 1e6}
	return State{Channels: []hal.Channel{ch, ch}, TimeDivS: 1e-3, Running: true}
}

// Equal must look at every field: a field added to State and left out of
// Equal would make a change of it move no generation, so the owner would
// never commit it.
// TRLC-LINKS: REQ-DSO-008
func TestEqualSeesEveryField(t *testing.T) {
	base := twoChannels()
	v := reflect.ValueOf(&base).Elem()
	for i := range v.NumField() {
		s := base.Clone()
		f := reflect.ValueOf(&s).Elem().Field(i)
		change, ok := map[reflect.Kind]func(){
			reflect.Slice: func() { s.Channels[1].Invert = true },
			reflect.Struct: func() { // its last field, a number
				g := f.Field(f.NumField() - 1)
				if g.Kind() == reflect.Float64 {
					g.SetFloat(g.Float() + 1)
				} else {
					g.SetInt(g.Int() + 1)
				}
			},
			reflect.Bool:    func() { f.SetBool(!f.Bool()) },
			reflect.Float64: func() { f.SetFloat(f.Float() + 1) },
			reflect.Int:     func() { f.SetInt(f.Int() + 1) },
		}[f.Kind()]
		if !ok {
			t.Fatalf("field %s: kind %v not covered by the test", v.Type().Field(i).Name, f.Kind())
		}
		change()
		if s.Equal(base) || base.Equal(s) {
			t.Errorf("field %s: a change is not seen", v.Type().Field(i).Name)
		}
	}
	if !base.Equal(base.Clone()) {
		t.Error("a clone differs")
	}
}

// A change moves the generation once; one that alters nothing, an error
// from the edit or a refusal from the check leaves state and generation.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-072
func TestChangeAndGeneration(t *testing.T) {
	refuse := hal.Refuse("V/div", "outside the ladder")
	m := New(twoChannels(), func(s State) error {
		if s.Channels[0].VoltsDiv > 10 {
			return refuse
		}
		return nil
	})
	g0 := m.Generation()
	if err := m.Change(func(s *State) error { s.TimeDivS = 1e-3; return nil }); err != nil || m.Generation() != g0 {
		t.Fatalf("no-op change: err %v gen %d want %d", err, m.Generation(), g0)
	}
	if err := m.Change(func(s *State) error { s.Channels[0].VoltsDiv = 2; return nil }); err != nil || m.Generation() != g0+1 {
		t.Fatalf("change: err %v gen %d", err, m.Generation())
	}
	if err := m.Change(func(s *State) error { s.Channels[0].VoltsDiv = 20; return nil }); !errors.Is(err, refuse) {
		t.Fatalf("check: %v", err)
	}
	boom := errors.New("boom")
	if err := m.Change(func(s *State) error { s.TimeDivS = 5; return boom }); !errors.Is(err, boom) {
		t.Fatalf("edit error: %v", err)
	}
	if got := m.State(); got.Channels[0].VoltsDiv != 2 || got.TimeDivS != 1e-3 || m.Generation() != g0+1 {
		t.Fatalf("state after refusals: %+v gen %d", got, m.Generation())
	}
	// State hands out a copy.
	m.State().Channels[0].VoltsDiv = 99
	if m.State().Channels[0].VoltsDiv != 2 {
		t.Fatal("State shares its channels")
	}
}

// The owner takes the force and the posted work once; what it applied
// comes back unless a controller changed the state since.
// TRLC-LINKS: REQ-DSO-008
func TestOwnerSide(t *testing.T) {
	m := New(twoChannels(), nil)
	_ = m.Force()
	ran := 0
	m.Post(func(context.Context) { ran++ })
	if !m.ForcePending() || !m.TakeForce() || m.TakeForce() || m.ForcePending() {
		t.Fatal("the force is not taken once")
	}
	if !m.HasWork() {
		t.Fatal("no work posted")
	}
	work := m.TakeWork()
	if len(work) != 1 || m.HasWork() || len(m.TakeWork()) != 0 {
		t.Fatalf("work taken %d", len(work))
	}
	work[0](context.Background())
	if ran != 1 {
		t.Fatalf("ran %d", ran)
	}
	ps, pgen := m.Wanted()
	if pgen != m.Generation() {
		t.Fatalf("generation %d want %d", pgen, m.Generation())
	}
	p := struct {
		State State
		Gen   uint64
	}{ps, pgen}
	snapped := p.State.Clone()
	snapped.Channels[0].VoltsDiv = 0.998
	m.Applied(snapped, p.Gen)
	if m.State().Channels[0].VoltsDiv != 0.998 {
		t.Fatal("applied state not taken back")
	}
	_ = m.Change(func(s *State) error { s.TimeDivS = 2e-3; return nil })
	stale := p.State.Clone()
	stale.Running = false
	m.Applied(stale, p.Gen)
	if got := m.State(); !got.Running || got.TimeDivS != 2e-3 {
		t.Fatalf("a newer change was overwritten: %+v", got)
	}

	if m.Acquisition() != Armed {
		t.Fatalf("run state %v", m.Acquisition())
	}
	m.SetRun(Triggered)
	if m.Acquisition() != Triggered {
		t.Fatalf("run state %v", m.Acquisition())
	}
	_ = m.Change(func(s *State) error { s.Running = false; return nil })
	if m.Acquisition() != Stopped {
		t.Fatalf("stopped run state %v", m.Acquisition())
	}

	var got []error
	m.OnRefused(func(err error) { got = append(got, err) })
	m.Refused(hal.Refuse("trigger zone", "no zone node"))
	if len(got) != 1 {
		t.Fatalf("refusals heard: %v", got)
	}
}

type frames struct{ f *sample.Frame }

func (s frames) WithFrame(fn func(*sample.Frame)) { fn(s.f) }

// Frames come from the fan-out when there is one, else from the owner's
// last published frame.
// TRLC-LINKS: REQ-DSO-020
func TestWithFrame(t *testing.T) {
	m := New(twoChannels(), nil)
	m.WithFrame(func(f *sample.Frame) {
		if f != nil {
			t.Fatal("a frame before any was published")
		}
	})
	own := &sample.Frame{Seq: 1}
	m.Published(own)
	m.WithFrame(func(f *sample.Frame) {
		if f != own {
			t.Fatal("not the published frame")
		}
	})
	fan := &sample.Frame{Seq: 2}
	m.SetFrames(frames{fan})
	m.WithFrame(func(f *sample.Frame) {
		if f != fan {
			t.Fatal("not the fan-out's frame")
		}
	})
}
